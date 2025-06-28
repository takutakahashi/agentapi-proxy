package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestNewGitHubAuthProvider(t *testing.T) {
	tests := []struct {
		name   string
		config *config.GitHubAuthConfig
	}{
		{
			name: "valid config",
			config: &config.GitHubAuthConfig{
				Enabled: true,
				BaseURL: "https://api.github.com",
			},
		},
		{
			name:   "nil config",
			config: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubAuthProvider(tt.config)
			assert.NotNil(t, provider)
		})
	}
}

func TestGitHubAuthProvider_Authenticate(t *testing.T) {
	tests := []struct {
		name         string
		token        string
		mockResponse *GitHubUserInfo
		mockStatus   int
		wantUser     *UserContext
		wantErr      bool
	}{
		{
			name:  "successful authentication",
			token: "valid-token",
			mockResponse: &GitHubUserInfo{
				Login: "testuser",
				ID:    12345,
				Email: "test@example.com",
				Name:  "Test User",
			},
			mockStatus: http.StatusOK,
			wantUser: &UserContext{
				UserID:   "testuser",
				AuthType: "github_oauth",
			},
			wantErr: false,
		},
		{
			name:         "unauthorized token",
			token:        "invalid-token",
			mockResponse: nil,
			mockStatus:   http.StatusUnauthorized,
			wantUser:     nil,
			wantErr:      true,
		},
		{
			name:         "empty token",
			token:        "",
			mockResponse: nil,
			mockStatus:   http.StatusUnauthorized,
			wantUser:     nil,
			wantErr:      true,
		},
		{
			name:  "user without email",
			token: "valid-token",
			mockResponse: &GitHubUserInfo{
				Login: "testuser",
				ID:    12345,
				Name:  "Test User",
			},
			mockStatus: http.StatusOK,
			wantUser: &UserContext{
				UserID:   "testuser",
				AuthType: "github_oauth",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					assert.Equal(t, "GET", r.Method)
					// Only check authorization header if token is not empty
					if tt.token != "" {
						assert.Equal(t, "token "+tt.token, r.Header.Get("Authorization"))
					}
					w.WriteHeader(tt.mockStatus)
					if tt.mockResponse != nil {
						_ = json.NewEncoder(w).Encode(tt.mockResponse)
					}
				case "/user/orgs":
					w.WriteHeader(http.StatusOK)
					_ = json.NewEncoder(w).Encode([]GitHubOrganization{})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer mockServer.Close()

			provider := &GitHubAuthProvider{
				config: &config.GitHubAuthConfig{
					BaseURL: mockServer.URL,
					UserMapping: config.GitHubUserMapping{
						DefaultRole:        "user",
						DefaultPermissions: []string{"read"},
					},
				},
				client: &http.Client{},
			}

			user, err := provider.Authenticate(context.Background(), tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantUser.UserID, user.UserID)
				assert.Equal(t, tt.wantUser.AuthType, user.AuthType)
			}
		})
	}
}

func TestGitHubAuthProvider_GetUser(t *testing.T) {
	tests := []struct {
		name         string
		token        string
		mockResponse *GitHubUserInfo
		mockStatus   int
		wantErr      bool
	}{
		{
			name:  "successful request",
			token: "valid-token",
			mockResponse: &GitHubUserInfo{
				Login: "testuser",
				ID:    12345,
				Email: "test@example.com",
				Name:  "Test User",
			},
			mockStatus: http.StatusOK,
			wantErr:    false,
		},
		{
			name:         "unauthorized",
			token:        "invalid-token",
			mockResponse: nil,
			mockStatus:   http.StatusUnauthorized,
			wantErr:      true,
		},
		{
			name:         "server error",
			token:        "valid-token",
			mockResponse: nil,
			mockStatus:   http.StatusInternalServerError,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/user", r.URL.Path)
				assert.Equal(t, "token "+tt.token, r.Header.Get("Authorization"))

				w.WriteHeader(tt.mockStatus)
				if tt.mockResponse != nil {
					_ = json.NewEncoder(w).Encode(tt.mockResponse)
				}
			}))
			defer mockServer.Close()

			provider := &GitHubAuthProvider{
				config: &config.GitHubAuthConfig{
					BaseURL: mockServer.URL,
				},
				client: &http.Client{},
			}

			user, err := provider.getUser(context.Background(), tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.mockResponse.Login, user.Login)
				assert.Equal(t, tt.mockResponse.ID, user.ID)
				assert.Equal(t, tt.mockResponse.Email, user.Email)
			}
		})
	}
}

func TestGitHubAuthProvider_GetUserOrganizations(t *testing.T) {
	mockOrgs := []GitHubOrganization{
		{
			Login: "test-org",
			ID:    123,
		},
		{
			Login: "another-org",
			ID:    456,
		},
	}

	tests := []struct {
		name       string
		token      string
		mockStatus int
		mockOrgs   []GitHubOrganization
		wantErr    bool
	}{
		{
			name:       "successful request",
			token:      "valid-token",
			mockStatus: http.StatusOK,
			mockOrgs:   mockOrgs,
			wantErr:    false,
		},
		{
			name:       "unauthorized",
			token:      "invalid-token",
			mockStatus: http.StatusUnauthorized,
			mockOrgs:   nil,
			wantErr:    true,
		},
		{
			name:       "empty organizations",
			token:      "valid-token",
			mockStatus: http.StatusOK,
			mockOrgs:   []GitHubOrganization{},
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/user/orgs", r.URL.Path)
				assert.Equal(t, "token "+tt.token, r.Header.Get("Authorization"))

				w.WriteHeader(tt.mockStatus)
				if tt.mockOrgs != nil {
					_ = json.NewEncoder(w).Encode(tt.mockOrgs)
				}
			}))
			defer mockServer.Close()

			provider := &GitHubAuthProvider{
				config: &config.GitHubAuthConfig{
					BaseURL: mockServer.URL,
				},
				client: &http.Client{},
			}

			orgs, err := provider.getUserOrganizations(context.Background(), tt.token)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.mockOrgs), len(orgs))
				for i, org := range orgs {
					assert.Equal(t, tt.mockOrgs[i].Login, org.Login)
					assert.Equal(t, tt.mockOrgs[i].ID, org.ID)
				}
			}
		})
	}
}

func TestGitHubAuthProvider_MapUserPermissions(t *testing.T) {
	tests := []struct {
		name               string
		teams              []GitHubTeamMembership
		config             config.GitHubUserMapping
		expectedRole       string
		expectedPermCount  int
		expectedPermExists []string
	}{
		{
			name: "user with admin team",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "admins",
					Role:         "maintainer",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/admins": {
						Role:        "admin",
						Permissions: []string{"admin", "write", "read"},
					},
				},
			},
			expectedRole:       "admin",
			expectedPermCount:  3,
			expectedPermExists: []string{"admin", "write", "read"},
		},
		{
			name:  "user with no team memberships",
			teams: []GitHubTeamMembership{},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
			},
			expectedRole:       "user",
			expectedPermCount:  1,
			expectedPermExists: []string{"read"},
		},
		{
			name: "user with multiple teams",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "developers",
					Role:         "member",
				},
				{
					Organization: "test-org",
					TeamSlug:     "reviewers",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/developers": {
						Role:        "developer",
						Permissions: []string{"write", "read"},
					},
					"test-org/reviewers": {
						Role:        "member",
						Permissions: []string{"review"},
					},
				},
			},
			expectedRole:       "developer",
			expectedPermCount:  3,
			expectedPermExists: []string{"read", "write", "review"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &GitHubAuthProvider{
				config: &config.GitHubAuthConfig{
					UserMapping: tt.config,
				},
				client: &http.Client{},
			}

			role, permissions := provider.mapUserPermissions(tt.teams)
			assert.Equal(t, tt.expectedRole, role)
			assert.Equal(t, tt.expectedPermCount, len(permissions))

			for _, expectedPerm := range tt.expectedPermExists {
				assert.Contains(t, permissions, expectedPerm)
			}
		})
	}
}

func TestExtractTokenFromHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected string
	}{
		{
			name:     "Bearer token",
			header:   "Bearer gho_1234567890abcdef",
			expected: "gho_1234567890abcdef",
		},
		{
			name:     "token prefix",
			header:   "token gho_1234567890abcdef",
			expected: "gho_1234567890abcdef",
		},
		{
			name:     "raw token",
			header:   "gho_1234567890abcdef",
			expected: "gho_1234567890abcdef",
		},
		{
			name:     "empty header",
			header:   "",
			expected: "",
		},
		{
			name:     "whitespace only",
			header:   "   ",
			expected: "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractTokenFromHeader(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGitHubAuthProvider_Integration(t *testing.T) {
	// Mock GitHub API server with full endpoints
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "token integration-test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/user":
			user := GitHubUserInfo{
				Login: "integration-user",
				ID:    999999,
				Email: "integration@test.com",
				Name:  "Integration Test User",
			}
			_ = json.NewEncoder(w).Encode(user)

		case "/user/orgs":
			orgs := []GitHubOrganization{
				{Login: "test-org", ID: 12345},
			}
			_ = json.NewEncoder(w).Encode(orgs)

		case "/orgs/test-org/teams":
			teams := []struct {
				Slug string `json:"slug"`
				Name string `json:"name"`
			}{
				{Slug: "developers", Name: "Developers"},
			}
			_ = json.NewEncoder(w).Encode(teams)

		case "/orgs/test-org/teams/developers/memberships/integration-user":
			membership := struct {
				State string `json:"state"`
				Role  string `json:"role"`
			}{
				State: "active",
				Role:  "member",
			}
			_ = json.NewEncoder(w).Encode(membership)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	provider := &GitHubAuthProvider{
		config: &config.GitHubAuthConfig{
			BaseURL: mockServer.URL,
			UserMapping: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/developers": {
						Role:        "developer",
						Permissions: []string{"write", "read"},
					},
				},
			},
		},
		client: &http.Client{},
	}

	userCtx, err := provider.Authenticate(context.Background(), "integration-test-token")
	require.NoError(t, err)

	assert.Equal(t, "integration-user", userCtx.UserID)
	assert.Equal(t, "developer", userCtx.Role)
	assert.Contains(t, userCtx.Permissions, "read")
	assert.Contains(t, userCtx.Permissions, "write")
	assert.Equal(t, "github_oauth", userCtx.AuthType)
	assert.NotNil(t, userCtx.GitHubUser)
	assert.Equal(t, "integration@test.com", userCtx.GitHubUser.Email)
}
