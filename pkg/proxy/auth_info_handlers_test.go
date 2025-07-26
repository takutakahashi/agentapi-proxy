package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
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
		user     *entities.User
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
			user: func() *entities.User {
				user := entities.NewUser(
					entities.UserID("test-user"),
					entities.UserTypeAPIKey,
					"test-user",
				)
				user.SetRoles([]entities.Role{entities.RoleUser})
				user.SetPermissions([]entities.Permission{entities.PermissionSessionCreate, entities.PermissionSessionRead})
				return user
			}(),
			expected: AuthStatusResponse{
				Authenticated: true,
				AuthType:      "api_key",
				UserID:        "test-user",
				Role:          "user",
				Permissions:   []string{"session:create", "session:read"},
			},
		},
		{
			name: "authenticated with github oauth",
			user: func() *entities.User {
				githubInfo := entities.NewGitHubUserInfo(0, "octocat", "The Octocat", "octocat@github.com", "", "", "")
				user := entities.NewGitHubUser(
					entities.UserID("github-user"),
					"github-user",
					"octocat@github.com",
					githubInfo,
				)
				user.SetRoles([]entities.Role{entities.RoleAdmin})
				user.SetPermissions([]entities.Permission{entities.PermissionAdmin})
				return user
			}(),
			expected: AuthStatusResponse{
				Authenticated: true,
				AuthType:      "github",
				UserID:        "github-user",
				Role:          "admin",
				Permissions:   []string{"admin"},
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
				c.Set("internal_user", tt.user)
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
