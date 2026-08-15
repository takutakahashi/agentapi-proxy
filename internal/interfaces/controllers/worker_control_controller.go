package controllers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// WorkerControlController is the deliberately small API surface available to
// background workers. Its credential is not accepted by provisioner or
// session-manager endpoints.
type WorkerControlController struct {
	manager repositories.SessionManager
	token   string
	teams   workerTeamEnsurer
	routes  repositories.SessionRouteRepository
}

type workerTeamEnsurer interface {
	EnsureTeamServiceAccount(context.Context, string) error
	EnsurePersonalAPIKey(context.Context, string) error
}

type workerStockManager interface {
	CreateStockSession(context.Context, bool) error
	CountStockSessions(context.Context, bool) (int, error)
	PurgeStaleStockSessions(context.Context) error
}

type workerSessionLister interface {
	ListSessionsContext(context.Context, entities.SessionFilter) ([]entities.Session, error)
}

type workerSessionInfo struct {
	ID            string                 `json:"id"`
	UserID        string                 `json:"user_id"`
	Scope         entities.ResourceScope `json:"scope"`
	TeamID        string                 `json:"team_id"`
	Tags          map[string]string      `json:"tags"`
	Status        string                 `json:"status"`
	StartedAt     time.Time              `json:"started_at"`
	LastMessageAt time.Time              `json:"last_message_at"`
}

func workerSessionInfoFrom(session entities.Session) workerSessionInfo {
	tags := make(map[string]string, len(session.Tags())+1)
	for key, value := range session.Tags() {
		tags[key] = value
	}
	if provider, ok := session.(interface {
		Request() *entities.RunServerRequest
	}); ok && provider.Request() != nil && provider.Request().SessionTTL != "" {
		tags["session_ttl"] = provider.Request().SessionTTL
	}
	return workerSessionInfo{ID: session.ID(), UserID: session.UserID(), Scope: session.Scope(), TeamID: session.TeamID(), Tags: tags, Status: session.Status(), StartedAt: session.StartedAt(), LastMessageAt: session.LastMessageAt()}
}

func NewWorkerControlController(manager repositories.SessionManager, token string, teams workerTeamEnsurer, routes repositories.SessionRouteRepository) *WorkerControlController {
	controller := &WorkerControlController{manager: manager, token: token}
	controller.teams = teams
	controller.routes = routes
	return controller
}

func (wc *WorkerControlController) runtimeID(ctx context.Context, publicID string) string {
	if wc.routes == nil {
		return publicID
	}
	route, err := wc.routes.Get(ctx, publicID)
	if err == nil && route != nil && route.ProxyURL == "" && route.RemoteSessionID != "" {
		return route.RemoteSessionID
	}
	return publicID
}

func (wc *WorkerControlController) authorized(c echo.Context) bool {
	if wc == nil || wc.manager == nil || wc.token == "" {
		return false
	}
	header := c.Request().Header.Get(echo.HeaderAuthorization)
	provided, ok := strings.CutPrefix(header, "Bearer ")
	return ok && len(provided) == len(wc.token) && subtle.ConstantTimeCompare([]byte(provided), []byte(wc.token)) == 1
}

func (wc *WorkerControlController) CreateSession(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var req entities.RunServerRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if req.Scope == entities.ScopeTeam && req.TeamID != "" && wc.teams != nil {
		if err := wc.teams.EnsureTeamServiceAccount(c.Request().Context(), req.TeamID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	} else if wc.teams != nil {
		if err := wc.teams.EnsurePersonalAPIKey(c.Request().Context(), req.UserID); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}
	session, err := wc.manager.CreateSession(c.Request().Context(), c.Param("sessionId"), &req, nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusCreated, workerSessionInfoFrom(session))
}

func (wc *WorkerControlController) ListSessions(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var sessions []entities.Session
	var err error
	if lister, ok := wc.manager.(workerSessionLister); ok {
		sessions, err = lister.ListSessionsContext(c.Request().Context(), entities.SessionFilter{})
	} else {
		sessions = wc.manager.ListSessions(entities.SessionFilter{})
	}
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if wc.routes != nil {
		routes, routeErr := wc.routes.List(c.Request().Context(), "")
		if routeErr != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": routeErr.Error()})
		}
		byID := make(map[string]entities.Session, len(sessions))
		for _, session := range sessions {
			byID[session.ID()] = session
		}
		aliasedRuntime := make(map[string]bool)
		for _, route := range routes {
			if route.ProxyURL != "" || route.RemoteSessionID == "" {
				continue
			}
			if runtime := byID[route.RemoteSessionID]; runtime != nil {
				sessions = append(sessions, &workerAliasSession{Session: runtime, id: route.SessionID})
				aliasedRuntime[route.RemoteSessionID] = true
			}
		}
		filtered := make([]entities.Session, 0, len(sessions))
		for _, session := range sessions {
			if !aliasedRuntime[session.ID()] {
				filtered = append(filtered, session)
			}
		}
		sessions = filtered
	}
	result := make([]workerSessionInfo, 0, len(sessions))
	for _, session := range sessions {
		result = append(result, workerSessionInfoFrom(session))
	}
	return c.JSON(http.StatusOK, result)
}

func (wc *WorkerControlController) DeleteSession(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if err := wc.manager.DeleteSession(wc.runtimeID(c.Request().Context(), c.Param("sessionId"))); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (wc *WorkerControlController) SendMessage(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var req struct {
		Message string `json:"message"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if err := wc.manager.SendMessage(c.Request().Context(), wc.runtimeID(c.Request().Context(), c.Param("sessionId")), req.Message); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (wc *WorkerControlController) StopAgent(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if err := wc.manager.StopAgent(c.Request().Context(), wc.runtimeID(c.Request().Context(), c.Param("sessionId"))); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

type workerAliasSession struct {
	entities.Session
	id string
}

func (s *workerAliasSession) ID() string { return s.id }

func (wc *WorkerControlController) Stock(c echo.Context) error {
	if !wc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	stock, ok := wc.manager.(workerStockManager)
	if !ok {
		return c.NoContent(http.StatusNotImplemented)
	}
	dind := c.QueryParam("dind") == "true"
	switch c.Request().Method {
	case http.MethodGet:
		count, err := stock.CountStockSessions(c.Request().Context(), dind)
		if err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.JSON(http.StatusOK, map[string]int{"count": count})
	case http.MethodPost:
		if err := stock.CreateStockSession(c.Request().Context(), dind); err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.NoContent(http.StatusCreated)
	case http.MethodDelete:
		if err := stock.PurgeStaleStockSessions(c.Request().Context()); err != nil {
			return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return c.NoContent(http.StatusNoContent)
	default:
		return c.NoContent(http.StatusMethodNotAllowed)
	}
}
