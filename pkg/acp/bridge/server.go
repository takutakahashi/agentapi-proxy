package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

// Server is a minimal HTTP transport for ACP.
// It exposes three endpoints that mirror the ACP JSON-RPC 2.0 protocol over HTTP:
//
//	GET  /session  – session info (sessionId, status)
//	POST /rpc      – JSON-RPC 2.0 messages from HTTP client to ACP agent
//	GET  /sse      – SSE stream of JSON-RPC 2.0 messages from ACP agent to HTTP client
//	GET  /health   – health check (for compatibility)
type Server struct {
	bridge  *Bridge
	echo    *echo.Echo
	verbose bool
	msgSeq  atomic.Int64 // used to generate unique JSON-RPC IDs for POST /message
}

// NewServer creates a new HTTP transport server backed by the given Bridge.
func NewServer(b *Bridge, verbose bool) *Server {
	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	if verbose {
		e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
			LogStatus: true,
			LogURI:    true,
			LogMethod: true,
			LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
				log.Printf("[acp-server] %s %s -> %d", v.Method, v.URI, v.Status)
				return nil
			},
		}))
	}

	s := &Server{bridge: b, echo: e, verbose: verbose}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.echo.GET("/health", s.handleHealth)
	// /status is polled by the provisioner to detect when the agent is ready.
	// It must return HTTP 200; the body mirrors the agentapi status shape so
	// that existing tooling can parse it without changes.
	s.echo.GET("/status", s.handleStatus)
	s.echo.GET("/session", s.handleGetSession)
	// /messages returns the full in-memory message history as a JSON array of
	// raw JSON-RPC 2.0 objects.  Reconnecting clients use this to replay events
	// they missed while disconnected from the SSE stream.
	s.echo.GET("/messages", s.handleGetMessages)
	s.echo.POST("/rpc", s.handleRPC)
	// /message is the agentapi-compatible endpoint used by the proxy and SlackBot
	// to send follow-up user messages to an active session (multi-turn support).
	// Accepts {"content": "...", "type": "user"} and forwards the text to the agent.
	s.echo.POST("/message", s.handleMessage)
	s.echo.GET("/sse", s.handleSSE)
	// /events is the agentapi-compatible SSE endpoint consumed by the proxy's
	// watchAgentAPIStatus goroutine.  It emits status_change events whenever
	// the agent transitions between "running" and "stable".
	s.echo.GET("/events", s.handleEvents)
}

// Start starts the HTTP server on the given address (e.g. ":3284").
// It blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context, addr string) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.echo.Start(addr); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*1e9) // 5s
		defer cancel()
		return s.echo.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// ----------------------------------------------------------------------------
// Handlers
// ----------------------------------------------------------------------------

// GET /health
func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// GET /status
// Returns agentapi-compatible status so the provisioner's health check passes.
// Returns the actual agent status ("running" while processing a prompt, "stable" otherwise)
// so that the UI can disable input and show a stop button while the agent is busy.
// Includes agent_type from the AGENTAPI_AGENT_TYPE environment variable (if set)
// so that the UI can detect the ACP session type and enable appropriate features
// such as Markdown rendering.
func (s *Server) handleStatus(c echo.Context) error {
	resp := map[string]string{"status": s.bridge.Status()}
	if agentType := os.Getenv("AGENTAPI_AGENT_TYPE"); agentType != "" {
		resp["agent_type"] = agentType
	}
	return c.JSON(http.StatusOK, resp)
}

// GET /messages
// Returns all JSON-RPC 2.0 messages that have been broadcast since the bridge started.
// The response is a JSON object with a "messages" array so the client can distinguish
// an empty history ({"messages":[]}) from an error.
func (s *Server) handleGetMessages(c echo.Context) error {
	msgs := s.bridge.Messages()
	if msgs == nil {
		msgs = []json.RawMessage{}
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"messages": msgs})
}

// GET /session
// Returns the session ID and current status so clients know what ID to use in RPC calls.
func (s *Server) handleGetSession(c echo.Context) error {
	sessionId := s.bridge.SessionID()
	log.Printf("[acp-server] GET /session -> sessionId=%s (remoteAddr=%s)", sessionId, c.RealIP())
	return c.JSON(http.StatusOK, map[string]string{
		"sessionId": sessionId,
		"status":    "ready",
	})
}

// POST /message
//
// agentapi-compatible endpoint for sending a user message to the active session.
// Used by the proxy and SlackBot for multi-turn conversations.
// Accepts {"content": "...", "type": "user"} and forwards the text to the ACP agent.
func (s *Server) handleMessage(c echo.Context) error {
	var payload struct {
		Content string `json:"content"`
		Type    string `json:"type"`
	}
	if err := c.Bind(&payload); err != nil {
		log.Printf("[acp-server] POST /message parse error (remoteAddr=%s): %v", c.RealIP(), err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request"})
	}
	if payload.Content == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "content is required"})
	}
	log.Printf("[acp-server] POST /message content=%q (remoteAddr=%s)", payload.Content, c.RealIP())
	id := s.msgSeq.Add(1)
	idRaw, _ := json.Marshal(id)
	if err := s.bridge.SendPrompt(idRaw, payload.Content); err != nil {
		log.Printf("[acp-server] POST /message SendPrompt error: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// rpcEnvelope is the JSON-RPC 2.0 message envelope received on POST /rpc.
// Covers all three message kinds: request, notification, and response.
type rpcEnvelope struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   json.RawMessage  `json:"error,omitempty"`
}

// POST /rpc
//
// Accepts three kinds of JSON-RPC 2.0 messages:
//
//  1. Client-initiated requests (id + method):
//     - "session/prompt"  → forwards text to ACP agent; result comes via SSE
//     - "session/cancel"  → cancels the current agent turn
//
//  2. Client-initiated notifications (no id, method set):
//     - "session/cancel"  → same as above, no response expected
//
//  3. Client responses to agent-initiated requests (id set, no method):
//     - Response to "session/request_permission" emitted via SSE
func (s *Server) handleRPC(c echo.Context) error {
	var env rpcEnvelope
	if err := c.Bind(&env); err != nil {
		log.Printf("[acp-server] POST /rpc parse error (remoteAddr=%s): %v", c.RealIP(), err)
		return c.JSON(http.StatusBadRequest, rpcErrorResp(nil, -32700, "Parse error"))
	}
	log.Printf("[acp-server] POST /rpc method=%q id=%v (remoteAddr=%s)", env.Method, env.ID, c.RealIP())

	// ── Case 1: Response to an agent-initiated request ───────────────────────
	// Identified by: id is set, method is absent.
	if env.ID != nil && env.Method == "" {
		if env.Result == nil {
			return c.JSON(http.StatusBadRequest,
				rpcErrorResp(env.ID, -32600, "result is required for response messages"))
		}
		var id int64
		if err := json.Unmarshal(*env.ID, &id); err != nil {
			return c.JSON(http.StatusBadRequest,
				rpcErrorResp(env.ID, -32600, "id must be an integer for agent-initiated requests"))
		}
		if err := s.bridge.HandleReply(id, env.Result); err != nil {
			return c.JSON(http.StatusBadRequest, rpcErrorResp(env.ID, -32600, err.Error()))
		}
		return c.JSON(http.StatusOK, map[string]bool{"ok": true})
	}

	// ── Case 2: Client-initiated request or notification ─────────────────────
	switch env.Method {
	case "session/prompt":
		// Require an id so we can correlate the async result via SSE.
		if env.ID == nil {
			return c.JSON(http.StatusBadRequest,
				rpcErrorResp(nil, -32600, "id is required for session/prompt"))
		}
		var params acp.PromptParams
		if err := json.Unmarshal(env.Params, &params); err != nil {
			return c.JSON(http.StatusBadRequest,
				rpcErrorResp(env.ID, -32602, "invalid params: "+err.Error()))
		}
		// Extract the first text content block.
		text := ""
		for _, block := range params.Prompt {
			if block.Type == acp.ContentBlockTypeText {
				text = block.Text
				break
			}
		}
		if text == "" {
			return c.JSON(http.StatusBadRequest,
				rpcErrorResp(env.ID, -32602, "prompt must contain at least one text content block"))
		}
		if err := s.bridge.SendPrompt(*env.ID, text); err != nil {
			return c.JSON(http.StatusInternalServerError,
				rpcErrorResp(env.ID, -32000, err.Error()))
		}
		// 202: the agent is working asynchronously; the result arrives via SSE.
		return c.JSON(http.StatusAccepted, map[string]bool{"ok": true})

	case "session/cancel":
		_ = s.bridge.Cancel(c.Request().Context())
		// Notifications don't require a response; requests get a simple ack.
		return c.JSON(http.StatusOK, map[string]bool{"ok": true})

	default:
		return c.JSON(http.StatusBadRequest,
			rpcErrorResp(env.ID, -32601, "method not supported: "+env.Method))
	}
}

// GET /sse
//
// Server-Sent Events stream of JSON-RPC 2.0 messages from the ACP agent.
// Supports the SSE Last-Event-ID resumption mechanism: if the client sends a
// Last-Event-ID header, only messages with a higher index are replayed, then
// live events are streamed. On first connect (no Last-Event-ID) all history is
// replayed so the client gets the full conversation context.
//
// Each SSE event carries an id (0-based history index) and a raw JSON-RPC object:
//
//	id: 0
//	event: message
//	data: {"jsonrpc":"2.0","method":"session/update","params":{...}}
//
//	id: 1
//	event: message
//	data: {"jsonrpc":"2.0","id":1,"result":{"stopReason":"end_turn"}}
func (s *Server) handleSSE(c echo.Context) error {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	lastEventIDStr := c.Request().Header.Get("Last-Event-ID")
	// Default: subscribe from current position (no history replay).
	// Clients that want history should call GET /messages first, then reconnect
	// with Last-Event-ID to resume from a known point.
	lastEventID := SubscribeFromCurrent
	if lastEventIDStr != "" {
		if id, err := strconv.Atoi(lastEventIDStr); err == nil {
			lastEventID = id
		}
	}
	log.Printf("[acp-server] GET /sse connected (remoteAddr=%s, lastEventID=%d)", c.RealIP(), lastEventID)

	ch, history, nextIdx, cancel := s.bridge.SubscribeFrom(lastEventID)
	defer func() {
		cancel()
		log.Printf("[acp-server] GET /sse disconnected (remoteAddr=%s)", c.RealIP())
	}()

	flusher, hasFlusher := w.Writer.(http.Flusher)

	// Replay history events that the client missed (or all of them on first connect).
	historyStartIdx := lastEventID + 1
	if historyStartIdx < 0 {
		historyStartIdx = 0
	}
	for i, msg := range history {
		_, _ = fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", historyStartIdx+i, msg)
	}
	if hasFlusher {
		flusher.Flush()
	}

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case raw, open := <-ch:
			if !open {
				return nil
			}
			_, _ = fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", nextIdx, raw)
			nextIdx++
			if hasFlusher {
				flusher.Flush()
			}
		}
	}
}

// GET /events
//
// agentapi-compatible SSE stream consumed by the proxy's watchAgentAPIStatus goroutine.
// On connect it immediately emits a status_change event with the current status so
// reconnecting consumers always get a consistent starting point, then streams every
// subsequent status transition.
func (s *Server) handleEvents(c echo.Context) error {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.Writer.(http.Flusher)

	// Emit the current status immediately so the consumer doesn't have to wait
	// for the next transition.
	_, _ = fmt.Fprintf(w, "event: status_change\ndata: {\"status\":%q}\n\n", s.bridge.Status())
	if hasFlusher {
		flusher.Flush()
	}

	ch, cancel := s.bridge.SubscribeStatus()
	defer cancel()

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case status, open := <-ch:
			if !open {
				return nil
			}
			_, _ = fmt.Fprintf(w, "event: status_change\ndata: {\"status\":%q}\n\n", status)
			if hasFlusher {
				flusher.Flush()
			}
		}
	}
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// rpcErrorResp builds a JSON-RPC 2.0 error response object.
func rpcErrorResp(id *json.RawMessage, code int, message string) map[string]interface{} {
	return map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	}
}
