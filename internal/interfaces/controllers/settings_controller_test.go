package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// mockSettingsRepository is a mock implementation of SettingsRepository
type mockSettingsRepository struct {
	settings map[string]*entities.Settings
}

func newMockSettingsRepository() *mockSettingsRepository {
	return &mockSettingsRepository{
		settings: make(map[string]*entities.Settings),
	}
}

func (m *mockSettingsRepository) Save(ctx context.Context, settings *entities.Settings) error {
	m.settings[settings.Name()] = settings
	return nil
}

func (m *mockSettingsRepository) FindByName(ctx context.Context, name string) (*entities.Settings, error) {
	if s, ok := m.settings[name]; ok {
		return s, nil
	}
	return nil, &notFoundError{name: name}
}

func (m *mockSettingsRepository) Delete(ctx context.Context, name string) error {
	delete(m.settings, name)
	return nil
}

func (m *mockSettingsRepository) Exists(ctx context.Context, name string) (bool, error) {
	_, ok := m.settings[name]
	return ok, nil
}

func (m *mockSettingsRepository) List(ctx context.Context) ([]*entities.Settings, error) {
	result := make([]*entities.Settings, 0, len(m.settings))
	for _, s := range m.settings {
		result = append(result, s)
	}
	return result, nil
}

type notFoundError struct {
	name string
}

func (e *notFoundError) Error() string {
	return "settings not found: " + e.name
}

func createTestUser(userID string, isAdmin bool) *entities.User {
	user := entities.NewUser(
		entities.UserID(userID),
		entities.UserTypeAPIKey,
		userID,
	)
	if isAdmin {
		_ = user.SetRoles([]entities.Role{entities.RoleAdmin})
	} else {
		_ = user.SetRoles([]entities.Role{entities.RoleUser})
	}
	return user
}

// createTestGitHubUser creates a GitHub user with team memberships for testing
func createTestGitHubUser(userID string, teams []entities.GitHubTeamMembership) *entities.User {
	githubInfo := entities.NewGitHubUserInfo(
		12345,
		userID,
		"Test User",
		"test@example.com",
		"https://example.com/avatar.png",
		"Test Corp",
		"Tokyo",
	)
	user := entities.NewGitHubUser(
		entities.UserID(userID),
		userID,
		"test@example.com",
		githubInfo,
	)
	user.SetGitHubInfo(githubInfo, teams)
	return user
}

func TestValidateTeamSettingsName(t *testing.T) {
	tests := []struct {
		name        string
		user        *entities.User
		settingName string
		wantErr     bool
		errContains string
	}{
		{
			name:        "non-GitHub user bypasses validation",
			user:        createTestUser("test-user", false),
			settingName: "my-team",
			wantErr:     false,
		},
		{
			name:        "GitHub user without teams bypasses validation",
			user:        createTestGitHubUser("test-user", []entities.GitHubTeamMembership{}),
			settingName: "my-team",
			wantErr:     false,
		},
		{
			name: "correct format org/team passes validation",
			user: createTestGitHubUser("test-user", []entities.GitHubTeamMembership{
				{Organization: "myorg", TeamSlug: "myteam", Role: "maintainer"},
			}),
			settingName: "myorg/myteam",
			wantErr:     false,
		},
		{
			name: "incorrect format team-only returns error",
			user: createTestGitHubUser("test-user", []entities.GitHubTeamMembership{
				{Organization: "myorg", TeamSlug: "myteam", Role: "maintainer"},
			}),
			settingName: "myteam",
			wantErr:     true,
			errContains: "myorg/myteam",
		},
		{
			name: "unrelated name passes validation",
			user: createTestGitHubUser("test-user", []entities.GitHubTeamMembership{
				{Organization: "myorg", TeamSlug: "myteam", Role: "maintainer"},
			}),
			settingName: "unrelated-name",
			wantErr:     false,
		},
		{
			name: "case-insensitive matching for team slug",
			user: createTestGitHubUser("test-user", []entities.GitHubTeamMembership{
				{Organization: "MyOrg", TeamSlug: "MyTeam", Role: "maintainer"},
			}),
			settingName: "myteam",
			wantErr:     true,
			errContains: "MyOrg/MyTeam",
		},
		{
			name: "multiple teams - matches first team slug",
			user: createTestGitHubUser("test-user", []entities.GitHubTeamMembership{
				{Organization: "org1", TeamSlug: "team1", Role: "maintainer"},
				{Organization: "org2", TeamSlug: "team2", Role: "member"},
			}),
			settingName: "team1",
			wantErr:     true,
			errContains: "org1/team1",
		},
		{
			name: "multiple teams - matches second team slug",
			user: createTestGitHubUser("test-user", []entities.GitHubTeamMembership{
				{Organization: "org1", TeamSlug: "team1", Role: "maintainer"},
				{Organization: "org2", TeamSlug: "team2", Role: "member"},
			}),
			settingName: "team2",
			wantErr:     true,
			errContains: "org2/team2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockSettingsRepository()
			h := NewSettingsController(repo)

			err := h.validateTeamSettingsName(tt.user, tt.settingName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// createTestGitHubAdminUser creates a GitHub user with admin role and team memberships for testing
func createTestGitHubAdminUser(userID string, teams []entities.GitHubTeamMembership) *entities.User {
	user := createTestGitHubUser(userID, teams)
	_ = user.SetRoles([]entities.Role{entities.RoleAdmin})
	return user
}

func TestUpdateSettings_TeamNameValidation(t *testing.T) {
	tests := []struct {
		name           string
		user           *entities.User
		settingName    string
		expectedStatus int
		errContains    string
	}{
		{
			name: "team slug only format rejected",
			user: createTestGitHubAdminUser("test-user", []entities.GitHubTeamMembership{
				{Organization: "myorg", TeamSlug: "myteam", Role: "maintainer"},
			}),
			settingName:    "myteam",
			expectedStatus: http.StatusBadRequest,
			errContains:    "myorg/myteam",
		},
		{
			name: "org/team format accepted",
			user: createTestGitHubAdminUser("test-user", []entities.GitHubTeamMembership{
				{Organization: "myorg", TeamSlug: "myteam", Role: "maintainer"},
			}),
			settingName:    "myorg/myteam",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockSettingsRepository()
			h := NewSettingsController(repo)

			requestBody := UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled: true,
				},
			}
			body, err := json.Marshal(requestBody)
			require.NoError(t, err)

			e := echo.New()
			req := httptest.NewRequest(http.MethodPut, "/settings/"+tt.settingName, bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("name")
			c.SetParamValues(tt.settingName)
			c.Set("internal_user", tt.user)

			err = h.UpdateSettings(c)

			if tt.expectedStatus == http.StatusOK {
				assert.NoError(t, err)
				assert.Equal(t, http.StatusOK, rec.Code)
			} else {
				require.Error(t, err)
				httpErr, ok := err.(*echo.HTTPError)
				require.True(t, ok)
				assert.Equal(t, tt.expectedStatus, httpErr.Code)
				if tt.errContains != "" {
					assert.Contains(t, httpErr.Message, tt.errContains)
				}
			}
		})
	}
}

func TestUpdateSettings_PreserveExistingCredentials(t *testing.T) {
	tests := []struct {
		name                    string
		existingSettings        *entities.Settings
		requestBody             UpdateSettingsRequest
		expectedAccessKeyID     string
		expectedSecretAccessKey string
		expectedRoleARN         string
		expectedProfile         string
	}{
		{
			name: "preserve all credentials when empty request",
			existingSettings: func() *entities.Settings {
				s := entities.NewSettings("test-user")
				b := entities.NewBedrockSettings(true)
				b.SetAccessKeyID("existing-key-id")
				b.SetSecretAccessKey("existing-secret-key")
				b.SetRoleARN("existing-role-arn")
				b.SetProfile("existing-profile")
				s.SetBedrock(b)
				return s
			}(),
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled: true,
				},
			},
			expectedAccessKeyID:     "existing-key-id",
			expectedSecretAccessKey: "existing-secret-key",
			expectedRoleARN:         "existing-role-arn",
			expectedProfile:         "existing-profile",
		},
		{
			name: "update only access_key_id, preserve others",
			existingSettings: func() *entities.Settings {
				s := entities.NewSettings("test-user")
				b := entities.NewBedrockSettings(true)
				b.SetAccessKeyID("existing-key-id")
				b.SetSecretAccessKey("existing-secret-key")
				b.SetRoleARN("existing-role-arn")
				b.SetProfile("existing-profile")
				s.SetBedrock(b)
				return s
			}(),
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled:     true,
					AccessKeyID: "new-key-id",
				},
			},
			expectedAccessKeyID:     "new-key-id",
			expectedSecretAccessKey: "existing-secret-key",
			expectedRoleARN:         "existing-role-arn",
			expectedProfile:         "existing-profile",
		},
		{
			name: "update only secret_access_key, preserve others",
			existingSettings: func() *entities.Settings {
				s := entities.NewSettings("test-user")
				b := entities.NewBedrockSettings(true)
				b.SetAccessKeyID("existing-key-id")
				b.SetSecretAccessKey("existing-secret-key")
				b.SetRoleARN("existing-role-arn")
				b.SetProfile("existing-profile")
				s.SetBedrock(b)
				return s
			}(),
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled:         true,
					SecretAccessKey: "new-secret-key",
				},
			},
			expectedAccessKeyID:     "existing-key-id",
			expectedSecretAccessKey: "new-secret-key",
			expectedRoleARN:         "existing-role-arn",
			expectedProfile:         "existing-profile",
		},
		{
			name: "update all credentials",
			existingSettings: func() *entities.Settings {
				s := entities.NewSettings("test-user")
				b := entities.NewBedrockSettings(true)
				b.SetAccessKeyID("existing-key-id")
				b.SetSecretAccessKey("existing-secret-key")
				b.SetRoleARN("existing-role-arn")
				b.SetProfile("existing-profile")
				s.SetBedrock(b)
				return s
			}(),
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled:         true,
					AccessKeyID:     "new-key-id",
					SecretAccessKey: "new-secret-key",
					RoleARN:         "new-role-arn",
					Profile:         "new-profile",
				},
			},
			expectedAccessKeyID:     "new-key-id",
			expectedSecretAccessKey: "new-secret-key",
			expectedRoleARN:         "new-role-arn",
			expectedProfile:         "new-profile",
		},
		{
			name:             "new settings with credentials",
			existingSettings: nil,
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled:         true,
					AccessKeyID:     "new-key-id",
					SecretAccessKey: "new-secret-key",
				},
			},
			expectedAccessKeyID:     "new-key-id",
			expectedSecretAccessKey: "new-secret-key",
			expectedRoleARN:         "",
			expectedProfile:         "",
		},
		{
			name:             "new settings with empty credentials",
			existingSettings: nil,
			requestBody: UpdateSettingsRequest{
				Bedrock: &BedrockSettingsRequest{
					Enabled: true,
				},
			},
			expectedAccessKeyID:     "",
			expectedSecretAccessKey: "",
			expectedRoleARN:         "",
			expectedProfile:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockSettingsRepository()
			if tt.existingSettings != nil {
				err := repo.Save(context.Background(), tt.existingSettings)
				require.NoError(t, err)
			}

			h := NewSettingsController(repo)

			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			e := echo.New()
			req := httptest.NewRequest(http.MethodPut, "/settings/test-user", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("name")
			c.SetParamValues("test-user")

			user := createTestUser("test-user", true)
			c.Set("internal_user", user)

			err = h.UpdateSettings(c)
			assert.NoError(t, err)
			assert.Equal(t, http.StatusOK, rec.Code)

			savedSettings, err := repo.FindByName(context.Background(), "test-user")
			require.NoError(t, err)

			bedrock := savedSettings.Bedrock()
			require.NotNil(t, bedrock)
			assert.Equal(t, tt.expectedAccessKeyID, bedrock.AccessKeyID())
			assert.Equal(t, tt.expectedSecretAccessKey, bedrock.SecretAccessKey())
			assert.Equal(t, tt.expectedRoleARN, bedrock.RoleARN())
			assert.Equal(t, tt.expectedProfile, bedrock.Profile())
		})
	}
}
