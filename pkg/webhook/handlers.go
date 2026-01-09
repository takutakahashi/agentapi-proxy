package webhook

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Handlers is an adapter that combines webhook controllers and implements app.CustomHandler interface
type Handlers struct {
	controller       *controllers.WebhookController
	githubController *controllers.WebhookGitHubController
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

	// Receiver endpoints
	hooks := e.Group("/hooks")
	hooks.POST("/github", h.githubController.HandleGitHubWebhook)

	log.Printf("Registered webhook management routes")
	return nil
}
