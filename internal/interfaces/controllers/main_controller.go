package controllers

import (
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// MainController manages all application routes and controllers
type MainController struct {
	// Core controllers
	sessionController      *SessionController
	authController         *AuthController
	notificationController *NotificationController
	proxyController        *ProxyController
	healthController       *HealthController

	// Services
	authService services.AuthService

	// Configuration
	config *config.Config
}

// NewMainController creates a new main controller instance
func NewMainController(
	sessionController *SessionController,
	authController *AuthController,
	notificationController *NotificationController,
	proxyController *ProxyController,
	healthController *HealthController,
	authService services.AuthService,
	config *config.Config,
) *MainController {
	return &MainController{
		sessionController:      sessionController,
		authController:         authController,
		notificationController: notificationController,
		proxyController:        proxyController,
		healthController:       healthController,
		authService:            authService,
		config:                 config,
	}
}

// RegisterRoutes registers all application routes
func (mc *MainController) RegisterRoutes(e *echo.Echo) {
	// Register health routes
	mc.healthController.RegisterRoutes(e)

	// Auth routes are handled by proxy/proxy.go
	// No additional registration needed here

	// Register API v1 routes
	mc.registerAPIV1Routes(e)

	// Register legacy proxy routes (for backward compatibility)
	mc.registerLegacyRoutes(e)
}

// registerAPIV1Routes registers all API v1 routes
func (mc *MainController) registerAPIV1Routes(e *echo.Echo) {
	// Session API v1 routes (already implemented in SessionController)
	mc.sessionController.RegisterRoutes(e, mc.authService)

	// Notification API v1 routes
	mc.registerNotificationAPIV1Routes(e)

	// Session proxy API v1 routes
	mc.registerSessionProxyAPIV1Routes(e)
}

// registerLegacyRoutes registers legacy routes for backward compatibility
func (mc *MainController) registerLegacyRoutes(e *echo.Echo) {
	// Legacy routes are handled by proxy/proxy.go
	// No additional registration needed here
}

// registerNotificationAPIV1Routes registers notification API v1 routes
func (mc *MainController) registerNotificationAPIV1Routes(e *echo.Echo) {
	// TODO: Add notification API v1 routes here
	// This will be implemented when notification endpoints are needed
}

// registerSessionProxyAPIV1Routes registers session proxy API v1 routes
func (mc *MainController) registerSessionProxyAPIV1Routes(e *echo.Echo) {
	// TODO: Add session proxy API v1 routes here
	// This will handle /api/v1/sessions/:sessionId/* transparent proxy
}

// SetConfig sets the configuration
func (mc *MainController) SetConfig(config *config.Config) {
	mc.config = config
}
