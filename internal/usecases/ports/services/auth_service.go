package services

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// AuthService defines the interface for authentication services
type AuthService interface {
	// AuthenticateUser authenticates a user with the given credentials
	AuthenticateUser(ctx context.Context, credentials *Credentials) (*entities.User, error)

	// ValidateAPIKey validates an API key and returns the associated user
	ValidateAPIKey(ctx context.Context, apiKey string) (*entities.User, error)

	// ValidatePermission checks if a user has a specific permission
	ValidatePermission(ctx context.Context, user *entities.User, permission entities.Permission) error

	// GenerateAPIKey generates a new API key for a user
	GenerateAPIKey(ctx context.Context, userID entities.UserID, permissions []entities.Permission) (*APIKey, error)

	// RevokeAPIKey revokes an existing API key
	RevokeAPIKey(ctx context.Context, apiKey string) error

	// RefreshUserInfo refreshes user information from external sources
	RefreshUserInfo(ctx context.Context, user *entities.User) (*entities.User, error)
}

// GitHubAuthService defines the interface for GitHub authentication
type GitHubAuthService interface {
	// AuthenticateWithToken authenticates using a GitHub token
	AuthenticateWithToken(ctx context.Context, token string) (*entities.User, error)

	// GetUserInfo retrieves user information from GitHub
	GetUserInfo(ctx context.Context, token string) (*entities.GitHubUserInfo, error)

	// GetUserTeams retrieves user team memberships from GitHub
	GetUserTeams(ctx context.Context, token string, user *entities.GitHubUserInfo) ([]entities.GitHubTeamMembership, error)

	// GetUserRepositories retrieves user repositories from GitHub
	GetUserRepositories(ctx context.Context, token string) ([]entities.GitHubRepository, error)

	// ValidateGitHubToken validates a GitHub token
	ValidateGitHubToken(ctx context.Context, token string) (bool, error)

	// GenerateOAuthURL generates an OAuth authorization URL
	GenerateOAuthURL(ctx context.Context, redirectURI string) (string, string, error)

	// ExchangeCodeForToken exchanges an OAuth code for an access token
	ExchangeCodeForToken(ctx context.Context, code, state string) (*OAuthToken, error)

	// RevokeOAuthToken revokes an OAuth token
	RevokeOAuthToken(ctx context.Context, token string) error
}

// Credentials represents authentication credentials
type Credentials struct {
	Type     CredentialType
	Username string
	Password string
	Token    string
	APIKey   string
}

// CredentialType represents the type of credentials
type CredentialType string

const (
	CredentialTypePassword CredentialType = "password"
	CredentialTypeToken    CredentialType = "token"
	CredentialTypeAPIKey   CredentialType = "api_key"
	CredentialTypeOAuth    CredentialType = "oauth"
)

// APIKey represents an API key
type APIKey struct {
	Key         string
	UserID      entities.UserID
	Permissions []entities.Permission
	ExpiresAt   *string
	CreatedAt   string
}

// OAuthToken represents an OAuth token
type OAuthToken struct {
	AccessToken string
	TokenType   string
	Scope       string
	ExpiresAt   *string
}
