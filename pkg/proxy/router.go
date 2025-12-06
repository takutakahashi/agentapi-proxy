package proxy

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// Router handles route registration and management
type Router struct {
	echo     *echo.Echo
	proxy    *Proxy
	handlers *HandlerRegistry
}

// HandlerRegistry contains all handlers
type HandlerRegistry struct {
	notificationHandlers *NotificationHandlers
	healthHandlers       *HealthHandlers
	customHandlers       []CustomHandler
}

// CustomHandler interface for adding custom routes
type CustomHandler interface {
	RegisterRoutes(e *echo.Echo, proxy *Proxy) error
	GetName() string
}

// NewRouter creates a new Router instance
func NewRouter(e *echo.Echo, proxy *Proxy) *Router {
	return &Router{
		echo:  e,
		proxy: proxy,
		handlers: &HandlerRegistry{
			notificationHandlers: NewNotificationHandlers(proxy.notificationSvc),
			healthHandlers:       NewHealthHandlers(),
			customHandlers:       make([]CustomHandler, 0),
		},
	}
}

// AddCustomHandler adds a custom handler to the registry
func (r *Router) AddCustomHandler(handler CustomHandler) {
	r.handlers.customHandlers = append(r.handlers.customHandlers, handler)
	log.Printf("Added custom handler: %s", handler.GetName())
}

// RegisterRoutes registers all routes
func (r *Router) RegisterRoutes() error {
	// Register core routes
	if err := r.registerCoreRoutes(); err != nil {
		return err
	}

	// Register conditional routes based on configuration
	if err := r.registerConditionalRoutes(); err != nil {
		return err
	}

	// Register custom handlers
	if err := r.registerCustomHandlers(); err != nil {
		return err
	}

	return nil
}

// registerCoreRoutes registers the core routes that are always available
func (r *Router) registerCoreRoutes() error {
	// Health check endpoint
	r.echo.GET("/health", r.handlers.healthHandlers.HealthCheck)

	// Session management routes - temporarily disabled due to removed SessionHandlers
	// TODO: Re-implement session management endpoints
	// r.echo.POST("/start", ...)
	// r.echo.GET("/search", ...)
	// r.echo.DELETE("/sessions/:sessionId", ...)

	// Add explicit OPTIONS handler for DELETE endpoint to ensure CORS preflight works
	r.echo.OPTIONS("/sessions/:sessionId", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	// Add explicit OPTIONS handler for session proxy routes to ensure CORS preflight works
	r.echo.OPTIONS("/:sessionId/*", func(c echo.Context) error {
		// Set CORS headers for preflight
		c.Response().Header().Set("Access-Control-Allow-Origin", "*")
		c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
		c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
		c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
		c.Response().Header().Set("Access-Control-Max-Age", "86400")
		return c.NoContent(http.StatusNoContent)
	})

	// Session proxy routes - temporarily disabled due to removed SessionHandlers
	// TODO: Re-implement session proxy endpoint
	// r.echo.Any("/:sessionId/*", ...)

	return nil
}

// registerConditionalRoutes registers routes based on proxy configuration
func (r *Router) registerConditionalRoutes() error {
	// Add notification routes if service is available
	if r.proxy.notificationSvc != nil {
		log.Printf("[ROUTES] Registering notification endpoints...")
		// UI-compatible routes (proxied from agentapi-ui)
		r.echo.POST("/notification/subscribe", r.handlers.notificationHandlers.Subscribe, auth.RequirePermission(entities.PermissionSessionRead, r.proxy.container.AuthService))
		r.echo.GET("/notification/subscribe", r.handlers.notificationHandlers.GetSubscriptions, auth.RequirePermission(entities.PermissionSessionRead, r.proxy.container.AuthService))
		r.echo.DELETE("/notification/subscribe", r.handlers.notificationHandlers.DeleteSubscription, auth.RequirePermission(entities.PermissionSessionRead, r.proxy.container.AuthService))

		// Internal routes
		r.echo.POST("/notifications/webhook", r.handlers.notificationHandlers.Webhook)
		r.echo.GET("/notifications/history", r.handlers.notificationHandlers.GetHistory, auth.RequirePermission(entities.PermissionSessionRead, r.proxy.container.AuthService))
		log.Printf("[ROUTES] Notification endpoints registered")
	} else {
		log.Printf("[ROUTES] Notification service not available, skipping notification routes")
	}

	return nil
}

// registerCustomHandlers registers all custom handlers
func (r *Router) registerCustomHandlers() error {
	for _, handler := range r.handlers.customHandlers {
		log.Printf("[ROUTES] Registering custom handler: %s", handler.GetName())
		if err := handler.RegisterRoutes(r.echo, r.proxy); err != nil {
			log.Printf("[ROUTES] Failed to register custom handler %s: %v", handler.GetName(), err)
			return err
		}
		log.Printf("[ROUTES] Successfully registered custom handler: %s", handler.GetName())
	}

	return nil
}
