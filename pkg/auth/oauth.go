package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

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
	// Generate secure random state
	state, err := p.generateState()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate state: %w", err)
	}

	// Store state with expiration (15 minutes)
	p.stateStore.Store(state, &OAuthState{
		State:       state,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
	})

	// Clean up expired states
	p.cleanupExpiredStates()

	// Build authorization URL
	params := url.Values{
		"client_id":    {p.config.ClientID},
		"redirect_uri": {redirectURI},
		"scope":        {p.config.Scope},
		"state":        {state},
	}

	authURL := fmt.Sprintf("%s/login/oauth/authorize?%s",
		strings.TrimSuffix(p.config.BaseURL, "/"),
		params.Encode())

	return authURL, state, nil
}

// ExchangeCode exchanges the authorization code for an access token
func (p *GitHubOAuthProvider) ExchangeCode(ctx context.Context, code, state string) (*UserContext, error) {
	// Verify state
	stateValue, exists := p.stateStore.Load(state)
	if !exists {
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
	tokenURL := fmt.Sprintf("%s/login/oauth/access_token",
		strings.TrimSuffix(p.config.BaseURL, "/"))

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

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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

// RevokeToken revokes a GitHub access token
func (p *GitHubOAuthProvider) RevokeToken(ctx context.Context, token string) error {
	revokeURL := fmt.Sprintf("%s/applications/%s/token",
		strings.TrimSuffix(p.config.BaseURL, "/"),
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

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to revoke token: status %d", resp.StatusCode)
	}

	return nil
}
