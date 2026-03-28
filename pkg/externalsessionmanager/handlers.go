// Package externalsessionmanager implements the external session manager registration API.
//
// This allows Proxy A to register one or more Proxy B instances ("external session managers")
// by name and URL. On registration, a shared HMAC-SHA256 secret is generated and returned once.
// Proxy A uses this secret to sign requests to Proxy B's /api/v1/sessions endpoints.
package externalsessionmanager

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// Handlers implements app.CustomHandler for the external session manager API
type Handlers struct {
	controller *controllers.ExternalSessionManagerController
}

// NewHandlers creates a new Handlers instance
func NewHandlers(repo repositories.ExternalSessionManagerRepository) *Handlers {
	return &Handlers{
		controller: controllers.NewExternalSessionManagerController(repo),
	}
}

// GetName returns the handler name for logging
func (h *Handlers) GetName() string {
	return "ExternalSessionManagerHandlers"
}

// RegisterRoutes registers /external-session-managers routes
func (h *Handlers) RegisterRoutes(e *echo.Echo, server *app.Server) error {
	authService := server.GetContainer().AuthService

	g := e.Group("/external-session-managers")
	g.POST("", h.controller.Create,
		auth.RequirePermission(entities.PermissionSessionCreate, authService))
	g.GET("", h.controller.List,
		auth.RequirePermission(entities.PermissionSessionRead, authService))
	g.GET("/:id", h.controller.Get,
		auth.RequirePermission(entities.PermissionSessionRead, authService))
	g.PUT("/:id", h.controller.Update,
		auth.RequirePermission(entities.PermissionSessionCreate, authService))
	g.DELETE("/:id", h.controller.Delete,
		auth.RequirePermission(entities.PermissionSessionCreate, authService))
	g.POST("/:id/regenerate-secret", h.controller.RegenerateSecret,
		auth.RequirePermission(entities.PermissionSessionCreate, authService))

	log.Printf("[EXTERNAL_SESSION_MANAGER] Registered routes under /external-session-managers")
	return nil
}
