package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"strings"
	"sync"
	"time"
)

// SimpleAuthService implements AuthService with simple in-memory authentication
type SimpleAuthService struct {
	mu          sync.RWMutex
	apiKeys     map[string]*services.APIKey
	users       map[entities.UserID]*entities.User
	keyToUserID map[string]entities.UserID
}

// NewSimpleAuthService creates a new SimpleAuthService
func NewSimpleAuthService() *SimpleAuthService {
	return &SimpleAuthService{
		apiKeys:     make(map[string]*services.APIKey),
		users:       make(map[entities.UserID]*entities.User),
		keyToUserID: make(map[string]entities.UserID),
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
	// Simple token authentication for demo
	// In a real implementation, this would validate JWT tokens or similar
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
