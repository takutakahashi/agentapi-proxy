package controllers

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"net/http"
)

type HTTPServer struct {
	echo                   *echo.Echo
	sessionController      *SessionController
	authController         *AuthController
	notificationController *NotificationController
	authMiddleware         *AuthMiddleware
}

func NewHTTPServer(
	sessionController *SessionController,
	authController *AuthController,
	notificationController *NotificationController,
	authMiddleware *AuthMiddleware,
) *HTTPServer {
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	server := &HTTPServer{
		echo:                   e,
		sessionController:      sessionController,
		authController:         authController,
		notificationController: notificationController,
		authMiddleware:         authMiddleware,
	}

	server.setupRoutes()

	return server
}

// Convert http.HandlerFunc to echo.HandlerFunc
func wrapHandler(handler func(http.ResponseWriter, *http.Request)) echo.HandlerFunc {
	return func(c echo.Context) error {
		handler(c.Response(), c.Request())
		return nil
	}
}

// Convert middleware to echo middleware
func wrapMiddleware(middleware func(http.Handler) http.Handler) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Create a custom handler that calls the next echo handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.SetRequest(r)
				_ = next(c)
			})

			// Apply the middleware
			wrapped := middleware(handler)

			// Execute the wrapped handler
			wrapped.ServeHTTP(c.Response(), c.Request())

			return nil
		}
	}
}

func (s *HTTPServer) setupRoutes() {
	// API group
	api := s.echo.Group("/api/v1")

	// Auth routes
	api.POST("/auth/login", wrapHandler(s.authController.Login))

	// Protected routes
	protected := api.Group("")
	protected.Use(wrapMiddleware(s.authMiddleware.Authenticate))

	// Session routes
	protected.POST("/sessions", wrapHandler(s.sessionController.CreateSession))
	protected.GET("/sessions", wrapHandler(s.sessionController.ListSessions))
	protected.GET("/sessions/:id", wrapHandler(s.sessionController.GetSession))
	protected.DELETE("/sessions/:id", wrapHandler(s.sessionController.DeleteSession))
	protected.GET("/sessions/:id/monitor", wrapHandler(s.sessionController.MonitorSession))

	// Notification routes
	protected.POST("/notifications", wrapHandler(s.notificationController.SendNotification))
}

func (s *HTTPServer) Start(address string) error {
	return s.echo.Start(address)
}

func (s *HTTPServer) Shutdown() error {
	return s.echo.Close()
}
