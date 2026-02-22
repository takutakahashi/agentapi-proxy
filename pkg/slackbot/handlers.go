package slackbot

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Handlers combines SlackBot management and event handling, implementing app.CustomHandler
type Handlers struct {
	controller   *controllers.SlackBotController
	eventHandler *controllers.SlackBotEventHandler
}

// NewHandlers creates a new SlackBot Handlers instance
func NewHandlers(
	repo repositories.SlackBotRepository,
	sessionManager repositories.SessionManager,
	defaultSigningSecret string,
	defaultBotTokenSecretName string,
	defaultBotTokenSecretKey string,
	baseURL string,
) *Handlers {
	controller := controllers.NewSlackBotController(repo, baseURL)
	eventHandler := controllers.NewSlackBotEventHandler(
		repo,
		sessionManager,
		defaultSigningSecret,
		defaultBotTokenSecretName,
		defaultBotTokenSecretKey,
	)
	return &Handlers{
		controller:   controller,
		eventHandler: eventHandler,
	}
}

// GetName returns the name of this handler for logging
func (h *Handlers) GetName() string {
	return "SlackBotHandlers"
}

// RegisterRoutes registers SlackBot management and event receiver routes
// Implements the app.CustomHandler interface
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
	// Management API (authentication required via middleware)
	g := e.Group("/slackbots")
	g.POST("", h.controller.CreateSlackBot)
	g.GET("", h.controller.ListSlackBots)
	g.GET("/:id", h.controller.GetSlackBot)
	g.PUT("/:id", h.controller.UpdateSlackBot)
	g.DELETE("/:id", h.controller.DeleteSlackBot)

	// Event receiver endpoint (no auth, protected by Slack signature verification)
	// :id = UUID (registered SlackBot) or "default" (server startup config)
	e.POST("/hooks/slack/:id", h.eventHandler.HandleSlackEvent)

	log.Printf("Registered SlackBot management and event receiver routes")
	return nil
}
