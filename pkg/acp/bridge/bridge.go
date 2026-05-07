// Package bridge provides a minimal HTTP transport layer for the Agent Client Protocol (ACP).
// Instead of translating ACP messages to a proprietary format, it relays JSON-RPC 2.0 messages
// directly over HTTP: POST /rpc (client→agent) and GET /sse (agent→client).
package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

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
	acp       *acp.Client
	sessionId string
	verbose   bool
	serverCtx context.Context

	subsMu sync.Mutex
	subs   []*subscriber

	// Agent-initiated RPCs (e.g. session/request_permission):
	// We assign local sequential ids, emit them via SSE, and await replies on POST /rpc.
	agentReqSeq    atomic.Int64
	pendingReplyMu sync.Mutex
	pendingReplies map[int64]chan json.RawMessage // local agentReqId → reply channel
}

// New creates a Bridge backed by the given ACP client.
// sessionId must be the id returned by acp.Client.SessionID() after session/new.
func New(client *acp.Client, sessionId string, verbose bool) *Bridge {
	return &Bridge{
		acp:            client,
		sessionId:      sessionId,
		verbose:        verbose,
		pendingReplies: make(map[int64]chan json.RawMessage),
	}
}

// SessionID returns the ACP session ID.
func (b *Bridge) SessionID() string { return b.sessionId }

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

// emitUpdate broadcasts a session/update notification as a JSON-RPC 2.0 message.
func (b *Bridge) emitUpdate(update acp.SessionUpdate) {
	msg := jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: b.sessionId,
			Update:    update,
		},
	}
	b.broadcast(msg)
}

// handlePermissionRequest emits a session/request_permission request via SSE
// and waits for the HTTP client to reply via POST /rpc.
func (b *Bridge) handlePermissionRequest(req acp.PermissionRequest) {
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

	go func() {
		stopReason, err := b.acp.Prompt(promptCtx, text)

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

// Cancel cancels the current agent turn.
func (b *Bridge) Cancel(ctx context.Context) error {
	return b.acp.Cancel(ctx)
}

// Subscribe returns a channel of raw JSON-RPC messages and a cancel function.
// The cancel function must be called when the subscriber disconnects.
func (b *Bridge) Subscribe() (<-chan json.RawMessage, func()) {
	sub := &subscriber{ch: make(chan json.RawMessage, 128)}

	b.subsMu.Lock()
	b.subs = append(b.subs, sub)
	count := len(b.subs)
	b.subsMu.Unlock()

	log.Printf("[bridge] SSE subscriber connected (session=%s, total=%d)", b.sessionId, count)

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
	return sub.ch, cancel
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

	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	for _, sub := range b.subs {
		select {
		case sub.ch <- raw:
		default:
			// Slow subscriber – drop the message rather than blocking.
		}
	}
}
