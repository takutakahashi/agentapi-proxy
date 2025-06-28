package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestGetAuthTypes(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected AuthTypesResponse
	}{
		{
			name: "auth disabled",
			cfg: &config.Config{
				Auth: config.AuthConfig{
					Enabled: false,
				},
			},
			expected: AuthTypesResponse{
				Enabled: false,
				Types:   []AuthType{},
			},
		},
		{
			name: "api key only",
			cfg: &config.Config{
				Auth: config.AuthConfig{
					Enabled: true,
					Static: &config.StaticAuthConfig{
						Enabled: true,
					},
				},
			},
			expected: AuthTypesResponse{
				Enabled: true,
				Types: []AuthType{
					{Type: "api_key", Name: "API Key", Available: true},
				},
			},
		},
		{
			name: "github token only",
			cfg: &config.Config{
				Auth: config.AuthConfig{
					Enabled: true,
					GitHub: &config.GitHubAuthConfig{
						Enabled: true,
					},
				},
			},
			expected: AuthTypesResponse{
				Enabled: true,
				Types: []AuthType{
					{Type: "github", Name: "GitHub", Available: true},
				},
			},
		},
		{
			name: "github oauth",
			cfg: &config.Config{
				Auth: config.AuthConfig{
					Enabled: true,
					GitHub: &config.GitHubAuthConfig{
						Enabled: true,
						OAuth: &config.GitHubOAuthConfig{
							ClientID:     "test-client-id",
							ClientSecret: "test-client-secret",
						},
					},
				},
			},
			expected: AuthTypesResponse{
				Enabled: true,
				Types: []AuthType{
					{Type: "github_oauth", Name: "GitHub OAuth", Available: true},
				},
			},
		},
		{
			name: "all auth types",
			cfg: &config.Config{
				Auth: config.AuthConfig{
					Enabled: true,
					Static: &config.StaticAuthConfig{
						Enabled: true,
					},
					GitHub: &config.GitHubAuthConfig{
						Enabled: true,
						OAuth: &config.GitHubOAuthConfig{
							ClientID:     "test-client-id",
							ClientSecret: "test-client-secret",
						},
					},
				},
			},
			expected: AuthTypesResponse{
				Enabled: true,
				Types: []AuthType{
					{Type: "api_key", Name: "API Key", Available: true},
					{Type: "github_oauth", Name: "GitHub OAuth", Available: true},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/auth/types", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			h := NewAuthInfoHandlers(tt.cfg)
			err := h.GetAuthTypes(c)

			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, rec.Code)

			var response AuthTypesResponse
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, response)
		})
	}
}

func TestGetAuthStatus(t *testing.T) {
	tests := []struct {
		name     string
		user     *auth.UserContext
		expected AuthStatusResponse
	}{
		{
			name: "not authenticated",
			user: nil,
			expected: AuthStatusResponse{
				Authenticated: false,
			},
		},
		{
			name: "authenticated with api key",
			user: &auth.UserContext{
				UserID:      "test-user",
				Role:        "user",
				Permissions: []string{"session:create", "session:list"},
				AuthType:    "api_key",
			},
			expected: AuthStatusResponse{
				Authenticated: true,
				AuthType:      "api_key",
				UserID:        "test-user",
				Role:          "user",
				Permissions:   []string{"session:create", "session:list"},
			},
		},
		{
			name: "authenticated with github oauth",
			user: &auth.UserContext{
				UserID:      "github-user",
				Role:        "admin",
				Permissions: []string{"*"},
				AuthType:    "github_oauth",
				GitHubUser: &auth.GitHubUserInfo{
					Login: "octocat",
					Name:  "The Octocat",
					Email: "octocat@github.com",
				},
			},
			expected: AuthStatusResponse{
				Authenticated: true,
				AuthType:      "github_oauth",
				UserID:        "github-user",
				Role:          "admin",
				Permissions:   []string{"*"},
				GitHubUser: &auth.GitHubUserInfo{
					Login: "octocat",
					Name:  "The Octocat",
					Email: "octocat@github.com",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			if tt.user != nil {
				c.Set("user", tt.user)
			}

			h := NewAuthInfoHandlers(&config.Config{})
			err := h.GetAuthStatus(c)

			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, rec.Code)

			var response AuthStatusResponse
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, response)
		})
	}
}
