package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ACPMessage represents a single message in the ACP session history.
type ACPMessage struct {
	ID              int    `json:"id"`
	Role            string `json:"role"`
	Content         string `json:"content"`
	Time            string `json:"time"`
	ToolUseID       string `json:"toolUseId,omitempty"`
	ParentToolUseID string `json:"parentToolUseId,omitempty"`
	Status          string `json:"status,omitempty"`
}

// ACPInterceptServer intercepts WebSocket traffic between the proxy and the
// underlying acp-ws-server, accumulating message history and storing the ACP
// session ID for reconnects.
type ACPInterceptServer struct {
	mu               sync.RWMutex
	acpSessionID     string
	messages         []ACPMessage
	nextID           int
	downstreamAddr   string           // e.g. "localhost:8081"
	pendingSessionNew json.RawMessage // original session/new params, set when rewriting to session/resume
}

// sessionStateDir returns the directory used to persist ACP session state.
// It can be overridden with ACP_SESSION_STATE_DIR. The default is
// ~/workdir/.session which lands on the PVC-backed persistent volume that
// session pods mount at /home/agentapi/workdir.
func sessionStateDir() string {
	if d := os.Getenv("ACP_SESSION_STATE_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".session"
	}
	return filepath.Join(home, "workdir", ".session")
}

// sessionIDFile returns the path to the persisted ACP session ID file.
func sessionIDFile() string {
	return filepath.Join(sessionStateDir(), "acp_session_id")
}

// loadPersistedSessionID reads the ACP session ID from disk, returning ""
// if the file does not exist or cannot be read.
func loadPersistedSessionID() string {
	data, err := os.ReadFile(sessionIDFile())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// persistSessionID writes the ACP session ID to disk.
func persistSessionID(id string) {
	dir := sessionStateDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("[ACP_INTERCEPT] Failed to create session state dir %s: %v", dir, err)
		return
	}
	if err := os.WriteFile(sessionIDFile(), []byte(id), 0600); err != nil {
		log.Printf("[ACP_INTERCEPT] Failed to persist session ID: %v", err)
	}
}

// clearPersistedSessionID removes the persisted session ID file.
func clearPersistedSessionID() {
	if err := os.Remove(sessionIDFile()); err != nil && !os.IsNotExist(err) {
		log.Printf("[ACP_INTERCEPT] Failed to clear persisted session ID: %v", err)
	}
}

// NewACPInterceptServer creates a new ACPInterceptServer that proxies to the
// given downstream address. It loads any previously persisted ACP session ID
// so that session/resume can be attempted on the first connection.
func NewACPInterceptServer(downstreamAddr string) *ACPInterceptServer {
	s := &ACPInterceptServer{
		downstreamAddr: downstreamAddr,
		nextID:         1,
	}
	if id := loadPersistedSessionID(); id != "" {
		s.acpSessionID = id
		log.Printf("[ACP_INTERCEPT] Loaded persisted ACP session ID: %s", id)
	}
	return s
}

// Start starts the HTTP/WebSocket server on listenAddr (e.g. ":8080").
// It blocks until ctx is cancelled.
func (s *ACPInterceptServer) Start(ctx context.Context, listenAddr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/messages", s.handleMessages)
	mux.HandleFunc("/", s.handleRoot)

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	log.Printf("[ACP_INTERCEPT] Listening on %s, proxying to %s", listenAddr, s.downstreamAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("acp intercept server: %w", err)
	}
	return nil
}

// handleMessages returns the accumulated message history as JSON.
func (s *ACPInterceptServer) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	msgs := make([]ACPMessage, len(s.messages))
	copy(msgs, s.messages)
	s.mu.RUnlock()

	if msgs == nil {
		msgs = []ACPMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": msgs,
	})
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleRoot upgrades HTTP connections to WebSocket and proxies them to the
// downstream acp-ws-server.
func (s *ACPInterceptServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	// Only upgrade WebSocket connections; serve 404 for plain HTTP.
	if !websocket.IsWebSocketUpgrade(r) {
		http.NotFound(w, r)
		return
	}

	clientConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ACP_INTERCEPT] Failed to upgrade client connection: %v", err)
		return
	}
	defer clientConn.Close()

	// Determine the downstream WS URL. Forward the original path.
	path := r.URL.Path
	if path == "" {
		path = "/ws"
	}
	downstreamURL := fmt.Sprintf("ws://%s%s", s.downstreamAddr, path)
	if r.URL.RawQuery != "" {
		downstreamURL += "?" + r.URL.RawQuery
	}

	downstreamConn, _, err := websocket.DefaultDialer.Dial(downstreamURL, nil)
	if err != nil {
		log.Printf("[ACP_INTERCEPT] Failed to connect to downstream %s: %v", downstreamURL, err)
		_ = clientConn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "downstream unavailable"))
		return
	}
	defer downstreamConn.Close()

	log.Printf("[ACP_INTERCEPT] WS session started, proxying to %s", downstreamURL)

	// ── Phase 1: session establishment ──────────────────────────────────────
	// Read the first client message (expected: session/new or session/resume).
	// If we rewrite it to session/resume and the server returns "Resource not
	// found", we reset state and retry with the original session/new.
	{
		msgType, data, err := clientConn.ReadMessage()
		if err != nil {
			log.Printf("[ACP_INTERCEPT] Error reading first client message: %v", err)
			return
		}

		rewritten, wasResume := s.interceptClientFrame(data)
		if err := downstreamConn.WriteMessage(msgType, rewritten); err != nil {
			log.Printf("[ACP_INTERCEPT] Error sending session message to downstream: %v", err)
			return
		}

		// Wait for the session establishment response (skip any notifications).
		for {
			_, resp, err := downstreamConn.ReadMessage()
			if err != nil {
				log.Printf("[ACP_INTERCEPT] Error reading session response: %v", err)
				return
			}

			// Check if it's a notification (has method, no id) — forward and keep waiting.
			var base jsonRPCBase
			if json.Unmarshal(resp, &base) == nil && base.Method != "" && base.ID == nil {
				s.interceptDownstreamFrame(resp)
				_ = clientConn.WriteMessage(websocket.TextMessage, resp)
				continue
			}

			// It's a response (has id).
			if wasResume && isResourceNotFoundError(resp) {
				log.Printf("[ACP_INTERCEPT] session/resume failed (Resource not found), falling back to session/new")
				// Reset stored session state (in-memory and on disk).
				s.mu.Lock()
				s.acpSessionID = ""
				s.messages = nil
				s.nextID = 1
				orig := s.pendingSessionNew
				s.pendingSessionNew = nil
				s.mu.Unlock()
				clearPersistedSessionID()

				// Send the original session/new to downstream.
				if orig != nil {
					if err := downstreamConn.WriteMessage(websocket.TextMessage, orig); err != nil {
						log.Printf("[ACP_INTERCEPT] Error sending session/new fallback: %v", err)
						return
					}
					// Wait for the new session response.
					continue
				}
			}

			// Forward the response to the client and proceed to normal proxy.
			s.interceptDownstreamFrame(resp)
			_ = clientConn.WriteMessage(websocket.TextMessage, resp)
			break
		}
	}

	// ── Phase 2: normal bidirectional proxy ──────────────────────────────────
	errCh := make(chan error, 2)

	// client → downstream
	go func() {
		for {
			msgType, data, err := clientConn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			out, _ := s.interceptClientFrame(data)
			if err := downstreamConn.WriteMessage(msgType, out); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// downstream → client
	go func() {
		for {
			msgType, data, err := downstreamConn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			s.interceptDownstreamFrame(data)
			if err := clientConn.WriteMessage(msgType, data); err != nil {
				errCh <- err
				return
			}
		}
	}()

	<-errCh
	log.Printf("[ACP_INTERCEPT] WS session ended")
}

// ── JSON-RPC helpers ─────────────────────────────────────────────────────────

type jsonRPCBase struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
}

// interceptClientFrame processes frames sent from client → downstream.
// It may rewrite the frame (e.g. session/new → session/resume).
// Returns the (possibly rewritten) frame, and whether it was rewritten to session/resume.
func (s *ACPInterceptServer) interceptClientFrame(data []byte) (out []byte, wasResume bool) {
	var base jsonRPCBase
	if err := json.Unmarshal(data, &base); err != nil || base.Method == "" {
		return data, false
	}

	switch base.Method {
	case "session/new":
		s.mu.RLock()
		sessionID := s.acpSessionID
		s.mu.RUnlock()

		if sessionID != "" {
			// Parse original session/new params to extract cwd/mcpServers.
			// session/resume requires the same params as session/new plus sessionId.
			var req jsonRPCRequest
			if err := json.Unmarshal(data, &req); err != nil {
				break
			}
			var origParams map[string]interface{}
			if err := json.Unmarshal(req.Params, &origParams); err != nil {
				origParams = map[string]interface{}{}
			}
			// Build session/resume params: keep all original fields, add sessionId
			origParams["sessionId"] = sessionID
			rewritten := map[string]interface{}{
				"jsonrpc": "2.0",
				"method":  "session/resume",
				"params":  origParams,
			}
			if base.ID != nil {
				rewritten["id"] = base.ID
			}
			if newData, err := json.Marshal(rewritten); err == nil {
				log.Printf("[ACP_INTERCEPT] Rewrote session/new → session/resume (sessionId=%s)", sessionID)
				// Store original session/new for potential fallback
				s.mu.Lock()
				s.pendingSessionNew = data
				s.mu.Unlock()
				return newData, true
			}
		}

	case "session/prompt":
		s.extractUserMessage(data)
	}

	return data, false
}

// isResourceNotFoundError checks whether a JSON-RPC response is an error
// indicating that a resource (session) was not found.
func isResourceNotFoundError(data []byte) bool {
	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil || resp.Error == nil {
		return false
	}
	// -32002 is "Resource not found" in the ACP spec
	return resp.Error.Code == -32002
}

// interceptDownstreamFrame processes frames sent from downstream → client.
func (s *ACPInterceptServer) interceptDownstreamFrame(data []byte) {
	var base jsonRPCBase
	if err := json.Unmarshal(data, &base); err != nil {
		return
	}

	if base.Method != "" {
		// It's a notification or request from the server.
		s.handleServerNotification(base.Method, data)
		return
	}

	// It might be a response (has id, no method).
	if base.ID != nil {
		s.handleServerResponse(data)
	}
}

// handleServerNotification processes JSON-RPC notifications from the server.
func (s *ACPInterceptServer) handleServerNotification(method string, data []byte) {
	if method != "session/update" {
		return
	}

	var notif jsonRPCNotification
	if err := json.Unmarshal(data, &notif); err != nil {
		return
	}

	// params.update
	var params struct {
		Update json.RawMessage `json:"update"`
	}
	if err := json.Unmarshal(notif.Params, &params); err != nil {
		return
	}

	var update struct {
		SessionUpdate string          `json:"sessionUpdate"`
		Content       json.RawMessage `json:"content"`
		ToolCallID    string          `json:"toolCallId"`
		Meta          struct {
			ClaudeCode struct {
				ToolName string `json:"toolName"`
			} `json:"claudeCode"`
		} `json:"_meta"`
		RawInput  json.RawMessage `json:"rawInput"`
		Status    string          `json:"status"`
		RawOutput json.RawMessage `json:"rawOutput"`
	}
	if err := json.Unmarshal(params.Update, &update); err != nil {
		return
	}

	switch update.SessionUpdate {
	case "agent_message_chunk":
		var content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(update.Content, &content); err != nil {
			return
		}
		if content.Type != "text" {
			return
		}
		s.appendAssistantChunk(content.Text)

	case "tool_call":
		toolName := update.Meta.ClaudeCode.ToolName
		rawInput := "{}"
		if update.RawInput != nil {
			rawInput = string(update.RawInput)
		}
		s.addToolUseMessage(update.ToolCallID, toolName, rawInput)

	case "tool_call_update":
		status := "success"
		if update.Status == "failed" {
			status = "error"
		}
		s.addToolResultMessage(update.ToolCallID, update.RawOutput, status)
	}
}

// handleServerResponse processes JSON-RPC responses from the server, looking
// for a sessionId in the result.
func (s *ACPInterceptServer) handleServerResponse(data []byte) {
	var resp jsonRPCResponse
	if err := json.Unmarshal(data, &resp); err != nil || resp.Result == nil {
		return
	}

	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil || result.SessionID == "" {
		return
	}

	s.mu.Lock()
	s.acpSessionID = result.SessionID
	s.mu.Unlock()
	persistSessionID(result.SessionID)
	log.Printf("[ACP_INTERCEPT] Stored ACP session ID: %s", result.SessionID)
}

// extractUserMessage parses a session/prompt request and stores the user text.
func (s *ACPInterceptServer) extractUserMessage(data []byte) {
	var req jsonRPCRequest
	if err := json.Unmarshal(data, &req); err != nil || req.Params == nil {
		return
	}

	var params struct {
		Prompt []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"prompt"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	var parts []string
	for _, p := range params.Prompt {
		if p.Type == "text" && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	if len(parts) == 0 {
		return
	}

	text := strings.Join(parts, "\n")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, ACPMessage{
		ID:      s.nextID,
		Role:    "user",
		Content: text,
		Time:    time.Now().Format(time.RFC3339),
	})
	s.nextID++
}

// appendAssistantChunk appends text to the last assistant message, or creates
// a new one if the last message is not an assistant message.
func (s *ACPInterceptServer) appendAssistantChunk(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.messages) > 0 && s.messages[len(s.messages)-1].Role == "assistant" {
		s.messages[len(s.messages)-1].Content += text
		return
	}

	s.messages = append(s.messages, ACPMessage{
		ID:      s.nextID,
		Role:    "assistant",
		Content: text,
		Time:    time.Now().Format(time.RFC3339),
	})
	s.nextID++
}

// addToolUseMessage adds a tool_use message. Duplicate tool call IDs are
// ignored.
func (s *ACPInterceptServer) addToolUseMessage(toolCallID, toolName, rawInput string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.messages {
		if m.Role == "agent" && m.ToolUseID == toolCallID {
			return // duplicate
		}
	}

	// Build content as the JSON shape expected by the UI's MessageItem component:
	// {type: "tool_use", name: "...", id: "...", input: {...}}
	var inputObj interface{}
	if rawInput != "" {
		if err := json.Unmarshal([]byte(rawInput), &inputObj); err != nil {
			inputObj = map[string]string{"raw": rawInput}
		}
	} else {
		inputObj = map[string]interface{}{}
	}
	contentBytes, err := json.Marshal(map[string]interface{}{
		"type":  "tool_use",
		"name":  toolName,
		"id":    toolCallID,
		"input": inputObj,
	})
	content := "{}"
	if err == nil {
		content = string(contentBytes)
	}

	s.messages = append(s.messages, ACPMessage{
		ID:        s.nextID,
		Role:      "agent",
		Content:   content,
		Time:      time.Now().Format(time.RFC3339),
		ToolUseID: toolCallID,
	})
	s.nextID++
}

// addToolResultMessage adds a tool_result message for the given toolCallId.
// Duplicate completions are ignored.
func (s *ACPInterceptServer) addToolResultMessage(toolCallID string, rawOutput json.RawMessage, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.messages {
		if m.Role == "tool_result" && m.ParentToolUseID == toolCallID {
			return // duplicate
		}
	}

	// Build content as the JSON shape expected by the UI's MessageItem component:
	// {result: ...} for success, {error: ...} for failure
	// rawOutput can be any JSON value (string, object, array, etc.)
	outputVal := interface{}(nil)
	if len(rawOutput) > 0 {
		_ = json.Unmarshal(rawOutput, &outputVal)
	}
	var contentMap map[string]interface{}
	if status == "error" {
		contentMap = map[string]interface{}{"error": outputVal}
	} else {
		contentMap = map[string]interface{}{"result": outputVal}
	}
	contentBytes, err := json.Marshal(contentMap)
	content := "{}"
	if err == nil {
		content = string(contentBytes)
	}

	s.messages = append(s.messages, ACPMessage{
		ID:              s.nextID,
		Role:            "tool_result",
		Content:         content,
		Time:            time.Now().Format(time.RFC3339),
		ParentToolUseID: toolCallID,
		Status:          status,
	})
	s.nextID++
}
