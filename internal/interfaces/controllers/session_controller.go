package controllers

import (
	"encoding/json"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/presenters"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"net/http"
	"strconv"
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

// CreateSession handles POST /sessions
func (c *SessionController) CreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context (set by auth middleware)
	userID, ok := ctx.Value("userID").(entities.UserID)
	if !ok {
		c.sessionPresenter.PresentError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.sessionPresenter.PresentError(w, "invalid request body", http.StatusBadRequest)
		return
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
			c.sessionPresenter.PresentError(w, "invalid repository: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Repository.Branch != "" {
			if err := repo.SetBranch(req.Repository.Branch); err != nil {
				c.sessionPresenter.PresentError(w, "invalid branch: "+err.Error(), http.StatusBadRequest)
				return
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
	response, err := c.createSessionUC.Execute(ctx, ucReq)
	if err != nil {
		c.sessionPresenter.PresentError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Present response
	c.sessionPresenter.PresentCreateSession(w, response)
}

// GetSession handles GET /sessions/{id}
func (c *SessionController) GetSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context
	userID, ok := ctx.Value("userID").(entities.UserID)
	if !ok {
		c.sessionPresenter.PresentError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract session ID from URL path
	sessionID := extractSessionID(r)
	if sessionID == "" {
		c.sessionPresenter.PresentError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Execute use case
	ucReq := &session.GetSessionByIDRequest{
		SessionID: entities.SessionID(sessionID),
		UserID:    userID,
	}

	response, err := c.getSessionByIDUC.Execute(ctx, ucReq)
	if err != nil {
		c.sessionPresenter.PresentError(w, err.Error(), http.StatusNotFound)
		return
	}

	// Present response
	c.sessionPresenter.PresentSession(w, response.Session)
}

// ListSessions handles GET /sessions
func (c *SessionController) ListSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context
	userID, ok := ctx.Value("userID").(entities.UserID)
	if !ok {
		c.sessionPresenter.PresentError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse query parameters
	query := r.URL.Query()

	ucReq := &session.ListSessionsRequest{
		UserID: userID,
	}

	// Parse status filter
	if statusStr := query.Get("status"); statusStr != "" {
		status := entities.SessionStatus(statusStr)
		ucReq.Status = &status
	}

	// Parse limit and offset
	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil {
			ucReq.Limit = limit
		}
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil {
			ucReq.Offset = offset
		}
	}

	// Parse sort parameters
	if sortBy := query.Get("sort_by"); sortBy != "" {
		ucReq.SortBy = session.SortField(sortBy)
	}

	if sortOrder := query.Get("sort_order"); sortOrder != "" {
		ucReq.SortOrder = session.SortOrder(sortOrder)
	}

	// Execute use case
	response, err := c.listSessionsUC.Execute(ctx, ucReq)
	if err != nil {
		c.sessionPresenter.PresentError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Present response
	c.sessionPresenter.PresentSessionList(w, response)
}

// DeleteSession handles DELETE /sessions/{id}
func (c *SessionController) DeleteSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context
	userID, ok := ctx.Value("userID").(entities.UserID)
	if !ok {
		c.sessionPresenter.PresentError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract session ID from URL path
	sessionID := extractSessionID(r)
	if sessionID == "" {
		c.sessionPresenter.PresentError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	force := query.Get("force") == "true"

	// Execute use case
	ucReq := &session.DeleteSessionRequest{
		SessionID: entities.SessionID(sessionID),
		UserID:    userID,
		Force:     force,
	}

	response, err := c.deleteSessionUC.Execute(ctx, ucReq)
	if err != nil {
		c.sessionPresenter.PresentError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Present response
	c.sessionPresenter.PresentDeleteSession(w, response)
}

// MonitorSession handles GET /sessions/{id}/monitor
func (c *SessionController) MonitorSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context
	userID, ok := ctx.Value("userID").(entities.UserID)
	if !ok {
		c.sessionPresenter.PresentError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract session ID from URL path
	sessionID := extractSessionID(r)
	if sessionID == "" {
		c.sessionPresenter.PresentError(w, "session ID is required", http.StatusBadRequest)
		return
	}

	// Execute use case
	ucReq := &session.MonitorSessionRequest{
		SessionID: entities.SessionID(sessionID),
		UserID:    userID,
	}

	response, err := c.monitorSessionUC.Execute(ctx, ucReq)
	if err != nil {
		c.sessionPresenter.PresentError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Present response
	c.sessionPresenter.PresentMonitorSession(w, response)
}

// extractSessionID extracts session ID from URL path
// This is a simplified implementation - in real applications, use a proper router
func extractSessionID(r *http.Request) string {
	// Assuming URL format: /sessions/{id} or /sessions/{id}/monitor
	path := r.URL.Path
	// This is a placeholder implementation
	// In real applications, use a proper router like gorilla/mux or chi
	_ = path             // Acknowledge the variable is intentionally unused in this placeholder
	return "session_123" // Placeholder
}
