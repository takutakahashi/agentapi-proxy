package controllers

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// ProxyMessageWatcher is implemented by session managers that support per-session
// real-time message update notifications via long polling.
// KubernetesSessionManager implements this interface.
type ProxyMessageWatcher interface {
	SubscribeMessageEvents(sessionID string) (<-chan services.SessionMessageEvent, func())
}

// WaitSessionMessages handles GET /sessions/:sessionId/messages/wait.
// It blocks until a message_update event is received from the agentapi backend for the
// specified session, then returns {"updated": true, "session_id": "...", "timestamp": "..."}.
// If the timeout elapses before any message update, it returns {"updated": false}.
//
// Query parameters:
//   - timeout: max wait time in seconds (default 30, max 60)
func (c *SessionController) WaitSessionMessages(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")
	authzCtx := auth.GetAuthorizationContext(ctx)

	manager := c.getSessionManager()
	session := manager.GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	timeoutSec := 30
	if t := ctx.QueryParam("timeout"); t != "" {
		if v, err := strconv.Atoi(t); err == nil {
			timeoutSec = clampStatusTimeout(v) // reuse from session_status_controller.go
		}
	}

	watcher, ok := manager.(ProxyMessageWatcher)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented,
			"message waiting not supported by this session manager")
	}

	eventCh, cancel := watcher.SubscribeMessageEvents(sessionID)
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
				// Session deleted or manager shutting down
				return ctx.JSON(http.StatusOK, map[string]interface{}{"updated": false})
			}
			log.Printf("[MSG_WAIT] Session %s: message update received at %s", sessionID, evt.Timestamp)
			return ctx.JSON(http.StatusOK, map[string]interface{}{
				"updated":    true,
				"session_id": evt.SessionID,
				"timestamp":  evt.Timestamp,
			})

		case <-timer.C:
			return ctx.JSON(http.StatusOK, map[string]interface{}{"updated": false})
		}
	}
}
