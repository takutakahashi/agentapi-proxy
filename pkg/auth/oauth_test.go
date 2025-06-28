package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestGitHubOAuthProvider_GenerateAuthURL(t *testing.T) {
	cfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scope:        "read:user read:org",
		BaseURL:      "https://github.com",
	}

	githubCfg := &config.GitHubAuthConfig{
		Enabled:     true,
		BaseURL:     "https://api.github.com",
		TokenHeader: "Authorization",
	}

	provider := NewGitHubOAuthProvider(cfg, githubCfg)

	redirectURI := "http://localhost:3000/callback"
	authURL, state, err := provider.GenerateAuthURL(redirectURI)

	assert.NoError(t, err)
	assert.NotEmpty(t, authURL)
	assert.NotEmpty(t, state)
	assert.Contains(t, authURL, "https://github.com/login/oauth/authorize")
	assert.Contains(t, authURL, "client_id=test-client-id")
	assert.Contains(t, authURL, "redirect_uri=http%3A%2F%2Flocalhost%3A3000%2Fcallback")
	assert.Contains(t, authURL, "scope=read%3Auser+read%3Aorg")
	assert.Contains(t, authURL, "state=")
	// State is URL encoded, so we just check that it exists and is not empty
	stateIndex := strings.Index(authURL, "state=")
	assert.True(t, stateIndex > 0)
	stateValue := authURL[stateIndex+6:]
	assert.NotEmpty(t, stateValue)
}

func TestGitHubOAuthProvider_ExchangeCode(t *testing.T) {
	// Mock GitHub OAuth server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			// Mock token exchange response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"gho_test_token","token_type":"bearer","scope":"read:user,read:org"}`))
		case "/user":
			// Mock user API response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"login":"testuser","id":123456,"email":"test@example.com","name":"Test User"}`))
		case "/user/orgs":
			// Mock organizations API response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"login":"test-org","id":789}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	cfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scope:        "read:user read:org",
		BaseURL:      mockServer.URL,
	}

	githubCfg := &config.GitHubAuthConfig{
		Enabled:     true,
		BaseURL:     mockServer.URL,
		TokenHeader: "Authorization",
		UserMapping: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping:    map[string]config.TeamRoleRule{},
		},
	}

	provider := NewGitHubOAuthProvider(cfg, githubCfg)

	// Generate state first
	_, state, err := provider.GenerateAuthURL("http://localhost:3000/callback")
	assert.NoError(t, err)

	// Exchange code
	ctx := context.Background()
	userContext, err := provider.ExchangeCode(ctx, "test-code", state)

	assert.NoError(t, err)
	assert.NotNil(t, userContext)
	assert.Equal(t, "testuser", userContext.UserID)
	assert.Equal(t, "user", userContext.Role)
	assert.Equal(t, "github_oauth", userContext.AuthType)
	assert.Equal(t, "gho_test_token", userContext.AccessToken)
	assert.NotNil(t, userContext.GitHubUser)
	assert.Equal(t, "testuser", userContext.GitHubUser.Login)
}

func TestGitHubOAuthProvider_ExchangeCode_InvalidState(t *testing.T) {
	cfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scope:        "read:user read:org",
		BaseURL:      "https://github.com",
	}

	githubCfg := &config.GitHubAuthConfig{
		Enabled:     true,
		BaseURL:     "https://api.github.com",
		TokenHeader: "Authorization",
	}

	provider := NewGitHubOAuthProvider(cfg, githubCfg)

	ctx := context.Background()
	_, err := provider.ExchangeCode(ctx, "test-code", "invalid-state")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid state parameter")
}

func TestGitHubOAuthProvider_ExchangeCode_ExpiredState(t *testing.T) {
	cfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scope:        "read:user read:org",
		BaseURL:      "https://github.com",
	}

	githubCfg := &config.GitHubAuthConfig{
		Enabled:     true,
		BaseURL:     "https://api.github.com",
		TokenHeader: "Authorization",
	}

	provider := NewGitHubOAuthProvider(cfg, githubCfg)

	// Manually create an expired state
	state := "expired-state"
	provider.stateStore.Store(state, &OAuthState{
		State:       state,
		RedirectURI: "http://localhost:3000/callback",
		CreatedAt:   time.Now().Add(-20 * time.Minute), // 20 minutes ago
	})

	ctx := context.Background()
	_, err := provider.ExchangeCode(ctx, "test-code", state)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "state expired")
}

func TestGitHubOAuthProvider_generateState(t *testing.T) {
	provider := &GitHubOAuthProvider{}

	state1, err1 := provider.generateState()
	state2, err2 := provider.generateState()

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NotEmpty(t, state1)
	assert.NotEmpty(t, state2)
	assert.NotEqual(t, state1, state2) // States should be unique
}

func TestGitHubOAuthProvider_cleanupExpiredStates(t *testing.T) {
	provider := &GitHubOAuthProvider{
		stateStore: &sync.Map{},
	}

	// Add valid state
	validState := "valid-state"
	provider.stateStore.Store(validState, &OAuthState{
		State:       validState,
		RedirectURI: "http://localhost:3000/callback",
		CreatedAt:   time.Now(),
	})

	// Add expired state
	expiredState := "expired-state"
	provider.stateStore.Store(expiredState, &OAuthState{
		State:       expiredState,
		RedirectURI: "http://localhost:3000/callback",
		CreatedAt:   time.Now().Add(-20 * time.Minute),
	})

	// Clean up
	provider.cleanupExpiredStates()

	// Check results
	_, validExists := provider.stateStore.Load(validState)
	_, expiredExists := provider.stateStore.Load(expiredState)
	assert.True(t, validExists)
	assert.False(t, expiredExists)
}
