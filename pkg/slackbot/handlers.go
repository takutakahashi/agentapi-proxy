package slackbot

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Handlers provides SlackBot management REST API, implementing app.CustomHandler
type Handlers struct {
	controller *controllers.SlackBotController
}

// NewHandlers creates a new SlackBot Handlers instance (management API only).
// Socket Mode event handling is managed separately by SlackSocketManager.
func NewHandlers(
	repo repositories.SlackBotRepository,
	baseURL string,
	defaultSigningSecret string,
) *Handlers {
	controller := controllers.NewSlackBotController(repo, baseURL, defaultSigningSecret)
	return &Handlers{
		controller: controller,
	}
}

// GetName returns the name of this handler for logging
func (h *Handlers) GetName() string {
	return "SlackBotHandlers"
}

// RegisterRoutes registers SlackBot management routes.
// Implements the app.CustomHandler interface.
// Note: Slack events are received via Socket Mode (not HTTP webhook).
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
	// Management API (authentication required via middleware)
	g := e.Group("/slackbots")
	g.POST("", h.controller.CreateSlackBot)
	g.GET("", h.controller.ListSlackBots)
	g.GET("/:id", h.controller.GetSlackBot)
	g.PUT("/:id", h.controller.UpdateSlackBot)
	g.DELETE("/:id", h.controller.DeleteSlackBot)

	log.Printf("Registered SlackBot management routes (Socket Mode event handling active)")
	return nil
}
