package auth

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// InternalAuthMiddleware creates authentication middleware using internal auth service
func InternalAuthMiddleware(cfg *config.Config, authService services.AuthService) echo.MiddlewareFunc {
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

// InternalRequirePermission creates permission-checking middleware using internal auth service
func InternalRequirePermission(permission entities.Permission, authService services.AuthService) echo.MiddlewareFunc {
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

			user := GetInternalUserFromContext(c)
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

// GetInternalUserFromContext retrieves internal user entity from Echo context
func GetInternalUserFromContext(c echo.Context) *entities.User {
	if user := c.Get("internal_user"); user != nil {
		if userEntity, ok := user.(*entities.User); ok {
			return userEntity
		}
	}
	return nil
}

// InternalUserOwnsSession checks if the current user owns the specified session using internal auth
func InternalUserOwnsSession(c echo.Context, sessionUserID string) bool {
	// If auth is disabled, allow access to all sessions
	if cfg := GetConfigFromContext(c); cfg != nil && !cfg.Auth.Enabled {
		return true
	}

	user := GetInternalUserFromContext(c)
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

// ConvertInternalUserToUserContext converts internal user entity to legacy UserContext for backward compatibility
func ConvertInternalUserToUserContext(user *entities.User) *UserContext {
	if user == nil {
		return nil
	}

	// Convert permissions to string slice
	permissions := make([]string, len(user.Permissions()))
	for i, perm := range user.Permissions() {
		permissions[i] = string(perm)
	}

	// Determine role (use first role or "user" as default)
	role := "user"
	if len(user.Roles()) > 0 {
		role = string(user.Roles()[0])
	}

	userCtx := &UserContext{
		UserID:      string(user.ID()),
		Role:        role,
		Permissions: permissions,
		AuthType:    string(user.UserType()),
		EnvFile:     user.EnvFile(),
	}

	// Add GitHub-specific information if available
	if user.UserType() == entities.UserTypeGitHub && user.GitHubInfo() != nil {
		githubInfo := user.GitHubInfo()
		userCtx.GitHubUser = &GitHubUserInfo{
			Login: githubInfo.Login(),
			ID:    githubInfo.ID(),
			Name:  githubInfo.Name(),
			Email: githubInfo.Email(),
		}
	}

	return userCtx
}

// GetUserFromInternalContext retrieves UserContext from internal user for backward compatibility
func GetUserFromInternalContext(c echo.Context) *UserContext {
	user := GetInternalUserFromContext(c)
	return ConvertInternalUserToUserContext(user)
}
