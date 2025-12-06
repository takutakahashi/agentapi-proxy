package auth

import (
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// UserContext represents the authenticated user context (for legacy compatibility)
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

// AuthMiddleware creates authentication middleware using internal auth service
func AuthMiddleware(cfg *config.Config, authService services.AuthService) echo.MiddlewareFunc {
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

			// Handle legacy endpoints with relaxed authentication
			if isLegacyEndpoint(path) {
				log.Printf("Legacy endpoint authentication for: %s", path)

				var user *entities.User
				var err error

				// Try authentication but don't fail if no credentials provided
				if cfg.Auth.Static != nil && cfg.Auth.Static.Enabled {
					if user, err = tryInternalAPIKeyAuth(c, cfg, authService); err == nil {
						c.Set("internal_user", user)
						log.Printf("API key authentication successful for legacy endpoint: user %s", user.ID())
						return next(c)
					}
				}

				if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
					if user, err = tryInternalGitHubAuth(c, cfg, authService); err == nil {
						c.Set("internal_user", user)
						log.Printf("GitHub authentication successful for legacy endpoint: user %s", user.ID())
						return next(c)
					}
				}

				// For legacy endpoints, continue without authentication
				log.Printf("Legacy endpoint accessed without authentication: %s from %s", path, c.RealIP())
				return next(c)
			}

			var user *entities.User
			var err error

			// Try API key authentication first
			if cfg.Auth.Static != nil && cfg.Auth.Static.Enabled {
				if user, err = tryInternalAPIKeyAuth(c, cfg, authService); err == nil {
					c.Set("internal_user", user)
					log.Printf("API key authentication successful: user %s (type: %s)", user.ID(), user.UserType())
					return next(c)
				}
				log.Printf("API key authentication failed: %v from %s", err, c.RealIP())
			}

			// Try GitHub authentication (placeholder for now)
			if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
				if user, err = tryInternalGitHubAuth(c, cfg, authService); err == nil {
					c.Set("internal_user", user)
					log.Printf("GitHub authentication successful: user %s (type: %s)", user.ID(), user.UserType())
					return next(c)
				}
				log.Printf("GitHub authentication failed: %v from %s", err, c.RealIP())
			}

			log.Printf("Authentication failed: no valid credentials provided from %s", c.RealIP())
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
		}
	}
}

// RequirePermission creates permission-checking middleware using internal auth service
func RequirePermission(permission entities.Permission, authService services.AuthService) echo.MiddlewareFunc {
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

			// Use internal auth service to validate permission
			if err := authService.ValidatePermission(c.Request().Context(), user, permission); err != nil {
				log.Printf("Authorization failed: user %s (type: %s) lacks permission %s: %v",
					user.ID(), user.UserType(), permission, err)
				return echo.NewHTTPError(http.StatusForbidden, "Insufficient permissions")
			}

			return next(c)
		}
	}
}

// GetUserFromContext retrieves internal user entity from Echo context
func GetUserFromContext(c echo.Context) *entities.User {
	if user := c.Get("internal_user"); user != nil {
		if userEntity, ok := user.(*entities.User); ok {
			return userEntity
		}
	}
	return nil
}

// UserOwnsSession checks if the current user owns the specified session using internal auth
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

	// Use internal user's permission checking
	sessionUserEntityID := entities.UserID(sessionUserID)
	canAccess := user.CanAccessSession(sessionUserEntityID)

	if !canAccess {
		log.Printf("Session access denied: user %s (type: %s) attempted to access session owned by %s from %s",
			user.ID(), user.UserType(), sessionUserID, c.RealIP())
	}

	return canAccess
}

// tryInternalAPIKeyAuth attempts API key authentication using internal auth service
func tryInternalAPIKeyAuth(c echo.Context, cfg *config.Config, authService services.AuthService) (*entities.User, error) {
	var apiKey string

	// First, try to get API key from the configured custom header
	if cfg.Auth.Static != nil {
		apiKey = c.Request().Header.Get(cfg.Auth.Static.HeaderName)
	}

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

	// Use internal auth service to validate API key
	user, err := authService.ValidateAPIKey(c.Request().Context(), apiKey)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid API key")
	}

	return user, nil
}

// tryInternalGitHubAuth attempts GitHub authentication using internal auth service
func tryInternalGitHubAuth(c echo.Context, cfg *config.Config, authService services.AuthService) (*entities.User, error) {
	if cfg.Auth.GitHub == nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub auth not configured")
	}

	tokenHeader := c.Request().Header.Get(cfg.Auth.GitHub.TokenHeader)
	if tokenHeader == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub token required")
	}

	token := ExtractTokenFromHeader(tokenHeader)
	if token == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "Invalid GitHub token format")
	}

	// Create credentials for token-based authentication
	credentials := &services.Credentials{
		Type:  services.CredentialTypeToken,
		Token: token,
	}

	// Use internal auth service to authenticate
	user, err := authService.AuthenticateUser(c.Request().Context(), credentials)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "GitHub authentication failed")
	}

	return user, nil
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

// isLegacyEndpoint checks if the given path is a legacy endpoint that should skip auth
func isLegacyEndpoint(path string) bool {
	legacyPaths := []string{
		"/start",
		"/search",
	}

	for _, legacyPath := range legacyPaths {
		if path == legacyPath {
			return true
		}
	}
	return false
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
