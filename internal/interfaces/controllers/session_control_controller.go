package controllers

import (
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type SessionControlController struct {
	store     core.Store
	manager   ProvisionerManager
	connected sync.Map
}

type SessionControlReaderController struct {
	store   core.Store
	manager sessionControlSessionReader
}

func NewSessionControlReaderController(store core.Store, manager sessionControlSessionReader) *SessionControlReaderController {
	return &SessionControlReaderController{store: store, manager: manager}
}

type sessionControlSessionReader interface {
	GetSession(string) entities.Session
}

func NewSessionControlController(store core.Store, manager ProvisionerManager) *SessionControlController {
	return &SessionControlController{store: store, manager: manager}
}

func (sc *SessionControlController) authorized(c echo.Context) bool {
	if sc.manager == nil {
		return false
	}
	h := c.Request().Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) <= len(prefix) || h[:len(prefix)] != prefix {
		return false
	}
	return sc.manager.ValidateSessionControlToken(c.Param("sessionId"), h[len(prefix):])
}

func (sc *SessionControlController) WaitCommands(c echo.Context) error {
	if !sc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if err := sc.store.TouchConnection(c.Request().Context(), c.Param("sessionId")); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if _, loaded := sc.connected.LoadOrStore(c.Param("sessionId"), struct{}{}); !loaded {
		log.Printf("[SESSION_CONTROL] Session %s connected through command long poll", c.Param("sessionId"))
	}
	commands, err := sc.store.ReadCommands(c.Request().Context(), c.Param("sessionId"), c.QueryParam("after"), parseWait(c.QueryParam("wait")), parseControlCount(c.QueryParam("count")))
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if len(commands) == 0 {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"commands": commands, "next_cursor": commands[len(commands)-1].StreamID})
}

func (sc *SessionControlController) AppendEvents(c echo.Context) error {
	if !sc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if err := sc.store.TouchConnection(c.Request().Context(), c.Param("sessionId")); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	var request struct {
		Events []core.Event `json:"events"`
	}
	if err := c.Bind(&request); err != nil || len(request.Events) == 0 || len(request.Events) > 100 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "events must contain between 1 and 100 entries"})
	}
	for _, event := range request.Events {
		if event.ID == "" || event.Type == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "event id and type are required"})
		}
		if (event.Type == "command_completed" || event.Type == "command_failed") && event.CommandStreamID != "" {
			if err := sc.store.AckCommand(c.Request().Context(), c.Param("sessionId"), event.CommandStreamID); err != nil {
				return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			}
		}
	}
	lastID, err := sc.store.AppendEvents(c.Request().Context(), c.Param("sessionId"), request.Events)
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusAccepted, map[string]string{"accepted_through": lastID})
}

func (sc *SessionControlController) WaitEvents(c echo.Context) error {
	reader, ok := sc.manager.(sessionControlSessionReader)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "session control events are unavailable")
	}
	return waitSessionControlEvents(c, sc.store, reader)
}

func (sc *SessionControlReaderController) WaitEvents(c echo.Context) error {
	return waitSessionControlEvents(c, sc.store, sc.manager)
}

func waitSessionControlEvents(c echo.Context, store core.Store, reader sessionControlSessionReader) error {
	session := reader.GetSession(c.Param("sessionId"))
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	authz := auth.GetAuthorizationContext(c)
	if !authz.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}
	events, err := store.ReadEvents(c.Request().Context(), c.Param("sessionId"), c.QueryParam("after"), parseWait(c.QueryParam("wait")), parseControlCount(c.QueryParam("count")))
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if len(events) == 0 {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"events": events, "next_cursor": events[len(events)-1].StreamID})
}

func parseControlCount(raw string) int64 {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 || value > 100 {
		return 100
	}
	return value
}
