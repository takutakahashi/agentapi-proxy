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

			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				BaseURL: mockServer.URL,
				UserMapping: config.GitHubUserMapping{
					DefaultRole:        "user",
					DefaultPermissions: []string{"read"},
				},
			})

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

			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				BaseURL: mockServer.URL,
			})

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

			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				BaseURL: mockServer.URL,
			})

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
			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				UserMapping: tt.config,
			})

			role, permissions, _ := provider.mapUserPermissions(tt.teams)
			assert.Equal(t, tt.expectedRole, role)
			assert.Equal(t, tt.expectedPermCount, len(permissions))

			for _, expectedPerm := range tt.expectedPermExists {
				assert.Contains(t, permissions, expectedPerm)
			}
		})
	}
}

func TestGitHubAuthProvider_MapUserPermissionsWithEnvFile(t *testing.T) {
	tests := []struct {
		name            string
		teams           []GitHubTeamMembership
		config          config.GitHubUserMapping
		expectedRole    string
		expectedEnvFile string
	}{
		{
			name: "team with env file",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "dev-team",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/dev-team": {
						Role:        "developer",
						Permissions: []string{"write", "read"},
						EnvFile:     "/etc/agentapi/envs/dev-team.env",
					},
				},
			},
			expectedRole:    "developer",
			expectedEnvFile: "/etc/agentapi/envs/dev-team.env",
		},
		{
			name: "multiple teams with env files - higher role wins",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "dev-team",
					Role:         "member",
				},
				{
					Organization: "test-org",
					TeamSlug:     "admin-team",
					Role:         "maintainer",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/dev-team": {
						Role:        "developer",
						Permissions: []string{"write", "read"},
						EnvFile:     "/etc/agentapi/envs/dev-team.env",
					},
					"test-org/admin-team": {
						Role:        "admin",
						Permissions: []string{"admin", "write", "read"},
						EnvFile:     "/etc/agentapi/envs/admin-team.env",
					},
				},
			},
			expectedRole:    "admin",
			expectedEnvFile: "/etc/agentapi/envs/admin-team.env",
		},
		{
			name: "team without env file",
			teams: []GitHubTeamMembership{
				{
					Organization: "test-org",
					TeamSlug:     "basic-team",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"test-org/basic-team": {
						Role:        "member",
						Permissions: []string{"read", "comment"},
					},
				},
			},
			expectedRole:    "member",
			expectedEnvFile: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				UserMapping: tt.config,
			})

			role, _, envFile := provider.mapUserPermissions(tt.teams)
			assert.Equal(t, tt.expectedRole, role)
			assert.Equal(t, tt.expectedEnvFile, envFile)
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

		case "/user/teams":
			teams := []struct {
				ID           int64  `json:"id"`
				Name         string `json:"name"`
				Slug         string `json:"slug"`
				Permission   string `json:"permission"`
				Organization struct {
					Login string `json:"login"`
				} `json:"organization"`
			}{
				{
					ID:         1,
					Name:       "Developers",
					Slug:       "developers",
					Permission: "push",
					Organization: struct {
						Login string `json:"login"`
					}{Login: "test-org"},
				},
			}
			_ = json.NewEncoder(w).Encode(teams)

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

	provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
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
	})

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

func TestGitHubAuthProvider_HasWildcardPatterns(t *testing.T) {
	tests := []struct {
		name        string
		teamMapping map[string]config.TeamRoleRule
		expected    bool
	}{
		{
			name: "no wildcard patterns",
			teamMapping: map[string]config.TeamRoleRule{
				"org/team":   {Role: "developer"},
				"org2/team2": {Role: "admin"},
			},
			expected: false,
		},
		{
			name: "has wildcard pattern",
			teamMapping: map[string]config.TeamRoleRule{
				"org/team":   {Role: "developer"},
				"*/cc-users": {Role: "admin"},
			},
			expected: true,
		},
		{
			name: "only wildcard patterns",
			teamMapping: map[string]config.TeamRoleRule{
				"*/cc-users": {Role: "developer"},
				"*/admins":   {Role: "admin"},
			},
			expected: true,
		},
		{
			name: "team wildcard pattern",
			teamMapping: map[string]config.TeamRoleRule{
				"myorg/*": {Role: "developer"},
			},
			expected: true,
		},
		{
			name: "prefix pattern",
			teamMapping: map[string]config.TeamRoleRule{
				"myorg/backend-*": {Role: "developer"},
			},
			expected: true,
		},
		{
			name: "suffix pattern",
			teamMapping: map[string]config.TeamRoleRule{
				"myorg/*-engineer": {Role: "developer"},
			},
			expected: true,
		},
		{
			name:        "empty mapping",
			teamMapping: map[string]config.TeamRoleRule{},
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				UserMapping: config.GitHubUserMapping{
					TeamRoleMapping: tt.teamMapping,
				},
			})

			result := provider.hasWildcardPatterns()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		str      string
		expected bool
	}{
		// Exact matches
		{name: "exact match", pattern: "team", str: "team", expected: true},
		{name: "exact no match", pattern: "team", str: "other", expected: false},

		// Full wildcard
		{name: "full wildcard", pattern: "*", str: "anything", expected: true},
		{name: "full wildcard empty", pattern: "*", str: "", expected: true},

		// Prefix patterns
		{name: "prefix match", pattern: "backend-*", str: "backend-team", expected: true},
		{name: "prefix match multiple", pattern: "backend-*", str: "backend-engineers", expected: true},
		{name: "prefix no match", pattern: "backend-*", str: "frontend-team", expected: false},
		{name: "prefix no match missing dash", pattern: "backend-*", str: "backend", expected: false},

		// Suffix patterns
		{name: "suffix match", pattern: "*-engineer", str: "backend-engineer", expected: true},
		{name: "suffix match multiple", pattern: "*-engineer", str: "frontend-engineer", expected: true},
		{name: "suffix no match", pattern: "*-engineer", str: "backend-team", expected: false},
		{name: "suffix no match missing dash", pattern: "*-engineer", str: "engineer", expected: false},

		// Edge cases
		{name: "empty pattern no match", pattern: "", str: "team", expected: false},
		{name: "empty string exact match", pattern: "", str: "", expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.str)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchTeamPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		org      string
		teamSlug string
		expected bool
	}{
		// Exact matches
		{name: "exact match", pattern: "myorg/myteam", org: "myorg", teamSlug: "myteam", expected: true},
		{name: "exact no match org", pattern: "myorg/myteam", org: "otherorg", teamSlug: "myteam", expected: false},
		{name: "exact no match team", pattern: "myorg/myteam", org: "myorg", teamSlug: "otherteam", expected: false},

		// Org wildcard
		{name: "org wildcard match", pattern: "*/myteam", org: "anyorg", teamSlug: "myteam", expected: true},
		{name: "org wildcard no match", pattern: "*/myteam", org: "anyorg", teamSlug: "otherteam", expected: false},

		// Team wildcard
		{name: "team wildcard match", pattern: "myorg/*", org: "myorg", teamSlug: "anyteam", expected: true},
		{name: "team wildcard no match", pattern: "myorg/*", org: "otherorg", teamSlug: "anyteam", expected: false},

		// Both wildcards
		{name: "both wildcards match", pattern: "*/*", org: "anyorg", teamSlug: "anyteam", expected: true},

		// Prefix patterns
		{name: "team prefix match", pattern: "myorg/backend-*", org: "myorg", teamSlug: "backend-team", expected: true},
		{name: "team prefix no match", pattern: "myorg/backend-*", org: "myorg", teamSlug: "frontend-team", expected: false},

		// Suffix patterns
		{name: "team suffix match", pattern: "myorg/*-engineer", org: "myorg", teamSlug: "backend-engineer", expected: true},
		{name: "team suffix match multiple", pattern: "myorg/*-engineer", org: "myorg", teamSlug: "frontend-engineer", expected: true},
		{name: "team suffix no match", pattern: "myorg/*-engineer", org: "myorg", teamSlug: "backend-team", expected: false},

		// Combined patterns
		{name: "org wildcard team suffix", pattern: "*/*-engineer", org: "anyorg", teamSlug: "backend-engineer", expected: true},
		{name: "org wildcard team prefix", pattern: "*/backend-*", org: "anyorg", teamSlug: "backend-team", expected: true},

		// Invalid patterns
		{name: "invalid pattern no slash", pattern: "invalid", org: "org", teamSlug: "team", expected: false},
		{name: "invalid pattern multiple slashes", pattern: "a/b/c", org: "org", teamSlug: "team", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchTeamPattern(tt.pattern, tt.org, tt.teamSlug)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGitHubAuthProvider_MapUserPermissionsWithWildcard(t *testing.T) {
	tests := []struct {
		name               string
		teams              []GitHubTeamMembership
		config             config.GitHubUserMapping
		expectedRole       string
		expectedPermCount  int
		expectedPermExists []string
	}{
		{
			name: "wildcard pattern matches team",
			teams: []GitHubTeamMembership{
				{
					Organization: "org-alpha",
					TeamSlug:     "cc-users",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"*/cc-users": {
						Role:        "developer",
						Permissions: []string{"write", "deploy"},
					},
				},
			},
			expectedRole:       "developer",
			expectedPermCount:  3, // read + write + deploy
			expectedPermExists: []string{"read", "write", "deploy"},
		},
		{
			name: "wildcard pattern matches multiple orgs",
			teams: []GitHubTeamMembership{
				{
					Organization: "org-alpha",
					TeamSlug:     "cc-users",
					Role:         "member",
				},
				{
					Organization: "org-beta",
					TeamSlug:     "cc-users",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"*/cc-users": {
						Role:        "developer",
						Permissions: []string{"write"},
					},
				},
			},
			expectedRole:       "developer",
			expectedPermCount:  2, // read + write
			expectedPermExists: []string{"read", "write"},
		},
		{
			name: "exact match and wildcard both apply - permissions combined",
			teams: []GitHubTeamMembership{
				{
					Organization: "myorg",
					TeamSlug:     "cc-users",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"*/cc-users": {
						Role:        "developer",
						Permissions: []string{"write"},
					},
					"myorg/cc-users": {
						Role:        "admin",
						Permissions: []string{"admin"},
					},
				},
			},
			expectedRole:       "admin",
			expectedPermCount:  3, // read + write + admin (both rules apply)
			expectedPermExists: []string{"read", "write", "admin"},
		},
		{
			name: "highest role wins across multiple wildcard matches",
			teams: []GitHubTeamMembership{
				{
					Organization: "org-a",
					TeamSlug:     "developers",
					Role:         "member",
				},
				{
					Organization: "org-b",
					TeamSlug:     "admins",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "guest",
				DefaultPermissions: []string{},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"*/developers": {
						Role:        "developer",
						Permissions: []string{"write"},
					},
					"*/admins": {
						Role:        "admin",
						Permissions: []string{"admin"},
					},
				},
			},
			expectedRole:       "admin",
			expectedPermCount:  2,
			expectedPermExists: []string{"write", "admin"},
		},
		{
			name: "team suffix pattern matches",
			teams: []GitHubTeamMembership{
				{
					Organization: "myorg",
					TeamSlug:     "backend-engineer",
					Role:         "member",
				},
				{
					Organization: "myorg",
					TeamSlug:     "frontend-engineer",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"myorg/*-engineer": {
						Role:        "developer",
						Permissions: []string{"write", "execute"},
					},
				},
			},
			expectedRole:       "developer",
			expectedPermCount:  3, // read + write + execute
			expectedPermExists: []string{"read", "write", "execute"},
		},
		{
			name: "team prefix pattern matches",
			teams: []GitHubTeamMembership{
				{
					Organization: "myorg",
					TeamSlug:     "backend-team",
					Role:         "member",
				},
				{
					Organization: "myorg",
					TeamSlug:     "backend-developers",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"myorg/backend-*": {
						Role:        "developer",
						Permissions: []string{"write", "debug"},
					},
				},
			},
			expectedRole:       "developer",
			expectedPermCount:  3, // read + write + debug
			expectedPermExists: []string{"read", "write", "debug"},
		},
		{
			name: "team wildcard pattern matches all teams",
			teams: []GitHubTeamMembership{
				{
					Organization: "myorg",
					TeamSlug:     "team-a",
					Role:         "member",
				},
				{
					Organization: "myorg",
					TeamSlug:     "team-b",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"myorg/*": {
						Role:        "member",
						Permissions: []string{"write"},
					},
				},
			},
			expectedRole:       "member",
			expectedPermCount:  2, // read + write
			expectedPermExists: []string{"read", "write"},
		},
		{
			name: "suffix pattern no match",
			teams: []GitHubTeamMembership{
				{
					Organization: "myorg",
					TeamSlug:     "backend-team",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"myorg/*-engineer": {
						Role:        "developer",
						Permissions: []string{"write"},
					},
				},
			},
			expectedRole:       "user",
			expectedPermCount:  1,
			expectedPermExists: []string{"read"},
		},
		{
			name: "prefix pattern no match",
			teams: []GitHubTeamMembership{
				{
					Organization: "myorg",
					TeamSlug:     "frontend-team",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"myorg/backend-*": {
						Role:        "developer",
						Permissions: []string{"write"},
					},
				},
			},
			expectedRole:       "user",
			expectedPermCount:  1,
			expectedPermExists: []string{"read"},
		},
		{
			name: "combined patterns - multiple matches",
			teams: []GitHubTeamMembership{
				{
					Organization: "myorg",
					TeamSlug:     "backend-engineer",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "user",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"myorg/backend-*": {
						Role:        "developer",
						Permissions: []string{"write"},
					},
					"myorg/*-engineer": {
						Role:        "developer",
						Permissions: []string{"execute"},
					},
				},
			},
			expectedRole:       "developer",
			expectedPermCount:  3, // read + write + execute (both patterns match)
			expectedPermExists: []string{"read", "write", "execute"},
		},
		{
			name: "no match - default only",
			teams: []GitHubTeamMembership{
				{
					Organization: "org-a",
					TeamSlug:     "other-team",
					Role:         "member",
				},
			},
			config: config.GitHubUserMapping{
				DefaultRole:        "guest",
				DefaultPermissions: []string{"read"},
				TeamRoleMapping: map[string]config.TeamRoleRule{
					"*/cc-users": {
						Role:        "developer",
						Permissions: []string{"write"},
					},
				},
			},
			expectedRole:       "guest",
			expectedPermCount:  1,
			expectedPermExists: []string{"read"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
				UserMapping: tt.config,
			})

			role, permissions, _ := provider.mapUserPermissions(tt.teams)
			assert.Equal(t, tt.expectedRole, role)
			assert.Equal(t, tt.expectedPermCount, len(permissions))

			for _, expectedPerm := range tt.expectedPermExists {
				assert.Contains(t, permissions, expectedPerm)
			}
		})
	}
}

func TestGitHubAuthProvider_WildcardIntegration(t *testing.T) {
	// Mock GitHub API server with /user/teams endpoint
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "token wildcard-test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/user":
			user := GitHubUserInfo{
				Login: "wildcard-user",
				ID:    123456,
			}
			_ = json.NewEncoder(w).Encode(user)

		case "/user/teams":
			teams := []struct {
				ID           int64  `json:"id"`
				Slug         string `json:"slug"`
				Name         string `json:"name"`
				Permission   string `json:"permission"`
				Organization struct {
					Login string `json:"login"`
				} `json:"organization"`
			}{
				{
					ID:         1,
					Slug:       "cc-users",
					Name:       "CC Users",
					Permission: "push",
					Organization: struct {
						Login string `json:"login"`
					}{Login: "org-alpha"},
				},
				{
					ID:         2,
					Slug:       "cc-users",
					Name:       "CC Users",
					Permission: "push",
					Organization: struct {
						Login string `json:"login"`
					}{Login: "org-beta"},
				},
				{
					ID:         3,
					Slug:       "other-team",
					Name:       "Other Team",
					Permission: "pull",
					Organization: struct {
						Login string `json:"login"`
					}{Login: "org-alpha"},
				},
			}
			_ = json.NewEncoder(w).Encode(teams)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	provider := NewGitHubAuthProvider(&config.GitHubAuthConfig{
		BaseURL: mockServer.URL,
		UserMapping: config.GitHubUserMapping{
			DefaultRole:        "guest",
			DefaultPermissions: []string{},
			TeamRoleMapping: map[string]config.TeamRoleRule{
				"*/cc-users": {
					Role:        "developer",
					Permissions: []string{"read", "write"},
				},
			},
		},
	})

	userCtx, err := provider.Authenticate(context.Background(), "wildcard-test-token")
	require.NoError(t, err)

	assert.Equal(t, "wildcard-user", userCtx.UserID)
	assert.Equal(t, "developer", userCtx.Role)
	assert.Contains(t, userCtx.Permissions, "read")
	assert.Contains(t, userCtx.Permissions, "write")

	// Verify that both org-alpha/cc-users and org-beta/cc-users matched
	assert.Len(t, userCtx.GitHubUser.Teams, 2)
}
