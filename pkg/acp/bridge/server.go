package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Server is an agentapi-compatible HTTP server backed by a Bridge.
// It exposes the following endpoints (compatible with takutakahashi/claude-agentapi):
//
//	GET  /health
//	GET  /status
//	GET  /messages
//	POST /message
//	GET  /events   (SSE)
//	GET  /action
//	POST /action
type Server struct {
	bridge  *Bridge
	echo    *echo.Echo
	verbose bool
}

// NewServer creates a new HTTP server wrapping the given Bridge.
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
	s.echo.GET("/status", s.handleStatus)
	s.echo.GET("/messages", s.handleGetMessages)
	s.echo.POST("/message", s.handlePostMessage)
	s.echo.GET("/events", s.handleEvents)
	s.echo.GET("/action", s.handleGetAction)
	s.echo.POST("/action", s.handlePostAction)
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
func (s *Server) handleStatus(c echo.Context) error {
	return c.JSON(http.StatusOK, s.bridge.GetStatus())
}

// GET /messages
//
// Query parameters (takutakahashi/claude-agentapi compatible):
//
//	limit     int    – max messages to return (default 50)
//	direction string – "head" | "tail" (default "tail")
//	around    int    – message ID to centre the window on
//	context   int    – number of messages before/after `around` (default 10)
//	after     int    – cursor: messages with ID > after
//	before    int    – cursor: messages with ID < before
func (s *Server) handleGetMessages(c echo.Context) error {
	all := s.bridge.GetMessages()

	limit := 50
	if v := c.QueryParam("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	direction := c.QueryParam("direction")
	if direction == "" {
		direction = "tail"
	}

	// Cursor filters
	if after := c.QueryParam("after"); after != "" {
		if id, err := strconv.ParseInt(after, 10, 64); err == nil {
			filtered := all[:0]
			for _, m := range all {
				if m.ID > id {
					filtered = append(filtered, m)
				}
			}
			all = filtered
		}
	}
	if before := c.QueryParam("before"); before != "" {
		if id, err := strconv.ParseInt(before, 10, 64); err == nil {
			filtered := all[:0]
			for _, m := range all {
				if m.ID < id {
					filtered = append(filtered, m)
				}
			}
			all = filtered
		}
	}

	// around / context window
	if aroundStr := c.QueryParam("around"); aroundStr != "" {
		if aroundID, err := strconv.ParseInt(aroundStr, 10, 64); err == nil {
			ctx := 10
			if v := c.QueryParam("context"); v != "" {
				if n, err := strconv.Atoi(v); err == nil && n >= 0 {
					ctx = n
				}
			}
			centerIdx := -1
			for i, m := range all {
				if m.ID == aroundID {
					centerIdx = i
					break
				}
			}
			if centerIdx >= 0 {
				start := centerIdx - ctx
				if start < 0 {
					start = 0
				}
				end := centerIdx + ctx + 1
				if end > len(all) {
					end = len(all)
				}
				all = all[start:end]
			}
		}
	}

	total := len(all)
	hasMore := false

	if direction == "tail" {
		if len(all) > limit {
			all = all[len(all)-limit:]
			hasMore = true
		}
	} else {
		if len(all) > limit {
			all = all[:limit]
			hasMore = true
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"messages": all,
		"total":    total,
		"hasMore":  hasMore,
	})
}

// postMessageRequest is the body for POST /message.
type postMessageRequest struct {
	Content string `json:"content"`
	Type    string `json:"type"` // "user" | "raw"
}

// POST /message
func (s *Server) handlePostMessage(c echo.Context) error {
	var req postMessageRequest
	if err := c.Bind(&req); err != nil {
		return problemJSON(c, http.StatusBadRequest, "invalid request body", err.Error())
	}
	if req.Content == "" {
		return problemJSON(c, http.StatusBadRequest, "content is required", "")
	}

	// "raw" type is not applicable for ACP (no PTY) – treat same as user.
	if err := s.bridge.SendMessage(c.Request().Context(), req.Content); err != nil {
		if err.Error() == "agent is busy" {
			return problemJSON(c, http.StatusConflict, "agent is busy", "wait for status to become stable")
		}
		return problemJSON(c, http.StatusInternalServerError, "failed to send message", err.Error())
	}

	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// GET /events – Server-Sent Events
func (s *Server) handleEvents(c echo.Context) error {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.bridge.Subscribe()
	defer cancel()

	flusher, ok := w.Writer.(http.Flusher)

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-ch:
			if !open {
				return nil
			}
			data, err := json.Marshal(event.Data)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			if ok {
				flusher.Flush()
			}
		}
	}
}

// GET /action
func (s *Server) handleGetAction(c echo.Context) error {
	return c.JSON(http.StatusOK, s.bridge.GetPendingActions())
}

// POST /action
func (s *Server) handlePostAction(c echo.Context) error {
	var req ActionRequest
	if err := c.Bind(&req); err != nil {
		return problemJSON(c, http.StatusBadRequest, "invalid request body", err.Error())
	}

	if err := s.bridge.SubmitAction(c.Request().Context(), req); err != nil {
		return problemJSON(c, http.StatusBadRequest, "action failed", err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// problemJSON returns an RFC 9457 problem detail response.
func problemJSON(c echo.Context, status int, title, detail string) error {
	body := map[string]interface{}{
		"type":   fmt.Sprintf("https://httpstatuses.io/%d", status),
		"title":  title,
		"status": status,
	}
	if detail != "" {
		body["detail"] = detail
	}
	return c.JSON(status, body)
}
