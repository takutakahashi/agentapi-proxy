package auth

import (
	"context"
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
	AuthType    string          // "api_key" or "github_oauth"
	GitHubUser  *GitHubUserInfo // GitHub user info when using GitHub auth
	AccessToken string          // OAuth access token (not serialized)
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

			var userCtx *UserContext
			var err error

			// Try GitHub authentication first
			if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
				if userCtx, err = tryGitHubAuth(c, cfg.Auth.GitHub); err == nil {
					c.Set("user", userCtx)
					log.Printf("GitHub authentication successful: user %s (role: %s) from %s", userCtx.UserID, userCtx.Role, c.RealIP())
					return next(c)
				}
				log.Printf("GitHub authentication failed: %v from %s", err, c.RealIP())
			}

			// Try static API key authentication
			if cfg.Auth.Static != nil && cfg.Auth.Static.Enabled {
				if userCtx, err = tryStaticAuth(c, cfg.Auth.Static, cfg); err == nil {
					c.Set("user", userCtx)
					log.Printf("Static authentication successful: user %s (role: %s) from %s", userCtx.UserID, userCtx.Role, c.RealIP())
					return next(c)
				}
				log.Printf("Static authentication failed: %v from %s", err, c.RealIP())
			}

			log.Printf("Authentication failed: no valid credentials provided from %s", c.RealIP())
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
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
				log.Printf("Authorization failed: authentication required for permission %s from %s", permission, c.RealIP())
				return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
			}

			if !hasPermission(user.Permissions, permission) {
				log.Printf("Authorization failed: user %s (role: %s) lacks permission %s, has permissions: %v, from %s",
					user.UserID, user.Role, permission, user.Permissions, c.RealIP())
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

// tryGitHubAuth attempts GitHub OAuth authentication
func tryGitHubAuth(c echo.Context, cfg *config.GitHubAuthConfig) (*UserContext, error) {
	tokenHeader := c.Request().Header.Get(cfg.TokenHeader)
	if tokenHeader == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub token required")
	}

	token := ExtractTokenFromHeader(tokenHeader)
	if token == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid GitHub token format")
	}

	provider := NewGitHubAuthProvider(cfg)
	ctx := context.WithValue(c.Request().Context(), echoContextKey, c)

	userCtx, err := provider.Authenticate(ctx, token)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub authentication failed")
	}

	return userCtx, nil
}

// tryStaticAuth attempts static API key authentication
func tryStaticAuth(c echo.Context, staticCfg *config.StaticAuthConfig, cfg *config.Config) (*UserContext, error) {
	apiKey := c.Request().Header.Get(staticCfg.HeaderName)
	if apiKey == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "API key required")
	}

	keyInfo, valid := cfg.ValidateAPIKey(apiKey)
	if !valid {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid API key")
	}

	return &UserContext{
		UserID:      keyInfo.UserID,
		Role:        keyInfo.Role,
		Permissions: keyInfo.Permissions,
		APIKey:      apiKey,
		AuthType:    "api_key",
	}, nil
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
		log.Printf("Session access denied: no authenticated user for session %s from %s", sessionUserID, c.RealIP())
		return false
	}

	// Admin role can access all sessions
	if user.Role == "admin" {
		return true
	}

	// Users can only access their own sessions
	if user.UserID != sessionUserID {
		log.Printf("Session access denied: user %s (role: %s) attempted to access session owned by %s from %s",
			user.UserID, user.Role, sessionUserID, c.RealIP())
		return false
	}
	return true
}
