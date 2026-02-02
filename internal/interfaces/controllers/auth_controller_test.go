package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/auth"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// MockAuthPresenter implements AuthPresenter for testing
type MockAuthPresenter struct {
	lastError      string
	lastStatusCode int
	authResponse   *auth.AuthenticateUserResponse
	ghAuthResponse *auth.GitHubAuthenticateResponse
	validResponse  *auth.ValidateAPIKeyResponse
	logoutCalled   bool
}

func (m *MockAuthPresenter) PresentAuthentication(w http.ResponseWriter, response *auth.AuthenticateUserResponse) {
	m.authResponse = response
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *MockAuthPresenter) PresentGitHubAuthentication(w http.ResponseWriter, response *auth.GitHubAuthenticateResponse) {
	m.ghAuthResponse = response
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *MockAuthPresenter) PresentValidation(w http.ResponseWriter, response *auth.ValidateAPIKeyResponse) {
	m.validResponse = response
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *MockAuthPresenter) PresentLogout(w http.ResponseWriter) {
	m.logoutCalled = true
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *MockAuthPresenter) PresentError(w http.ResponseWriter, message string, statusCode int) {
	m.lastError = message
	m.lastStatusCode = statusCode
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// MockUserRepository implements UserRepository for testing
type MockUserRepository struct {
	users     map[entities.UserID]*entities.User
	saveErr   error
	updateErr error
	findErr   error
}

func NewMockUserRepository() *MockUserRepository {
	return &MockUserRepository{
		users: make(map[entities.UserID]*entities.User),
	}
}

func (m *MockUserRepository) FindByID(ctx context.Context, id entities.UserID) (*entities.User, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	if user, ok := m.users[id]; ok {
		return user, nil
	}
	return nil, errors.New("user not found")
}

func (m *MockUserRepository) Save(ctx context.Context, user *entities.User) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.users[user.ID()] = user
	return nil
}

func (m *MockUserRepository) Update(ctx context.Context, user *entities.User) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.users[user.ID()] = user
	return nil
}

func (m *MockUserRepository) Delete(ctx context.Context, id entities.UserID) error {
	delete(m.users, id)
	return nil
}

func (m *MockUserRepository) FindByUsername(ctx context.Context, username string) (*entities.User, error) {
	for _, user := range m.users {
		if user.Username() == username {
			return user, nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *MockUserRepository) FindAll(ctx context.Context) ([]*entities.User, error) {
	users := make([]*entities.User, 0, len(m.users))
	for _, user := range m.users {
		users = append(users, user)
	}
	return users, nil
}

func (m *MockUserRepository) FindByEmail(ctx context.Context, email string) (*entities.User, error) {
	for _, user := range m.users {
		if user.Email() != nil && *user.Email() == email {
			return user, nil
		}
	}
	return nil, errors.New("user not found")
}

func (m *MockUserRepository) FindByGitHubID(ctx context.Context, githubID int) (*entities.User, error) {
	return nil, errors.New("user not found")
}

func (m *MockUserRepository) FindByStatus(ctx context.Context, status entities.UserStatus) ([]*entities.User, error) {
	var result []*entities.User
	for _, user := range m.users {
		if user.Status() == status {
			result = append(result, user)
		}
	}
	return result, nil
}

func (m *MockUserRepository) FindByType(ctx context.Context, userType entities.UserType) ([]*entities.User, error) {
	var result []*entities.User
	for _, user := range m.users {
		if user.Type() == userType {
			result = append(result, user)
		}
	}
	return result, nil
}

func (m *MockUserRepository) CountByStatus(ctx context.Context, status entities.UserStatus) (int, error) {
	count := 0
	for _, user := range m.users {
		if user.Status() == status {
			count++
		}
	}
	return count, nil
}

func (m *MockUserRepository) Exists(ctx context.Context, id entities.UserID) (bool, error) {
	_, ok := m.users[id]
	return ok, nil
}

func (m *MockUserRepository) Count(ctx context.Context) (int, error) {
	return len(m.users), nil
}

func (m *MockUserRepository) FindWithFilters(ctx context.Context, filters repositories.UserFilters) ([]*entities.User, error) {
	return m.FindAll(ctx)
}

// MockAuthService implements AuthService for testing
type MockAuthService struct {
	authenticateUser func(ctx context.Context, credentials *services.Credentials) (*entities.User, error)
	validateAPIKey   func(ctx context.Context, apiKey string) (*entities.User, error)
	generateAPIKey   func(ctx context.Context, userID entities.UserID, permissions []entities.Permission) (*services.APIKey, error)
}

func (m *MockAuthService) AuthenticateUser(ctx context.Context, credentials *services.Credentials) (*entities.User, error) {
	if m.authenticateUser != nil {
		return m.authenticateUser(ctx, credentials)
	}
	return entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser"), nil
}

func (m *MockAuthService) ValidateAPIKey(ctx context.Context, apiKey string) (*entities.User, error) {
	if m.validateAPIKey != nil {
		return m.validateAPIKey(ctx, apiKey)
	}
	return entities.NewUser("user_123", entities.UserTypeAPIKey, "testuser"), nil
}

func (m *MockAuthService) ValidatePermission(ctx context.Context, user *entities.User, permission entities.Permission) error {
	return nil
}

func (m *MockAuthService) GenerateAPIKey(ctx context.Context, userID entities.UserID, permissions []entities.Permission) (*services.APIKey, error) {
	if m.generateAPIKey != nil {
		return m.generateAPIKey(ctx, userID, permissions)
	}
	return &services.APIKey{
		Key:         "test_api_key",
		UserID:      userID,
		Permissions: permissions,
	}, nil
}

func (m *MockAuthService) RevokeAPIKey(ctx context.Context, apiKey string) error {
	return nil
}

func (m *MockAuthService) RefreshUserInfo(ctx context.Context, user *entities.User) (*entities.User, error) {
	return user, nil
}

// MockGitHubAuthService implements GitHubAuthService for testing
type MockGitHubAuthService struct {
	authenticateWithToken func(ctx context.Context, token string) (*entities.User, error)
	getUserInfo           func(ctx context.Context, token string) (*entities.GitHubUserInfo, error)
	getUserTeams          func(ctx context.Context, token string, user *entities.GitHubUserInfo) ([]entities.GitHubTeamMembership, error)
	getUserRepositories   func(ctx context.Context, token string) ([]entities.GitHubRepository, error)
}

func (m *MockGitHubAuthService) AuthenticateWithToken(ctx context.Context, token string) (*entities.User, error) {
	if m.authenticateWithToken != nil {
		return m.authenticateWithToken(ctx, token)
	}
	return entities.NewUser("gh_user_123", entities.UserTypeGitHub, "ghuser"), nil
}

func (m *MockGitHubAuthService) GetUserInfo(ctx context.Context, token string) (*entities.GitHubUserInfo, error) {
	if m.getUserInfo != nil {
		return m.getUserInfo(ctx, token)
	}
	return entities.NewGitHubUserInfo(123, "ghuser", "GitHub User", "gh@example.com", "https://avatar.example.com", "Example Corp", "Tokyo"), nil
}

func (m *MockGitHubAuthService) GetUserTeams(ctx context.Context, token string, user *entities.GitHubUserInfo) ([]entities.GitHubTeamMembership, error) {
	if m.getUserTeams != nil {
		return m.getUserTeams(ctx, token, user)
	}
	return []entities.GitHubTeamMembership{}, nil
}

func (m *MockGitHubAuthService) GetUserRepositories(ctx context.Context, token string) ([]entities.GitHubRepository, error) {
	if m.getUserRepositories != nil {
		return m.getUserRepositories(ctx, token)
	}
	return []entities.GitHubRepository{}, nil
}

func (m *MockGitHubAuthService) ValidateGitHubToken(ctx context.Context, token string) (bool, error) {
	return true, nil
}

func (m *MockGitHubAuthService) GenerateOAuthURL(ctx context.Context, redirectURI string) (string, string, error) {
	return "https://github.com/oauth", "state123", nil
}

func (m *MockGitHubAuthService) ExchangeCodeForToken(ctx context.Context, code, state string) (*services.OAuthToken, error) {
	return &services.OAuthToken{
		AccessToken: "test_token",
		TokenType:   "Bearer",
	}, nil
}

func (m *MockGitHubAuthService) RevokeOAuthToken(ctx context.Context, token string) error {
	return nil
}

func TestNewAuthController(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)
	presenter := &MockAuthPresenter{}

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	assert.NotNil(t, controller)
}

func TestAuthController_Login_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	reqBody := LoginRequest{
		Type:     "password",
		Username: "testuser",
		Password: "testpass",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.Login(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, presenter.authResponse)
}

func TestAuthController_Login_InvalidRequestBody(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.Login(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid request body", presenter.lastError)
}

func TestAuthController_Login_AuthenticationFailed(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{
		authenticateUser: func(ctx context.Context, credentials *services.Credentials) (*entities.User, error) {
			return nil, errors.New("authentication failed")
		},
	}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	reqBody := LoginRequest{
		Type:     "password",
		Username: "testuser",
		Password: "wrongpass",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.Login(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "authentication failed", presenter.lastError)
}

func TestAuthController_GitHubLogin_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	ghAuthenticateUC := auth.NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, ghAuthenticateUC, validatePermUC, presenter)

	reqBody := GitHubLoginRequest{
		Token: "gh_test_token",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/auth/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.GitHubLogin(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, presenter.ghAuthResponse)
}

func TestAuthController_GitHubLogin_InvalidRequestBody(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	ghAuthSvc := &MockGitHubAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	ghAuthenticateUC := auth.NewGitHubAuthenticateUseCase(userRepo, authSvc, ghAuthSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, ghAuthenticateUC, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/github", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.GitHubLogin(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid request body", presenter.lastError)
}

func TestAuthController_ValidateAPIKey_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/validate", nil)
	req.Header.Set("Authorization", "Bearer test_api_key")
	w := httptest.NewRecorder()

	controller.ValidateAPIKey(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotNil(t, presenter.validResponse)
}

func TestAuthController_ValidateAPIKey_MissingAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/validate", nil)
	w := httptest.NewRecorder()

	controller.ValidateAPIKey(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "API key is required", presenter.lastError)
}

func TestAuthController_Logout(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	authenticateUC := auth.NewAuthenticateUserUseCase(userRepo, authSvc)
	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)
	validatePermUC := auth.NewValidatePermissionUseCase(authSvc)

	controller := NewAuthController(authenticateUC, validateUC, nil, validatePermUC, presenter)

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	w := httptest.NewRecorder()

	controller.Logout(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, presenter.logoutCalled)
}

func TestExtractAPIKeyFromHeader(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		expected   string
	}{
		{
			name:       "Bearer token",
			authHeader: "Bearer test_api_key_123",
			expected:   "test_api_key_123",
		},
		{
			name:       "API-Key header",
			authHeader: "API-Key my_api_key",
			expected:   "my_api_key",
		},
		{
			name:       "Raw value",
			authHeader: "raw_key_value",
			expected:   "raw_key_value",
		},
		{
			name:       "Empty header",
			authHeader: "",
			expected:   "",
		},
		{
			name:       "Short Bearer",
			authHeader: "Bearer",
			expected:   "Bearer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			result := extractAPIKeyFromHeader(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewAuthMiddleware(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)

	middleware := NewAuthMiddleware(validateUC, presenter)

	assert.NotNil(t, middleware)
}

func TestAuthMiddleware_Authenticate_Success(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)

	middleware := NewAuthMiddleware(validateUC, presenter)

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer valid_api_key")
	w := httptest.NewRecorder()

	middleware.Authenticate(nextHandler).ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_Authenticate_MissingAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{}
	presenter := &MockAuthPresenter{}

	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)

	middleware := NewAuthMiddleware(validateUC, presenter)

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	middleware.Authenticate(nextHandler).ServeHTTP(w, req)

	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "authentication required", presenter.lastError)
}

func TestAuthMiddleware_Authenticate_InvalidAPIKey(t *testing.T) {
	userRepo := NewMockUserRepository()
	authSvc := &MockAuthService{
		validateAPIKey: func(ctx context.Context, apiKey string) (*entities.User, error) {
			return nil, errors.New("invalid api key")
		},
	}
	presenter := &MockAuthPresenter{}

	validateUC := auth.NewValidateAPIKeyUseCase(userRepo, authSvc)

	middleware := NewAuthMiddleware(validateUC, presenter)

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid_api_key")
	w := httptest.NewRecorder()

	middleware.Authenticate(nextHandler).ServeHTTP(w, req)

	assert.False(t, nextCalled)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "invalid API key", presenter.lastError)
}
