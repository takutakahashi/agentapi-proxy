package webhook

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Handlers is an adapter that combines webhook controllers and implements app.CustomHandler interface
type Handlers struct {
	controller       *controllers.WebhookController
	githubController *controllers.WebhookGitHubController
	customController *controllers.WebhookCustomController
}

// NewHandlers creates a new Handlers instance
func NewHandlers(repo repositories.WebhookRepository, sessionManager repositories.SessionManager, baseURL string) *Handlers {
	controller := controllers.NewWebhookController(repo)
	if baseURL != "" {
		controller.SetBaseURL(baseURL)
	}
	return &Handlers{
		controller:       controller,
		githubController: controllers.NewWebhookGitHubController(repo, sessionManager),
		customController: controllers.NewWebhookCustomController(repo, sessionManager),
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
	g.POST("", h.controller.CreateWebhook)
	g.GET("", h.controller.ListWebhooks)
	g.GET("/:id", h.controller.GetWebhook)
	g.PUT("/:id", h.controller.UpdateWebhook)
	g.DELETE("/:id", h.controller.DeleteWebhook)
	g.POST("/:id/regenerate-secret", h.controller.RegenerateSecret)
	g.POST("/:id/trigger", h.TriggerWebhook)

	// Receiver endpoints
	hooks := e.Group("/hooks")
	hooks.POST("/github/:id", h.githubController.HandleGitHubWebhook)
	hooks.POST("/custom/:id", h.customController.HandleCustomWebhook)

	log.Printf("Registered webhook management and receiver routes")
	return nil
}

// TriggerWebhook handles POST /webhooks/:id/trigger
func (h *Handlers) TriggerWebhook(ctx echo.Context) error {
	h.setCORSHeaders(ctx)

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Webhook ID is required")
	}

	// Fetch webhook
	webhook, err := h.controller.Repo().Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrWebhookNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Webhook not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get webhook")
	}

	// Authorization check
	if !h.controller.UserCanAccessWebhook(ctx, webhook) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to trigger this webhook")
	}

	// Parse request body
	var req controllers.TriggerWebhookRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if req.Payload == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "payload is required")
	}

	// Delegate to type-specific logic
	switch webhook.WebhookType() {
	case entities.WebhookTypeGitHub:
		return h.triggerGitHubWebhook(ctx, webhook, &req)
	case entities.WebhookTypeCustom:
		return h.triggerCustomWebhook(ctx, webhook, &req)
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "Unsupported webhook type")
	}
}

func (h *Handlers) triggerGitHubWebhook(ctx echo.Context, webhook *entities.Webhook, req *controllers.TriggerWebhookRequest) error {
	if req.Event == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "event is required for GitHub webhooks")
	}

	// Build GitHubPayload from the test payload
	payloadBytes, _ := json.Marshal(req.Payload)
	var payload controllers.GitHubPayload
	_ = json.Unmarshal(payloadBytes, &payload)
	payload.Raw = req.Payload

	// Match triggers (no signature verification)
	matchResult := h.githubController.MatchTriggersForTest(webhook.Triggers(), req.Event, &payload)

	response := controllers.TriggerWebhookResponse{
		DryRun:  req.DryRun,
		Matched: matchResult != nil,
	}

	if matchResult == nil {
		return ctx.JSON(http.StatusOK, response)
	}

	response.MatchedTrigger = &controllers.TriggerMatchedTriggerInfo{
		ID:   matchResult.ID(),
		Name: matchResult.Name(),
	}

	// Build tags
	tags := h.githubController.BuildGitHubTagsForTest(webhook, matchResult, req.Event, &payload)
	defaultMessage := h.githubController.BuildDefaultInitialMessageForTest(req.Event, &payload)

	if req.DryRun {
		// Compute config without creating session
		dryResult, err := h.githubController.SessionService().DryRunSessionConfig(controllers.SessionCreationParams{
			Webhook:        webhook,
			Trigger:        matchResult,
			Payload:        req.Payload,
			Tags:           tags,
			DefaultMessage: defaultMessage,
		})
		if err != nil {
			response.Error = err.Error()
		} else if dryResult.Error != "" {
			response.Error = dryResult.Error
		} else {
			response.InitialMessage = dryResult.InitialMessage
			response.Tags = dryResult.Tags
			response.Environment = dryResult.Environment
		}
		return ctx.JSON(http.StatusOK, response)
	}

	// Actually create session (dry_run=false)
	sessionID, sessionReused, err := h.githubController.SessionService().CreateSessionFromWebhook(ctx, controllers.SessionCreationParams{
		Webhook:        webhook,
		Trigger:        matchResult,
		Payload:        req.Payload,
		RawPayload:     nil,
		Tags:           tags,
		DefaultMessage: defaultMessage,
	})
	if err != nil {
		response.Error = fmt.Sprintf("Failed to create session: %v", err)
		return ctx.JSON(http.StatusOK, response)
	}

	response.SessionID = sessionID
	response.SessionReused = sessionReused
	return ctx.JSON(http.StatusOK, response)
}

func (h *Handlers) triggerCustomWebhook(ctx echo.Context, webhook *entities.Webhook, req *controllers.TriggerWebhookRequest) error {
	// Match triggers
	matchResult := h.customController.MatchTriggersForTest(webhook.Triggers(), req.Payload)

	response := controllers.TriggerWebhookResponse{
		DryRun:  req.DryRun,
		Matched: matchResult != nil,
	}

	if matchResult == nil {
		return ctx.JSON(http.StatusOK, response)
	}

	response.MatchedTrigger = &controllers.TriggerMatchedTriggerInfo{
		ID:   matchResult.ID(),
		Name: matchResult.Name(),
	}

	// Build tags
	tags := h.customController.BuildCustomTagsForTest(webhook, matchResult)
	defaultMessage := h.customController.BuildDefaultMessageForTest(req.Payload)

	if req.DryRun {
		dryResult, err := h.customController.SessionService().DryRunSessionConfig(controllers.SessionCreationParams{
			Webhook:        webhook,
			Trigger:        matchResult,
			Payload:        req.Payload,
			Tags:           tags,
			DefaultMessage: defaultMessage,
		})
		if err != nil {
			response.Error = err.Error()
		} else if dryResult.Error != "" {
			response.Error = dryResult.Error
		} else {
			response.InitialMessage = dryResult.InitialMessage
			response.Tags = dryResult.Tags
			response.Environment = dryResult.Environment
		}
		return ctx.JSON(http.StatusOK, response)
	}

	// Create actual session
	payloadBytes, _ := json.Marshal(req.Payload)
	sessionID, sessionReused, err := h.customController.SessionService().CreateSessionFromWebhook(ctx, controllers.SessionCreationParams{
		Webhook:        webhook,
		Trigger:        matchResult,
		Payload:        req.Payload,
		RawPayload:     payloadBytes,
		Tags:           tags,
		DefaultMessage: defaultMessage,
	})
	if err != nil {
		response.Error = fmt.Sprintf("Failed to create session: %v", err)
		return ctx.JSON(http.StatusOK, response)
	}

	response.SessionID = sessionID
	response.SessionReused = sessionReused
	return ctx.JSON(http.StatusOK, response)
}

func (h *Handlers) setCORSHeaders(ctx echo.Context) {
	ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")
	ctx.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	ctx.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	ctx.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	ctx.Response().Header().Set("Access-Control-Max-Age", "86400")
}
