package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// isVerboseLoggingEnabled checks if verbose OAuth logging is enabled via environment variable
func isVerboseLoggingEnabled() bool {
	return strings.ToLower(os.Getenv("AGENTAPI_OAUTH_VERBOSE_LOGGING")) == "true" ||
		strings.ToLower(os.Getenv("AGENTAPI_VERBOSE_LOGGING")) == "true"
}

// logVerbose logs a message if verbose logging is enabled
func logVerbose(format string, args ...interface{}) {
	if isVerboseLoggingEnabled() {
		log.Printf("[OAUTH_VERBOSE] "+format, args...)
	}
}

// OAuthState represents a pending OAuth authentication state
type OAuthState struct {
	State       string    `json:"state"`
	RedirectURI string    `json:"redirect_uri"`
	CreatedAt   time.Time `json:"created_at"`
}

// OAuthTokenResponse represents the GitHub OAuth token response
type OAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

// GitHubOAuthProvider handles GitHub OAuth2 authentication flow
type GitHubOAuthProvider struct {
	config         *config.GitHubOAuthConfig
	client         *http.Client
	stateStore     *sync.Map // Thread-safe map for concurrent access
	githubProvider *GitHubAuthProvider
}

// NewGitHubOAuthProvider creates a new GitHub OAuth provider
func NewGitHubOAuthProvider(cfg *config.GitHubOAuthConfig, githubCfg *config.GitHubAuthConfig) *GitHubOAuthProvider {
	return &GitHubOAuthProvider{
		config: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		stateStore:     &sync.Map{},
		githubProvider: NewGitHubAuthProvider(githubCfg),
	}
}

// GenerateAuthURL generates the GitHub OAuth authorization URL
func (p *GitHubOAuthProvider) GenerateAuthURL(redirectURI string) (string, string, error) {
	logVerbose("GenerateAuthURL called with redirectURI: %s", redirectURI)

	// Generate self-contained state (doesn't need storage)
	state, err := p.generateSelfContainedState(redirectURI)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate state: %w", err)
	}

	logVerbose("Generated self-contained state: %s", state)

	// Still keep the old state store for backwards compatibility
	// but also store in the new system for transition
	fallbackState, err := p.generateState()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate fallback state: %w", err)
	}

	oauthState := &OAuthState{
		State:       state, // Store the self-contained state
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
	}
	p.stateStore.Store(state, oauthState)
	p.stateStore.Store(fallbackState, oauthState) // Also store with fallback key
	logVerbose("Stored state in store: %s at %v", state, oauthState.CreatedAt)

	// Clean up expired states
	p.cleanupExpiredStates()

	// Build authorization URL
	params := url.Values{
		"client_id":    {p.config.ClientID},
		"redirect_uri": {redirectURI},
		"scope":        {p.config.Scope},
		"state":        {state},
	}

	// Determine the OAuth host URL based on the base URL
	oauthHost := p.getOAuthHost()
	authURL := fmt.Sprintf("%s/login/oauth/authorize?%s",
		strings.TrimSuffix(oauthHost, "/"),
		params.Encode())

	logVerbose("Generated authorization URL: %s", authURL)
	return authURL, state, nil
}

// ExchangeCode exchanges the authorization code for an access token
func (p *GitHubOAuthProvider) ExchangeCode(ctx context.Context, code, state string) (*UserContext, error) {
	logVerbose("ExchangeCode called with code: %s, state: %s", code, state)

	var redirectURI string

	// First try to validate as self-contained state
	selfContainedRedirectURI, err := p.validateSelfContainedState(state)
	if err == nil {
		logVerbose("Successfully validated self-contained state")
		redirectURI = selfContainedRedirectURI
	} else {
		logVerbose("Self-contained state validation failed: %v, falling back to store lookup", err)

		// Debug: Log all stored states
		logVerbose("Stored states:")
		p.stateStore.Range(func(key, value interface{}) bool {
			storedState := key.(string)
			stateData := value.(*OAuthState)
			logVerbose("  - State: %s, CreatedAt: %v", storedState, stateData.CreatedAt)
			return true
		})

		// Fallback to old state store method
		stateValue, exists := p.stateStore.Load(state)
		if !exists {
			logVerbose("State parameter not found in store: %s", state)
			return nil, fmt.Errorf("invalid state parameter")
		}
		oauthState := stateValue.(*OAuthState)

		// Check if state is expired (15 minutes)
		if time.Since(oauthState.CreatedAt) > 15*time.Minute {
			p.stateStore.Delete(state)
			return nil, fmt.Errorf("state expired")
		}

		// Remove state after use
		p.stateStore.Delete(state)

		redirectURI = oauthState.RedirectURI
	}

	// Exchange code for token
	token, err := p.exchangeCodeForToken(ctx, code, redirectURI)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Use the existing GitHub provider to authenticate with the token
	userContext, err := p.githubProvider.Authenticate(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to authenticate with token: %w", err)
	}

	// Add OAuth-specific metadata
	userContext.AuthType = "github_oauth"
	userContext.AccessToken = token.AccessToken

	return userContext, nil
}

// exchangeCodeForToken exchanges authorization code for access token
func (p *GitHubOAuthProvider) exchangeCodeForToken(ctx context.Context, code, redirectURI string) (*OAuthTokenResponse, error) {
	oauthHost := p.getOAuthHost()
	tokenURL := fmt.Sprintf("%s/login/oauth/access_token",
		strings.TrimSuffix(oauthHost, "/"))

	params := url.Values{
		"client_id":     {p.config.ClientID},
		"client_secret": {p.config.ClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	logVerbose("Making token exchange request: POST %s", tokenURL)
	resp, err := p.client.Do(req)
	if err != nil {
		logVerbose("Token exchange request failed: %v", err)
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	logVerbose("Token exchange response: %d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}

	var tokenResp OAuthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("no access token in response")
	}

	return &tokenResp, nil
}

// generateState generates a secure random state parameter
func (p *GitHubOAuthProvider) generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// generateSelfContainedState generates a self-contained state that includes timestamp and signature
func (p *GitHubOAuthProvider) generateSelfContainedState(redirectURI string) (string, error) {
	// Create state data with timestamp
	timestamp := time.Now().Unix()
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	randomPart := base64.URLEncoding.EncodeToString(randomBytes)

	// Create payload: timestamp:randomPart:redirectURI
	payload := fmt.Sprintf("%d:%s:%s", timestamp, randomPart, redirectURI)

	// Generate HMAC signature using client secret as key
	h := hmac.New(sha256.New, []byte(p.config.ClientSecret))
	h.Write([]byte(payload))
	signature := base64.URLEncoding.EncodeToString(h.Sum(nil))

	// Combine payload and signature
	state := base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", payload, signature)))

	logVerbose("Generated self-contained state: %s", state)
	return state, nil
}

// validateSelfContainedState validates a self-contained state parameter
func (p *GitHubOAuthProvider) validateSelfContainedState(state string) (string, error) {
	// Decode state
	decoded, err := base64.URLEncoding.DecodeString(state)
	if err != nil {
		return "", fmt.Errorf("invalid state encoding: %w", err)
	}

	// Split into payload and signature
	parts := strings.Split(string(decoded), ":")
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid state format")
	}

	// Reconstruct payload and signature
	payload := strings.Join(parts[:3], ":")
	signature := parts[3]

	// Verify signature
	h := hmac.New(sha256.New, []byte(p.config.ClientSecret))
	h.Write([]byte(payload))
	expectedSignature := base64.URLEncoding.EncodeToString(h.Sum(nil))

	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return "", fmt.Errorf("invalid state signature")
	}

	// Parse timestamp
	timestampStr := parts[0]
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp in state: %w", err)
	}

	// Check if state is expired (15 minutes)
	if time.Since(time.Unix(timestamp, 0)) > 15*time.Minute {
		return "", fmt.Errorf("state expired")
	}

	// Return redirect URI
	redirectURI := parts[2]
	logVerbose("Validated self-contained state, redirectURI: %s", redirectURI)
	return redirectURI, nil
}

// cleanupExpiredStates removes expired states from the store
func (p *GitHubOAuthProvider) cleanupExpiredStates() {
	now := time.Now()
	var toDelete []string

	p.stateStore.Range(func(key, value interface{}) bool {
		state := key.(string)
		oauthState := value.(*OAuthState)
		if now.Sub(oauthState.CreatedAt) > 15*time.Minute {
			toDelete = append(toDelete, state)
		}
		return true
	})

	for _, state := range toDelete {
		p.stateStore.Delete(state)
	}
}

// getOAuthHost returns the appropriate OAuth host URL based on the configured base URL
func (p *GitHubOAuthProvider) getOAuthHost() string {
	baseURL := p.config.BaseURL
	if baseURL == "" {
		baseURL = "https://github.com"
	}

	// Convert API URLs to OAuth host URLs
	if strings.Contains(baseURL, "api.github.com") {
		return "https://github.com"
	} else if strings.Contains(baseURL, "/api/v3") {
		// GitHub Enterprise Server format: https://github.enterprise.com/api/v3
		// Extract the host part before /api/v3
		parts := strings.Split(baseURL, "/api/v3")
		if len(parts) > 0 {
			return parts[0]
		}
	}

	// If it's already a GitHub host URL, use it as is
	return baseURL
}

// RevokeToken revokes a GitHub access token
func (p *GitHubOAuthProvider) RevokeToken(ctx context.Context, token string) error {
	oauthHost := p.getOAuthHost()
	revokeURL := fmt.Sprintf("%s/applications/%s/token",
		strings.TrimSuffix(oauthHost, "/"),
		p.config.ClientID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", revokeURL, nil)
	if err != nil {
		return err
	}

	// Use basic auth with client ID and secret
	req.SetBasicAuth(p.config.ClientID, p.config.ClientSecret)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")

	// Add the token to revoke in the body
	body := fmt.Sprintf(`{"access_token":"%s"}`, token)
	req.Body = io.NopCloser(strings.NewReader(body))
	req.ContentLength = int64(len(body))

	logVerbose("Making token revocation request: DELETE %s", revokeURL)
	resp, err := p.client.Do(req)
	if err != nil {
		logVerbose("Token revocation request failed: %v", err)
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	logVerbose("Token revocation response: %d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to revoke token: status %d", resp.StatusCode)
	}

	return nil
}
