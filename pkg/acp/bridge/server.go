package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

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
	s.echo.GET("/session", s.handleGetSession)
	s.echo.POST("/rpc", s.handleRPC)
	s.echo.GET("/sse", s.handleSSE)
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
// Each SSE event has type "message" and carries a raw JSON-RPC object:
//
//	event: message
//	data: {"jsonrpc":"2.0","method":"session/update","params":{...}}
//
//	event: message
//	data: {"jsonrpc":"2.0","id":1,"method":"session/request_permission","params":{...}}
//
//	event: message
//	data: {"jsonrpc":"2.0","id":1,"result":{"stopReason":"end_turn"}}
func (s *Server) handleSSE(c echo.Context) error {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	log.Printf("[acp-server] GET /sse connected (remoteAddr=%s, lastEventID=%q)", c.RealIP(), c.Request().Header.Get("Last-Event-ID"))

	ch, cancel := s.bridge.Subscribe()
	defer func() {
		cancel()
		log.Printf("[acp-server] GET /sse disconnected (remoteAddr=%s)", c.RealIP())
	}()

	flusher, hasFlusher := w.Writer.(http.Flusher)

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case raw, open := <-ch:
			if !open {
				return nil
			}
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", raw)
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
