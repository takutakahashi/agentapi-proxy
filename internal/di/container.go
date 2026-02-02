package di

import (
	"context"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/presenters"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/auth"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/notification"
	repositories_ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	services_ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// Container holds all dependencies for the application
type Container struct {
	// Repositories
	UserRepo         repositories_ports.UserRepository
	NotificationRepo repositories_ports.NotificationRepository

	// Services
	AuthService         services_ports.AuthService
	NotificationService services_ports.NotificationService
	ProxyService        services_ports.ProxyService
	GitHubAuthService   services_ports.GitHubAuthService

	// Use Cases
	AuthenticateUserUC   *auth.AuthenticateUserUseCase
	ValidateAPIKeyUC     *auth.ValidateAPIKeyUseCase
	GitHubAuthenticateUC *auth.GitHubAuthenticateUseCase
	ValidatePermissionUC *auth.ValidatePermissionUseCase

	SendNotificationUC   *notification.SendNotificationUseCase
	ManageSubscriptionUC *notification.ManageSubscriptionUseCase

	// Presenters
	AuthPresenter         presenters.AuthPresenter
	NotificationPresenter presenters.NotificationPresenter

	// Controllers
	AuthController         *controllers.AuthController
	NotificationController *controllers.NotificationController
	AuthMiddleware         *controllers.AuthMiddleware
}

// NewContainer creates and configures a new dependency injection container
func NewContainer() *Container {
	container := &Container{}

	// Initialize repositories
	container.initRepositories()

	// Initialize services
	container.initServices()

	// Initialize use cases
	container.initUseCases()

	// Initialize presenters
	container.initPresenters()

	// Initialize controllers
	container.initControllers()

	// Seed initial data
	container.seedData()

	return container
}

// initRepositories initializes all repository dependencies
func (c *Container) initRepositories() {
	c.UserRepo = repositories.NewMemoryUserRepository()
	c.NotificationRepo = repositories.NewMemoryNotificationRepository()
}

// initServices initializes all service dependencies
func (c *Container) initServices() {
	c.AuthService = services.NewSimpleAuthService()
	c.NotificationService = services.NewSimpleNotificationService()

	// Initialize proxy service (simple implementation)
	c.ProxyService = &SimpleProxyService{}

	// Initialize GitHub auth service (simple implementation)
	c.GitHubAuthService = &SimpleGitHubAuthService{}
}

// initUseCases initializes all use case dependencies
func (c *Container) initUseCases() {
	// Auth use cases
	c.AuthenticateUserUC = auth.NewAuthenticateUserUseCase(
		c.UserRepo,
		c.AuthService,
	)

	c.ValidateAPIKeyUC = auth.NewValidateAPIKeyUseCase(
		c.UserRepo,
		c.AuthService,
	)

	c.GitHubAuthenticateUC = auth.NewGitHubAuthenticateUseCase(
		c.UserRepo,
		c.AuthService,
		c.GitHubAuthService,
	)

	c.ValidatePermissionUC = auth.NewValidatePermissionUseCase(
		c.AuthService,
	)

	// Notification use cases
	c.SendNotificationUC = notification.NewSendNotificationUseCase(
		c.NotificationRepo,
		c.UserRepo,
		c.NotificationService,
	)

	c.ManageSubscriptionUC = notification.NewManageSubscriptionUseCase(
		c.NotificationRepo,
		c.UserRepo,
		c.NotificationService,
	)
}

// initPresenters initializes all presenter dependencies
func (c *Container) initPresenters() {
	c.AuthPresenter = presenters.NewHTTPAuthPresenter()
	c.NotificationPresenter = presenters.NewHTTPNotificationPresenter()
}

// initControllers initializes all controller dependencies
func (c *Container) initControllers() {
	c.AuthController = controllers.NewAuthController(
		c.AuthenticateUserUC,
		c.ValidateAPIKeyUC,
		c.GitHubAuthenticateUC,
		c.ValidatePermissionUC,
		c.AuthPresenter,
	)

	c.NotificationController = controllers.NewNotificationController(
		c.SendNotificationUC,
		c.ManageSubscriptionUC,
		c.NotificationPresenter,
	)

	c.AuthMiddleware = controllers.NewAuthMiddleware(
		c.ValidateAPIKeyUC,
		c.AuthPresenter,
	)
}

// seedData seeds initial data for development and testing
func (c *Container) seedData() {
	// Create admin user
	adminUser := entities.NewUser(
		entities.UserID("admin"),
		entities.UserTypeAdmin,
		"admin",
	)

	// Add admin user to auth service
	if simpleAuth, ok := c.AuthService.(*services.SimpleAuthService); ok {
		simpleAuth.AddUser(adminUser)
	}

	// Save admin user to repository
	_ = c.UserRepo.Save(context.TODO(), adminUser)

	// Create regular test user
	testUser := entities.NewUser(
		entities.UserID("user_test"),
		entities.UserTypeRegular,
		"testuser",
	)

	// Add test user to auth service
	if simpleAuth, ok := c.AuthService.(*services.SimpleAuthService); ok {
		simpleAuth.AddUser(testUser)
	}

	// Save test user to repository
	_ = c.UserRepo.Save(context.TODO(), testUser)
}

// SimpleProxyService is a simple implementation of ProxyService
type SimpleProxyService struct{}

func (s *SimpleProxyService) RouteRequest(ctx context.Context, sessionID entities.SessionID, request *services_ports.HTTPRequest) (*services_ports.HTTPResponse, error) {
	// Simple implementation - return a basic response
	return &services_ports.HTTPResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       []byte("Hello from session " + string(sessionID)),
	}, nil
}

func (s *SimpleProxyService) IsSessionReachable(ctx context.Context, sessionID entities.SessionID) (bool, error) {
	// Simple implementation - assume session is reachable
	return true, nil
}

func (s *SimpleProxyService) GetSessionURL(ctx context.Context, sessionID entities.SessionID) (string, error) {
	return fmt.Sprintf("http://session-%s:9000", sessionID), nil
}

// SimpleGitHubAuthService is a simple implementation of GitHubAuthService
type SimpleGitHubAuthService struct{}

func (s *SimpleGitHubAuthService) AuthenticateWithToken(ctx context.Context, token string) (*entities.User, error) {
	// Simple implementation - create user based on token
	userID := entities.UserID("github_" + token[:8])
	return entities.NewUser(
		userID,
		entities.UserTypeRegular,
		"github_user",
	), nil
}

func (s *SimpleGitHubAuthService) GetUserInfo(ctx context.Context, token string) (*entities.GitHubUserInfo, error) {
	return entities.NewGitHubUserInfo(
		12345,
		"github_user",
		"GitHub User",
		"github@example.com",
		"https://github.com/avatar.png",
		"",
		"",
	), nil
}

func (s *SimpleGitHubAuthService) GetUserTeams(ctx context.Context, token string, user *entities.GitHubUserInfo) ([]entities.GitHubTeamMembership, error) {
	return []entities.GitHubTeamMembership{}, nil
}

func (s *SimpleGitHubAuthService) GetUserRepositories(ctx context.Context, token string) ([]entities.GitHubRepository, error) {
	return []entities.GitHubRepository{}, nil
}

func (s *SimpleGitHubAuthService) ValidateGitHubToken(ctx context.Context, token string) (bool, error) {
	return len(token) > 0, nil
}

func (s *SimpleGitHubAuthService) GenerateOAuthURL(ctx context.Context, redirectURI string) (string, string, error) {
	return "https://github.com/login/oauth/authorize", "state123", nil
}

func (s *SimpleGitHubAuthService) ExchangeCodeForToken(ctx context.Context, code, state string) (*services_ports.OAuthToken, error) {
	return &services_ports.OAuthToken{
		AccessToken: "github_token_" + code,
		TokenType:   "bearer",
		Scope:       "user:email",
	}, nil
}

func (s *SimpleGitHubAuthService) RevokeOAuthToken(ctx context.Context, token string) error {
	return nil
}
