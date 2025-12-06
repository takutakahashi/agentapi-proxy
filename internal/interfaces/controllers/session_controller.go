package controllers

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/presenters"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// SessionController handles HTTP requests for session operations
type SessionController struct {
	createSessionUC  *session.CreateSessionUseCase
	deleteSessionUC  *session.DeleteSessionUseCase
	listSessionsUC   *session.ListSessionsUseCase
	getSessionByIDUC *session.GetSessionByIDUseCase
	monitorSessionUC *session.MonitorSessionUseCase
	sessionPresenter presenters.SessionPresenter
}

// RegisterRoutes registers session routes with authentication middleware
func (c *SessionController) RegisterRoutes(e *echo.Echo, authService services.AuthService) {
	// Create middleware for different permissions
	sessionReadMiddleware := auth.RequirePermission(entities.PermissionSessionRead, authService)
	sessionCreateMiddleware := auth.RequirePermission(entities.PermissionSessionCreate, authService)
	sessionDeleteMiddleware := auth.RequirePermission(entities.PermissionSessionDelete, authService)

	// Register legacy routes (for backward compatibility) - auth is handled by base AuthMiddleware
	e.POST("/start", c.StartSession)
	e.GET("/search", c.SearchSessions)

	// Create session group with appropriate permissions
	sessionGroup := e.Group("/sessions")
	sessionGroup.DELETE("/:sessionId", c.DeleteSession, sessionDeleteMiddleware)
	sessionGroup.GET("/:sessionId", c.GetSession, sessionReadMiddleware)
	sessionGroup.GET("", c.ListSessions, sessionReadMiddleware)
	sessionGroup.GET("/:sessionId/monitor", c.MonitorSession, sessionReadMiddleware)

	// Register API v1 routes
	apiV1 := e.Group("/api/v1")
	apiV1.POST("/sessions", c.StartSession, sessionCreateMiddleware)
	apiV1.GET("/sessions/search", c.SearchSessions, sessionReadMiddleware)
	apiV1.DELETE("/sessions/:sessionId", c.DeleteSession, sessionDeleteMiddleware)
	apiV1.GET("/sessions/:sessionId", c.GetSession, sessionReadMiddleware)
	apiV1.GET("/sessions", c.ListSessions, sessionReadMiddleware)
	apiV1.GET("/sessions/:sessionId/monitor", c.MonitorSession, sessionReadMiddleware)
}

// NewSessionController creates a new SessionController
func NewSessionController(
	createSessionUC *session.CreateSessionUseCase,
	deleteSessionUC *session.DeleteSessionUseCase,
	listSessionsUC *session.ListSessionsUseCase,
	getSessionByIDUC *session.GetSessionByIDUseCase,
	monitorSessionUC *session.MonitorSessionUseCase,
	sessionPresenter presenters.SessionPresenter,
) *SessionController {
	return &SessionController{
		createSessionUC:  createSessionUC,
		deleteSessionUC:  deleteSessionUC,
		listSessionsUC:   listSessionsUC,
		getSessionByIDUC: getSessionByIDUC,
		monitorSessionUC: monitorSessionUC,
		sessionPresenter: sessionPresenter,
	}
}

// CreateSessionRequest represents the HTTP request for creating a session
type CreateSessionRequest struct {
	Environment map[string]string  `json:"environment"`
	Tags        map[string]string  `json:"tags"`
	Repository  *RepositoryRequest `json:"repository,omitempty"`
	Port        *int               `json:"port,omitempty"`
}

// RepositoryRequest represents repository information in HTTP requests
type RepositoryRequest struct {
	URL    string `json:"url"`
	Branch string `json:"branch,omitempty"`
	Token  string `json:"token,omitempty"`
}

// StartSession handles POST /start
func (c *SessionController) StartSession(ctx echo.Context) error {
	return c.CreateSession(ctx)
}

// SearchSessions handles GET /search
func (c *SessionController) SearchSessions(ctx echo.Context) error {
	return c.ListSessions(ctx)
}

// CreateSession handles POST /sessions
func (c *SessionController) CreateSession(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract user from context (set by auth middleware)
	var userID entities.UserID
	if user := auth.GetUserFromContext(ctx); user != nil {
		userID = user.ID()
	} else {
		// For legacy compatibility, create a default anonymous user
		userID = entities.UserID("anonymous")
	}

	// Parse request body
	var req CreateSessionRequest
	if err := ctx.Bind(&req); err != nil {
		return c.sessionPresenter.PresentError(ctx.Response(), "invalid request body", http.StatusBadRequest)
	}

	// Convert to use case request
	ucReq := &session.CreateSessionRequest{
		UserID:      userID,
		Environment: entities.Environment(req.Environment),
		Tags:        entities.Tags(req.Tags),
	}

	// Convert repository if provided
	if req.Repository != nil {
		repo, err := entities.NewRepository(entities.RepositoryURL(req.Repository.URL))
		if err != nil {
			return c.sessionPresenter.PresentError(ctx.Response(), "invalid repository: "+err.Error(), http.StatusBadRequest)
		}
		if req.Repository.Branch != "" {
			if err := repo.SetBranch(req.Repository.Branch); err != nil {
				return c.sessionPresenter.PresentError(ctx.Response(), "invalid branch: "+err.Error(), http.StatusBadRequest)
			}
		}
		if req.Repository.Token != "" {
			repo.SetAccessToken(req.Repository.Token)
		}
		ucReq.Repository = repo
	}

	// Convert port if provided
	if req.Port != nil {
		port := entities.Port(*req.Port)
		ucReq.Port = &port
	}

	// Execute use case
	response, err := c.createSessionUC.Execute(reqCtx, ucReq)
	if err != nil {
		return c.sessionPresenter.PresentError(ctx.Response(), err.Error(), http.StatusInternalServerError)
	}

	// Present response
	return c.sessionPresenter.PresentCreateSession(ctx.Response(), response)
}

// GetSession handles GET /sessions/{id}
func (c *SessionController) GetSession(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract user from context (set by auth middleware)
	var userID entities.UserID
	if user := auth.GetUserFromContext(ctx); user != nil {
		userID = user.ID()
	} else {
		// For legacy compatibility, create a default anonymous user
		userID = entities.UserID("anonymous")
	}

	// Extract session ID from URL path
	sessionID := ctx.Param("sessionId")
	if sessionID == "" {
		return c.sessionPresenter.PresentError(ctx.Response(), "session ID is required", http.StatusBadRequest)
	}

	// Execute use case
	ucReq := &session.GetSessionByIDRequest{
		SessionID: entities.SessionID(sessionID),
		UserID:    userID,
	}

	response, err := c.getSessionByIDUC.Execute(reqCtx, ucReq)
	if err != nil {
		return c.sessionPresenter.PresentError(ctx.Response(), err.Error(), http.StatusNotFound)
	}

	// Present response
	return c.sessionPresenter.PresentSession(ctx.Response(), response.Session)
}

// ListSessions handles GET /sessions
func (c *SessionController) ListSessions(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract user from context (set by auth middleware)
	var userID entities.UserID
	if user := auth.GetUserFromContext(ctx); user != nil {
		userID = user.ID()
	} else {
		// For legacy compatibility, create a default anonymous user
		userID = entities.UserID("anonymous")
	}

	ucReq := &session.ListSessionsRequest{
		UserID: userID,
	}

	// Parse status filter
	if statusStr := ctx.QueryParam("status"); statusStr != "" {
		status := entities.SessionStatus(statusStr)
		ucReq.Status = &status
	}

	// Parse limit and offset
	if limitStr := ctx.QueryParam("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			ucReq.Limit = limit
		}
	}

	if offsetStr := ctx.QueryParam("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			ucReq.Offset = offset
		}
	}

	// Parse sort parameters
	if sortBy := ctx.QueryParam("sort_by"); sortBy != "" {
		ucReq.SortBy = session.SortField(sortBy)
	}

	if sortOrder := ctx.QueryParam("sort_order"); sortOrder != "" {
		ucReq.SortOrder = session.SortOrder(sortOrder)
	}

	// Execute use case
	response, err := c.listSessionsUC.Execute(reqCtx, ucReq)
	if err != nil {
		return c.sessionPresenter.PresentError(ctx.Response(), err.Error(), http.StatusInternalServerError)
	}

	// Present response
	return c.sessionPresenter.PresentSessionList(ctx.Response(), response)
}

// DeleteSession handles DELETE /sessions/{id}
func (c *SessionController) DeleteSession(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract user from context (set by auth middleware)
	var userID entities.UserID
	if user := auth.GetUserFromContext(ctx); user != nil {
		userID = user.ID()
	} else {
		// For legacy compatibility, create a default anonymous user
		userID = entities.UserID("anonymous")
	}

	// Extract session ID from URL path
	sessionID := ctx.Param("sessionId")
	if sessionID == "" {
		return c.sessionPresenter.PresentError(ctx.Response(), "session ID is required", http.StatusBadRequest)
	}

	// Parse query parameters
	force := ctx.QueryParam("force") == "true"

	// Execute use case
	ucReq := &session.DeleteSessionRequest{
		SessionID: entities.SessionID(sessionID),
		UserID:    userID,
		Force:     force,
	}

	response, err := c.deleteSessionUC.Execute(reqCtx, ucReq)
	if err != nil {
		return c.sessionPresenter.PresentError(ctx.Response(), err.Error(), http.StatusInternalServerError)
	}

	// Present response
	return c.sessionPresenter.PresentDeleteSession(ctx.Response(), response)
}

// MonitorSession handles GET /sessions/{id}/monitor
func (c *SessionController) MonitorSession(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract user from context (set by auth middleware)
	var userID entities.UserID
	if user := auth.GetUserFromContext(ctx); user != nil {
		userID = user.ID()
	} else {
		// For legacy compatibility, create a default anonymous user
		userID = entities.UserID("anonymous")
	}

	// Extract session ID from URL path
	sessionID := ctx.Param("sessionId")
	if sessionID == "" {
		return c.sessionPresenter.PresentError(ctx.Response(), "session ID is required", http.StatusBadRequest)
	}

	// Execute use case
	ucReq := &session.MonitorSessionRequest{
		SessionID: entities.SessionID(sessionID),
		UserID:    userID,
	}

	response, err := c.monitorSessionUC.Execute(reqCtx, ucReq)
	if err != nil {
		return c.sessionPresenter.PresentError(ctx.Response(), err.Error(), http.StatusInternalServerError)
	}

	// Present response
	return c.sessionPresenter.PresentMonitorSession(ctx.Response(), response)
}
