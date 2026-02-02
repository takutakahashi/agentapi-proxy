package auth

import (
	"context"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// AuthenticateUserUseCase handles user authentication
type AuthenticateUserUseCase struct {
	userRepo    repositories.UserRepository
	authService services.AuthService
}

// NewAuthenticateUserUseCase creates a new AuthenticateUserUseCase
func NewAuthenticateUserUseCase(
	userRepo repositories.UserRepository,
	authService services.AuthService,
) *AuthenticateUserUseCase {
	return &AuthenticateUserUseCase{
		userRepo:    userRepo,
		authService: authService,
	}
}

// AuthenticateUserRequest represents the input for user authentication
type AuthenticateUserRequest struct {
	Credentials *services.Credentials
}

// AuthenticateUserResponse represents the output of user authentication
type AuthenticateUserResponse struct {
	User        *entities.User
	APIKey      *services.APIKey
	Permissions []entities.Permission
}

// Execute authenticates a user with the given credentials
func (uc *AuthenticateUserUseCase) Execute(ctx context.Context, req *AuthenticateUserRequest) (*AuthenticateUserResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Authenticate user with the auth service
	user, err := uc.authService.AuthenticateUser(ctx, req.Credentials)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	// Verify user is active
	if !user.IsActive() {
		return nil, errors.New("user account is not active")
	}

	// Update user's last used timestamp
	user.UpdateLastUsed()

	// Save or update user in repository
	existingUser, err := uc.userRepo.FindByID(ctx, user.ID())
	if err != nil {
		// User doesn't exist, create new
		if err := uc.userRepo.Save(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to save user: %w", err)
		}
	} else {
		// User exists, update
		existingUser.UpdateLastUsed()
		if err := uc.userRepo.Update(ctx, existingUser); err != nil {
			return nil, fmt.Errorf("failed to update user: %w", err)
		}
		user = existingUser
	}

	// Generate API key for the session
	apiKey, err := uc.authService.GenerateAPIKey(ctx, user.ID(), user.Permissions())
	if err != nil {
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	return &AuthenticateUserResponse{
		User:        user,
		APIKey:      apiKey,
		Permissions: user.Permissions(),
	}, nil
}

// validateRequest validates the authenticate user request
func (uc *AuthenticateUserUseCase) validateRequest(req *AuthenticateUserRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.Credentials == nil {
		return errors.New("credentials cannot be nil")
	}

	// Validate credentials based on type
	switch req.Credentials.Type {
	case services.CredentialTypePassword:
		if req.Credentials.Username == "" || req.Credentials.Password == "" {
			return errors.New("username and password are required for password authentication")
		}
	case services.CredentialTypeToken:
		if req.Credentials.Token == "" {
			return errors.New("token is required for token authentication")
		}
	case services.CredentialTypeAPIKey:
		if req.Credentials.APIKey == "" {
			return errors.New("API key is required for API key authentication")
		}
	default:
		return fmt.Errorf("unsupported credential type: %s", req.Credentials.Type)
	}

	return nil
}

// ValidateAPIKeyUseCase handles API key validation
type ValidateAPIKeyUseCase struct {
	userRepo    repositories.UserRepository
	authService services.AuthService
}

// NewValidateAPIKeyUseCase creates a new ValidateAPIKeyUseCase
func NewValidateAPIKeyUseCase(
	userRepo repositories.UserRepository,
	authService services.AuthService,
) *ValidateAPIKeyUseCase {
	return &ValidateAPIKeyUseCase{
		userRepo:    userRepo,
		authService: authService,
	}
}

// ValidateAPIKeyRequest represents the input for API key validation
type ValidateAPIKeyRequest struct {
	APIKey string
}

// ValidateAPIKeyResponse represents the output of API key validation
type ValidateAPIKeyResponse struct {
	User        *entities.User
	Permissions []entities.Permission
	Valid       bool
}

// Execute validates an API key and returns the associated user
func (uc *ValidateAPIKeyUseCase) Execute(ctx context.Context, req *ValidateAPIKeyRequest) (*ValidateAPIKeyResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Validate API key with the auth service
	user, err := uc.authService.ValidateAPIKey(ctx, req.APIKey)
	if err != nil {
		return &ValidateAPIKeyResponse{
			Valid: false,
		}, nil // Don't return error for invalid keys, just mark as invalid
	}

	// Verify user is active
	if !user.IsActive() {
		return &ValidateAPIKeyResponse{
			Valid: false,
		}, nil
	}

	// Update user's last used timestamp
	user.UpdateLastUsed()
	if err := uc.userRepo.Update(ctx, user); err != nil {
		// Log warning but don't fail validation
		fmt.Printf("Warning: failed to update user last used timestamp: %v\n", err)
	}

	return &ValidateAPIKeyResponse{
		User:        user,
		Permissions: user.Permissions(),
		Valid:       true,
	}, nil
}

// validateRequest validates the validate API key request
func (uc *ValidateAPIKeyUseCase) validateRequest(req *ValidateAPIKeyRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.APIKey == "" {
		return errors.New("API key cannot be empty")
	}

	return nil
}

// GitHubAuthenticateUseCase handles GitHub authentication
type GitHubAuthenticateUseCase struct {
	userRepo          repositories.UserRepository
	authService       services.AuthService
	githubAuthService services.GitHubAuthService
}

// NewGitHubAuthenticateUseCase creates a new GitHubAuthenticateUseCase
func NewGitHubAuthenticateUseCase(
	userRepo repositories.UserRepository,
	authService services.AuthService,
	githubAuthService services.GitHubAuthService,
) *GitHubAuthenticateUseCase {
	return &GitHubAuthenticateUseCase{
		userRepo:          userRepo,
		authService:       authService,
		githubAuthService: githubAuthService,
	}
}

// GitHubAuthenticateRequest represents the input for GitHub authentication
type GitHubAuthenticateRequest struct {
	Token string
}

// GitHubAuthenticateResponse represents the output of GitHub authentication
type GitHubAuthenticateResponse struct {
	User        *entities.User
	APIKey      *services.APIKey
	Permissions []entities.Permission
	GitHubInfo  *entities.GitHubUserInfo
}

// Execute authenticates a user using GitHub token
func (uc *GitHubAuthenticateUseCase) Execute(ctx context.Context, req *GitHubAuthenticateRequest) (*GitHubAuthenticateResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Authenticate with GitHub
	user, err := uc.githubAuthService.AuthenticateWithToken(ctx, req.Token)
	if err != nil {
		return nil, fmt.Errorf("GitHub authentication failed: %w", err)
	}

	// Get additional GitHub user info
	githubInfo, err := uc.githubAuthService.GetUserInfo(ctx, req.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub user info: %w", err)
	}

	// Get user teams for permissions
	teams, err := uc.githubAuthService.GetUserTeams(ctx, req.Token, githubInfo)
	if err != nil {
		// Log warning but don't fail - teams are optional
		fmt.Printf("Warning: failed to get user teams: %v\n", err)
		teams = []entities.GitHubTeamMembership{}
	}

	// Get user repositories
	repositories, err := uc.githubAuthService.GetUserRepositories(ctx, req.Token)
	if err != nil {
		// Log warning but don't fail - repositories are optional
		fmt.Printf("Warning: failed to get user repositories: %v\n", err)
		repositories = []entities.GitHubRepository{}
	}

	// Update user with GitHub information
	user.SetGitHubInfo(githubInfo, teams, repositories)

	// Verify user is active
	if !user.IsActive() {
		return nil, errors.New("user account is not active")
	}

	// Update user's last used timestamp
	user.UpdateLastUsed()

	// Save or update user in repository
	existingUser, err := uc.userRepo.FindByID(ctx, user.ID())
	if err != nil {
		// User doesn't exist, create new
		if err := uc.userRepo.Save(ctx, user); err != nil {
			return nil, fmt.Errorf("failed to save user: %w", err)
		}
	} else {
		// User exists, update with new GitHub info
		existingUser.SetGitHubInfo(githubInfo, teams, repositories)
		existingUser.UpdateLastUsed()
		if err := uc.userRepo.Update(ctx, existingUser); err != nil {
			return nil, fmt.Errorf("failed to update user: %w", err)
		}
		user = existingUser
	}

	// Generate API key for the session
	apiKey, err := uc.authService.GenerateAPIKey(ctx, user.ID(), user.Permissions())
	if err != nil {
		return nil, fmt.Errorf("failed to generate API key: %w", err)
	}

	return &GitHubAuthenticateResponse{
		User:        user,
		APIKey:      apiKey,
		Permissions: user.Permissions(),
		GitHubInfo:  githubInfo,
	}, nil
}

// validateRequest validates the GitHub authenticate request
func (uc *GitHubAuthenticateUseCase) validateRequest(req *GitHubAuthenticateRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.Token == "" {
		return errors.New("GitHub token cannot be empty")
	}

	return nil
}

// ValidatePermissionUseCase handles permission validation
type ValidatePermissionUseCase struct {
	authService services.AuthService
}

// NewValidatePermissionUseCase creates a new ValidatePermissionUseCase
func NewValidatePermissionUseCase(
	authService services.AuthService,
) *ValidatePermissionUseCase {
	return &ValidatePermissionUseCase{
		authService: authService,
	}
}

// ValidatePermissionRequest represents the input for permission validation
type ValidatePermissionRequest struct {
	User       *entities.User
	Permission entities.Permission
}

// ValidatePermissionResponse represents the output of permission validation
type ValidatePermissionResponse struct {
	HasPermission bool
	Error         error
}

// Execute validates if a user has a specific permission
func (uc *ValidatePermissionUseCase) Execute(ctx context.Context, req *ValidatePermissionRequest) (*ValidatePermissionResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Validate permission
	err := uc.authService.ValidatePermission(ctx, req.User, req.Permission)
	if err != nil {
		return &ValidatePermissionResponse{
			HasPermission: false,
			Error:         err,
		}, nil
	}

	return &ValidatePermissionResponse{
		HasPermission: true,
	}, nil
}

// validateRequest validates the validate permission request
func (uc *ValidatePermissionUseCase) validateRequest(req *ValidatePermissionRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.User == nil {
		return errors.New("user cannot be nil")
	}

	if req.Permission == "" {
		return errors.New("permission cannot be empty")
	}

	return nil
}
