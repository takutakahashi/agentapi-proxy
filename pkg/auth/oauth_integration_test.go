package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// Integration test for the complete GitHub OAuth flow
func TestGitHubOAuthIntegration(t *testing.T) {
	// Mock GitHub API server
	mockGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/authorize":
			// GitHub authorization page - in real flow, user would authorize here
			code := r.URL.Query().Get("client_id")
			state := r.URL.Query().Get("state")
			redirectURI := r.URL.Query().Get("redirect_uri")

			// Simulate user authorization and redirect
			callbackURL, _ := url.Parse(redirectURI)
			q := callbackURL.Query()
			q.Set("code", "test-auth-code-"+code)
			q.Set("state", state)
			callbackURL.RawQuery = q.Encode()

			w.Header().Set("Location", callbackURL.String())
			w.WriteHeader(http.StatusFound)

		case "/login/oauth/access_token":
			// Token exchange endpoint
			_ = r.ParseForm()
			code := r.Form.Get("code")
			clientID := r.Form.Get("client_id")
			clientSecret := r.Form.Get("client_secret")

			if clientID == "test-client-id" && clientSecret == "test-client-secret" && code == "test-auth-code-test-client-id" {
				response := map[string]interface{}{
					"access_token": "gho_integration_test_token",
					"token_type":   "bearer",
					"scope":        "read:user,read:org",
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(response)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
			}

		case "/user":
			authHeader := r.Header.Get("Authorization")
			if authHeader == "token gho_integration_test_token" {
				user := map[string]interface{}{
					"login": "integration-test-user",
					"id":    999999,
					"email": "integration@test.com",
					"name":  "Integration Test User",
				}
				_ = json.NewEncoder(w).Encode(user)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}

		case "/user/orgs":
			authHeader := r.Header.Get("Authorization")
			if authHeader == "token gho_integration_test_token" {
				orgs := []map[string]interface{}{
					{
						"login": "test-org",
						"id":    12345,
					},
				}
				_ = json.NewEncoder(w).Encode(orgs)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}

		case "/orgs/test-org/teams":
			authHeader := r.Header.Get("Authorization")
			if authHeader == "token gho_integration_test_token" {
				teams := []map[string]interface{}{
					{
						"slug": "engineering",
						"name": "Engineering",
					},
					{
						"slug": "admins",
						"name": "Administrators",
					},
				}
				_ = json.NewEncoder(w).Encode(teams)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}

		case "/orgs/test-org/teams/engineering/memberships/integration-test-user":
			authHeader := r.Header.Get("Authorization")
			if authHeader == "token gho_integration_test_token" {
				membership := map[string]interface{}{
					"state": "active",
					"role":  "member",
				}
				_ = json.NewEncoder(w).Encode(membership)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}

		case "/orgs/test-org/teams/admins/memberships/integration-test-user":
			authHeader := r.Header.Get("Authorization")
			if authHeader == "token gho_integration_test_token" {
				membership := map[string]interface{}{
					"state": "active",
					"role":  "maintainer",
				}
				_ = json.NewEncoder(w).Encode(membership)
			} else {
				w.WriteHeader(http.StatusUnauthorized)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockGitHub.Close()

	// Create OAuth provider with test configuration
	githubCfg := &config.GitHubAuthConfig{
		Enabled: true,
		BaseURL: mockGitHub.URL,
		UserMapping: config.GitHubUserMapping{
			DefaultRole:        "user",
			DefaultPermissions: []string{"read"},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"test-org/engineering": {
					Role:        "developer",
					Permissions: []string{"read", "write"},
				},
				"test-org/admins": {
					Role:        "admin",
					Permissions: []string{"admin"},
				},
			},
		},
	}

	oauthCfg := &config.GitHubOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		Scope:        "read:user read:org",
		BaseURL:      mockGitHub.URL,
	}

	// Create providers
	authProvider := NewGitHubAuthProvider(githubCfg)
	oauthProvider := NewGitHubOAuthProvider(oauthCfg, githubCfg)

	t.Run("complete OAuth flow", func(t *testing.T) {
		ctx := context.Background()

		// Step 1: Generate authorization URL
		authURL, state, err := oauthProvider.GenerateAuthURL("http://localhost:8080/oauth/callback")
		require.NoError(t, err)
		require.NotEmpty(t, authURL)
		require.NotEmpty(t, state)

		// Verify the auth URL is properly formatted
		parsedURL, err := url.Parse(authURL)
		require.NoError(t, err)
		assert.Equal(t, "/login/oauth/authorize", parsedURL.Path)
		assert.Equal(t, "test-client-id", parsedURL.Query().Get("client_id"))
		assert.Equal(t, state, parsedURL.Query().Get("state"))

		// Step 2: Exchange authorization code for access token and get user context
		userContext, err := oauthProvider.ExchangeCode(ctx, "test-auth-code-test-client-id", state)
		require.NoError(t, err)
		assert.Equal(t, "integration-test-user", userContext.UserID)
		assert.Equal(t, "integration@test.com", userContext.GitHubUser.Email)
		assert.Equal(t, "gho_integration_test_token", userContext.AccessToken)

		// Verify permissions from team mappings
		assert.Contains(t, userContext.Permissions, "read")
		assert.Contains(t, userContext.Permissions, "write")
		assert.Contains(t, userContext.Permissions, "admin")
	})

	t.Run("handle OAuth errors", func(t *testing.T) {
		ctx := context.Background()

		// Generate a valid state first
		_, validState, _ := oauthProvider.GenerateAuthURL("http://localhost:8080/oauth/callback")

		// Test invalid code
		_, err := oauthProvider.ExchangeCode(ctx, "invalid-code", validState)
		assert.Error(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "failed to exchange")
		}

		// Test invalid state
		_, err = oauthProvider.ExchangeCode(ctx, "test-auth-code-test-client-id", "invalid-state")
		assert.Error(t, err)

		// Test authentication with invalid token
		_, err = authProvider.Authenticate(ctx, "invalid-token")
		assert.Error(t, err)
	})

	t.Run("concurrent OAuth flows", func(t *testing.T) {
		ctx := context.Background()
		numFlows := 3

		// Run multiple OAuth flows concurrently
		errChan := make(chan error, numFlows)
		userChan := make(chan string, numFlows)

		for i := 0; i < numFlows; i++ {
			go func(flowNum int) {
				// Each flow gets its own state
				_, state, err := oauthProvider.GenerateAuthURL("http://localhost:8080/oauth/callback")
				if err != nil {
					errChan <- err
					return
				}

				// Exchange code and get user context
				userContext, err := oauthProvider.ExchangeCode(ctx, "test-auth-code-test-client-id", state)
				if err != nil {
					errChan <- err
					return
				}

				userChan <- userContext.UserID
				errChan <- nil
			}(i)
		}

		// Collect results
		successCount := 0
		for i := 0; i < numFlows; i++ {
			err := <-errChan
			if err == nil {
				successCount++
				username := <-userChan
				assert.Equal(t, "integration-test-user", username)
			}
		}

		assert.Equal(t, numFlows, successCount, "All concurrent flows should succeed")
	})
}

// Test OAuth flow with various edge cases
func TestGitHubOAuthEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		serverHandler http.HandlerFunc
		expectError   bool
		errorContains string
	}{
		{
			name: "GitHub API timeout",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				// Simulate timeout by not responding
				time.Sleep(100 * time.Millisecond)
				w.WriteHeader(http.StatusGatewayTimeout)
			},
			expectError:   true,
			errorContains: "504",
		},
		{
			name: "GitHub API rate limit",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", "1234567890")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"message": "API rate limit exceeded",
				})
			},
			expectError:   true,
			errorContains: "403",
		},
		{
			name: "Invalid JSON response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{invalid json`))
			},
			expectError:   true,
			errorContains: "invalid character",
		},
		{
			name: "Empty access token response",
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login/oauth/access_token" {
					_ = json.NewEncoder(w).Encode(map[string]string{
						"access_token": "",
					})
				}
			},
			expectError:   true,
			errorContains: "no access token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(tt.serverHandler)
			defer mockServer.Close()

			githubCfg := &config.GitHubAuthConfig{
				Enabled: true,
				BaseURL: mockServer.URL,
			}

			oauthCfg := &config.GitHubOAuthConfig{
				ClientID:     "test-client-id",
				ClientSecret: "test-client-secret",
				BaseURL:      mockServer.URL,
			}

			oauthProvider := NewGitHubOAuthProvider(oauthCfg, githubCfg)

			ctx := context.Background()
			// Generate a valid state first
			_, state, _ := oauthProvider.GenerateAuthURL("http://localhost:8080/oauth/callback")
			_, err := oauthProvider.ExchangeCode(ctx, "test-code", state)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Benchmark OAuth operations
func BenchmarkGitHubOAuth(b *testing.B) {
	// Mock server with minimal overhead
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"bench_token","token_type":"bearer"}`))
		case "/user":
			_, _ = w.Write([]byte(`{"login":"benchuser","id":123}`))
		case "/user/orgs":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	githubCfg := &config.GitHubAuthConfig{
		Enabled: true,
		BaseURL: mockServer.URL,
	}

	oauthCfg := &config.GitHubOAuthConfig{
		ClientID:     "bench-client",
		ClientSecret: "bench-secret",
		BaseURL:      mockServer.URL,
	}

	authProvider := NewGitHubAuthProvider(githubCfg)
	oauthProvider := NewGitHubOAuthProvider(oauthCfg, githubCfg)

	ctx := context.Background()

	b.Run("GenerateAuthURL", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _, _ = oauthProvider.GenerateAuthURL("http://localhost/callback")
		}
	})

	b.Run("ExchangeCode", func(b *testing.B) {
		_, state, _ := oauthProvider.GenerateAuthURL("http://localhost/callback")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = oauthProvider.ExchangeCode(ctx, "bench-code", state)
		}
	})

	b.Run("Authenticate", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = authProvider.Authenticate(ctx, "bench_token")
		}
	})
}
