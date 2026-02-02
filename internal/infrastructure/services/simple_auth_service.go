package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"strings"
	"sync"
	"time"
)

// SimpleAuthService implements AuthService with simple in-memory authentication
type SimpleAuthService struct {
	mu               sync.RWMutex
	apiKeys          map[string]*services.APIKey
	users            map[entities.UserID]*entities.User
	keyToUserID      map[string]entities.UserID
	githubProvider   *auth.GitHubAuthProvider
	githubAuthConfig *config.GitHubAuthConfig
}

// NewSimpleAuthService creates a new SimpleAuthService
func NewSimpleAuthService() *SimpleAuthService {
	return &SimpleAuthService{
		apiKeys:     make(map[string]*services.APIKey),
		users:       make(map[entities.UserID]*entities.User),
		keyToUserID: make(map[string]entities.UserID),
	}
}

// SetGitHubAuthConfig sets the GitHub authentication configuration
func (s *SimpleAuthService) SetGitHubAuthConfig(cfg *config.GitHubAuthConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubAuthConfig = cfg
	if cfg != nil && cfg.Enabled {
		s.githubProvider = auth.NewGitHubAuthProvider(cfg)
	}
}

// AuthenticateUser authenticates a user with the given credentials
func (s *SimpleAuthService) AuthenticateUser(ctx context.Context, credentials *services.Credentials) (*entities.User, error) {
	if credentials == nil {
		return nil, errors.New("credentials cannot be nil")
	}

	switch credentials.Type {
	case services.CredentialTypePassword:
		return s.authenticateWithPassword(credentials.Username, credentials.Password)
	case services.CredentialTypeToken:
		return s.authenticateWithToken(credentials.Token)
	case services.CredentialTypeAPIKey:
		return s.authenticateWithAPIKey(credentials.APIKey)
	default:
		return nil, fmt.Errorf("unsupported credential type: %s", credentials.Type)
	}
}

// ValidateAPIKey validates an API key and returns the associated user
func (s *SimpleAuthService) ValidateAPIKey(ctx context.Context, apiKey string) (*entities.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if API key exists
	key, exists := s.apiKeys[apiKey]
	if !exists {
		return nil, errors.New("invalid API key")
	}

	// Check if API key is expired
	if key.ExpiresAt != nil {
		expiresAt, err := time.Parse(time.RFC3339, *key.ExpiresAt)
		if err == nil && time.Now().After(expiresAt) {
			return nil, errors.New("API key expired")
		}
	}

	// Get associated user
	userID := s.keyToUserID[apiKey]
	user, exists := s.users[userID]
	if !exists {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// ValidatePermission checks if a user has a specific permission
func (s *SimpleAuthService) ValidatePermission(ctx context.Context, user *entities.User, permission entities.Permission) error {
	if user == nil {
		return errors.New("user cannot be nil")
	}

	// Check if user has the permission
	for _, userPerm := range user.Permissions() {
		if userPerm == permission {
			return nil
		}
	}

	// Check if user is admin (admins have all permissions)
	if user.IsAdmin() {
		return nil
	}

	return fmt.Errorf("user does not have permission: %s", permission)
}

// GenerateAPIKey generates a new API key for a user
func (s *SimpleAuthService) GenerateAPIKey(ctx context.Context, userID entities.UserID, permissions []entities.Permission) (*services.APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate random API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	keyString := hex.EncodeToString(keyBytes)

	// Set expiration (24 hours from now)
	expiresAt := time.Now().Add(24 * time.Hour).Format(time.RFC3339)

	// Create API key
	apiKey := &services.APIKey{
		Key:         keyString,
		UserID:      userID,
		Permissions: permissions,
		ExpiresAt:   &expiresAt,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	// Store API key
	s.apiKeys[keyString] = apiKey
	s.keyToUserID[keyString] = userID

	return apiKey, nil
}

// RevokeAPIKey revokes an existing API key
func (s *SimpleAuthService) RevokeAPIKey(ctx context.Context, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if API key exists
	if _, exists := s.apiKeys[apiKey]; !exists {
		return errors.New("API key not found")
	}

	// Remove API key
	delete(s.apiKeys, apiKey)
	delete(s.keyToUserID, apiKey)

	return nil
}

// RefreshUserInfo refreshes user information from external sources
func (s *SimpleAuthService) RefreshUserInfo(ctx context.Context, user *entities.User) (*entities.User, error) {
	// In a simple implementation, just return the user as-is
	// In a real implementation, this would fetch updated info from external sources
	return user, nil
}

// AddUser adds a user to the service (for testing/demo purposes)
func (s *SimpleAuthService) AddUser(user *entities.User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[user.ID()] = user
}

// authenticateWithPassword authenticates using username and password
func (s *SimpleAuthService) authenticateWithPassword(username, password string) (*entities.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Simple hardcoded authentication for demo
	if username == "admin" && password == "admin" {
		// Create admin user if not exists
		userID := entities.UserID("admin")
		if user, exists := s.users[userID]; exists {
			return user, nil
		}

		// Create new admin user
		adminUser := entities.NewUser(
			userID,
			entities.UserTypeAdmin,
			"admin",
		)

		s.users[userID] = adminUser
		return adminUser, nil
	}

	return nil, errors.New("invalid username or password")
}

// authenticateWithToken authenticates using a token
func (s *SimpleAuthService) authenticateWithToken(token string) (*entities.User, error) {
	// First try GitHub authentication if configured
	s.mu.RLock()
	githubProvider := s.githubProvider
	s.mu.RUnlock()

	if githubProvider != nil {
		// Try to authenticate with GitHub
		userContext, err := githubProvider.Authenticate(context.Background(), token)
		if err == nil && userContext != nil {
			// Convert GitHub user context to entity user
			userID := entities.UserID(userContext.UserID)

			// Convert GitHub user info to entity GitHubUserInfo
			var githubInfo *entities.GitHubUserInfo
			var teams []entities.GitHubTeamMembership
			var repositories []entities.GitHubRepository
			if userContext.GitHubUser != nil {
				githubInfo = entities.NewGitHubUserInfo(
					userContext.GitHubUser.ID,
					userContext.GitHubUser.Login,
					userContext.GitHubUser.Name,
					userContext.GitHubUser.Email,
					"", // avatarURL
					"", // company
					"", // location
				)
				// Convert teams
				for _, t := range userContext.GitHubUser.Teams {
					teams = append(teams, entities.GitHubTeamMembership{
						Organization: t.Organization,
						TeamSlug:     t.TeamSlug,
						TeamName:     t.TeamName,
						Role:         t.Role,
					})
				}
				// Convert repositories
				for _, r := range userContext.GitHubUser.Repositories {
					repositories = append(repositories, entities.GitHubRepository{
						Name:     r.Name,
						FullName: r.FullName,
					})
				}
			}

			// Check if user already exists
			s.mu.RLock()
			existingUser, exists := s.users[userID]
			s.mu.RUnlock()

			if exists {
				// Update existing user with latest GitHub info, teams, and repositories
				existingUser.SetGitHubInfo(githubInfo, teams, repositories)
				return existingUser, nil
			}

			// Skip creating user if GitHubUser is nil (e.g., GitHub App authentication)
			if userContext.GitHubUser == nil {
				// Fall through to simple token auth
				return nil, errors.New("GitHub user info not available")
			}

			// Create new user from GitHub context
			s.mu.Lock()
			defer s.mu.Unlock()

			newUser := entities.NewGitHubUser(
				userID,
				userContext.UserID,
				userContext.GitHubUser.Email,
				githubInfo,
			)
			// Set teams and repositories
			newUser.SetGitHubInfo(githubInfo, teams, repositories)

			// Set permissions from GitHub context
			for _, perm := range userContext.Permissions {
				permission := entities.Permission(perm)
				newUser.AddPermission(permission)
			}

			s.users[userID] = newUser
			return newUser, nil
		}
		// If GitHub auth fails, continue with simple token auth
	}

	// Simple token authentication for demo
	if strings.HasPrefix(token, "user_") {
		userID := entities.UserID(token)

		s.mu.RLock()
		user, exists := s.users[userID]
		s.mu.RUnlock()

		if exists {
			return user, nil
		}

		// Create new user from token
		s.mu.Lock()
		defer s.mu.Unlock()

		newUser := entities.NewUser(
			userID,
			entities.UserTypeRegular,
			string(userID),
		)

		s.users[userID] = newUser
		return newUser, nil
	}

	return nil, errors.New("invalid token")
}

// authenticateWithAPIKey authenticates using an API key
func (s *SimpleAuthService) authenticateWithAPIKey(apiKey string) (*entities.User, error) {
	return s.ValidateAPIKey(context.Background(), apiKey)
}
