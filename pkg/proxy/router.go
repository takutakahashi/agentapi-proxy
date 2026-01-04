package proxy

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
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
	healthController     *controllers.HealthController
	sessionController    *controllers.SessionController
	settingsController   *controllers.SettingsController
	userController       *controllers.UserController
	shareController      *controllers.ShareController
	customHandlers       []CustomHandler
}

// CustomHandler interface for adding custom routes
type CustomHandler interface {
	RegisterRoutes(e *echo.Echo, proxy *Proxy) error
	GetName() string
}

// NewRouter creates a new Router instance
func NewRouter(e *echo.Echo, proxy *Proxy) *Router {
	// Create settings controller
	settingsController := controllers.NewSettingsController(proxy.settingsRepo)

	// Set credentials secret syncer and MCP secret syncer if Kubernetes mode is enabled
	if k8sManager, ok := proxy.sessionManager.(*KubernetesSessionManager); ok {
		// Set credentials secret syncer
		credSyncer := services.NewKubernetesCredentialsSecretSyncer(
			k8sManager.GetClient(),
			k8sManager.GetNamespace(),
		)
		settingsController.SetCredentialsSecretSyncer(credSyncer)
		log.Printf("[ROUTER] Credentials secret syncer configured for settings controller")

		// Set MCP secret syncer
		mcpSyncer := services.NewKubernetesMCPSecretSyncer(
			k8sManager.GetClient(),
			k8sManager.GetNamespace(),
		)
		settingsController.SetMCPSecretSyncer(mcpSyncer)
		log.Printf("[ROUTER] MCP secret syncer configured for settings controller")
	}

	// Create session controller with proper dependencies
	// proxy implements SessionManagerProvider interface via GetSessionManager()
	sessionController := controllers.NewSessionController(
		proxy, // Proxy implements SessionManagerProvider interface
		proxy, // Proxy implements SessionCreator interface
	)

	// Create share controller if share repository is available
	var shareController *controllers.ShareController
	if proxy.shareRepo != nil {
		shareController = controllers.NewShareController(
			proxy, // Proxy implements SessionManagerProvider interface
			proxy.shareRepo,
		)
		log.Printf("[ROUTER] Share controller initialized")
	}

	return &Router{
		echo:  e,
		proxy: proxy,
		handlers: &HandlerRegistry{
			notificationHandlers: NewNotificationHandlers(proxy.notificationSvc),
			healthController:     controllers.NewHealthController(),
			sessionController:    sessionController,
			settingsController:   settingsController,
			userController:       controllers.NewUserController(),
			shareController:      shareController,
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
	r.echo.GET("/health", r.handlers.healthController.HealthCheck)

	// Session management routes
	log.Printf("[ROUTES] Registering session management endpoints...")
	r.echo.POST("/start", r.handlers.sessionController.StartSession)
	r.echo.GET("/search", r.handlers.sessionController.SearchSessions)
	r.echo.DELETE("/sessions/:sessionId", r.handlers.sessionController.DeleteSession)

	// Session sharing routes
	if r.handlers.shareController != nil {
		log.Printf("[ROUTES] Registering session sharing endpoints...")
		r.echo.POST("/sessions/:sessionId/share", r.handlers.shareController.CreateShare)
		r.echo.GET("/sessions/:sessionId/share", r.handlers.shareController.GetShare)
		r.echo.DELETE("/sessions/:sessionId/share", r.handlers.shareController.DeleteShare)
		// Add OPTIONS handler for session share endpoints (CORS preflight)
		r.echo.OPTIONS("/sessions/:sessionId/share", func(c echo.Context) error {
			c.Response().Header().Set("Access-Control-Allow-Origin", "*")
			c.Response().Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
			c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
			c.Response().Header().Set("Access-Control-Max-Age", "86400")
			return c.NoContent(http.StatusNoContent)
		})
		// Shared session access route (read-only)
		r.echo.Any("/s/:shareToken/*", r.handlers.shareController.RouteToSharedSession)
		r.echo.OPTIONS("/s/:shareToken/*", func(c echo.Context) error {
			c.Response().Header().Set("Access-Control-Allow-Origin", "*")
			c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
			c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
			c.Response().Header().Set("Access-Control-Max-Age", "86400")
			return c.NoContent(http.StatusNoContent)
		})
		log.Printf("[ROUTES] Session sharing endpoints registered")
	}

	// Session proxy route
	r.echo.Any("/:sessionId/*", r.handlers.sessionController.RouteToSession)
	log.Printf("[ROUTES] Session management endpoints registered")

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

	return nil
}

// registerConditionalRoutes registers routes based on proxy configuration
func (r *Router) registerConditionalRoutes() error {
	// User info endpoint (requires authentication)
	log.Printf("[ROUTES] Registering user info endpoint...")
	r.echo.GET("/user/info", r.handlers.userController.GetUserInfo, auth.RequirePermission(entities.PermissionSessionRead, r.proxy.container.AuthService))
	log.Printf("[ROUTES] User info endpoint registered")

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

	// Add settings routes if settings repository is available (Kubernetes mode only)
	if r.proxy.settingsRepo != nil && r.handlers.settingsController != nil {
		log.Printf("[ROUTES] Registering settings endpoints...")
		r.echo.GET("/settings/:name", r.handlers.settingsController.GetSettings, auth.RequirePermission(entities.PermissionSessionRead, r.proxy.container.AuthService))
		r.echo.PUT("/settings/:name", r.handlers.settingsController.UpdateSettings, auth.RequirePermission(entities.PermissionSessionCreate, r.proxy.container.AuthService))
		r.echo.DELETE("/settings/:name", r.handlers.settingsController.DeleteSettings, auth.RequirePermission(entities.PermissionSessionCreate, r.proxy.container.AuthService))
		log.Printf("[ROUTES] Settings endpoints registered")
	} else {
		log.Printf("[ROUTES] Settings repository not available, skipping settings routes")
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
