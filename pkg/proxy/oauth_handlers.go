package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// OAuthLoginRequest represents the request body for OAuth login
type OAuthLoginRequest struct {
	RedirectURI string `json:"redirect_uri"`
}

// OAuthLoginResponse represents the response for OAuth login
type OAuthLoginResponse struct {
	AuthURL string `json:"auth_url"`
	State   string `json:"state"`
}

// OAuthCallbackRequest represents the OAuth callback parameters
type OAuthCallbackRequest struct {
	Code  string `query:"code"`
	State string `query:"state"`
}

// OAuthTokenResponse represents the response after successful OAuth
type OAuthTokenResponse struct {
	AccessToken string            `json:"access_token"`
	TokenType   string            `json:"token_type"`
	ExpiresAt   time.Time         `json:"expires_at"`
	User        *auth.UserContext `json:"user"`
}

// OAuthSessionResponse represents the response with session information
type OAuthSessionResponse struct {
	SessionID   string            `json:"session_id"`
	AccessToken string            `json:"access_token"`
	TokenType   string            `json:"token_type"`
	ExpiresAt   time.Time         `json:"expires_at"`
	User        *auth.UserContext `json:"user"`
}

// handleOAuthLogin initiates the OAuth flow
func (p *Proxy) handleOAuthLogin(c echo.Context) error {
	var req OAuthLoginRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate redirect URI
	if req.RedirectURI == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "redirect_uri is required")
	}

	// Validate redirect URI format
	redirectURL, err := url.Parse(req.RedirectURI)
	if err != nil || redirectURL.Scheme == "" || redirectURL.Host == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid redirect_uri format")
	}

	// Validate redirect URI against whitelist
	if !isAllowedRedirectURI(req.RedirectURI) {
		log.Printf("Blocked unauthorized redirect URI: %s", req.RedirectURI)
		return echo.NewHTTPError(http.StatusBadRequest, "Unauthorized redirect_uri")
	}

	// Generate OAuth URL
	authURL, state, err := p.oauthProvider.GenerateAuthURL(req.RedirectURI)
	if err != nil {
		log.Printf("Failed to generate OAuth URL: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to generate authorization URL")
	}

	return c.JSON(http.StatusOK, OAuthLoginResponse{
		AuthURL: authURL,
		State:   state,
	})
}

// handleOAuthCallback handles the OAuth callback
func (p *Proxy) handleOAuthCallback(c echo.Context) error {
	var req OAuthCallbackRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid callback parameters")
	}

	// Validate required parameters
	if req.Code == "" || req.State == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Missing code or state parameter")
	}

	// Exchange code for token
	userContext, err := p.oauthProvider.ExchangeCode(c.Request().Context(), req.Code, req.State)
	if err != nil {
		log.Printf("OAuth code exchange failed: %v", err)
		return echo.NewHTTPError(http.StatusUnauthorized, "OAuth authentication failed")
	}

	// Create a new session for the authenticated user
	sessionID := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour) // Token expires in 24 hours

	// Store session information (in production, use a proper session store)
	p.oauthSessions.Store(sessionID, &OAuthSession{
		ID:          sessionID,
		UserContext: userContext,
		CreatedAt:   time.Now(),
		ExpiresAt:   expiresAt,
	})

	// Return session information
	return c.JSON(http.StatusOK, OAuthSessionResponse{
		SessionID:   sessionID,
		AccessToken: userContext.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		User:        userContext,
	})
}

// handleOAuthLogout handles OAuth logout
func (p *Proxy) handleOAuthLogout(c echo.Context) error {
	// Get the session ID from the Authorization header or query parameter
	sessionID := c.Request().Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = c.QueryParam("session_id")
	}

	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID required")
	}

	// Get session from store
	sessionValue, ok := p.oauthSessions.Load(sessionID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	session := sessionValue.(*OAuthSession)

	// Revoke the GitHub token
	if err := p.oauthProvider.RevokeToken(c.Request().Context(), session.UserContext.AccessToken); err != nil {
		log.Printf("Failed to revoke GitHub token: %v", err)
		// Continue with logout even if revocation fails
	}

	// Remove session from store
	p.oauthSessions.Delete(sessionID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "Successfully logged out",
	})
}

// handleOAuthRefresh handles token refresh (if needed in the future)
func (p *Proxy) handleOAuthRefresh(c echo.Context) error {
	// GitHub OAuth tokens don't expire, so this is a placeholder
	// In a real implementation, you might want to validate the token is still valid
	sessionID := c.Request().Header.Get("X-Session-ID")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID required")
	}

	sessionValue, ok := p.oauthSessions.Load(sessionID)
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	session := sessionValue.(*OAuthSession)

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		p.oauthSessions.Delete(sessionID)
		return echo.NewHTTPError(http.StatusUnauthorized, "Session expired")
	}

	// Extend session expiration
	session.ExpiresAt = time.Now().Add(24 * time.Hour)

	return c.JSON(http.StatusOK, OAuthTokenResponse{
		AccessToken: session.UserContext.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   session.ExpiresAt,
		User:        session.UserContext,
	})
}

// OAuthSession represents an authenticated OAuth session
type OAuthSession struct {
	ID          string
	UserContext *auth.UserContext
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// isAllowedRedirectURI validates if the redirect URI is in the allowed list
func isAllowedRedirectURI(redirectURI string) bool {
	// Get allowed redirect URIs from environment variable
	allowedURIs := os.Getenv("OAUTH_ALLOWED_REDIRECT_URIS")
	if allowedURIs == "" {
		// Fallback to localhost for development
		allowedDefaultURIs := []string{
			"http://localhost",
			"https://localhost",
			"http://127.0.0.1",
			"https://127.0.0.1",
		}
		for _, allowed := range allowedDefaultURIs {
			if strings.HasPrefix(redirectURI, allowed) {
				return true
			}
		}
		return false
	}

	// Parse comma-separated allowed URIs
	uris := strings.Split(allowedURIs, ",")
	for _, allowed := range uris {
		allowed = strings.TrimSpace(allowed)
		if allowed == redirectURI {
			return true
		}
		// Also allow prefix match for same domain with different paths
		if strings.HasPrefix(redirectURI, allowed) {
			return true
		}
	}

	return false
}

// validateOAuthSession validates an OAuth session from the request
func (p *Proxy) validateOAuthSession(c echo.Context) (*auth.UserContext, error) {
	// Try to get session ID from header first, then from query parameter
	sessionID := c.Request().Header.Get("X-Session-ID")
	if sessionID == "" {
		sessionID = c.QueryParam("session_id")
	}

	if sessionID == "" {
		// Try to extract from Authorization header as Bearer token
		authHeader := c.Request().Header.Get("Authorization")
		if authHeader != "" && len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			sessionID = authHeader[7:]
		}
	}

	if sessionID == "" {
		return nil, fmt.Errorf("no session ID provided")
	}

	// Get session from store
	sessionValue, ok := p.oauthSessions.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found")
	}

	session := sessionValue.(*OAuthSession)

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		p.oauthSessions.Delete(sessionID)
		return nil, fmt.Errorf("session expired")
	}

	return session.UserContext, nil
}

// cleanupExpiredOAuthSessions periodically cleans up expired OAuth sessions
func (p *Proxy) cleanupExpiredOAuthSessions() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		var toDelete []string

		p.oauthSessions.Range(func(key, value interface{}) bool {
			sessionID := key.(string)
			session := value.(*OAuthSession)

			if now.After(session.ExpiresAt) {
				toDelete = append(toDelete, sessionID)
			}
			return true
		})

		for _, sessionID := range toDelete {
			p.oauthSessions.Delete(sessionID)
			log.Printf("Cleaned up expired OAuth session: %s", sessionID)
		}
	}
}
