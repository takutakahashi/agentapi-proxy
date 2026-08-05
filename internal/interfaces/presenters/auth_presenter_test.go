package presenters

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/auth"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

func TestNewHTTPAuthPresenter(t *testing.T) {
	presenter := NewHTTPAuthPresenter()
	assert.NotNil(t, presenter)
}

func TestHTTPAuthPresenter_PresentAuthentication(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	user.SetEmail("test@example.com")
	user.SetPermissions([]entities.Permission{entities.PermissionSessionRead})

	response := &auth.AuthenticateUserResponse{
		User: user,
		APIKey: &services.APIKey{
			Key:    "api_key_123",
			UserID: "user_123",
		},
		Permissions: []entities.Permission{entities.PermissionSessionRead},
	}

	w := httptest.NewRecorder()
	presenter.PresentAuthentication(w, response)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result AuthenticationResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.NotNil(t, result.User)
	assert.Equal(t, "api_key_123", result.APIKey)
	assert.Contains(t, result.Permissions, "session:read")
}

func TestHTTPAuthPresenter_PresentAuthentication_WithExpiry(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	expiresAt := "2024-01-01T00:00:00Z"

	response := &auth.AuthenticateUserResponse{
		User: user,
		APIKey: &services.APIKey{
			Key:       "api_key_123",
			UserID:    "user_123",
			ExpiresAt: &expiresAt,
		},
		Permissions: []entities.Permission{},
	}

	w := httptest.NewRecorder()
	presenter.PresentAuthentication(w, response)

	assert.Equal(t, http.StatusOK, w.Code)

	var result AuthenticationResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.NotNil(t, result.ExpiresAt)
	assert.Equal(t, expiresAt, *result.ExpiresAt)
}

func TestHTTPAuthPresenter_PresentGitHubAuthentication(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	user := entities.NewUser("gh_user_123", entities.UserTypeGitHub, "ghuser")
	githubInfo := entities.NewGitHubUserInfo(
		12345,
		"ghuser",
		"GitHub User",
		"gh@example.com",
		"https://avatars.githubusercontent.com/u/12345",
		"Example Corp",
		"Tokyo",
	)

	response := &auth.GitHubAuthenticateResponse{
		User: user,
		APIKey: &services.APIKey{
			Key:    "gh_api_key_123",
			UserID: "gh_user_123",
		},
		Permissions: []entities.Permission{entities.PermissionSessionCreate},
		GitHubInfo:  githubInfo,
	}

	w := httptest.NewRecorder()
	presenter.PresentGitHubAuthentication(w, response)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result GitHubAuthenticationResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.NotNil(t, result.User)
	assert.NotNil(t, result.GitHubInfo)
	assert.Equal(t, "gh_api_key_123", result.APIKey)
	assert.Equal(t, int64(12345), result.GitHubInfo.ID)
	assert.Equal(t, "ghuser", result.GitHubInfo.Login)
}

func TestHTTPAuthPresenter_PresentValidation_Valid(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")

	response := &auth.ValidateAPIKeyResponse{
		Valid:       true,
		User:        user,
		Permissions: []entities.Permission{entities.PermissionSessionRead},
	}

	w := httptest.NewRecorder()
	presenter.PresentValidation(w, response)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result ValidationResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.True(t, result.Valid)
	assert.NotNil(t, result.User)
	assert.Contains(t, result.Permissions, "session:read")
}

func TestHTTPAuthPresenter_PresentValidation_Invalid(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	response := &auth.ValidateAPIKeyResponse{
		Valid: false,
	}

	w := httptest.NewRecorder()
	presenter.PresentValidation(w, response)

	assert.Equal(t, http.StatusOK, w.Code)

	var result ValidationResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Nil(t, result.User)
}

func TestHTTPAuthPresenter_PresentLogout(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	w := httptest.NewRecorder()
	presenter.PresentLogout(w)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result LogoutResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "Successfully logged out", result.Message)
}

func TestHTTPAuthPresenter_PresentError(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		statusCode int
	}{
		{
			name:       "Bad Request",
			message:    "invalid input",
			statusCode: http.StatusBadRequest,
		},
		{
			name:       "Unauthorized",
			message:    "authentication required",
			statusCode: http.StatusUnauthorized,
		},
		{
			name:       "Internal Server Error",
			message:    "something went wrong",
			statusCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			presenter := NewHTTPAuthPresenter()

			w := httptest.NewRecorder()
			presenter.PresentError(w, tt.message, tt.statusCode)

			assert.Equal(t, tt.statusCode, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			var result entities.ErrorResponse
			err := json.Unmarshal(w.Body.Bytes(), &result)
			assert.NoError(t, err)
			assert.Equal(t, tt.message, result.Message)
			assert.Equal(t, http.StatusText(tt.statusCode), result.Error)
		})
	}
}

func TestHTTPAuthPresenter_convertUserToResponse(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	email := "test@example.com"
	user.SetEmail(email)
	user.SetPermissions([]entities.Permission{entities.PermissionSessionRead, entities.PermissionSessionCreate})
	_ = user.SetRoles([]entities.Role{entities.RoleUser, entities.RoleDeveloper})

	result := presenter.convertUserToResponse(user)

	assert.Equal(t, "user_123", result.ID)
	assert.Equal(t, "api_key", result.Type)
	assert.Equal(t, "testuser", result.Username)
	assert.NotNil(t, result.Email)
	assert.Equal(t, email, *result.Email)
	assert.Equal(t, "active", result.Status)
	assert.Contains(t, result.Roles, "user")
	assert.Contains(t, result.Roles, "developer")
	assert.Contains(t, result.Permissions, "session:read")
	assert.Contains(t, result.Permissions, "session:create")
}

func TestHTTPAuthPresenter_convertUserToResponse_WithLastUsed(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	user := entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser")
	user.UpdateLastUsed()

	result := presenter.convertUserToResponse(user)

	assert.NotNil(t, result.LastUsedAt)
	// Verify the format is correct
	_, err := time.Parse("2006-01-02T15:04:05Z07:00", *result.LastUsedAt)
	assert.NoError(t, err)
}

func TestHTTPAuthPresenter_convertGitHubInfoToResponse(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	githubInfo := entities.NewGitHubUserInfo(
		12345,
		"ghuser",
		"GitHub User",
		"gh@example.com",
		"https://avatars.githubusercontent.com/u/12345",
		"Example Corp",
		"Tokyo",
	)

	result := presenter.convertGitHubInfoToResponse(githubInfo)

	assert.NotNil(t, result)
	assert.Equal(t, int64(12345), result.ID)
	assert.Equal(t, "ghuser", result.Login)
	assert.NotNil(t, result.Name)
	assert.Equal(t, "GitHub User", *result.Name)
	assert.NotNil(t, result.Email)
	assert.Equal(t, "gh@example.com", *result.Email)
	assert.Equal(t, "https://avatars.githubusercontent.com/u/12345", result.AvatarURL)
	assert.NotNil(t, result.Company)
	assert.Equal(t, "Example Corp", *result.Company)
	assert.NotNil(t, result.Location)
	assert.Equal(t, "Tokyo", *result.Location)
}

func TestHTTPAuthPresenter_convertGitHubInfoToResponse_Nil(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	result := presenter.convertGitHubInfoToResponse(nil)

	assert.Nil(t, result)
}

func TestHTTPAuthPresenter_convertGitHubInfoToResponse_MinimalInfo(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	// Create with minimal info (empty strings)
	githubInfo := entities.NewGitHubUserInfo(
		12345,
		"ghuser",
		"", // no name
		"", // no email
		"https://avatar.example.com",
		"", // no company
		"", // no location
	)

	result := presenter.convertGitHubInfoToResponse(githubInfo)

	assert.NotNil(t, result)
	assert.Equal(t, int64(12345), result.ID)
	assert.Equal(t, "ghuser", result.Login)
	assert.Nil(t, result.Name)
	assert.Nil(t, result.Email)
	assert.Nil(t, result.Company)
	assert.Nil(t, result.Location)
}

func TestHTTPAuthPresenter_convertRolesToStrings(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	roles := []entities.Role{entities.RoleAdmin, entities.RoleUser, entities.RoleDeveloper}

	result := presenter.convertRolesToStrings(roles)

	assert.Len(t, result, 3)
	assert.Contains(t, result, "admin")
	assert.Contains(t, result, "user")
	assert.Contains(t, result, "developer")
}

func TestHTTPAuthPresenter_convertRolesToStrings_Empty(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	roles := []entities.Role{}

	result := presenter.convertRolesToStrings(roles)

	assert.Len(t, result, 0)
}

func TestHTTPAuthPresenter_convertPermissionsToStrings(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	permissions := []entities.Permission{
		entities.PermissionSessionCreate,
		entities.PermissionSessionRead,
		entities.PermissionAdmin,
	}

	result := presenter.convertPermissionsToStrings(permissions)

	assert.Len(t, result, 3)
	assert.Contains(t, result, "session:create")
	assert.Contains(t, result, "session:read")
	assert.Contains(t, result, "admin")
}

func TestHTTPAuthPresenter_convertPermissionsToStrings_Empty(t *testing.T) {
	presenter := NewHTTPAuthPresenter()

	permissions := []entities.Permission{}

	result := presenter.convertPermissionsToStrings(permissions)

	assert.Len(t, result, 0)
}
