// Package bridge provides a minimal HTTP transport layer for the Agent Client Protocol (ACP).
// Instead of translating ACP messages to a proprietary format, it relays JSON-RPC 2.0 messages
// directly over HTTP: POST /rpc (client→agent) and GET /sse (agent→client).
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

// ----------------------------------------------------------------------------
// Wire types
// ----------------------------------------------------------------------------

// jsonRPCMsg is a JSON-RPC 2.0 message emitted to SSE subscribers.
type jsonRPCMsg struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  interface{}      `json:"params,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// sessionUpdateParams is the params envelope for a session/update notification.
type sessionUpdateParams struct {
	SessionId string            `json:"sessionId"`
	Update    acp.SessionUpdate `json:"update"`
	Time      time.Time         `json:"time"`
}

// ----------------------------------------------------------------------------
// Subscriber
// ----------------------------------------------------------------------------

type subscriber struct {
	ch chan json.RawMessage
}

// ----------------------------------------------------------------------------
// Bridge
// ----------------------------------------------------------------------------

// Bridge connects an ACP client to HTTP SSE subscribers via raw JSON-RPC 2.0 messages.
// It does not translate ACP semantics – it reconstructs JSON-RPC envelopes from
// the parsed events that acp.Client exposes and broadcasts them to subscribers.
type Bridge struct {
	acp         *acp.Client
	sessionId   string
	verbose     bool
	autoApprove bool // when true, permission requests are auto-approved without broadcasting to the UI
	serverCtx   context.Context
	outputFile  string // path to append conversation history in acp-posts format

	subsMu sync.Mutex
	subs   []*subscriber

	// Message history: every broadcast message is appended so that reconnecting
	// SSE clients can replay missed events via GET /messages.
	histMu  sync.RWMutex
	history []json.RawMessage

	// Agent-initiated RPCs (e.g. session/request_permission):
	// We assign local sequential ids, emit them via SSE, and await replies on POST /rpc.
	agentReqSeq    atomic.Int64
	pendingReplyMu sync.Mutex
	pendingReplies map[int64]chan json.RawMessage // local agentReqId → reply channel

	// Chunk buffer: accumulates consecutive chunk updates of the same kind and
	// emits them as a single batched session/update when the kind changes or the
	// agent turn ends.  This converts the per-chunk stream into a per-comment bulk
	// delivery so that consumers receive one complete message at a time.
	chunkMu   sync.Mutex
	chunkKind acp.SessionUpdateKind
	chunkText strings.Builder

	// Status tracking: "running" while a prompt is being processed, "stable" otherwise.
	// Mirrors the agentapi status convention so the proxy can watch GET /events.
	statusMu      sync.RWMutex
	currentStatus string
	statusSubsMu  sync.Mutex
	statusSubs    []*statusSubscriber
}

type statusSubscriber struct {
	ch chan string
}

// New creates a Bridge backed by the given ACP client.
// sessionId must be the id returned by acp.Client.SessionID() after session/new.
// outputFile, when non-empty, is a path where completed agent messages are appended
// in acp-posts JSONL format for consumption by the acp-posts Slack integration.
// autoApprove, when true, automatically approves all permission requests without
// broadcasting them to the UI (equivalent to always selecting the first option).
func New(client *acp.Client, sessionId string, verbose bool, outputFile string, autoApprove bool) *Bridge {
	return &Bridge{
		acp:            client,
		sessionId:      sessionId,
		verbose:        verbose,
		autoApprove:    autoApprove,
		outputFile:     outputFile,
		pendingReplies: make(map[int64]chan json.RawMessage),
		currentStatus:  "stable",
	}
}

// acpPostsEnvelope is the JSONL envelope written to the output file for acp-posts.
type acpPostsEnvelope struct {
	Type      string          `json:"type"`
	Message   acpPostsMessage `json:"message"`
	SessionID string          `json:"sessionId"`
}

type acpPostsMessage struct {
	Content []acpPostsContent `json:"content"`
}

type acpPostsContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// appendToOutputFile writes a completed agent message to b.outputFile in acp-posts format.
func (b *Bridge) appendToOutputFile(text string) {
	if b.outputFile == "" || text == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(b.outputFile), 0o755); err != nil {
		log.Printf("[bridge] failed to create output dir for %s: %v", b.outputFile, err)
		return
	}
	envelope := acpPostsEnvelope{
		Type: "assistant",
		Message: acpPostsMessage{
			Content: []acpPostsContent{{Type: "text", Text: text}},
		},
		SessionID: b.sessionId,
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("[bridge] failed to marshal acp-posts envelope: %v", err)
		return
	}
	f, err := os.OpenFile(b.outputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[bridge] failed to open output file %s: %v", b.outputFile, err)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "%s\n", data); err != nil {
		log.Printf("[bridge] failed to write to output file %s: %v", b.outputFile, err)
	}
}

// Messages returns a snapshot of all broadcasted JSON-RPC messages since the
// bridge started.  Callers can use this to replay history for reconnecting clients.
func (b *Bridge) Messages() []json.RawMessage {
	b.histMu.RLock()
	defer b.histMu.RUnlock()
	out := make([]json.RawMessage, len(b.history))
	copy(out, b.history)
	return out
}

// SessionID returns the ACP session ID.
func (b *Bridge) SessionID() string { return b.sessionId }

// SessionRuntimeInfo returns the runtime information reported by the ACP agent.
func (b *Bridge) SessionRuntimeInfo() acp.SessionRuntimeInfo {
	return b.acp.SessionRuntimeInfo()
}

// Status returns the current agent status ("running" or "stable").
func (b *Bridge) Status() string {
	b.statusMu.RLock()
	defer b.statusMu.RUnlock()
	return b.currentStatus
}

// SubscribeStatus returns a channel that receives status strings whenever the
// agent status changes, and a cancel function that must be called on disconnect.
func (b *Bridge) SubscribeStatus() (<-chan string, func()) {
	sub := &statusSubscriber{ch: make(chan string, 4)}
	b.statusSubsMu.Lock()
	b.statusSubs = append(b.statusSubs, sub)
	b.statusSubsMu.Unlock()

	cancel := func() {
		b.statusSubsMu.Lock()
		for i, s := range b.statusSubs {
			if s == sub {
				b.statusSubs = append(b.statusSubs[:i], b.statusSubs[i+1:]...)
				break
			}
		}
		b.statusSubsMu.Unlock()
		close(sub.ch)
	}
	return sub.ch, cancel
}

// setStatus updates the current status and notifies all subscribers.
func (b *Bridge) setStatus(status string) {
	b.statusMu.Lock()
	prev := b.currentStatus
	b.currentStatus = status
	b.statusMu.Unlock()

	if prev == status {
		return
	}
	if b.verbose {
		log.Printf("[bridge] status: %s → %s (session=%s)", prev, status, b.sessionId)
	}

	b.statusSubsMu.Lock()
	defer b.statusSubsMu.Unlock()
	for _, sub := range b.statusSubs {
		select {
		case sub.ch <- status:
		default:
		}
	}
}

// ----------------------------------------------------------------------------
// Event loop
// ----------------------------------------------------------------------------

// Run starts the event loop. It blocks until ctx is cancelled.
// Call this in a goroutine before starting the HTTP server.
func (b *Bridge) Run(ctx context.Context) {
	b.serverCtx = ctx
	updates := b.acp.Updates()
	perms := b.acp.PermissionRequests()

	for {
		select {
		case <-ctx.Done():
			return

		case update, ok := <-updates:
			if !ok {
				return
			}
			b.emitUpdate(update)

		case req, ok := <-perms:
			if !ok {
				return
			}
			b.handlePermissionRequest(req)
		}
	}
}

// isChunkKind reports whether kind is a streaming chunk that should be buffered.
func isChunkKind(kind acp.SessionUpdateKind) bool {
	return kind == acp.SessionUpdateKindAgentMessageChunk ||
		kind == acp.SessionUpdateKindUserMessageChunk ||
		kind == acp.SessionUpdateKindAgentThoughtChunk
}

// emitUpdate buffers consecutive chunk updates of the same kind and broadcasts
// them as a single batched session/update when the kind changes or a non-chunk
// event arrives.  Non-chunk events (tool_call, plan, …) are broadcast immediately
// after the pending chunk buffer is flushed.
func (b *Bridge) emitUpdate(update acp.SessionUpdate) {
	if isChunkKind(update.Kind) {
		b.chunkMu.Lock()
		if b.chunkKind != "" && b.chunkKind != update.Kind {
			// Kind changed – flush the previous buffer before accumulating.
			b.flushChunkBufferLocked()
		}
		b.chunkKind = update.Kind
		b.chunkText.WriteString(acp.ExtractTextContent(update.Content))
		b.chunkMu.Unlock()
		return
	}

	// Non-chunk event: flush pending chunks first, then broadcast immediately.
	b.flushChunkBuffer()

	msg := jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: b.sessionId,
			Update:    update,
			Time:      time.Now(),
		},
	}
	b.broadcast(msg)
}

// flushChunkBuffer flushes the accumulated chunk buffer as a single session/update.
// Safe to call from any goroutine.
func (b *Bridge) flushChunkBuffer() {
	b.chunkMu.Lock()
	defer b.chunkMu.Unlock()
	b.flushChunkBufferLocked()
}

// flushChunkBufferLocked flushes the buffer. Must be called with b.chunkMu held.
func (b *Bridge) flushChunkBufferLocked() {
	if b.chunkKind == "" || b.chunkText.Len() == 0 {
		return
	}
	text := b.chunkText.String()
	kind := b.chunkKind
	b.chunkKind = ""
	b.chunkText.Reset()

	contentRaw, _ := json.Marshal(acp.ContentBlockText{Type: "text", Text: text})
	now := time.Now()
	msg := jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: b.sessionId,
			Update: acp.SessionUpdate{
				Kind:    kind,
				Content: json.RawMessage(contentRaw),
			},
			Time: now,
		},
	}
	// broadcast without holding chunkMu to avoid lock-order inversion.
	b.chunkMu.Unlock()
	b.broadcast(msg)
	// Persist completed agent messages to the output file for acp-posts consumption.
	if kind == acp.SessionUpdateKindAgentMessageChunk {
		b.appendToOutputFile(text)
	}
	b.chunkMu.Lock()
}

// handlePermissionRequest emits a session/request_permission request via SSE
// and waits for the HTTP client to reply via POST /rpc.
// When autoApprove is set, the first available option is selected immediately
// without broadcasting to the UI.
func (b *Bridge) handlePermissionRequest(req acp.PermissionRequest) {
	if b.autoApprove {
		if len(req.Params.Options) > 0 {
			log.Printf("[bridge] auto-approving permission request (option=%s)", req.Params.Options[0].OptionId)
			_ = req.Reply(req.Params.Options[0].OptionId)
		} else {
			_ = req.Reply("")
		}
		return
	}

	id := b.agentReqSeq.Add(1)
	idRaw, _ := json.Marshal(id)
	idRawMsg := json.RawMessage(idRaw)

	replyCh := make(chan json.RawMessage, 1)

	b.pendingReplyMu.Lock()
	b.pendingReplies[id] = replyCh
	b.pendingReplyMu.Unlock()

	msg := jsonRPCMsg{
		JSONRPC: "2.0",
		ID:      &idRawMsg,
		Method:  "session/request_permission",
		Params:  req.Params,
	}
	b.broadcast(msg)

	go func() {
		defer func() {
			b.pendingReplyMu.Lock()
			delete(b.pendingReplies, id)
			b.pendingReplyMu.Unlock()
		}()

		select {
		case <-b.serverCtx.Done():
			_ = req.Reply("")
		case raw := <-replyCh:
			var result acp.RequestPermissionResult
			if err := json.Unmarshal(raw, &result); err == nil {
				_ = req.Reply(result.Outcome.OptionId)
			} else {
				_ = req.Reply("")
			}
		}
	}()
}

// ----------------------------------------------------------------------------
// Public API (called by the HTTP server)
// ----------------------------------------------------------------------------

// SendPrompt sends a session/prompt request to the ACP agent.
// clientID is the raw JSON-RPC id from the HTTP client; the result (or error)
// is emitted via SSE with that same id so the client can correlate the response.
func (b *Bridge) SendPrompt(clientID json.RawMessage, text string) error {
	promptCtx := b.serverCtx
	if promptCtx == nil {
		promptCtx = context.Background()
	}

	log.Printf("[bridge] SendPrompt (session=%s, clientID=%s, textLen=%d)", b.sessionId, clientID, len(text))

	// Broadcast the user's prompt as a synthetic session/update notification.
	// The ACP server does not echo user messages, so we emit it here to ensure
	// it appears in both the SSE live stream and GET /messages history.
	userContentRaw, _ := json.Marshal(acp.ContentBlockText{Type: "text", Text: text})
	b.broadcast(jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: b.sessionId,
			Update: acp.SessionUpdate{
				Kind:    acp.SessionUpdateKindUserMessageChunk,
				Content: json.RawMessage(userContentRaw),
			},
			Time: time.Now(),
		},
	})

	b.setStatus("running")

	go func() {
		stopReason, err := b.acp.Prompt(promptCtx, text)

		// Flush any chunk that was still buffered when the agent turn ended.
		b.flushChunkBuffer()
		b.setStatus("stable")

		if err != nil {
			log.Printf("[bridge] Prompt error (session=%s): %v", b.sessionId, err)
			b.broadcast(jsonRPCMsg{
				JSONRPC: "2.0",
				ID:      &clientID,
				Error:   &rpcError{Code: -32000, Message: err.Error()},
			})
			return
		}
		log.Printf("[bridge] Prompt done (session=%s, stopReason=%s)", b.sessionId, stopReason)
		b.broadcast(jsonRPCMsg{
			JSONRPC: "2.0",
			ID:      &clientID,
			Result:  acp.PromptResult{StopReason: stopReason},
		})
	}()

	return nil
}

// HandleReply routes a JSON-RPC result from the HTTP client to a pending
// agent-initiated request (e.g. session/request_permission).
// id is the integer id the bridge assigned when it emitted the agent request.
func (b *Bridge) HandleReply(id int64, result json.RawMessage) error {
	b.pendingReplyMu.Lock()
	ch, ok := b.pendingReplies[id]
	b.pendingReplyMu.Unlock()

	if !ok {
		return fmt.Errorf("no pending agent request for id %d", id)
	}
	select {
	case ch <- result:
	default:
	}
	return nil
}

// SetSessionConfigOption forwards a session/set_config_option request to the ACP agent.
// The result is also emitted as a config_option_update notification so live UI clients
// observe the same configuration state that the HTTP caller receives.
func (b *Bridge) SetSessionConfigOption(ctx context.Context, configId, value string) (acp.SessionSetConfigOptionResult, error) {
	result, err := b.acp.SetSessionConfigOption(ctx, configId, value)
	if err != nil {
		return acp.SessionSetConfigOptionResult{}, err
	}

	b.broadcast(jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: b.sessionId,
			Update: acp.SessionUpdate{
				Kind:          acp.SessionUpdateKindConfigOptionUpdate,
				ConfigOptions: result.ConfigOptions,
			},
			Time: time.Now(),
		},
	})

	return result, nil
}

// Cancel cancels the current agent turn.
func (b *Bridge) Cancel(ctx context.Context) error {
	return b.acp.Cancel(ctx)
}

// SubscribeFromCurrent is a sentinel value for SubscribeFrom that means
// "subscribe from the current position without replaying any history".
// Use this when the caller obtains history separately (e.g. GET /messages).
const SubscribeFromCurrent = -2

// SubscribeFrom subscribes to new messages and atomically returns a snapshot of
// history starting from lastEventID+1. This prevents race conditions where messages
// could be missed between fetching history and subscribing to the live channel.
//
// Pass SubscribeFromCurrent as lastEventID to subscribe from the current position
// without replaying any history (useful when history is fetched via GET /messages).
//
// Callers must invoke the returned cancel function when done.
// The returned nextIdx is the history index that the first channel message will have.
func (b *Bridge) SubscribeFrom(lastEventID int) (<-chan json.RawMessage, []json.RawMessage, int, func()) {
	// Hold subsMu first to block concurrent broadcasts while we snapshot history
	// and add the subscriber atomically. broadcast() must also acquire subsMu first.
	b.subsMu.Lock()

	b.histMu.RLock()
	histLen := len(b.history)
	var start int
	if lastEventID == SubscribeFromCurrent {
		start = histLen // no replay: subscribe from current position
	} else {
		start = lastEventID + 1
		if start < 0 {
			start = 0
		}
		if start > histLen {
			start = histLen
		}
	}
	history := make([]json.RawMessage, histLen-start)
	copy(history, b.history[start:])
	b.histMu.RUnlock()

	sub := &subscriber{ch: make(chan json.RawMessage, 128)}
	b.subs = append(b.subs, sub)
	count := len(b.subs)
	b.subsMu.Unlock()

	log.Printf("[bridge] SSE subscriber connected (session=%s, total=%d, lastEventID=%d, histLen=%d)", b.sessionId, count, lastEventID, histLen)

	cancel := func() {
		b.subsMu.Lock()
		for i, s := range b.subs {
			if s == sub {
				b.subs = append(b.subs[:i], b.subs[i+1:]...)
				break
			}
		}
		remaining := len(b.subs)
		b.subsMu.Unlock()
		close(sub.ch)
		log.Printf("[bridge] SSE subscriber disconnected (session=%s, remaining=%d)", b.sessionId, remaining)
	}
	return sub.ch, history, histLen, cancel
}

// ----------------------------------------------------------------------------
// Internal helpers
// ----------------------------------------------------------------------------

func (b *Bridge) broadcast(msg jsonRPCMsg) {
	raw, err := json.Marshal(msg)
	if err != nil {
		if b.verbose {
			log.Printf("[bridge] marshal error: %v", err)
		}
		return
	}
	if b.verbose {
		log.Printf("[bridge] broadcast: %s", raw)
	}

	// Acquire subsMu FIRST (same order as SubscribeFrom) to ensure that when a
	// new subscriber is being added concurrently, the boundary between history
	// and live channel messages is consistent with no gaps or duplicates.
	b.subsMu.Lock()
	defer b.subsMu.Unlock()

	// Persist to history inside subsMu so SubscribeFrom sees a consistent snapshot.
	b.histMu.Lock()
	b.history = append(b.history, raw)
	b.histMu.Unlock()

	for _, sub := range b.subs {
		select {
		case sub.ch <- raw:
		default:
			// Slow subscriber – drop the message rather than blocking.
		}
	}
}
