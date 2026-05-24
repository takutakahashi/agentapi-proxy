package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

	provider := NewGitHubOAuthProvider(cfg, NewGitHubAuthProvider(githubCfg))

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
		case "/applications/test-client-id/token":
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
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

	provider := NewGitHubOAuthProvider(cfg, NewGitHubAuthProvider(githubCfg))

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

	provider := NewGitHubOAuthProvider(cfg, NewGitHubAuthProvider(githubCfg))

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

	provider := NewGitHubOAuthProvider(cfg, NewGitHubAuthProvider(githubCfg))

	// Manually create an expired state
	state := "expired-state"
	ctx := context.Background()
	_ = provider.stateStore.Store(ctx, state, &OAuthState{
		State:       state,
		RedirectURI: "http://localhost:3000/callback",
		CreatedAt:   time.Now().Add(-20 * time.Minute), // 20 minutes ago
	})

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
	ctx := context.Background()
	provider := &GitHubOAuthProvider{
		stateStore: &memoryOAuthStateStore{},
	}

	// Add valid state
	validState := "valid-state"
	_ = provider.stateStore.Store(ctx, validState, &OAuthState{
		State:       validState,
		RedirectURI: "http://localhost:3000/callback",
		CreatedAt:   time.Now(),
	})

	// Add expired state
	expiredState := "expired-state"
	_ = provider.stateStore.Store(ctx, expiredState, &OAuthState{
		State:       expiredState,
		RedirectURI: "http://localhost:3000/callback",
		CreatedAt:   time.Now().Add(-20 * time.Minute),
	})

	// Clean up
	provider.cleanupExpiredStates(ctx)

	// Check results
	_, validExists, _ := provider.stateStore.Load(ctx, validState)
	_, expiredExists, _ := provider.stateStore.Load(ctx, expiredState)
	assert.True(t, validExists)
	assert.False(t, expiredExists)
}

func TestGitHubOAuthProvider_RevokeToken(t *testing.T) {
	var capturedMethod, capturedPath, capturedToken string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		if r.Method == http.MethodDelete {
			body, _ := io.ReadAll(r.Body)
			var payload map[string]string
			_ = json.Unmarshal(body, &payload)
			capturedToken = payload["access_token"]
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer mockServer.Close()

	cfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		BaseURL:      "https://github.com",
	}
	githubCfg := &config.GitHubAuthConfig{
		Enabled:     true,
		BaseURL:     mockServer.URL,
		TokenHeader: "Authorization",
	}

	provider := NewGitHubOAuthProvider(cfg, NewGitHubAuthProvider(githubCfg))

	err := provider.RevokeToken(context.Background(), "gho_test_token")

	assert.NoError(t, err)
	assert.Equal(t, http.MethodDelete, capturedMethod)
	assert.Equal(t, "/applications/test-client-id/token", capturedPath)
	assert.Equal(t, "gho_test_token", capturedToken)
}

func TestGitHubOAuthProvider_RevokeToken_APIURLUsed(t *testing.T) {
	// Verify that RevokeToken uses the API host (api.github.com), not the OAuth host (github.com).
	// With GitHubOAuthConfig.BaseURL="https://github.com" and GitHubAuthConfig.BaseURL="https://api.github.com",
	// the revoke request must go to api.github.com.
	var capturedHost string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHost = r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mockServer.Close()

	cfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		BaseURL:      "https://github.com",
	}
	githubCfg := &config.GitHubAuthConfig{
		Enabled:     true,
		BaseURL:     mockServer.URL,
		TokenHeader: "Authorization",
	}

	provider := NewGitHubOAuthProvider(cfg, NewGitHubAuthProvider(githubCfg))
	err := provider.RevokeToken(context.Background(), "gho_token")

	assert.NoError(t, err)
	// The request must have gone to the mock server (the API host), not to github.com
	assert.NotEmpty(t, capturedHost)
}
