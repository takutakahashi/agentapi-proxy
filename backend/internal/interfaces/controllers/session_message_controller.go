package controllers

import (
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// ProxyMessageWatcher is implemented by session managers that support per-session
// real-time message update notifications via long polling.
// KubernetesSessionManager implements this interface.
type ProxyMessageWatcher interface {
	SubscribeMessageEvents(sessionID string) (<-chan portrepos.SessionMessageEvent, func())
}

// WaitSessionMessages handles GET /sessions/:sessionId/messages/wait.
// It blocks until a message_update event is received from the agentapi backend for the
// specified session, then returns {"updated": true, "session_id": "...", "timestamp": "..."}.
// If the timeout elapses before any message update, it returns {"updated": false}.
//
// Query parameters:
//   - timeout: max wait time in seconds (default 30, max 60)
//   - since: Unix timestamp in milliseconds (or RFC3339 string) of the last known update.
//     If the session has received a message_update after this timestamp, the response is
//     returned immediately without waiting. This allows clients that were inactive to
//     catch up to the latest state on their next poll.
func (c *SessionController) WaitSessionMessages(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")
	authzCtx := auth.GetAuthorizationContext(ctx)

	manager := c.getSessionManager()
	session := manager.GetSession(sessionID)
	if session == nil {
		// Remote sessions are represented by a durable route in the parent, not
		// by an in-process Session. Their runtime event stream is transported by
		// the ESM tunnel, so the local message watcher cannot subscribe to it.
		// Return a paced update hint instead of 404: the UI then refreshes the
		// canonical /<sessionId>/messages endpoint through that tunnel. This is a
		// bounded compatibility poll until message events are carried by the
		// direct runtime protocol.
		if c.sessionRouteRepo != nil {
			route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to look up session route")
			}
			if route != nil && route.RemoteSessionID != "" {
				if !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
					return echo.NewHTTPError(http.StatusForbidden, "access denied")
				}
				return waitRemoteMessageRefresh(ctx, sessionID)
			}
		}
		// Direct runtimes renew a parent-owned connection lease keyed by the
		// public session ID. This is also authoritative while a route repository
		// replica is catching up after rollout.
		if c.esmControlTunnel != nil && c.esmControlTunnel.IsConnected(ctx.Request().Context(), sessionID) {
			return waitRemoteMessageRefresh(ctx, sessionID)
		}
		// This endpoint carries no session data; it only tells an authenticated
		// client to re-read the separately authorized /messages endpoint. Some
		// API-only compositions intentionally keep routing out of this controller,
		// so absence from both local state and injected route caches must not turn
		// the chat refresh loop into a permanent 404.
		return waitRemoteMessageRefresh(ctx, sessionID)
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

	// Parse the optional since parameter (Unix ms or RFC3339).
	var since time.Time
	if s := ctx.QueryParam("since"); s != "" {
		if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
			since = time.UnixMilli(ms)
		} else if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			since = t
		}
	}

	watcher, ok := manager.(ProxyMessageWatcher)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented,
			"message waiting not supported by this session manager")
	}

	// Subscribe before reading lastMessageAt to avoid a TOCTOU race: if a
	// broadcast arrives between the check and the subscribe, the channel will
	// already have the event and we return on the first select iteration.
	eventCh, cancel := watcher.SubscribeMessageEvents(sessionID)
	defer cancel()

	// If the caller provided a since timestamp and the session already has a
	// newer message update, return immediately so that clients resuming after
	// inactivity catch up without waiting for the next event.
	if !since.IsZero() {
		if lastMsgAt := session.LastMessageAt(); lastMsgAt.After(since) {
			log.Printf("[MSG_WAIT] Session %s: already updated at %s (since=%s), returning immediately",
				sessionID, lastMsgAt, since)
			return ctx.JSON(http.StatusOK, map[string]interface{}{
				"updated":    true,
				"session_id": sessionID,
				"timestamp":  lastMsgAt,
			})
		}
	}

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

func waitRemoteMessageRefresh(ctx echo.Context, sessionID string) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Request().Context().Done():
		return nil
	case timestamp := <-timer.C:
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"updated": true, "session_id": sessionID, "timestamp": timestamp,
		})
	}
}
