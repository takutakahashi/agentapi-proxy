package auth

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// UserContext represents the authenticated user context
type UserContext struct {
	UserID      string
	Role        string
	Permissions []string
	APIKey      string
}

// AuthMiddleware creates authentication middleware
func AuthMiddleware(cfg *config.Config) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Store config in context for permission checks
			c.Set("config", cfg)

			// Skip auth if disabled
			if !cfg.Auth.Enabled {
				return next(c)
			}

			// Skip auth for OPTIONS requests (CORS preflight)
			if c.Request().Method == "OPTIONS" {
				log.Printf("Skipping auth for OPTIONS request: %s", c.Request().URL.Path)
				return next(c)
			}

			// Get API key from header
			apiKey := c.Request().Header.Get(cfg.Auth.HeaderName)
			if apiKey == "" {
				log.Printf("Authentication failed: missing API key in header %s from %s", cfg.Auth.HeaderName, c.RealIP())
				return echo.NewHTTPError(http.StatusUnauthorized, "API key required")
			}

			// Validate API key
			keyInfo, valid := cfg.ValidateAPIKey(apiKey)
			if !valid {
				log.Printf("Authentication failed: invalid API key from %s", c.RealIP())
				return echo.NewHTTPError(http.StatusUnauthorized, "Invalid API key")
			}

			// Create user context
			userCtx := &UserContext{
				UserID:      keyInfo.UserID,
				Role:        keyInfo.Role,
				Permissions: keyInfo.Permissions,
				APIKey:      apiKey,
			}

			// Store user context in Echo context
			c.Set("user", userCtx)

			log.Printf("Authentication successful: user %s (role: %s) from %s", userCtx.UserID, userCtx.Role, c.RealIP())
			return next(c)
		}
	}
}

// RequirePermission creates permission-checking middleware
func RequirePermission(permission string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Check if auth is disabled by looking for config in context
			if cfg := GetConfigFromContext(c); cfg != nil && !cfg.Auth.Enabled {
				return next(c)
			}

			// Skip permission check for OPTIONS requests (CORS preflight)
			if c.Request().Method == "OPTIONS" {
				return next(c)
			}

			user := GetUserFromContext(c)
			if user == nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
			}

			if !hasPermission(user.Permissions, permission) {
				log.Printf("Authorization failed: user %s lacks permission %s", user.UserID, permission)
				return echo.NewHTTPError(http.StatusForbidden, "Insufficient permissions")
			}

			return next(c)
		}
	}
}

// GetUserFromContext retrieves user context from Echo context
func GetUserFromContext(c echo.Context) *UserContext {
	if user := c.Get("user"); user != nil {
		if userCtx, ok := user.(*UserContext); ok {
			return userCtx
		}
	}
	return nil
}

// GetConfigFromContext retrieves config from Echo context
func GetConfigFromContext(c echo.Context) *config.Config {
	if cfg := c.Get("config"); cfg != nil {
		if configObj, ok := cfg.(*config.Config); ok {
			return configObj
		}
	}
	return nil
}

// hasPermission checks if user has a specific permission
func hasPermission(permissions []string, required string) bool {
	for _, perm := range permissions {
		if perm == required || perm == "*" {
			return true
		}
	}
	return false
}

// UserOwnsSession checks if the current user owns the specified session
func UserOwnsSession(c echo.Context, sessionUserID string) bool {
	// If auth is disabled, allow access to all sessions
	if cfg := GetConfigFromContext(c); cfg != nil && !cfg.Auth.Enabled {
		return true
	}

	user := GetUserFromContext(c)
	if user == nil {
		return false
	}

	// Admin role can access all sessions
	if user.Role == "admin" {
		return true
	}

	// Users can only access their own sessions
	return user.UserID == sessionUserID
}
