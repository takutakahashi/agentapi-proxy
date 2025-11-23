package http

import (
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
)

// SetupProxyRoutes sets up clean architecture proxy routes
func SetupProxyRoutes(e *echo.Echo, container *di.Container) {
	// Agent session management routes
	e.POST("/start", container.ProxyController.StartAgent)
	e.DELETE("/:sessionId", container.ProxyController.StopAgent)
	e.GET("/sessions", container.ProxyController.ListSessions)
}
