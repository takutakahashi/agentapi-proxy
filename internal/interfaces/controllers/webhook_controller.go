package controllers

import (
	"fmt"
	"log"
	"net/http"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/webhook"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// WebhookController handles webhook management HTTP requests
type WebhookController struct {
	repo    repositories.WebhookRepository
	baseURL string
}

// NewWebhookController creates a new webhook controller
func NewWebhookController(repo repositories.WebhookRepository) *WebhookController {
	return &WebhookController{
		repo: repo,
	}
}

// SetBaseURL sets the base URL for webhook URLs
func (c *WebhookController) SetBaseURL(baseURL string) {
	c.baseURL = baseURL
}

// GetName returns the name of this controller for logging
func (c *WebhookController) GetName() string {
	return "WebhookController"
}

// CreateWebhookRequest represents the request body for creating a webhook
type CreateWebhookRequest struct {
	Name            string                        `json:"name"`
	Scope           entities.ResourceScope        `json:"scope,omitempty"`
	TeamID          string                        `json:"team_id,omitempty"`
	Type            entities.WebhookType          `json:"type"`
	SignatureHeader string                        `json:"signature_header,omitempty"`
	SignatureType   entities.WebhookSignatureType `json:"signature_type,omitempty"`
	GitHub          *GitHubConfigRequest          `json:"github,omitempty"`
	Triggers        []TriggerRequest              `json:"triggers"`
	SessionConfig   *SessionConfigRequest         `json:"session_config,omitempty"`
	MaxSessions     int                           `json:"max_sessions,omitempty"`
}

// GitHubConfigRequest represents GitHub-specific configuration in requests
type GitHubConfigRequest struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

// TriggerRequest represents a trigger in requests
type TriggerRequest struct {
	ID            string                   `json:"id,omitempty"`
	Name          string                   `json:"name"`
	Priority      int                      `json:"priority"`
	Enabled       bool                     `json:"enabled"`
	Conditions    TriggerConditionsRequest `json:"conditions"`
	SessionConfig *SessionConfigRequest    `json:"session_config,omitempty"`
	StopOnMatch   bool                     `json:"stop_on_match"`
}

// TriggerConditionsRequest represents trigger conditions in requests
type TriggerConditionsRequest struct {
	GitHub     *GitHubConditionsRequest   `json:"github,omitempty"`
	JSONPath   []JSONPathConditionRequest `json:"jsonpath,omitempty"`
	GoTemplate string                     `json:"go_template,omitempty"`
}

// JSONPathConditionRequest represents a JSONPath condition in requests
type JSONPathConditionRequest struct {
	Path     string      `json:"path"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

// GitHubConditionsRequest represents GitHub-specific conditions in requests
type GitHubConditionsRequest struct {
	Events       []string `json:"events,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	BaseBranches []string `json:"base_branches,omitempty"`
	Draft        *bool    `json:"draft,omitempty"`
	Sender       []string `json:"sender,omitempty"`
}

// SessionConfigRequest represents session configuration in requests
type SessionConfigRequest struct {
	Environment            map[string]string     `json:"environment,omitempty"`
	Tags                   map[string]string     `json:"tags,omitempty"`
	InitialMessageTemplate string                `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                `json:"reuse_message_template,omitempty"`
	Params                 *SessionParamsRequest `json:"params,omitempty"`
	ReuseSession           bool                  `json:"reuse_session,omitempty"`
	MountPayload           bool                  `json:"mount_payload,omitempty"`
}

// SessionParamsRequest represents session params in requests
type SessionParamsRequest struct {
	GithubToken string `json:"github_token,omitempty"`
}

// UpdateWebhookRequest represents the request body for updating a webhook
type UpdateWebhookRequest struct {
	Name            *string                        `json:"name,omitempty"`
	Status          *entities.WebhookStatus        `json:"status,omitempty"`
	SignatureHeader *string                        `json:"signature_header,omitempty"`
	SignatureType   *entities.WebhookSignatureType `json:"signature_type,omitempty"`
	GitHub          *GitHubConfigRequest           `json:"github,omitempty"`
	Triggers        []TriggerRequest               `json:"triggers,omitempty"`
	SessionConfig   *SessionConfigRequest          `json:"session_config,omitempty"`
	MaxSessions     *int                           `json:"max_sessions,omitempty"`
}

// WebhookResponse represents the response for a webhook
type WebhookResponse struct {
	ID              string                        `json:"id"`
	Name            string                        `json:"name"`
	UserID          string                        `json:"user_id"`
	Scope           entities.ResourceScope        `json:"scope,omitempty"`
	TeamID          string                        `json:"team_id,omitempty"`
	Status          entities.WebhookStatus        `json:"status"`
	Type            entities.WebhookType          `json:"type"`
	Secret          string                        `json:"secret"`
	SignatureHeader string                        `json:"signature_header,omitempty"`
	SignatureType   entities.WebhookSignatureType `json:"signature_type,omitempty"`
	WebhookURL      string                        `json:"webhook_url"`
	GitHub          *GitHubConfigResponse         `json:"github,omitempty"`
	Triggers        []TriggerResponse             `json:"triggers"`
	SessionConfig   *SessionConfigResponse        `json:"session_config,omitempty"`
	MaxSessions     int                           `json:"max_sessions"`
	CreatedAt       string                        `json:"created_at"`
	UpdatedAt       string                        `json:"updated_at"`
	LastDelivery    *DeliveryRecordResponse       `json:"last_delivery,omitempty"`
	DeliveryCount   int64                         `json:"delivery_count"`
}

// GitHubConfigResponse represents GitHub-specific configuration in responses
type GitHubConfigResponse struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

// TriggerResponse represents a trigger in responses
type TriggerResponse struct {
	ID            string                    `json:"id"`
	Name          string                    `json:"name"`
	Priority      int                       `json:"priority"`
	Enabled       bool                      `json:"enabled"`
	Conditions    TriggerConditionsResponse `json:"conditions"`
	SessionConfig *SessionConfigResponse    `json:"session_config,omitempty"`
	StopOnMatch   bool                      `json:"stop_on_match"`
}

// TriggerConditionsResponse represents trigger conditions in responses
type TriggerConditionsResponse struct {
	GitHub     *GitHubConditionsResponse   `json:"github,omitempty"`
	JSONPath   []JSONPathConditionResponse `json:"jsonpath,omitempty"`
	GoTemplate string                      `json:"go_template,omitempty"`
}

// JSONPathConditionResponse represents a JSONPath condition in responses
type JSONPathConditionResponse struct {
	Path     string      `json:"path"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

// GitHubConditionsResponse represents GitHub-specific conditions in responses
type GitHubConditionsResponse struct {
	Events       []string `json:"events,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	BaseBranches []string `json:"base_branches,omitempty"`
	Draft        *bool    `json:"draft,omitempty"`
	Sender       []string `json:"sender,omitempty"`
}

// SessionConfigResponse represents session configuration in responses
type SessionConfigResponse struct {
	Environment            map[string]string      `json:"environment,omitempty"`
	Tags                   map[string]string      `json:"tags,omitempty"`
	InitialMessageTemplate string                 `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                 `json:"reuse_message_template,omitempty"`
	Params                 *SessionParamsResponse `json:"params,omitempty"`
	ReuseSession           bool                   `json:"reuse_session,omitempty"`
	MountPayload           bool                   `json:"mount_payload,omitempty"`
}

// SessionParamsResponse represents session params in responses
type SessionParamsResponse struct {
	GithubToken string `json:"github_token,omitempty"`
}

// DeliveryRecordResponse represents a delivery record in responses
type DeliveryRecordResponse struct {
	ID             string `json:"id"`
	ReceivedAt     string `json:"received_at"`
	Status         string `json:"status"`
	MatchedTrigger string `json:"matched_trigger,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	Error          string `json:"error,omitempty"`
}

// CreateWebhook handles POST /webhooks
func (c *WebhookController) CreateWebhook(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	var req CreateWebhookRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate request
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Type == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "type is required")
	}
	if req.Type != entities.WebhookTypeGitHub && req.Type != entities.WebhookTypeCustom {
		return echo.NewHTTPError(http.StatusBadRequest, "type must be 'github' or 'custom'")
	}
	if len(req.Triggers) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "at least one trigger is required")
	}

	// Validate templates before creating webhook
	if err := c.validateWebhookTemplates(req.Type, req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Template validation error: %v", err))
	}

	// Get user from context
	user := auth.GetUserFromContext(ctx)
	var userID string
	if user != nil {
		userID = string(user.ID())
	} else {
		userID = "anonymous"
	}

	// Validate team scope
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

	// Create webhook entity
	webhook := entities.NewWebhook(uuid.New().String(), req.Name, userID, req.Type)
	webhook.SetScope(req.Scope)
	webhook.SetTeamID(req.TeamID)
	if req.SignatureHeader != "" {
		webhook.SetSignatureHeader(req.SignatureHeader)
	}
	if req.SignatureType != "" {
		webhook.SetSignatureType(req.SignatureType)
	}
	if req.MaxSessions > 0 {
		webhook.SetMaxSessions(req.MaxSessions)
	}

	// Set GitHub config
	if req.GitHub != nil {
		github := entities.NewWebhookGitHubConfig()
		github.SetEnterpriseURL(req.GitHub.EnterpriseURL)
		github.SetAllowedEvents(req.GitHub.AllowedEvents)
		github.SetAllowedRepositories(req.GitHub.AllowedRepositories)
		webhook.SetGitHub(github)
	}

	// Set triggers
	triggers := make([]entities.WebhookTrigger, 0, len(req.Triggers))
	for _, t := range req.Triggers {
		triggerID := t.ID
		if triggerID == "" {
			triggerID = uuid.New().String()
		}
		trigger := entities.NewWebhookTrigger(triggerID, t.Name)
		trigger.SetPriority(t.Priority)
		trigger.SetEnabled(t.Enabled)
		trigger.SetStopOnMatch(t.StopOnMatch)

		// Set conditions
		var conditions entities.WebhookTriggerConditions
		if t.Conditions.GitHub != nil {
			ghCond := entities.NewWebhookGitHubConditions()
			ghCond.SetEvents(t.Conditions.GitHub.Events)
			ghCond.SetActions(t.Conditions.GitHub.Actions)
			ghCond.SetBranches(t.Conditions.GitHub.Branches)
			ghCond.SetRepositories(t.Conditions.GitHub.Repositories)
			ghCond.SetLabels(t.Conditions.GitHub.Labels)
			ghCond.SetPaths(t.Conditions.GitHub.Paths)
			ghCond.SetBaseBranches(t.Conditions.GitHub.BaseBranches)
			ghCond.SetDraft(t.Conditions.GitHub.Draft)
			ghCond.SetSender(t.Conditions.GitHub.Sender)
			conditions.SetGitHub(ghCond)
		}
		if t.Conditions.JSONPath != nil {
			jsonPathConditions := make([]entities.WebhookJSONPathCondition, 0, len(t.Conditions.JSONPath))
			for _, jp := range t.Conditions.JSONPath {
				jsonPathConditions = append(jsonPathConditions, entities.NewWebhookJSONPathCondition(
					jp.Path,
					jp.Operator,
					jp.Value,
				))
			}
			conditions.SetJSONPath(jsonPathConditions)
		}
		if t.Conditions.GoTemplate != "" {
			conditions.SetGoTemplate(t.Conditions.GoTemplate)
		}
		trigger.SetConditions(conditions)

		// Set session config
		if t.SessionConfig != nil {
			sessionConfig := c.requestToSessionConfig(t.SessionConfig)
			trigger.SetSessionConfig(sessionConfig)
		}

		triggers = append(triggers, trigger)
	}
	webhook.SetTriggers(triggers)

	// Set session config
	if req.SessionConfig != nil {
		webhook.SetSessionConfig(c.requestToSessionConfig(req.SessionConfig))
	}

	if err := c.repo.Create(ctx.Request().Context(), webhook); err != nil {
		log.Printf("Failed to create webhook: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create webhook")
	}

	log.Printf("Created webhook %s (%s) for user %s", webhook.ID(), webhook.Name(), userID)

	return ctx.JSON(http.StatusCreated, c.toResponse(ctx, webhook))
}

// ListWebhooks handles GET /webhooks
func (c *WebhookController) ListWebhooks(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	user := auth.GetUserFromContext(ctx)
	scopeFilter := ctx.QueryParam("scope")
	teamIDFilter := ctx.QueryParam("team_id")

	var userID string
	var userTeamIDs []string
	if user != nil && !user.IsAdmin() {
		userID = string(user.ID())
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				userTeamIDs = append(userTeamIDs, teamSlug)
			}
		}
	}

	// Build filter
	filter := repositories.WebhookFilter{
		Scope:   entities.ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}

	if typeParam := ctx.QueryParam("type"); typeParam != "" {
		filter.Type = entities.WebhookType(typeParam)
	}
	if status := ctx.QueryParam("status"); status != "" {
		filter.Status = entities.WebhookStatus(status)
	}
	if user != nil && !user.IsAdmin() && scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	webhooks, err := c.repo.List(ctx.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list webhooks: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list webhooks")
	}

	cfg := auth.GetConfigFromContext(ctx)
	authEnabled := cfg != nil && cfg.Auth.Enabled

	responses := make([]WebhookResponse, 0, len(webhooks))
	for _, w := range webhooks {
		if !authEnabled {
			responses = append(responses, c.toResponse(ctx, w))
			continue
		}

		webhookScope := w.Scope()
		if scopeFilter == string(entities.ScopeTeam) {
			if webhookScope != entities.ScopeTeam {
				continue
			}
		} else {
			if webhookScope == entities.ScopeTeam {
				continue
			}
		}

		if user != nil && user.IsAdmin() {
			responses = append(responses, c.toResponse(ctx, w))
			continue
		}

		if webhookScope == entities.ScopeTeam {
			if user != nil && user.IsMemberOfTeam(w.TeamID()) {
				responses = append(responses, c.toResponse(ctx, w))
			}
		} else {
			if user != nil && w.UserID() == string(user.ID()) {
				responses = append(responses, c.toResponse(ctx, w))
			}
		}
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"webhooks": responses,
	})
}

// GetWebhook handles GET /webhooks/:id
func (c *WebhookController) GetWebhook(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		log.Printf("Failed to get webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	if !c.userCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this webhook")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(ctx, webhook))
}

// UpdateWebhook handles PUT /webhooks/:id
func (c *WebhookController) UpdateWebhook(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	if !c.userCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to update this webhook")
	}

	var req UpdateWebhookRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate templates before updating webhook
	if err := c.validateWebhookTemplatesForUpdate(webhook.WebhookType(), req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Template validation error: %v", err))
	}

	// Apply updates
	if req.Name != nil {
		webhook.SetName(*req.Name)
	}
	if req.Status != nil {
		webhook.SetStatus(*req.Status)
	}
	if req.SignatureHeader != nil {
		webhook.SetSignatureHeader(*req.SignatureHeader)
	}
	if req.SignatureType != nil {
		webhook.SetSignatureType(*req.SignatureType)
	}
	if req.MaxSessions != nil && *req.MaxSessions > 0 {
		webhook.SetMaxSessions(*req.MaxSessions)
	}
	if req.GitHub != nil {
		github := entities.NewWebhookGitHubConfig()
		github.SetEnterpriseURL(req.GitHub.EnterpriseURL)
		github.SetAllowedEvents(req.GitHub.AllowedEvents)
		github.SetAllowedRepositories(req.GitHub.AllowedRepositories)
		webhook.SetGitHub(github)
	}
	if req.Triggers != nil {
		triggers := make([]entities.WebhookTrigger, 0, len(req.Triggers))
		for _, t := range req.Triggers {
			triggerID := t.ID
			if triggerID == "" {
				triggerID = uuid.New().String()
			}
			trigger := entities.NewWebhookTrigger(triggerID, t.Name)
			trigger.SetPriority(t.Priority)
			trigger.SetEnabled(t.Enabled)
			trigger.SetStopOnMatch(t.StopOnMatch)

			var conditions entities.WebhookTriggerConditions
			if t.Conditions.GitHub != nil {
				ghCond := entities.NewWebhookGitHubConditions()
				ghCond.SetEvents(t.Conditions.GitHub.Events)
				ghCond.SetActions(t.Conditions.GitHub.Actions)
				ghCond.SetBranches(t.Conditions.GitHub.Branches)
				ghCond.SetRepositories(t.Conditions.GitHub.Repositories)
				ghCond.SetLabels(t.Conditions.GitHub.Labels)
				ghCond.SetPaths(t.Conditions.GitHub.Paths)
				ghCond.SetBaseBranches(t.Conditions.GitHub.BaseBranches)
				ghCond.SetDraft(t.Conditions.GitHub.Draft)
				ghCond.SetSender(t.Conditions.GitHub.Sender)
				conditions.SetGitHub(ghCond)
			}
			if t.Conditions.JSONPath != nil {
				jsonPathConditions := make([]entities.WebhookJSONPathCondition, 0, len(t.Conditions.JSONPath))
				for _, jp := range t.Conditions.JSONPath {
					jsonPathConditions = append(jsonPathConditions, entities.NewWebhookJSONPathCondition(
						jp.Path,
						jp.Operator,
						jp.Value,
					))
				}
				conditions.SetJSONPath(jsonPathConditions)
			}
			if t.Conditions.GoTemplate != "" {
				conditions.SetGoTemplate(t.Conditions.GoTemplate)
			}
			trigger.SetConditions(conditions)

			if t.SessionConfig != nil {
				trigger.SetSessionConfig(c.requestToSessionConfig(t.SessionConfig))
			}

			triggers = append(triggers, trigger)
		}
		webhook.SetTriggers(triggers)
	}
	if req.SessionConfig != nil {
		webhook.SetSessionConfig(c.requestToSessionConfig(req.SessionConfig))
	}

	if err := c.repo.Update(ctx.Request().Context(), webhook); err != nil {
		log.Printf("Failed to update webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update webhook")
	}

	log.Printf("Updated webhook %s", id)

	return ctx.JSON(http.StatusOK, c.toResponse(ctx, webhook))
}

// DeleteWebhook handles DELETE /webhooks/:id
func (c *WebhookController) DeleteWebhook(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	if !c.userCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this webhook")
	}

	if err := c.repo.Delete(ctx.Request().Context(), id); err != nil {
		log.Printf("Failed to delete webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete webhook")
	}

	log.Printf("Deleted webhook %s", id)

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message": "Webhook deleted successfully",
		"id":      id,
	})
}

// RegenerateSecret handles POST /webhooks/:id/regenerate-secret
func (c *WebhookController) RegenerateSecret(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	webhook, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	if !c.userCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to regenerate secret for this webhook")
	}

	// Generate new secret
	newSecret := generateSecret()
	webhook.SetSecret(newSecret)

	if err := c.repo.Update(ctx.Request().Context(), webhook); err != nil {
		log.Printf("Failed to regenerate secret for webhook %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to regenerate secret")
	}

	log.Printf("Regenerated secret for webhook %s", id)

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"id":     id,
		"secret": newSecret,
	})
}

// requestToSessionConfig converts request to entity
func (c *WebhookController) requestToSessionConfig(req *SessionConfigRequest) *entities.WebhookSessionConfig {
	config := entities.NewWebhookSessionConfig()
	config.SetEnvironment(req.Environment)
	config.SetTags(req.Tags)
	config.SetInitialMessageTemplate(req.InitialMessageTemplate)
	config.SetReuseMessageTemplate(req.ReuseMessageTemplate)
	config.SetReuseSession(req.ReuseSession)
	config.SetMountPayload(req.MountPayload)
	if req.Params != nil {
		params := entities.NewWebhookSessionParams()
		params.SetGithubToken(req.Params.GithubToken)
		config.SetParams(params)
	}
	return config
}

// toResponse converts entity to response
func (c *WebhookController) toResponse(ctx echo.Context, w *entities.Webhook) WebhookResponse {
	resp := WebhookResponse{
		ID:              w.ID(),
		Name:            w.Name(),
		UserID:          w.UserID(),
		Scope:           w.Scope(),
		TeamID:          w.TeamID(),
		Status:          w.Status(),
		Type:            w.WebhookType(),
		Secret:          w.Secret(),
		SignatureHeader: w.SignatureHeader(),
		SignatureType:   w.SignatureType(),
		WebhookURL:      c.getWebhookURL(ctx, w),
		MaxSessions:     w.MaxSessions(),
		CreatedAt:       w.CreatedAt().Format(time.RFC3339),
		UpdatedAt:       w.UpdatedAt().Format(time.RFC3339),
		DeliveryCount:   w.DeliveryCount(),
	}

	// GitHub config
	if gh := w.GitHub(); gh != nil {
		resp.GitHub = &GitHubConfigResponse{
			EnterpriseURL:       gh.EnterpriseURL(),
			AllowedEvents:       gh.AllowedEvents(),
			AllowedRepositories: gh.AllowedRepositories(),
		}
	}

	// Triggers
	triggers := w.Triggers()
	resp.Triggers = make([]TriggerResponse, 0, len(triggers))
	for _, t := range triggers {
		tr := TriggerResponse{
			ID:          t.ID(),
			Name:        t.Name(),
			Priority:    t.Priority(),
			Enabled:     t.Enabled(),
			StopOnMatch: t.StopOnMatch(),
		}

		// Conditions
		cond := t.Conditions()
		if ghCond := cond.GitHub(); ghCond != nil {
			tr.Conditions.GitHub = &GitHubConditionsResponse{
				Events:       ghCond.Events(),
				Actions:      ghCond.Actions(),
				Branches:     ghCond.Branches(),
				Repositories: ghCond.Repositories(),
				Labels:       ghCond.Labels(),
				Paths:        ghCond.Paths(),
				BaseBranches: ghCond.BaseBranches(),
				Draft:        ghCond.Draft(),
				Sender:       ghCond.Sender(),
			}
		}
		if jsonPathConds := cond.JSONPath(); len(jsonPathConds) > 0 {
			tr.Conditions.JSONPath = make([]JSONPathConditionResponse, 0, len(jsonPathConds))
			for _, jp := range jsonPathConds {
				tr.Conditions.JSONPath = append(tr.Conditions.JSONPath, JSONPathConditionResponse{
					Path:     jp.Path(),
					Operator: string(jp.Operator()),
					Value:    jp.Value(),
				})
			}
		}
		if goTemplate := cond.GoTemplate(); goTemplate != "" {
			tr.Conditions.GoTemplate = goTemplate
		}

		// Session config
		if sc := t.SessionConfig(); sc != nil {
			tr.SessionConfig = c.sessionConfigToResponse(sc)
		}

		resp.Triggers = append(resp.Triggers, tr)
	}

	// Session config
	if sc := w.SessionConfig(); sc != nil {
		resp.SessionConfig = c.sessionConfigToResponse(sc)
	}

	// Last delivery
	if ld := w.LastDelivery(); ld != nil {
		resp.LastDelivery = &DeliveryRecordResponse{
			ID:             ld.ID(),
			ReceivedAt:     ld.ReceivedAt().Format(time.RFC3339),
			Status:         string(ld.Status()),
			MatchedTrigger: ld.MatchedTrigger(),
			SessionID:      ld.SessionID(),
			Error:          ld.Error(),
		}
	}

	return resp
}

func (c *WebhookController) sessionConfigToResponse(sc *entities.WebhookSessionConfig) *SessionConfigResponse {
	resp := &SessionConfigResponse{
		Environment:            sc.Environment(),
		Tags:                   sc.Tags(),
		InitialMessageTemplate: sc.InitialMessageTemplate(),
		ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
		ReuseSession:           sc.ReuseSession(),
		MountPayload:           sc.MountPayload(),
	}
	// GitHubトークンは機密情報なのでレスポンスに含めない
	// params フィールドは意図的に省略
	return resp
}

func (c *WebhookController) getWebhookURL(ctx echo.Context, w *entities.Webhook) string {
	baseURL := c.baseURL
	if baseURL == "" {
		scheme := "https"
		if ctx.Request().TLS == nil {
			if proto := ctx.Request().Header.Get("X-Forwarded-Proto"); proto != "" {
				scheme = proto
			} else {
				scheme = "http"
			}
		}
		host := ctx.Request().Host
		if fwdHost := ctx.Request().Header.Get("X-Forwarded-Host"); fwdHost != "" {
			host = fwdHost
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, host)
	}

	switch w.WebhookType() {
	case entities.WebhookTypeGitHub:
		return fmt.Sprintf("%s/hooks/github/%s", baseURL, w.ID())
	default:
		return fmt.Sprintf("%s/hooks/custom/%s", baseURL, w.ID())
	}
}

func (c *WebhookController) userCanAccessWebhook(ctx echo.Context, webhook *entities.Webhook) bool {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		cfg := auth.GetConfigFromContext(ctx)
		if cfg == nil || !cfg.Auth.Enabled {
			return true
		}
		return false
	}
	return user.CanAccessResource(
		entities.UserID(webhook.UserID()),
		string(webhook.Scope()),
		webhook.TeamID(),
	)
}

func (c *WebhookController) setCORSHeaders(ctx echo.Context) {
	ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")
	ctx.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	ctx.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	ctx.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	ctx.Response().Header().Set("Access-Control-Max-Age", "86400")
}

// generateSecret generates a random secret for webhook signature verification
func generateSecret() string {
	return uuid.New().String() + uuid.New().String()
}

// validateWebhookTemplates validates all templates in the webhook request
func (c *WebhookController) validateWebhookTemplates(webhookType entities.WebhookType, req CreateWebhookRequest) error {
	// Validate webhook-level session config template
	if req.SessionConfig != nil && req.SessionConfig.InitialMessageTemplate != "" {
		if err := c.validateInitialMessageTemplate(webhookType, req.SessionConfig.InitialMessageTemplate); err != nil {
			return fmt.Errorf("webhook session_config.initial_message_template: %w", err)
		}
	}

	// Validate trigger templates
	for i, trigger := range req.Triggers {
		// Validate GoTemplate condition
		if trigger.Conditions.GoTemplate != "" {
			if err := c.validateGoTemplateCondition(webhookType, trigger.Conditions.GoTemplate); err != nil {
				return fmt.Errorf("trigger[%d] (%s) conditions.go_template: %w", i, trigger.Name, err)
			}
		}

		// Validate trigger-level session config template
		if trigger.SessionConfig != nil && trigger.SessionConfig.InitialMessageTemplate != "" {
			if err := c.validateInitialMessageTemplate(webhookType, trigger.SessionConfig.InitialMessageTemplate); err != nil {
				return fmt.Errorf("trigger[%d] (%s) session_config.initial_message_template: %w", i, trigger.Name, err)
			}
		}
	}

	return nil
}

// validateWebhookTemplatesForUpdate validates all templates in the webhook update request
func (c *WebhookController) validateWebhookTemplatesForUpdate(webhookType entities.WebhookType, req UpdateWebhookRequest) error {
	// Validate webhook-level session config template
	if req.SessionConfig != nil && req.SessionConfig.InitialMessageTemplate != "" {
		if err := c.validateInitialMessageTemplate(webhookType, req.SessionConfig.InitialMessageTemplate); err != nil {
			return fmt.Errorf("webhook session_config.initial_message_template: %w", err)
		}
	}

	// Validate trigger templates
	if req.Triggers != nil {
		for i, trigger := range req.Triggers {
			// Validate GoTemplate condition
			if trigger.Conditions.GoTemplate != "" {
				if err := c.validateGoTemplateCondition(webhookType, trigger.Conditions.GoTemplate); err != nil {
					return fmt.Errorf("trigger[%d] (%s) conditions.go_template: %w", i, trigger.Name, err)
				}
			}

			// Validate trigger-level session config template
			if trigger.SessionConfig != nil && trigger.SessionConfig.InitialMessageTemplate != "" {
				if err := c.validateInitialMessageTemplate(webhookType, trigger.SessionConfig.InitialMessageTemplate); err != nil {
					return fmt.Errorf("trigger[%d] (%s) session_config.initial_message_template: %w", i, trigger.Name, err)
				}
			}
		}
	}

	return nil
}

// validateGoTemplateCondition validates a GoTemplate condition string
func (c *WebhookController) validateGoTemplateCondition(webhookType entities.WebhookType, tmplStr string) error {
	// Only validate template syntax, not execution with test payload
	// This allows flexible payload structures without predefined schema
	evaluator := webhook.NewGoTemplateEvaluator()
	_, err := template.New("condition").Funcs(evaluator.FuncMap()).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template parse failed: %w", err)
	}

	return nil
}

// validateInitialMessageTemplate validates an initial message template
func (c *WebhookController) validateInitialMessageTemplate(webhookType entities.WebhookType, tmplStr string) error {
	// Only validate template syntax, not execution with test payload
	// This allows flexible payload structures without predefined schema
	_, err := template.New("initial_message").Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template parse failed: %w", err)
	}

	return nil
}
