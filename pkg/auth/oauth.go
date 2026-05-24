package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
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
	stateStore     OAuthStateStore
	githubProvider *GitHubAuthProvider
}

// NewGitHubOAuthProvider creates a new GitHub OAuth provider.
// provider is the shared GitHubAuthProvider that handles token-based auth after
// the OAuth callback. Sharing the same instance across the application ensures
// a unified cache (userCache, teamCache, teamMappingRepo).
func NewGitHubOAuthProvider(cfg *config.GitHubOAuthConfig, provider *GitHubAuthProvider) *GitHubOAuthProvider {
	return &GitHubOAuthProvider{
		config:         cfg,
		client:         utils.NewDefaultHTTPClient(),
		stateStore:     &memoryOAuthStateStore{},
		githubProvider: provider,
	}
}

// SetStateStore replaces the default in-memory state store with a shared implementation
// (e.g. ConfigMap-backed) to support multi-pod deployments.
func (p *GitHubOAuthProvider) SetStateStore(store OAuthStateStore) {
	p.stateStore = store
}

// memoryOAuthStateStore is the default in-memory OAuthStateStore backed by sync.Map.
type memoryOAuthStateStore struct {
	m sync.Map
}

func (s *memoryOAuthStateStore) Store(_ context.Context, state string, entry *OAuthState) error {
	s.m.Store(state, entry)
	return nil
}

func (s *memoryOAuthStateStore) Load(_ context.Context, state string) (*OAuthState, bool, error) {
	v, ok := s.m.Load(state)
	if !ok {
		return nil, false, nil
	}
	return v.(*OAuthState), true, nil
}

func (s *memoryOAuthStateStore) Delete(_ context.Context, state string) error {
	s.m.Delete(state)
	return nil
}

func (s *memoryOAuthStateStore) Range(_ context.Context, fn func(string, *OAuthState) bool) error {
	s.m.Range(func(k, v interface{}) bool {
		return fn(k.(string), v.(*OAuthState))
	})
	return nil
}

// GenerateAuthURL generates the GitHub OAuth authorization URL
func (p *GitHubOAuthProvider) GenerateAuthURL(redirectURI string) (string, string, error) {
	ctx := context.Background()

	// Generate secure random state
	state, err := p.generateState()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate state: %w", err)
	}

	// Store state with expiration (15 minutes)
	if err := p.stateStore.Store(ctx, state, &OAuthState{
		State:       state,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
	}); err != nil {
		return "", "", fmt.Errorf("failed to store OAuth state: %w", err)
	}

	// Clean up expired states
	p.cleanupExpiredStates(ctx)

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
	// Verify state
	oauthState, exists, err := p.stateStore.Load(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("failed to load OAuth state: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("invalid state parameter")
	}

	// Check if state is expired (15 minutes)
	if time.Since(oauthState.CreatedAt) > 15*time.Minute {
		_ = p.stateStore.Delete(ctx, state)
		return nil, fmt.Errorf("state expired")
	}

	// Remove state after use
	_ = p.stateStore.Delete(ctx, state)

	// Exchange code for token
	token, err := p.exchangeCodeForToken(ctx, code, oauthState.RedirectURI)
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
	defer utils.SafeCloseResponse(resp)

	logVerbose("Token exchange response: %d %s", resp.StatusCode, resp.Status)
	if err := utils.CheckHTTPResponse(resp, tokenURL); err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
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

// cleanupExpiredStates removes expired states from the store
func (p *GitHubOAuthProvider) cleanupExpiredStates(ctx context.Context) {
	now := time.Now()
	var toDelete []string

	_ = p.stateStore.Range(ctx, func(state string, oauthState *OAuthState) bool {
		if now.Sub(oauthState.CreatedAt) > 15*time.Minute {
			toDelete = append(toDelete, state)
		}
		return true
	})

	for _, state := range toDelete {
		_ = p.stateStore.Delete(ctx, state)
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

// getAPIHost returns the GitHub REST API base URL for API calls (e.g. token revocation).
// Unlike getOAuthHost (which returns github.com for OAuth flows), this returns the API
// host (api.github.com or the GHE /api/v3 base).
func (p *GitHubOAuthProvider) getAPIHost() string {
	// Prefer the GitHub auth provider's base URL — it is already the API URL.
	if p.githubProvider != nil && p.githubProvider.config != nil && p.githubProvider.config.BaseURL != "" {
		return strings.TrimSuffix(p.githubProvider.config.BaseURL, "/")
	}

	baseURL := p.config.BaseURL
	if baseURL == "" || baseURL == "https://github.com" {
		return "https://api.github.com"
	}
	if strings.Contains(baseURL, "api.github.com") {
		return "https://api.github.com"
	}
	// GitHub Enterprise or custom host — use as-is
	return strings.TrimSuffix(baseURL, "/")
}

// RevokeToken revokes a GitHub access token
func (p *GitHubOAuthProvider) RevokeToken(ctx context.Context, token string) error {
	apiHost := p.getAPIHost()
	revokeURL := fmt.Sprintf("%s/applications/%s/token",
		strings.TrimSuffix(apiHost, "/"),
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
	defer utils.SafeCloseResponse(resp)

	logVerbose("Token revocation response: %d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to revoke token: status %d", resp.StatusCode)
	}

	return nil
}
