package controllers

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// SessionRuntimeController exposes a per-session reverse-RPC channel used by a
// Session Pod to communicate directly with the parent proxy.
type SessionRuntimeController struct {
	store  core.Store
	routes repositories.SessionRouteRepository
}

func NewSessionRuntimeController(store core.Store, routes repositories.SessionRouteRepository) *SessionRuntimeController {
	return &SessionRuntimeController{store: store, routes: routes}
}

func (c *SessionRuntimeController) authorize(ctx echo.Context) (*repositories.SessionRoute, int) {
	if c == nil || c.store == nil || c.routes == nil {
		return nil, http.StatusUnauthorized
	}
	route, err := c.routes.Get(ctx.Request().Context(), ctx.Param("sessionId"))
	if err != nil || route == nil || route.Transport != repositories.SessionRouteTransportDirectRuntime || route.RuntimeTokenHash == "" {
		return nil, http.StatusUnauthorized
	}
	generation, err := strconv.ParseInt(ctx.QueryParam("generation"), 10, 64)
	if err != nil {
		return nil, http.StatusUnauthorized
	}
	if generation != route.Generation {
		return route, http.StatusConflict
	}
	const prefix = "Bearer "
	header := ctx.Request().Header.Get("Authorization")
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return nil, http.StatusUnauthorized
	}
	digest := sha256.Sum256([]byte(header[len(prefix):]))
	actual := hex.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(actual), []byte(route.RuntimeTokenHash)) != 1 {
		return nil, http.StatusUnauthorized
	}
	return route, http.StatusOK
}

func (c *SessionRuntimeController) WaitRequests(ctx echo.Context) error {
	route, status := c.authorize(ctx)
	if status != http.StatusOK {
		return ctx.NoContent(status)
	}
	if err := c.store.TouchManager(ctx.Request().Context(), route.SessionID, ctx.QueryParam("instance_id")); err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	requests, err := c.store.ReadCommands(ctx.Request().Context(), route.SessionID, ctx.QueryParam("after"), parseWait(ctx.QueryParam("wait")), parseControlCount(ctx.QueryParam("count")))
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if len(requests) == 0 {
		return ctx.NoContent(http.StatusNoContent)
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"requests": requests, "next_cursor": requests[len(requests)-1].StreamID})
}

func (c *SessionRuntimeController) AppendFrames(ctx echo.Context) error {
	route, status := c.authorize(ctx)
	if status != http.StatusOK {
		return ctx.NoContent(status)
	}
	if err := c.store.TouchManager(ctx.Request().Context(), route.SessionID, ctx.QueryParam("instance_id")); err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	var request struct {
		Frames []core.ResponseFrame `json:"frames"`
	}
	if err := ctx.Bind(&request); err != nil || len(request.Frames) == 0 || len(request.Frames) > 100 {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "frames must contain between 1 and 100 entries"})
	}
	requestID := request.Frames[0].RequestID
	belongs, err := c.store.RequestBelongsToManager(ctx.Request().Context(), requestID, route.SessionID)
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if !belongs {
		return ctx.NoContent(http.StatusForbidden)
	}
	for i := range request.Frames {
		frame := &request.Frames[i]
		if frame.RequestID != requestID || frame.ID == "" {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "all frames must have the same request_id and a non-empty id"})
		}
	}
	last, err := c.store.AppendFrames(ctx.Request().Context(), requestID, request.Frames)
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	for _, frame := range request.Frames {
		if frame.CommandStreamID != "" {
			if err := c.store.AckCommand(ctx.Request().Context(), route.SessionID, frame.CommandStreamID); err != nil {
				return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			}
		}
	}
	return ctx.JSON(http.StatusAccepted, map[string]string{"accepted_through": last})
}
