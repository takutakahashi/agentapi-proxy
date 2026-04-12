package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	// Accept connections from any origin so that browser-based editors and
	// local tools can connect without CORS issues.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Handler is an http.Handler that upgrades each incoming request to a
// WebSocket connection and runs the ACP server protocol for the lifetime of
// that connection.
type Handler struct {
	agentapiURL string
}

// NewHandler creates an ACP handler that proxies to the agentapi server at
// agentapiURL (e.g. "http://localhost:8080").
func NewHandler(agentapiURL string) *Handler {
	return &Handler{agentapiURL: agentapiURL}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ACP] WebSocket upgrade failed: %v", err)
		return
	}
	defer func() { _ = conn.Close() }()

	sess := &session{
		conn:         conn,
		agentapi:     NewAgentapiClient(h.agentapiURL),
		sendMu:       &sync.Mutex{},
		pendingPerms: make(map[string]chan *RequestPermissionResult),
	}

	log.Printf("[ACP] New connection from %s", r.RemoteAddr)
	if err := sess.run(r.Context()); err != nil {
		log.Printf("[ACP] Session error from %s: %v", r.RemoteAddr, err)
	}
	log.Printf("[ACP] Connection closed for %s", r.RemoteAddr)
}

// ---- session ------------------------------------------------------------

// session holds all state for a single ACP WebSocket connection.
type session struct {
	conn        *websocket.Conn
	agentapi    *AgentapiClient
	sendMu      *sync.Mutex
	sessionID   string // ACP session ID (generated on session/new)
	initialized bool

	// pendingPerms: maps permReqID → channel that receives the client's
	// response to a session/request_permission call we sent.
	pendingPerms   map[string]chan *RequestPermissionResult
	pendingPermsMu sync.Mutex

	// promptCancel cancels the currently in-flight session/prompt goroutine.
	// promptID is a counter that identifies the active prompt; it is
	// incremented each time a new prompt starts so that the deferred cleanup
	// can skip cancelling a prompt that has already been superseded.
	promptCancel context.CancelFunc
	promptID     uint64
	promptMu     sync.Mutex
}

// run reads messages from the WebSocket connection and dispatches them.
// It returns when the connection is closed or an unrecoverable error occurs.
func (s *session) run(ctx context.Context) error {
	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseNoStatusReceived,
			) {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[ACP] Malformed message from client: %v", err)
			continue
		}

		log.Printf("[ACP] ← method=%q id=%v", msg.Method, msg.ID)

		switch {
		// ---- responses to requests we sent (e.g. session/request_permission)
		case msg.Method == "" && msg.ID != nil:
			s.handleResponse(&msg)

		// ---- requests / notifications from the client
		case msg.Method == MethodInitialize:
			s.handleInitialize(&msg)
		case msg.Method == MethodAuthenticate:
			s.handleAuthenticate(&msg)
		case msg.Method == MethodSessionNew:
			s.handleSessionNew(&msg)
		case msg.Method == MethodSessionLoad:
			s.handleSessionLoad(&msg)
		case msg.Method == MethodSessionPrompt:
			// Run in a goroutine so that the read loop stays alive and can
			// receive session/cancel notifications while the prompt is running.
			go s.handleSessionPrompt(ctx, &msg)
		case msg.Method == MethodSessionCancel:
			s.handleSessionCancel(ctx, &msg)
		case msg.Method == MethodSessionList:
			s.handleSessionList(&msg)
		case msg.Method == MethodSessionSetMode:
			s.handleSessionSetMode(&msg)
		default:
			log.Printf("[ACP] Unknown method: %q", msg.Method)
			if msg.ID != nil {
				s.sendError(msg.ID, -32601, "method not found: "+msg.Method)
			}
		}
	}
}

// ---- request handlers ---------------------------------------------------

func (s *session) handleInitialize(msg *Message) {
	var params InitializeParams
	if msg.Params != nil {
		_ = json.Unmarshal(msg.Params, &params)
	}
	s.initialized = true

	s.sendResult(msg.ID, InitializeResult{
		ProtocolVersion: ProtocolVersion,
		AgentInfo: &AgentInfo{
			Name:    "agentapi-proxy-acp",
			Version: "1.0.0",
		},
		AuthMethods: []string{},
		AgentCapabilities: AgentCapabilities{
			SessionCapabilities: SessionCapabilities{
				List: true,
			},
		},
	})
}

func (s *session) handleAuthenticate(msg *Message) {
	// No authentication required; accept all credentials.
	s.sendResult(msg.ID, map[string]interface{}{})
}

func (s *session) handleSessionNew(msg *Message) {
	s.sessionID = uuid.New().String()
	log.Printf("[ACP] session/new → sessionId=%s", s.sessionID)
	s.sendResult(msg.ID, NewSessionResult{SessionID: s.sessionID})
}

// handleSessionLoad tries to resume a previously identified session.
// Since claude-agentapi is single-session there is nothing to restore; we
// just reuse the requested sessionId.
func (s *session) handleSessionLoad(msg *Message) {
	var params struct {
		SessionID string `json:"sessionId"`
	}
	if msg.Params != nil {
		_ = json.Unmarshal(msg.Params, &params)
	}
	if params.SessionID != "" {
		s.sessionID = params.SessionID
	} else {
		s.sessionID = uuid.New().String()
	}
	log.Printf("[ACP] session/load → sessionId=%s", s.sessionID)
	s.sendResult(msg.ID, map[string]interface{}{
		"sessionId": s.sessionID,
		"messages":  []interface{}{},
	})
}

// handleSessionPrompt is the core ACP method.  It:
//  1. Converts the ACP prompt ContentBlocks to plain text.
//  2. POSTs the message to claude-agentapi.
//  3. Subscribes to the SSE /events stream and translates events to
//     session/update notifications sent to the ACP client.
//  4. Handles pending actions (approve_plan / answer_question) by sending
//     session/request_permission requests to the ACP client and forwarding
//     the client's response back to agentapi.
//  5. Replies to the original session/prompt request once the agent is stable.
func (s *session) handleSessionPrompt(ctx context.Context, msg *Message) {
	var params PromptParams
	if msg.Params != nil {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			s.sendError(msg.ID, -32602, fmt.Sprintf("invalid params: %v", err))
			return
		}
	}

	// Build plain-text content from the ACP ContentBlocks.
	content := blocksToText(params.Prompt)
	if content == "" {
		s.sendError(msg.ID, -32602, "no text content in prompt")
		return
	}

	// Create a context that can be cancelled by session/cancel.
	promptCtx, cancel := context.WithCancel(ctx)
	s.promptMu.Lock()
	if s.promptCancel != nil {
		s.promptCancel() // cancel any previous in-flight prompt
	}
	s.promptCancel = cancel
	myPromptID := s.promptID + 1
	s.promptID = myPromptID
	s.promptMu.Unlock()
	defer func() {
		cancel()
		s.promptMu.Lock()
		// Only clear promptCancel if it still belongs to this prompt.
		if s.promptID == myPromptID {
			s.promptCancel = nil
		}
		s.promptMu.Unlock()
	}()

	sessionID := params.SessionID
	if sessionID == "" {
		sessionID = s.sessionID
	}

	// Snapshot message count before sending so we can detect completion even
	// if we miss the running→stable status transition.
	initialCount := s.getMessageCount(promptCtx)

	// Subscribe to the SSE stream BEFORE posting the message so we do not
	// miss any events.
	eventCh, errCh := s.agentapi.StreamEvents(promptCtx)

	log.Printf("[ACP] Sending message to agentapi (len=%d)", len(content))
	if err := s.agentapi.PostMessage(promptCtx, content, "user"); err != nil {
		s.sendError(msg.ID, -32000, fmt.Sprintf("failed to send message: %v", err))
		return
	}

	stopReason, err := s.streamLoop(promptCtx, sessionID, initialCount, eventCh, errCh)
	if err != nil {
		if promptCtx.Err() != nil {
			// Context was cancelled (session/cancel).
			s.sendResult(msg.ID, PromptResult{StopReason: "cancelled"})
			return
		}
		s.sendError(msg.ID, -32000, fmt.Sprintf("streaming error: %v", err))
		return
	}

	log.Printf("[ACP] session/prompt done, stopReason=%s", stopReason)
	s.sendResult(msg.ID, PromptResult{StopReason: stopReason})
}

// streamLoop reads SSE events from the agentapi, translates them to ACP
// session/update notifications, handles pending actions, and returns when
// the agent becomes stable after the new message.
func (s *session) streamLoop(
	ctx context.Context,
	sessionID string,
	initialMsgCount int,
	eventCh <-chan SSEEvent,
	errCh <-chan error,
) (string, error) {
	// seenIDs prevents emitting duplicate session/update notifications for
	// messages that appear in both the init snapshot and later message_update
	// events.
	seenIDs := make(map[int]bool)
	sawRunning := false

	// Poll for pending actions at regular intervals while the agent runs.
	actionCh := make(chan PendingAction, 8)
	go s.pollActions(ctx, actionCh)

	// Overall timeout: 10 minutes for a single prompt.
	timeout := time.NewTimer(10 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case <-ctx.Done():
			return "cancelled", nil

		case <-timeout.C:
			log.Printf("[ACP] Prompt timed out after 10 minutes")
			return "max_turn_requests", nil

		case err, ok := <-errCh:
			if !ok {
				// errCh closed without error; the eventCh will close too.
				return "end_turn", nil
			}
			return "", err

		case evt, ok := <-eventCh:
			if !ok {
				// SSE stream closed.
				return "end_turn", nil
			}
			stop, err := s.handleSSEEvent(ctx, sessionID, evt, seenIDs, &sawRunning, initialMsgCount)
			if err != nil {
				return "", err
			}
			if stop != "" {
				return stop, nil
			}

		case action, ok := <-actionCh:
			if !ok {
				continue
			}
			if err := s.handlePendingAction(ctx, sessionID, action); err != nil {
				log.Printf("[ACP] handlePendingAction error: %v", err)
			}
		}
	}
}

// handleSSEEvent processes a single SSE event and returns ("stop_reason","")
// when the agent turn is over, or ("","") to continue.
func (s *session) handleSSEEvent(
	ctx context.Context,
	sessionID string,
	evt SSEEvent,
	seenIDs map[int]bool,
	sawRunning *bool,
	initialMsgCount int,
) (string, error) {
	switch evt.Event {
	case "init":
		// The init event carries the current snapshot; emit any messages the
		// agent has already produced and note the current status.
		var initData struct {
			Messages []AgentMessage `json:"messages"`
			Status   string         `json:"status"`
		}
		if err := json.Unmarshal(evt.Data, &initData); err != nil {
			log.Printf("[ACP] failed to parse init event: %v", err)
			return "", nil
		}
		if initData.Status == "running" {
			*sawRunning = true
		}
		for i := range initData.Messages {
			m := &initData.Messages[i]
			seenIDs[m.ID] = true
		}

	case "message_update":
		var m AgentMessage
		if err := json.Unmarshal(evt.Data, &m); err != nil {
			log.Printf("[ACP] failed to parse message_update: %v", err)
			return "", nil
		}
		if seenIDs[m.ID] {
			return "", nil
		}
		seenIDs[m.ID] = true

		if notif := messageToUpdate(sessionID, &m); notif != nil {
			s.sendNotification(MethodSessionUpdate, notif)
		}

	case "status_change":
		var sc struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(evt.Data, &sc); err != nil {
			log.Printf("[ACP] failed to parse status_change: %v", err)
			return "", nil
		}
		log.Printf("[ACP] agentapi status → %s", sc.Status)

		switch sc.Status {
		case "running":
			*sawRunning = true
		case "stable":
			// We are done when:
			//   (a) we observed the agent transition to running (normal path), OR
			//   (b) the message count grew (agent finished very quickly).
			currentCount := s.getMessageCount(ctx)
			if *sawRunning || currentCount > initialMsgCount {
				return "end_turn", nil
			}
			// Otherwise this "stable" is the pre-existing idle state; ignore.
		}
	}
	return "", nil
}

// handleSessionCancel aborts the current in-flight session/prompt.
func (s *session) handleSessionCancel(ctx context.Context, msg *Message) {
	s.promptMu.Lock()
	cancel := s.promptCancel
	s.promptMu.Unlock()
	if cancel != nil {
		cancel()
	}
	// Also tell agentapi to stop the running agent.
	if err := s.agentapi.PostAction(ctx, "stop_agent", nil, nil); err != nil {
		log.Printf("[ACP] stop_agent action failed: %v", err)
	}
}

func (s *session) handleSessionList(msg *Message) {
	sessions := []map[string]interface{}{}
	if s.sessionID != "" {
		sessions = append(sessions, map[string]interface{}{
			"sessionId": s.sessionID,
		})
	}
	s.sendResult(msg.ID, map[string]interface{}{"sessions": sessions})
}

func (s *session) handleSessionSetMode(msg *Message) {
	// claude-agentapi does not expose a mode-change API; accept silently.
	s.sendResult(msg.ID, map[string]interface{}{})
}

// ---- action / permission handling ---------------------------------------

// pollActions polls GET /action every 500 ms and forwards newly seen pending
// actions to actionCh.  It runs until ctx is cancelled.
func (s *session) pollActions(ctx context.Context, actionCh chan<- PendingAction) {
	defer close(actionCh)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	seen := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			actions, err := s.agentapi.GetActions(ctx)
			if err != nil {
				continue
			}
			for _, a := range actions.PendingActions {
				key := a.ToolUseID + "|" + a.Type
				if !seen[key] {
					seen[key] = true
					select {
					case actionCh <- a:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}
}

// handlePendingAction sends a session/request_permission request to the ACP
// client and posts the client's response back to agentapi.
func (s *session) handlePendingAction(ctx context.Context, sessionID string, action PendingAction) error {
	permReqID := uuid.New().String()
	replyCh := make(chan *RequestPermissionResult, 1)

	s.pendingPermsMu.Lock()
	s.pendingPerms[permReqID] = replyCh
	s.pendingPermsMu.Unlock()
	defer func() {
		s.pendingPermsMu.Lock()
		delete(s.pendingPerms, permReqID)
		s.pendingPermsMu.Unlock()
	}()

	// Build human-readable content for the permission request.
	var contentText string
	_ = json.Unmarshal(action.Content, &contentText)

	var content []ContentBlock
	if contentText != "" {
		content = []ContentBlock{{Type: "text", Text: contentText}}
	}

	s.sendRequest(permReqID, MethodSessionRequestPerm, RequestPermissionParams{
		SessionID: sessionID,
		PermissionRequest: PermReq{
			ToolUseID:   action.ToolUseID,
			Description: action.Type,
			Content:     content,
		},
	})

	// Wait for the client's response with a generous timeout.
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(5 * time.Minute):
		log.Printf("[ACP] Timeout waiting for permission response (toolUseId=%s), auto-denying", action.ToolUseID)
		// Auto-deny on timeout to unblock the agent.
		switch action.Type {
		case "approve_plan":
			f := false
			_ = s.agentapi.PostAction(ctx, "approve_plan", &f, nil)
		case "answer_question":
			_ = s.agentapi.PostAction(ctx, "answer_question", nil, map[string]string{action.ToolUseID: ""})
		}
	case result, ok := <-replyCh:
		if !ok || result == nil {
			return nil
		}
		switch action.Type {
		case "approve_plan":
			approved := result.Permission == "allow" || result.Permission == "allow_always"
			if err := s.agentapi.PostAction(ctx, "approve_plan", &approved, nil); err != nil {
				return fmt.Errorf("approve_plan action: %w", err)
			}
		case "answer_question":
			answer := result.Answer
			if err := s.agentapi.PostAction(ctx, "answer_question", nil, map[string]string{action.ToolUseID: answer}); err != nil {
				return fmt.Errorf("answer_question action: %w", err)
			}
		}
	}
	return nil
}

// handleResponse routes an incoming JSON-RPC response (from the client) to
// the goroutine waiting for a permission reply.
func (s *session) handleResponse(msg *Message) {
	idStr := fmt.Sprintf("%v", msg.ID)
	s.pendingPermsMu.Lock()
	ch, ok := s.pendingPerms[idStr]
	s.pendingPermsMu.Unlock()

	if !ok {
		log.Printf("[ACP] Unexpected response id=%v (no pending request)", msg.ID)
		return
	}

	var result RequestPermissionResult
	if msg.Result != nil {
		_ = json.Unmarshal(msg.Result, &result)
	}
	ch <- &result
}

// ---- helpers ------------------------------------------------------------

// getMessageCount fetches the current message count from agentapi.
func (s *session) getMessageCount(ctx context.Context) int {
	msgs, err := s.agentapi.GetMessages(ctx)
	if err != nil || msgs == nil {
		return 0
	}
	return len(msgs.Messages)
}

// blocksToText extracts plain text from a slice of ACP ContentBlocks.
func blocksToText(blocks []ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "resource_link":
			if b.URI != "" {
				parts = append(parts, b.URI)
			}
		}
	}
	return joinLines(parts)
}

func joinLines(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	out := ss[0]
	for _, s := range ss[1:] {
		out += "\n" + s
	}
	return out
}

// messageToUpdate converts one AgentMessage to an ACP SessionUpdateNotification.
// Returns nil if the message should not produce a notification (e.g. empty
// content with no known role mapping).
func messageToUpdate(sessionID string, m *AgentMessage) *SessionUpdateNotification {
	var update SessionUpdate

	switch {
	case m.Role == "user":
		update = SessionUpdate{
			Type:  "user_message_chunk",
			Chunk: &ContentBlock{Type: "text", Text: m.Content},
		}

	case m.Role == "assistant":
		update = SessionUpdate{
			Type:  "agent_message_chunk",
			Chunk: &ContentBlock{Type: "text", Text: m.Content},
		}

	case m.Role == "agent" && m.Type == "plan":
		// Plan update – surface as a single item.
		update = SessionUpdate{
			Type: "plan",
			Plan: &PlanUpdate{
				Items: []PlanItem{{Content: m.Content, Status: "in_progress"}},
			},
		}

	case m.Role == "agent" && m.ToolUseID != "":
		// In-flight tool invocation.
		update = SessionUpdate{
			Type:       "tool_call",
			ToolCallID: m.ToolUseID,
			Title:      m.Content,
			Kind:       "other",
			Status:     "in_progress",
			Content: []ToolCallContent{{
				Type:    "content",
				Content: []ContentBlock{{Type: "text", Text: m.Content}},
			}},
		}

	case m.Role == "tool_result":
		status := "completed"
		if m.Status == "error" {
			status = "failed"
		}
		update = SessionUpdate{
			Type:       "tool_call_update",
			ToolCallID: m.ParentToolUseID,
			Status:     status,
			Content: []ToolCallContent{{
				Type:    "content",
				Content: []ContentBlock{{Type: "text", Text: m.Content}},
			}},
		}

	default:
		if m.Content == "" {
			return nil
		}
		// Fallback: emit as an agent message chunk.
		update = SessionUpdate{
			Type:  "agent_message_chunk",
			Chunk: &ContentBlock{Type: "text", Text: m.Content},
		}
	}

	return &SessionUpdateNotification{SessionID: sessionID, Update: update}
}

// ---- WebSocket send helpers ---------------------------------------------

func (s *session) sendResult(id interface{}, result interface{}) {
	s.writeJSON(map[string]interface{}{"id": id, "result": result})
}

func (s *session) sendError(id interface{}, code int, message string) {
	s.writeJSON(map[string]interface{}{
		"id":    id,
		"error": map[string]interface{}{"code": code, "message": message},
	})
}

// sendNotification sends a JSON-RPC notification (no id, no response expected).
func (s *session) sendNotification(method string, params interface{}) {
	s.writeJSON(map[string]interface{}{"method": method, "params": params})
}

// sendRequest sends a JSON-RPC request from agent→client (has id, expects response).
func (s *session) sendRequest(id interface{}, method string, params interface{}) {
	s.writeJSON(map[string]interface{}{"id": id, "method": method, "params": params})
}

func (s *session) writeJSON(v interface{}) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if err := s.conn.WriteJSON(v); err != nil {
		log.Printf("[ACP] writeJSON error: %v", err)
	}
}
