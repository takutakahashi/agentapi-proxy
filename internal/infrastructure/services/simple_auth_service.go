package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// apiTokenRecord is the in-memory representation of a named API token used
// for fast authentication lookups and immediate revocation.
type apiTokenRecord struct {
	tokenID     string
	userID      entities.UserID
	scope       entities.APITokenScope
	teamID      string
	permissions []entities.Permission
	expiresAt   *time.Time
}

// SimpleAuthService implements AuthService with simple in-memory authentication
type SimpleAuthService struct {
	mu               sync.RWMutex
	apiKeys          map[string]*services.APIKey
	users            map[entities.UserID]*entities.User
	keyToUserID      map[string]entities.UserID
	apiTokens        map[string]*apiTokenRecord // keyed by plaintext secret
	apiTokenRepo     repositories.APITokenRepository
	githubProvider   *auth.GitHubAuthProvider
	githubAuthConfig *config.GitHubAuthConfig
}

// NewSimpleAuthService creates a new SimpleAuthService
func NewSimpleAuthService() *SimpleAuthService {
	return &SimpleAuthService{
		apiKeys:     make(map[string]*services.APIKey),
		users:       make(map[entities.UserID]*entities.User),
		keyToUserID: make(map[string]entities.UserID),
		apiTokens:   make(map[string]*apiTokenRecord),
	}
}

// SetAPITokenRepository wires the named API token repository so the auth
// service can periodically reconcile its in-memory token map against the
// authoritative Kubernetes Secret store. This is what makes revocation of a
// named token eventually consistent across replicas: when a token is deleted
// on one replica, every other replica drops it from its in-memory map on the
// next reconciliation pass. Without this, a replica would keep accepting a
// deleted named token for the lifetime of its process. Legacy static and
// personal API keys live in the separate apiKeys map and are unaffected.
func (s *SimpleAuthService) SetAPITokenRepository(repo repositories.APITokenRepository) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiTokenRepo = repo
}

// ReconcileAPITokens refreshes the in-memory named-token map from the
// repository. Tokens that no longer exist in the store (deleted on another
// replica) are dropped from memory; tokens created on another replica are
// loaded. It is safe to call concurrently with authentication since it holds
// the write lock only while swapping in the freshly built map. The legacy
// apiKeys map is untouched so static/personal API keys remain compatible.
//
// Revocation of a named token is therefore immediate on the replica that
// performs the delete (RevokeAPIToken) and eventually consistent across other
// replicas (bounded by the reconciliation cadence). Global immediate
// revocation across replicas is intentionally NOT claimed.
func (s *SimpleAuthService) ReconcileAPITokens(ctx context.Context) error {
	s.mu.RLock()
	repo := s.apiTokenRepo
	s.mu.RUnlock()
	if repo == nil {
		return nil
	}
	tokens, err := repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("reconcile api tokens: list: %w", err)
	}
	next := make(map[string]*apiTokenRecord, len(tokens))
	for _, t := range tokens {
		next[t.Secret()] = &apiTokenRecord{
			tokenID:     t.ID(),
			userID:      t.UserID(),
			scope:       t.Scope(),
			teamID:      t.TeamID(),
			permissions: t.Permissions(),
			expiresAt:   t.ExpiresAt(),
		}
	}
	s.mu.Lock()
	s.apiTokens = next
	s.mu.Unlock()
	return nil
}

// SetGitHubAuthConfig sets the GitHub authentication configuration.
// If a provider has already been set via SetGitHubProvider, it is preserved.
// Otherwise a new GitHubAuthProvider is created from the config.
func (s *SimpleAuthService) SetGitHubAuthConfig(cfg *config.GitHubAuthConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubAuthConfig = cfg
	if cfg != nil && cfg.Enabled && s.githubProvider == nil {
		s.githubProvider = auth.NewGitHubAuthProvider(cfg)
	}
}

// SetGitHubProvider injects a pre-configured GitHubAuthProvider.
// This allows the caller to supply a provider that already has optional
// dependencies (e.g. TeamMappingRepository) wired in.
func (s *SimpleAuthService) SetGitHubProvider(provider *auth.GitHubAuthProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.githubProvider = provider
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
		// Fall through to named API tokens (new multi-token system).
		return s.validateAPITokenLocked(apiKey)
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

// validateAPITokenLocked looks up a plaintext secret in the named API token
// map and returns a freshly constructed User entity that authenticates as the
// token's identity (personal owner or team service account). It must be
// called with s.mu already held (at least read-locked).
func (s *SimpleAuthService) validateAPITokenLocked(secret string) (*entities.User, error) {
	rec, ok := s.apiTokens[secret]
	if !ok {
		return nil, errors.New("invalid API key")
	}
	if rec.expiresAt != nil && time.Now().After(*rec.expiresAt) {
		return nil, errors.New("API key expired")
	}

	perms := make([]entities.Permission, len(rec.permissions))
	copy(perms, rec.permissions)

	switch rec.scope {
	case entities.APITokenScopeTeam:
		return entities.NewServiceAccountUser(rec.userID, rec.teamID, perms), nil
	default:
		// Personal token authenticates as the owner with the token's
		// explicit permissions.
		user := entities.NewUser(rec.userID, entities.UserTypeRegular, string(rec.userID))
		user.SetPermissions(perms)
		return user, nil
	}
}

// LoadAPIToken registers a named API token into the in-memory auth map so it
// is immediately authenticatable. It is safe to call repeatedly for the same
// secret (the latest token metadata wins).
func (s *SimpleAuthService) LoadAPIToken(ctx context.Context, token *entities.APIToken) error {
	if token == nil {
		return errors.New("token cannot be nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiTokens[token.Secret()] = &apiTokenRecord{
		tokenID:     token.ID(),
		userID:      token.UserID(),
		scope:       token.Scope(),
		teamID:      token.TeamID(),
		permissions: token.Permissions(),
		expiresAt:   token.ExpiresAt(),
	}
	return nil
}

// RevokeAPIToken removes a named API token from the in-memory auth map,
// effecting immediate revocation. It is safe to call for a secret that is not
// present (returns nil).
func (s *SimpleAuthService) RevokeAPIToken(secret string) {
	if secret == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.apiTokens, secret)
}

// IsAPITokenLoaded reports whether the given plaintext secret is currently
// registered as a named API token. Used by tests.
func (s *SimpleAuthService) IsAPITokenLoaded(secret string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.apiTokens[secret]
	return ok
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
			}

			// Check if user already exists
			s.mu.RLock()
			existingUser, exists := s.users[userID]
			s.mu.RUnlock()

			if exists {
				// Update existing user with latest GitHub info, teams, and role
				existingUser.SetGitHubInfo(githubInfo, teams)
				if userContext.Role != "" {
					if err := existingUser.SetRoles([]entities.Role{entities.Role(userContext.Role)}); err != nil {
						log.Printf("[AUTH] Warning: failed to update role %q from GitHub context for existing user %s: %v", userContext.Role, userContext.UserID, err)
					}
				}
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
			// Set teams
			newUser.SetGitHubInfo(githubInfo, teams)

			// Set role from GitHub context (e.g., "admin" for admin-mapped teams)
			if userContext.Role != "" {
				if err := newUser.SetRoles([]entities.Role{entities.Role(userContext.Role)}); err != nil {
					log.Printf("[AUTH] Warning: failed to set role %q from GitHub context for user %s: %v", userContext.Role, userContext.UserID, err)
				}
			}

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

// CreateServiceAccountForTeam creates a service account for a team
func (s *SimpleAuthService) CreateServiceAccountForTeam(ctx context.Context, teamID string, teamConfigRepo repositories.TeamConfigRepository) (*entities.User, *entities.ServiceAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, nil, fmt.Errorf("failed to generate API key: %w", err)
	}
	apiKey := hex.EncodeToString(keyBytes)

	// Generate UserID from team ID
	userID := entities.UserID(fmt.Sprintf("sa-%s", strings.ReplaceAll(teamID, "/", "-")))

	// Default permissions for service accounts
	permissions := []entities.Permission{
		entities.PermissionSessionCreate,
		entities.PermissionSessionRead,
		entities.PermissionSessionUpdate,
		entities.PermissionSessionDelete,
	}

	// Create User entity
	user := entities.NewServiceAccountUser(userID, teamID, permissions)

	// Create ServiceAccount entity
	serviceAccount := entities.NewServiceAccount(teamID, userID, apiKey, permissions)

	// Get or create TeamConfig
	teamConfig, err := teamConfigRepo.FindByTeamID(ctx, teamID)
	if err != nil {
		// TeamConfig doesn't exist, create new one
		teamConfig = entities.NewTeamConfig(teamID, serviceAccount, nil)
	} else {
		// Update existing TeamConfig with service account
		teamConfig.SetServiceAccount(serviceAccount)
	}

	// Save to repository
	if err := teamConfigRepo.Save(ctx, teamConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to save team config: %w", err)
	}

	// Store in memory maps
	s.apiKeys[apiKey] = &services.APIKey{
		Key:         apiKey,
		UserID:      userID,
		Permissions: permissions,
		CreatedAt:   serviceAccount.CreatedAt().Format(time.RFC3339),
		ExpiresAt:   nil, // Service accounts don't expire
	}
	s.keyToUserID[apiKey] = userID
	s.users[userID] = user

	return user, serviceAccount, nil
}

// LoadServiceAccountFromTeamConfig loads a service account from team config into memory
func (s *SimpleAuthService) LoadServiceAccountFromTeamConfig(ctx context.Context, teamConfig *entities.TeamConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	serviceAccount := teamConfig.ServiceAccount()
	if serviceAccount == nil {
		return errors.New("team config does not have a service account")
	}

	// Create User entity
	user := entities.NewServiceAccountUser(
		serviceAccount.UserID(),
		teamConfig.TeamID(),
		serviceAccount.Permissions(),
	)

	// Store in memory maps
	apiKey := serviceAccount.APIKey()
	s.apiKeys[apiKey] = &services.APIKey{
		Key:         apiKey,
		UserID:      serviceAccount.UserID(),
		Permissions: serviceAccount.Permissions(),
		CreatedAt:   serviceAccount.CreatedAt().Format(time.RFC3339),
		ExpiresAt:   nil, // Service accounts don't expire
	}
	s.keyToUserID[apiKey] = serviceAccount.UserID()
	s.users[serviceAccount.UserID()] = user

	return nil
}

// LoadPersonalAPIKey loads a personal API key into memory
func (s *SimpleAuthService) LoadPersonalAPIKey(ctx context.Context, personalAPIKey *entities.PersonalAPIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID := personalAPIKey.UserID()
	apiKey := personalAPIKey.APIKey()

	// Check if user already exists
	user, exists := s.users[userID]
	if !exists {
		// Create a new user entity for this personal API key
		// Default to regular user type with basic permissions
		user = entities.NewUser(
			userID,
			entities.UserTypeRegular,
			string(userID),
		)
		// Add default permissions
		user.AddPermission(entities.PermissionSessionCreate)
		user.AddPermission(entities.PermissionSessionRead)
		user.AddPermission(entities.PermissionSessionUpdate)
		user.AddPermission(entities.PermissionSessionDelete)

		s.users[userID] = user
	}

	// Store API key in memory maps
	s.apiKeys[apiKey] = &services.APIKey{
		Key:         apiKey,
		UserID:      userID,
		Permissions: user.Permissions(),
		CreatedAt:   personalAPIKey.CreatedAt().Format(time.RFC3339),
		ExpiresAt:   nil, // Personal API keys don't expire
	}
	s.keyToUserID[apiKey] = userID

	return nil
}
