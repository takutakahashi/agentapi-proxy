package webhook

import (
	"fmt"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// Handlers handles webhook management endpoints
type Handlers struct {
	manager        Manager
	sessionManager portrepos.SessionManager
	baseURL        string
}

// NewHandlers creates a new Handlers instance
func NewHandlers(manager Manager, sessionManager portrepos.SessionManager) *Handlers {
	return &Handlers{
		manager:        manager,
		sessionManager: sessionManager,
	}
}

// NewHandlersWithBaseURL creates a new Handlers instance with a custom base URL
func NewHandlersWithBaseURL(manager Manager, sessionManager portrepos.SessionManager, baseURL string) *Handlers {
	return &Handlers{
		manager:        manager,
		sessionManager: sessionManager,
		baseURL:        baseURL,
	}
}

// GetName returns the name of this handler for logging
func (h *Handlers) GetName() string {
	return "WebhookHandlers"
}

// RegisterRoutes registers webhook management routes
// Implements the app.CustomHandler interface
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
	// Management API
	g := e.Group("/webhooks")
	g.POST("", h.CreateWebhook)
	g.GET("", h.ListWebhooks)
	g.GET("/:id", h.GetWebhook)
	g.PUT("/:id", h.UpdateWebhook)
	g.DELETE("/:id", h.DeleteWebhook)
	g.POST("/:id/regenerate-secret", h.RegenerateSecret)

	// Receiver endpoints
	hooks := e.Group("/hooks")
	hooks.POST("/github", h.HandleGitHubWebhook)

	log.Printf("Registered webhook management routes")
	return nil
}

// CreateWebhookRequest represents the request body for creating a webhook
type CreateWebhookRequest struct {
	Name          string                 `json:"name"`
	Scope         entities.ResourceScope `json:"scope,omitempty"`
	TeamID        string                 `json:"team_id,omitempty"`
	Type          WebhookType            `json:"type"`
	GitHub        *GitHubConfig          `json:"github,omitempty"`
	Triggers      []Trigger              `json:"triggers"`
	SessionConfig *SessionConfig         `json:"session_config,omitempty"`
}

// UpdateWebhookRequest represents the request body for updating a webhook
type UpdateWebhookRequest struct {
	Name          *string        `json:"name,omitempty"`
	Status        *WebhookStatus `json:"status,omitempty"`
	GitHub        *GitHubConfig  `json:"github,omitempty"`
	Triggers      []Trigger      `json:"triggers,omitempty"`
	SessionConfig *SessionConfig `json:"session_config,omitempty"`
}

// WebhookResponse represents the response for a webhook
type WebhookResponse struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	UserID        string                 `json:"user_id"`
	Scope         entities.ResourceScope `json:"scope,omitempty"`
	TeamID        string                 `json:"team_id,omitempty"`
	Status        WebhookStatus          `json:"status"`
	Type          WebhookType            `json:"type"`
	Secret        string                 `json:"secret"` // Masked
	WebhookURL    string                 `json:"webhook_url"`
	GitHub        *GitHubConfig          `json:"github,omitempty"`
	Triggers      []Trigger              `json:"triggers"`
	SessionConfig *SessionConfig         `json:"session_config,omitempty"`
	CreatedAt     string                 `json:"created_at"`
	UpdatedAt     string                 `json:"updated_at"`
	LastDelivery  *DeliveryRecord        `json:"last_delivery,omitempty"`
	DeliveryCount int64                  `json:"delivery_count"`
}

// CreateWebhook handles POST /webhooks
func (h *Handlers) CreateWebhook(c echo.Context) error {
	h.setCORSHeaders(c)

	var req CreateWebhookRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate request
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Type == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "type is required")
	}
	if req.Type != WebhookTypeGitHub {
		return echo.NewHTTPError(http.StatusBadRequest, "currently only 'github' type is supported")
	}
	if len(req.Triggers) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "at least one trigger is required")
	}

	// Get user from context
	user := auth.GetUserFromContext(c)
	var userID string
	if user != nil {
		userID = string(user.ID())
	} else {
		userID = "anonymous"
	}

	// Validate team scope: user must be a member of the team
	if req.Scope == entities.ScopeTeam {
		if req.TeamID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if user == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required for team-scoped webhooks")
		}
		if !user.IsMemberOfTeam(req.TeamID) {
			log.Printf("User %s is not a member of team %s", userID, req.TeamID)
			return echo.NewHTTPError(http.StatusForbidden, "You are not a member of this team")
		}
	}

	// Generate trigger IDs if not provided
	for i := range req.Triggers {
		if req.Triggers[i].ID == "" {
			req.Triggers[i].ID = uuid.New().String()
		}
		// Set default enabled state
		if !req.Triggers[i].Enabled {
			req.Triggers[i].Enabled = true
		}
	}

	// Create webhook
	webhook := &WebhookConfig{
		ID:            uuid.New().String(),
		Name:          req.Name,
		UserID:        userID,
		Scope:         req.Scope,
		TeamID:        req.TeamID,
		Status:        WebhookStatusActive,
		Type:          req.Type,
		GitHub:        req.GitHub,
		Triggers:      req.Triggers,
		SessionConfig: req.SessionConfig,
	}

	if err := h.manager.Create(c.Request().Context(), webhook); err != nil {
		log.Printf("Failed to create webhook: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create webhook")
	}

	log.Printf("Created webhook %s (%s) for user %s", webhook.ID, webhook.Name, userID)

	return c.JSON(http.StatusCreated, h.toResponse(c, webhook))
}

// ListWebhooks handles GET /webhooks
func (h *Handlers) ListWebhooks(c echo.Context) error {
	h.setCORSHeaders(c)

	user := auth.GetUserFromContext(c)
	scopeFilter := c.QueryParam("scope")
	teamIDFilter := c.QueryParam("team_id")

	var userID string
	var userTeamIDs []string
	if user != nil && !user.IsAdmin() {
		userID = string(user.ID())
		// Extract user's team IDs for filtering team-scoped webhooks
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				userTeamIDs = append(userTeamIDs, teamSlug)
			}
		}
	}

	// Build filter
	filter := Filter{
		Scope:   entities.ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}

	// Filter by type if provided
	if typeParam := c.QueryParam("type"); typeParam != "" {
		filter.Type = WebhookType(typeParam)
	}

	// Filter by status if provided
	if status := c.QueryParam("status"); status != "" {
		filter.Status = WebhookStatus(status)
	}

	// For non-admin users, set UserID filter only if not filtering by team
	if user != nil && !user.IsAdmin() && scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	webhooks, err := h.manager.List(c.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list webhooks: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list webhooks")
	}

	// Check if auth is enabled
	cfg := auth.GetConfigFromContext(c)
	authEnabled := cfg != nil && cfg.Auth.Enabled

	// Filter by user authorization
	responses := make([]WebhookResponse, 0, len(webhooks))
	for _, w := range webhooks {
		// If auth is not enabled, return all webhooks
		if !authEnabled {
			responses = append(responses, h.toResponse(c, w))
			continue
		}

		// Scope isolation
		webhookScope := w.Scope
		if scopeFilter == string(entities.ScopeTeam) {
			if webhookScope != entities.ScopeTeam {
				continue
			}
		} else {
			if webhookScope == entities.ScopeTeam {
				continue
			}
		}

		// Admin can see all webhooks within the filtered scope
		if user != nil && user.IsAdmin() {
			responses = append(responses, h.toResponse(c, w))
			continue
		}

		// Check authorization based on scope
		if webhookScope == entities.ScopeTeam {
			if user != nil && user.IsMemberOfTeam(w.TeamID) {
				responses = append(responses, h.toResponse(c, w))
			}
		} else {
			if user != nil && w.UserID == string(user.ID()) {
				responses = append(responses, h.toResponse(c, w))
			}
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"webhooks": responses,
	})
}

// GetWebhook handles GET /webhooks/:id
func (h *Handlers) GetWebhook(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		log.Printf("Failed to get webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	// Check authorization
	if !h.userCanAccessWebhook(c, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this webhook")
	}

	return c.JSON(http.StatusOK, h.toResponse(c, webhook))
}

// UpdateWebhook handles PUT /webhooks/:id
func (h *Handlers) UpdateWebhook(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	// Check authorization
	if !h.userCanAccessWebhook(c, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to update this webhook")
	}

	var req UpdateWebhookRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Apply updates
	if req.Name != nil {
		webhook.Name = *req.Name
	}
	if req.Status != nil {
		webhook.Status = *req.Status
	}
	if req.GitHub != nil {
		webhook.GitHub = req.GitHub
	}
	if req.Triggers != nil {
		// Generate trigger IDs if not provided
		for i := range req.Triggers {
			if req.Triggers[i].ID == "" {
				req.Triggers[i].ID = uuid.New().String()
			}
		}
		webhook.Triggers = req.Triggers
	}
	if req.SessionConfig != nil {
		webhook.SessionConfig = req.SessionConfig
	}

	if err := h.manager.Update(c.Request().Context(), webhook); err != nil {
		log.Printf("Failed to update webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update webhook")
	}

	log.Printf("Updated webhook %s", id)

	return c.JSON(http.StatusOK, h.toResponse(c, webhook))
}

// DeleteWebhook handles DELETE /webhooks/:id
func (h *Handlers) DeleteWebhook(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	// Check authorization
	if !h.userCanAccessWebhook(c, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this webhook")
	}

	if err := h.manager.Delete(c.Request().Context(), id); err != nil {
		log.Printf("Failed to delete webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete webhook")
	}

	log.Printf("Deleted webhook %s", id)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Webhook deleted successfully",
		"id":      id,
	})
}

// RegenerateSecret handles POST /webhooks/:id/regenerate-secret
func (h *Handlers) RegenerateSecret(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	// Check authorization
	if !h.userCanAccessWebhook(c, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to regenerate secret for this webhook")
	}

	newSecret, err := h.manager.RegenerateSecret(c.Request().Context(), id)
	if err != nil {
		log.Printf("Failed to regenerate secret for webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to regenerate secret")
	}

	log.Printf("Regenerated secret for webhook %s", id)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"id":     id,
		"secret": newSecret,
	})
}

// toResponse converts a WebhookConfig to WebhookResponse
func (h *Handlers) toResponse(c echo.Context, w *WebhookConfig) WebhookResponse {
	webhookURL := h.getWebhookURL(c, w)

	return WebhookResponse{
		ID:            w.ID,
		Name:          w.Name,
		UserID:        w.UserID,
		Scope:         w.Scope,
		TeamID:        w.TeamID,
		Status:        w.Status,
		Type:          w.Type,
		Secret:        w.MaskSecret(),
		WebhookURL:    webhookURL,
		GitHub:        w.GitHub,
		Triggers:      w.Triggers,
		SessionConfig: w.SessionConfig,
		CreatedAt:     w.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:     w.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		LastDelivery:  w.LastDelivery,
		DeliveryCount: w.DeliveryCount,
	}
}

// getWebhookURL generates the webhook URL
func (h *Handlers) getWebhookURL(c echo.Context, w *WebhookConfig) string {
	baseURL := h.baseURL
	if baseURL == "" {
		// Try to construct from request
		scheme := "https"
		if c.Request().TLS == nil {
			// Check X-Forwarded-Proto header
			if proto := c.Request().Header.Get("X-Forwarded-Proto"); proto != "" {
				scheme = proto
			} else {
				scheme = "http"
			}
		}
		host := c.Request().Host
		if fwdHost := c.Request().Header.Get("X-Forwarded-Host"); fwdHost != "" {
			host = fwdHost
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, host)
	}

	switch w.Type {
	case WebhookTypeGitHub:
		return fmt.Sprintf("%s/hooks/github", baseURL)
	default:
		return fmt.Sprintf("%s/hooks/custom/%s", baseURL, w.ID)
	}
}

// userCanAccessWebhook checks if the current user can access the webhook
func (h *Handlers) userCanAccessWebhook(c echo.Context, webhook *WebhookConfig) bool {
	user := auth.GetUserFromContext(c)
	if user == nil {
		// If no auth is configured, allow access
		cfg := auth.GetConfigFromContext(c)
		if cfg == nil || !cfg.Auth.Enabled {
			return true
		}
		return false
	}
	return user.CanAccessResource(
		entities.UserID(webhook.UserID),
		string(webhook.Scope),
		webhook.TeamID,
	)
}

// setCORSHeaders sets CORS headers for all webhook endpoints
func (h *Handlers) setCORSHeaders(c echo.Context) {
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	c.Response().Header().Set("Access-Control-Max-Age", "86400")
}
