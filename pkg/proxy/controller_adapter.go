package proxy

import (
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// SetupControllersRoutes configures routes using the clean architecture controllers
// This function provides a gradual migration path from the monolithic proxy to clean architecture
func (p *Proxy) SetupControllersRoutes() {
	if p.container == nil {
		return
	}

	// Setup routes that use the clean architecture controllers where available
	// These can be enabled gradually as the controllers are tested and verified

	// Example of how to integrate controllers when ready:
	// if p.container.ProxyController != nil {
	//     // Agent session management routes using ProxyController
	//     p.echo.POST("/v2/start", p.container.ProxyController.StartAgent,
	//         auth.RequirePermission(entities.PermissionSessionCreate, p.container.AuthService))
	//     p.echo.DELETE("/v2/sessions/:sessionId", p.container.ProxyController.StopAgent,
	//         auth.RequirePermission(entities.PermissionSessionDelete, p.container.AuthService))
	//     p.echo.GET("/v2/sessions", p.container.ProxyController.ListSessions,
	//         auth.RequirePermission(entities.PermissionSessionRead, p.container.AuthService))
	// }

	// Health check endpoint - for now using simple implementation
	// The HealthController from interfaces/controllers uses Gorilla mux and would need adaptation
	p.echo.GET("/v2/health", func(c echo.Context) error {
		return c.JSON(200, map[string]string{"status": "ok", "version": "v2"})
	})
}

// UseControllerImplementations enables using the interfaces/controller implementations
// This provides a configuration option to switch between implementations
func (p *Proxy) UseControllerImplementations(enable bool) {
	// This method can be used to toggle between legacy and new implementations
	// For example, it could be called based on a configuration flag
	if enable && p.container != nil {
		p.SetupControllersRoutes()
	}
}

// GetProxyController returns the proxy controller for external access if needed
func (p *Proxy) GetProxyController() interface{} {
	if p.container != nil && p.container.ProxyController != nil {
		return p.container.ProxyController
	}
	return nil
}

// RegisterControllerRoute allows registering individual controller routes
// This provides fine-grained control over which routes use the new controllers
func (p *Proxy) RegisterControllerRoute(path string, method string, handler echo.HandlerFunc, permissions ...entities.Permission) {
	switch method {
	case "GET":
		if len(permissions) > 0 {
			p.echo.GET(path, handler, auth.RequirePermission(permissions[0], p.container.AuthService))
		} else {
			p.echo.GET(path, handler)
		}
	case "POST":
		if len(permissions) > 0 {
			p.echo.POST(path, handler, auth.RequirePermission(permissions[0], p.container.AuthService))
		} else {
			p.echo.POST(path, handler)
		}
	case "DELETE":
		if len(permissions) > 0 {
			p.echo.DELETE(path, handler, auth.RequirePermission(permissions[0], p.container.AuthService))
		} else {
			p.echo.DELETE(path, handler)
		}
	case "PUT":
		if len(permissions) > 0 {
			p.echo.PUT(path, handler, auth.RequirePermission(permissions[0], p.container.AuthService))
		} else {
			p.echo.PUT(path, handler)
		}
	}
}
