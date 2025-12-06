package controllers

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/presenters"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
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

// RegisterRoutes registers session routes with optional middleware
func (c *SessionController) RegisterRoutes(e *echo.Echo, middleware ...echo.MiddlewareFunc) {
	// Create route group for sessions
	sessionGroup := e.Group("/sessions", middleware...)

	// Register session routes
	e.POST("/start", c.StartSession, middleware...)
	e.GET("/search", c.SearchSessions, middleware...)
	sessionGroup.DELETE("/:sessionId", c.DeleteSession)
	sessionGroup.GET("/:sessionId", c.GetSession)
	sessionGroup.GET("", c.ListSessions)
	sessionGroup.GET("/:sessionId/monitor", c.MonitorSession)
}

// RegisterAPIRoutes registers session routes under /api/v1 prefix with optional middleware
func (c *SessionController) RegisterAPIRoutes(e *echo.Echo, middleware ...echo.MiddlewareFunc) {
	// Create API v1 group
	apiV1 := e.Group("/api/v1", middleware...)

	// Register session routes under /api/v1
	apiV1.POST("/sessions", c.StartSession)
	apiV1.GET("/sessions/search", c.SearchSessions)
	apiV1.DELETE("/sessions/:sessionId", c.DeleteSession)
	apiV1.GET("/sessions/:sessionId", c.GetSession)
	apiV1.GET("/sessions", c.ListSessions)
	apiV1.GET("/sessions/:sessionId/monitor", c.MonitorSession)
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

	// Extract user ID from context (set by auth middleware)
	userID, ok := reqCtx.Value("userID").(entities.UserID)
	if !ok {
		return c.sessionPresenter.PresentError(ctx.Response(), "unauthorized", http.StatusUnauthorized)
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

	// Extract user ID from context
	userID, ok := reqCtx.Value("userID").(entities.UserID)
	if !ok {
		return c.sessionPresenter.PresentError(ctx.Response(), "unauthorized", http.StatusUnauthorized)
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

	// Extract user ID from context
	userID, ok := reqCtx.Value("userID").(entities.UserID)
	if !ok {
		return c.sessionPresenter.PresentError(ctx.Response(), "unauthorized", http.StatusUnauthorized)
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

	// Extract user ID from context
	userID, ok := reqCtx.Value("userID").(entities.UserID)
	if !ok {
		return c.sessionPresenter.PresentError(ctx.Response(), "unauthorized", http.StatusUnauthorized)
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

	// Extract user ID from context
	userID, ok := reqCtx.Value("userID").(entities.UserID)
	if !ok {
		return c.sessionPresenter.PresentError(ctx.Response(), "unauthorized", http.StatusUnauthorized)
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
