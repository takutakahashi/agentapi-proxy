package auth

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
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

// CredentialKind records which authenticator accepted the request. Keeping it
// separate from the HTTP header prevents an API key from being reused as a
// GitHub credential by session-creation code.
type CredentialKind string

const (
	CredentialKindAPIKey CredentialKind = "api_key"
	CredentialKindGitHub CredentialKind = "github"
)

type CredentialContext struct {
	Kind  CredentialKind
	Token string
}

const credentialContextKey = "authenticated_credential"

// AuthMiddleware creates authentication middleware using internal auth service
func AuthMiddleware(provider config.Provider, authService services.AuthService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cfg := provider.Current()
			// Store config in context for permission checks
			c.Set("config", cfg)

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

			// Skip auth for webhook receiver endpoints (they use HMAC signature verification)
			if isWebhookReceiverEndpoint(path) {
				log.Printf("Skipping auth for webhook receiver endpoint: %s", path)
				return next(c)
			}

			// Skip auth for public static files
			if strings.HasPrefix(path, "/public") {
				return next(c)
			}

			// Shared-session read routes authenticate with the unguessable share
			// token in the path and enforce read-only access in ShareController.
			if strings.HasPrefix(path, "/s/") {
				return next(c)
			}

			// Skip user auth for session Pod provisioner calls. These endpoints
			// perform their own internal token check in the provisioner controller.
			if isInternalTokenEndpoint(path) {
				return next(c)
			}
			if strings.HasPrefix(path, "/external-session-managers/") && strings.HasSuffix(path, "/heartbeat") {
				return next(c)
			}
			if path == "/external-session-managers/enroll" {
				return next(c)
			}

			// Skip auth for HMAC-signed requests from a trusted 親プロキシ
			// This enables small-cluster mode where 親プロキシ proxies session requests to External Session Manager
			if cfg.SessionManager.HMACSecret != "" {
				if skipAuthForHMACRequest(c, cfg.SessionManager.HMACSecret) {
					// X-Forwarded-User is mandatory — reject requests without it to prevent
					// privilege escalation via a catch-all identity.
					forwardedUserID := c.Request().Header.Get("X-Forwarded-User")
					if forwardedUserID == "" {
						return echo.NewHTTPError(http.StatusUnauthorized, "X-Forwarded-User header is required for HMAC-authenticated requests")
					}
					// X-Forwarded-Team is optional; non-empty means the session is team-scoped.
					forwardedTeamID := c.Request().Header.Get("X-Forwarded-Team")
					// Set a synthetic trusted-proxy user context with the original user's identity
					proxyUser := entities.NewServiceAccountUser(
						entities.UserID(forwardedUserID),
						forwardedTeamID,
						[]entities.Permission{
							entities.PermissionSessionCreate,
							entities.PermissionSessionRead,
							entities.PermissionSessionUpdate,
							entities.PermissionSessionDelete,
						},
					)
					c.Set("internal_user", proxyUser)
					authzCtx := buildAuthorizationContext(proxyUser)
					c.Set("authz_context", authzCtx)
					return next(c)
				}
			}

			var user *entities.User
			var err error

			// Try API key authentication first
			// Always attempt API key auth if an API key is provided, regardless of Static Auth config
			// This allows personal API keys (loaded via bootstrap) to work
			if user, err = tryInternalAPIKeyAuth(c, cfg, authService); err == nil {
				c.Set("internal_user", user)
				SetCredentialContext(c, &CredentialContext{Kind: CredentialKindAPIKey})
				log.Printf("API key authentication successful: user %s (type: %s)", user.ID(), user.UserType())
				// Build and store authorization context
				authzCtx := buildAuthorizationContext(user)
				c.Set("authz_context", authzCtx)
				return next(c)
			}
			// Only log if Static Auth is explicitly enabled (to avoid noise for missing API keys)
			if cfg.Auth.Static != nil && cfg.Auth.Static.Enabled {
				log.Printf("API key authentication failed: %v from %s", err, c.RealIP())
			}

			// Try GitHub authentication (placeholder for now)
			if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
				if user, err = tryInternalGitHubAuth(c, cfg, authService); err == nil {
					c.Set("internal_user", user)
					token := ExtractTokenFromHeader(c.Request().Header.Get(cfg.Auth.GitHub.TokenHeader))
					SetCredentialContext(c, &CredentialContext{Kind: CredentialKindGitHub, Token: token})
					// Build and store authorization context
					authzCtx := buildAuthorizationContext(user)
					c.Set("authz_context", authzCtx)
					return next(c)
				}
				log.Printf("GitHub authentication failed: %v from %s", err, c.RealIP())
			}

			log.Printf("Authentication failed: no valid credentials provided from %s", c.RealIP())
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
		}
	}
}

func isInternalTokenEndpoint(path string) bool {
	return strings.HasPrefix(path, "/internal/session-provisioners") ||
		strings.HasPrefix(path, "/internal/session-control") ||
		strings.HasPrefix(path, "/internal/session-runtime") ||
		strings.HasPrefix(path, "/internal/session-allocations") ||
		strings.HasPrefix(path, "/internal/session-state") ||
		strings.HasPrefix(path, "/internal/session-runners") ||
		strings.HasPrefix(path, "/internal/session-managers/") ||
		strings.HasPrefix(path, "/internal/worker/") ||
		strings.HasPrefix(path, "/internal/external-session-manager")
}

// RequirePermission creates permission-checking middleware using internal auth service
func RequirePermission(permission entities.Permission, authService services.AuthService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
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

// GetAuthorizationContext retrieves the pre-built authorization context from Echo context
func GetAuthorizationContext(c echo.Context) *AuthorizationContext {
	if authzCtx := c.Get("authz_context"); authzCtx != nil {
		if ctx, ok := authzCtx.(*AuthorizationContext); ok {
			return ctx
		}
	}
	return nil
}

// GetGitHubTokenFromContext returns a token only when GitHub authentication,
// rather than API-key authentication, succeeded for this request.
func GetGitHubTokenFromContext(c echo.Context) (string, bool) {
	credential, ok := c.Get(credentialContextKey).(*CredentialContext)
	if !ok || credential == nil || credential.Kind != CredentialKindGitHub || credential.Token == "" {
		return "", false
	}
	return credential.Token, true
}

// SetCredentialContext records the credential accepted by an authentication
// adapter. Callers should never set a raw API key because downstream code only
// needs the GitHub token for user-scoped session enrichment.
func SetCredentialContext(c echo.Context, credential *CredentialContext) {
	c.Set(credentialContextKey, credential)
}

// buildAuthorizationContext builds authorization context from user entity
// This function resolves all authorization information upfront to avoid redundant checks in handlers
func buildAuthorizationContext(user *entities.User) *AuthorizationContext {
	if user == nil {
		return nil
	}

	authzCtx := &AuthorizationContext{
		User: user,
	}

	// Build personal scope auth
	authzCtx.PersonalScope = PersonalScopeAuth{
		UserID:    string(user.ID()),
		CanCreate: user.HasPermission(entities.PermissionSessionCreate),
		CanRead:   user.HasPermission(entities.PermissionSessionRead),
		CanUpdate: user.HasPermission(entities.PermissionSessionUpdate),
		CanDelete: user.HasPermission(entities.PermissionSessionDelete),
	}

	// Build team scope auth
	authzCtx.TeamScope = TeamScopeAuth{
		Teams:           make([]string, 0),
		TeamPermissions: make(map[string]TeamPermissions),
		IsAdmin:         user.IsAdmin(),
	}

	// Handle service accounts specially
	if user.UserType() == entities.UserTypeServiceAccount {
		// Service accounts are tied to a specific team
		if teamID := user.TeamID(); teamID != "" {
			authzCtx.TeamScope.Teams = []string{teamID}
			authzCtx.TeamScope.TeamPermissions[teamID] = TeamPermissions{
				TeamID:    teamID,
				CanCreate: user.HasPermission(entities.PermissionSessionCreate),
				CanRead:   user.HasPermission(entities.PermissionSessionRead),
				CanUpdate: user.HasPermission(entities.PermissionSessionUpdate),
				CanDelete: user.HasPermission(entities.PermissionSessionDelete),
			}
		}
	} else {
		// Extract team information from GitHub user info
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamID := team.Organization + "/" + team.TeamSlug
				authzCtx.TeamScope.Teams = append(authzCtx.TeamScope.Teams, teamID)

				// For now, all team members have the same permissions
				// In the future, we might differentiate based on team role
				authzCtx.TeamScope.TeamPermissions[teamID] = TeamPermissions{
					TeamID:    teamID,
					CanCreate: user.HasPermission(entities.PermissionSessionCreate),
					CanRead:   user.HasPermission(entities.PermissionSessionRead),
					CanUpdate: user.HasPermission(entities.PermissionSessionUpdate),
					CanDelete: user.HasPermission(entities.PermissionSessionDelete),
				}
			}
		}
	}

	return authzCtx
}

// UserOwnsSession checks if the current user owns the specified session using internal auth
func UserOwnsSession(c echo.Context, sessionUserID string) bool {
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
	if cfg.Auth.Static != nil && cfg.Auth.Static.HeaderName != "" {
		apiKey = c.Request().Header.Get(cfg.Auth.Static.HeaderName)
	}

	// Also try default X-API-Key header if not found yet
	if apiKey == "" {
		apiKey = c.Request().Header.Get("X-API-Key")
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

// isWebhookReceiverEndpoint checks if the given path is a webhook receiver endpoint
// These endpoints use HMAC signature verification instead of standard authentication
func isWebhookReceiverEndpoint(path string) bool {
	// Webhook receiver endpoints (not management endpoints)
	if strings.HasPrefix(path, "/hooks/") {
		return true
	}
	// Session manager forwarding endpoint — uses HMAC-SHA256 signature verification
	if strings.HasPrefix(path, "/api/v1/sessions") {
		return true
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

// skipAuthForHMACRequest checks if the request carries a valid HMAC-SHA256 signature.
// Used to allow trusted 親プロキシ requests to bypass standard authentication on External Session Manager.
// The signature must cover the canonical message: METHOD\nPATH?QUERY\nTIMESTAMP\nBODY
func skipAuthForHMACRequest(c echo.Context, hmacSecret string) bool {
	sig := c.Request().Header.Get("X-Hub-Signature-256")
	if sig == "" {
		return false
	}

	ts := c.Request().Header.Get(hmacutil.TimestampHeader)
	if ts == "" {
		return false
	}

	// Validate timestamp to prevent replay attacks
	if err := hmacutil.ValidateTimestamp(ts); err != nil {
		return false
	}

	// For non-GET/DELETE requests we read the body; for GET/DELETE use empty body
	var body []byte
	if c.Request().ContentLength > 0 || (c.Request().Method != http.MethodGet && c.Request().Method != http.MethodDelete) {
		var err error
		body, err = io.ReadAll(c.Request().Body)
		if err != nil {
			return false
		}
		c.Request().Body = io.NopCloser(bytes.NewReader(body))
	}

	// Build the canonical signing message: METHOD\nPATH?QUERY\nTIMESTAMP\nBODY
	pathWithQuery := c.Request().URL.RequestURI()
	msg := hmacutil.BuildMessage(c.Request().Method, pathWithQuery, ts, body)

	return hmacutil.Verify([]byte(hmacSecret), msg, sig)
}
