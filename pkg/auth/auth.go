package auth

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
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
	EnvFile     string          // Path to team-specific environment file
}

// AuthMiddleware creates authentication middleware
func AuthMiddleware(cfg *config.Config, githubProvider *GitHubAuthProvider, container *di.Container) echo.MiddlewareFunc {
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

			// Skip auth for OAuth endpoints (they handle authentication themselves)
			path := c.Request().URL.Path
			if isOAuthEndpoint(path) {
				log.Printf("Skipping auth for OAuth endpoint: %s", path)
				return next(c)
			}

			// Skip auth for health endpoint
			if path == "/health" {
				return next(c)
			}

			var userCtx *UserContext
			var err error

			// Store container in context for use cases
			c.Set("container", container)

			// Try GitHub authentication first
			if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled && githubProvider != nil {
				if userCtx, err = tryGitHubAuthWithCleanArchitecture(c, cfg.Auth.GitHub, githubProvider, container); err == nil {
					c.Set("user", userCtx)
					log.Printf("GitHub authentication successful via Clean Architecture: user %s (role: %s)", userCtx.UserID, userCtx.Role)
					return next(c)
				}
				log.Printf("GitHub authentication failed: %v from %s", err, c.RealIP())
			}

			// Try static API key authentication
			if cfg.Auth.Static != nil && cfg.Auth.Static.Enabled {
				if userCtx, err = tryStaticAuthWithCleanArchitecture(c, cfg.Auth.Static, cfg, container); err == nil {
					c.Set("user", userCtx)
					log.Printf("Static authentication successful via Clean Architecture: user %s (role: %s)", userCtx.UserID, userCtx.Role)
					return next(c)
				}
				log.Printf("Static authentication failed: %v from %s", err, c.RealIP())
			}

			log.Printf("Authentication failed: no valid credentials provided from %s", c.RealIP())
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
		}
	}
}

// isOAuthEndpoint checks if the given path is an OAuth endpoint that should skip auth
func isOAuthEndpoint(path string) bool {
	oauthPaths := []string{
		"/oauth/authorize",
		"/oauth/callback",
		"/oauth/logout",
		"/oauth/refresh",
	}

	for _, oauthPath := range oauthPaths {
		if strings.HasPrefix(path, oauthPath) {
			return true
		}
	}
	return false
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
				log.Printf("Authorization failed: user %s (role: %s) lacks permission %s",
					user.UserID, user.Role, permission)
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
func tryGitHubAuth(c echo.Context, cfg *config.GitHubAuthConfig, provider *GitHubAuthProvider) (*UserContext, error) {
	tokenHeader := c.Request().Header.Get(cfg.TokenHeader)
	if tokenHeader == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub token required")
	}

	token := ExtractTokenFromHeader(tokenHeader)
	if token == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid GitHub token format")
	}

	ctx := context.WithValue(c.Request().Context(), echoContextKey, c)

	userCtx, err := provider.Authenticate(ctx, token)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub authentication failed")
	}

	return userCtx, nil
}

// tryGitHubAuthWithCleanArchitecture uses Clean Architecture for GitHub authentication
func tryGitHubAuthWithCleanArchitecture(c echo.Context, cfg *config.GitHubAuthConfig, provider *GitHubAuthProvider, container *di.Container) (*UserContext, error) {
	tokenHeader := c.Request().Header.Get(cfg.TokenHeader)
	if tokenHeader == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub token required")
	}

	token := ExtractTokenFromHeader(tokenHeader)
	if token == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid GitHub token format")
	}

	ctx := context.WithValue(c.Request().Context(), echoContextKey, c)

	// Use Clean Architecture GitHubAuthenticateUC instead of direct provider call
	// For now, fall back to original implementation until fully integrated
	userCtx, err := provider.Authenticate(ctx, token)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub authentication failed")
	}

	return userCtx, nil
}

// tryStaticAuthWithCleanArchitecture uses Clean Architecture for static API key authentication
func tryStaticAuthWithCleanArchitecture(c echo.Context, staticCfg *config.StaticAuthConfig, cfg *config.Config, container *di.Container) (*UserContext, error) {
	var apiKey string

	// First, try to get API key from the configured custom header
	apiKey = c.Request().Header.Get(staticCfg.HeaderName)

	// If not found in custom header, try to extract from Authorization header (Bearer token)
	if apiKey == "" {
		authHeader := c.Request().Header.Get("Authorization")
		if authHeader != "" {
			apiKey = extractAPIKeyFromAuthHeader(authHeader)
		}
	}

	if apiKey == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "API key required")
	}

	// Use Clean Architecture ValidateAPIKeyUC instead of direct config validation
	// For now, fall back to original implementation until fully integrated
	keyInfo, valid := cfg.ValidateAPIKey(apiKey)
	if !valid {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid API key")
	}

	userCtx := &UserContext{
		UserID:      keyInfo.UserID,
		Role:        keyInfo.Role,
		Permissions: keyInfo.Permissions,
		APIKey:      apiKey,
		AuthType:    "api_key",
		EnvFile:     "", // TODO: Add EnvFile support to APIKey struct
	}

	return userCtx, nil
}

// tryStaticAuth attempts static API key authentication
func tryStaticAuth(c echo.Context, staticCfg *config.StaticAuthConfig, cfg *config.Config) (*UserContext, error) {
	var apiKey string

	// First, try to get API key from the configured custom header
	apiKey = c.Request().Header.Get(staticCfg.HeaderName)

	// If not found in custom header, try to extract from Authorization header (Bearer token)
	if apiKey == "" {
		authHeader := c.Request().Header.Get("Authorization")
		if authHeader != "" {
			apiKey = extractAPIKeyFromAuthHeader(authHeader)
		}
	}

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

// extractAPIKeyFromAuthHeader extracts API key from Authorization header
func extractAPIKeyFromAuthHeader(header string) string {
	if header == "" {
		return ""
	}

	// Handle "Bearer <token>" format
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}

	// Handle raw token
	return header
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
