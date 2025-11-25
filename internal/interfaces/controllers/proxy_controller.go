package controllers

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/proxy"
)

// ProxyController handles HTTP requests related to agent sessions
type ProxyController struct {
	startSessionUC *proxy.StartAgentSessionUseCase
	stopSessionUC  *proxy.StopAgentSessionUseCase
	listSessionsUC *proxy.ListAgentSessionsUseCase
}

// NewProxyController creates a new ProxyController
func NewProxyController(
	startSessionUC *proxy.StartAgentSessionUseCase,
	stopSessionUC *proxy.StopAgentSessionUseCase,
	listSessionsUC *proxy.ListAgentSessionsUseCase,
) *ProxyController {
	return &ProxyController{
		startSessionUC: startSessionUC,
		stopSessionUC:  stopSessionUC,
		listSessionsUC: listSessionsUC,
	}
}

func (pc *ProxyController) RegisterRoutes(router *mux.Router) {
	// Session proxy routes - these handle routing requests to AgentAPI instances
	router.PathPrefix("/{sessionId}/{path:.*}").HandlerFunc(pc.RouteToSession).Methods("GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS")
}

// StartAgentRequest represents the HTTP request body for starting an agent
type StartAgentRequest struct {
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Message     string            `json:"message,omitempty"`
}

// StartAgentResponse represents the HTTP response for starting an agent
type StartAgentResponse struct {
	SessionID string `json:"session_id"`
	Port      int    `json:"port"`
	Status    string `json:"status"`
}

// StartAgent handles POST /start requests
func (pc *ProxyController) StartAgent(c echo.Context) error {
	ctx := context.Background()

	// Get user ID from authentication context
	userID := getUserIDFromContext(c)
	if userID == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error": "user not authenticated",
		})
	}

	// Parse request body
	var req StartAgentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "invalid request body",
		})
	}

	// Extract repository information from tags
	repository := extractRepositoryFromTags(req.Tags)

	// Execute use case
	startReq := proxy.StartAgentSessionRequest{
		UserID:      userID,
		Environment: req.Environment,
		Tags:        req.Tags,
		Repository:  repository,
	}

	resp, err := pc.startSessionUC.Execute(ctx, startReq)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	// Return response
	return c.JSON(http.StatusOK, StartAgentResponse{
		SessionID: string(resp.Session.ID()),
		Port:      resp.Port,
		Status:    string(resp.Session.Status()),
	})
}

// StopAgent handles DELETE /:sessionId requests
func (pc *ProxyController) StopAgent(c echo.Context) error {
	ctx := context.Background()

	// Get user ID from authentication context
	userID := getUserIDFromContext(c)
	if userID == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error": "user not authenticated",
		})
	}

	// Get session ID from URL parameter
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "session ID is required",
		})
	}

	// Execute use case
	stopReq := proxy.StopAgentSessionRequest{
		SessionID: sessionID,
		UserID:    userID,
	}

	if err := pc.stopSessionUC.Execute(ctx, stopReq); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"status": "stopped",
	})
}

// SessionResponse represents a session in HTTP responses
type SessionResponse struct {
	ID          string            `json:"id"`
	UserID      string            `json:"user_id"`
	Port        int               `json:"port"`
	Status      string            `json:"status"`
	StartedAt   string            `json:"started_at"`
	Environment map[string]string `json:"environment"`
	Tags        map[string]string `json:"tags"`
}

// ListSessionsResponse represents the HTTP response for listing sessions
type ListSessionsResponse struct {
	Sessions []SessionResponse `json:"sessions"`
}

// ListSessions handles GET /sessions requests
func (pc *ProxyController) ListSessions(c echo.Context) error {
	ctx := context.Background()

	// Get user ID from authentication context
	userID := getUserIDFromContext(c)
	if userID == "" {
		return c.JSON(http.StatusUnauthorized, map[string]string{
			"error": "user not authenticated",
		})
	}

	// Execute use case
	listReq := proxy.ListAgentSessionsRequest{
		UserID: userID,
	}

	resp, err := pc.listSessionsUC.Execute(ctx, listReq)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}

	// Convert domain entities to HTTP response
	sessions := make([]SessionResponse, len(resp.Sessions))
	for i, session := range resp.Sessions {
		sessions[i] = SessionResponse{
			ID:          string(session.ID()),
			UserID:      string(session.UserID()),
			Port:        int(session.Port()),
			Status:      string(session.Status()),
			StartedAt:   session.StartedAt().Format("2006-01-02T15:04:05Z07:00"),
			Environment: session.Environment(),
			Tags:        session.Tags(),
		}
	}

	return c.JSON(http.StatusOK, ListSessionsResponse{
		Sessions: sessions,
	})
}

// RouteToSession handles routing requests to specific sessions
func (pc *ProxyController) RouteToSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["sessionId"]
	path := vars["path"]

	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// In a real implementation, this would:
	// 1. Validate user has access to the session
	// 2. Look up the session's port/host
	// 3. Proxy the request to the AgentAPI instance
	// 4. Return the response

	// For now, return a placeholder response
	_ = path // acknowledge the variable

	switch r.Method {
	case "OPTIONS":
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"message":"Request routed successfully"}`)); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
	}
}

// extractRepositoryFromTags extracts repository information from session tags
func extractRepositoryFromTags(tags map[string]string) *entities.Repository {
	if tags == nil {
		return nil
	}

	repoURL, hasRepoURL := tags["repository"]
	if !hasRepoURL || repoURL == "" {
		return nil
	}

	// Create repository from URL
	repo, err := entities.NewRepository(entities.RepositoryURL(repoURL))
	if err != nil {
		// If URL parsing fails, return nil
		return nil
	}

	return repo
}

// getUserIDFromContext extracts user ID from echo context
func getUserIDFromContext(c echo.Context) string {
	// Try to get user ID from context (set by auth middleware)
	if userID := c.Get("user_id"); userID != nil {
		if id, ok := userID.(string); ok {
			return id
		}
	}

	// For development/testing, return a default user
	return "admin"
}
