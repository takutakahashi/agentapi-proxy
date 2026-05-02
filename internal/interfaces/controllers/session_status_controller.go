package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// ProxyStatusWatcher is implemented by session managers that support proxy-wide
// real-time status change notifications. KubernetesSessionManager implements this.
// The session controller uses a type-assertion to obtain this capability, keeping
// the core SessionManager interface unchanged.
type ProxyStatusWatcher interface {
	SubscribeStatusEvents() (<-chan services.SessionStatusEvent, func())
}

// StreamSessionsStatus handles GET /sessions/status/stream.
// It opens a Server-Sent Events stream that pushes a SessionStatusEvent whenever
// any session accessible to the authenticated user changes its status.
// A heartbeat comment is sent every 30 seconds to keep the connection alive.
func (c *SessionController) StreamSessionsStatus(ctx echo.Context) error {
	authzCtx := auth.GetAuthorizationContext(ctx)

	manager := c.getSessionManager()
	watcher, ok := manager.(ProxyStatusWatcher)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "status streaming not supported by this session manager")
	}

	// Set SSE response headers
	r := ctx.Response()
	r.Header().Set("Content-Type", "text/event-stream")
	r.Header().Set("Cache-Control", "no-cache")
	r.Header().Set("Connection", "keep-alive")
	r.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	r.WriteHeader(http.StatusOK)

	eventCh, cancel := watcher.SubscribeStatusEvents()
	defer cancel()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	reqCtx := ctx.Request().Context()
	flusher, hasFlusher := r.Writer.(http.Flusher)

	for {
		select {
		case <-reqCtx.Done():
			return nil

		case evt, open := <-eventCh:
			if !open {
				return nil
			}
			// Authorization filter: only forward events for sessions this user can access.
			session := manager.GetSession(evt.SessionID)
			if session == nil {
				continue
			}
			if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
				continue
			}
			if err := writeSessionStatusSSEEvent(r, evt); err != nil {
				return nil
			}
			if hasFlusher {
				flusher.Flush()
			}

		case <-heartbeat.C:
			if _, err := fmt.Fprintf(r, ": heartbeat\n\n"); err != nil {
				return nil
			}
			if hasFlusher {
				flusher.Flush()
			}
		}
	}
}

// WaitSessionsStatus handles GET /sessions/status/wait.
// It blocks until any session accessible to the authenticated user changes status,
// then returns the event as JSON. If the timeout elapses before any change,
// it returns `{"events": []}`.
//
// Query parameters:
//   - timeout: max wait time in seconds (default 30, max 60)
func (c *SessionController) WaitSessionsStatus(ctx echo.Context) error {
	authzCtx := auth.GetAuthorizationContext(ctx)

	timeoutSec := 30
	if t := ctx.QueryParam("timeout"); t != "" {
		if v, err := strconv.Atoi(t); err == nil {
			timeoutSec = clampStatusTimeout(v)
		}
	}

	manager := c.getSessionManager()
	watcher, ok := manager.(ProxyStatusWatcher)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "status streaming not supported by this session manager")
	}

	eventCh, cancel := watcher.SubscribeStatusEvents()
	defer cancel()

	timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
	defer timer.Stop()

	reqCtx := ctx.Request().Context()

	for {
		select {
		case <-reqCtx.Done():
			return nil

		case evt, open := <-eventCh:
			if !open {
				// Manager shutting down
				return ctx.JSON(http.StatusOK, map[string]interface{}{"events": []interface{}{}})
			}
			// Authorization filter
			session := manager.GetSession(evt.SessionID)
			if session == nil {
				continue
			}
			if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
				continue
			}
			return ctx.JSON(http.StatusOK, evt)

		case <-timer.C:
			// Timeout reached: return empty event list so callers can distinguish
			// a real change from a timeout without relying on HTTP status codes.
			return ctx.JSON(http.StatusOK, map[string]interface{}{"events": []interface{}{}})
		}
	}
}

// writeSessionStatusSSEEvent marshals evt and writes a data event to the SSE response.
func writeSessionStatusSSEEvent(w *echo.Response, evt services.SessionStatusEvent) error {
	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
	return err
}

// clampStatusTimeout returns v clamped to [1, 60].
func clampStatusTimeout(v int) int {
	if v < 1 {
		return 1
	}
	if v > 60 {
		return 60
	}
	return v
}
