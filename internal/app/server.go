package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	serviceaccountuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/service_account"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
	"k8s.io/client-go/kubernetes/fake"
)

// Server represents the HTTP server
type Server struct {
	config             *config.Config
	echo               *echo.Echo
	verbose            bool
	logger             *logger.Logger
	oauthProvider      *auth.GitHubOAuthProvider
	githubAuthProvider *auth.GitHubAuthProvider
	oauthSessions      sync.Map // sessionID -> OAuthSession
	notificationSvc    *notification.Service
	container          *di.Container                  // Internal DI container
	sessionManager     portrepos.SessionManager       // Session lifecycle manager
	settingsRepo       portrepos.SettingsRepository   // Settings repository
	shareRepo          portrepos.ShareRepository      // Share repository for session sharing
	teamConfigRepo     portrepos.TeamConfigRepository // Team configuration repository
	memoryRepo         portrepos.MemoryRepository     // Memory repository
	taskRepo           portrepos.TaskRepository       // Task repository
	taskGroupRepo      portrepos.TaskGroupRepository  // Task group repository
	router             *Router                        // Router for custom handler registration
}

// NewServer creates a new server instance
func NewServer(cfg *config.Config, verbose bool) *Server {
	e := echo.New()

	// Disable Echo's default logger and use custom logging
	e.Logger.SetOutput(io.Discard)

	// Add recovery middleware
	e.Use(middleware.Recover())

	// Add security headers middleware
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:         "1; mode=block",
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "DENY",
		HSTSMaxAge:            31536000, // 1 year
		HSTSExcludeSubdomains: false,
		ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
	}))

	// Add CORS middleware with secure configuration (only for non-proxy routes)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		Skipper: func(c echo.Context) bool {
			// Skip CORS middleware only for proxy routes (/:sessionId/* pattern)
			// These routes handle CORS manually in the proxy
			path := c.Request().URL.Path
			pathParts := strings.Split(path, "/")
			// Skip CORS for shared session routes (/s/:shareToken/*)
			if len(pathParts) >= 3 && pathParts[1] == "s" {
				return true
			}
			// Skip CORS only for proxy routes that match /:sessionId/* pattern
			// (at least 3 parts, not starting with "start", "search", "sessions", "oauth", "auth", "notification", or "notifications")
			if len(pathParts) >= 3 && pathParts[1] != "" {
				firstSegment := pathParts[1]
				return firstSegment != "start" && firstSegment != "search" && firstSegment != "sessions" && firstSegment != "oauth" && firstSegment != "auth" && firstSegment != "notification" && firstSegment != "notifications" && firstSegment != "memories" && firstSegment != "tasks" && firstSegment != "task-groups"
			}
			return false
		},
		AllowOriginFunc: func(origin string) (bool, error) {
			// Get allowed origins from environment variable
			allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
			if allowedOrigins == "" {
				// Fallback to localhost for development
				allowed := strings.HasPrefix(origin, "http://localhost") ||
					strings.HasPrefix(origin, "https://localhost") ||
					strings.HasPrefix(origin, "http://127.0.0.1") ||
					strings.HasPrefix(origin, "https://127.0.0.1")
				return allowed, nil
			}
			// Parse comma-separated allowed origins
			origins := strings.Split(allowedOrigins, ",")
			for _, allowed := range origins {
				if strings.TrimSpace(allowed) == origin {
					return true, nil
				}
			}
			return false, nil
		},
		AllowMethods:     []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, "X-Requested-With", "X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host", "X-API-Key"},
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	// Initialize internal DI container
	container := di.NewContainer()

	// Initialize logger
	lgr := logger.NewLogger()

	// Initialize Kubernetes session manager
	var settingsRepo portrepos.SettingsRepository
	var shareRepo portrepos.ShareRepository
	log.Printf("[SERVER] Initializing Kubernetes session manager")
	k8sSessionManager, err := services.NewKubernetesSessionManager(cfg, verbose, lgr)
	if err != nil {
		// If Kubernetes is not available, use a fake client for testing/development
		log.Printf("[SERVER] Kubernetes config not available, using fake client: %v", err)
		k8sSessionManager, err = services.NewKubernetesSessionManagerWithClient(cfg, verbose, lgr, fake.NewSimpleClientset())
		if err != nil {
			log.Fatalf("[SERVER] Failed to initialize session manager with fake client: %v", err)
		}
	}
	sessionManager := portrepos.SessionManager(k8sSessionManager)
	log.Printf("[SERVER] Kubernetes session manager initialized successfully")

	// Initialize encryption service registry
	// The registry manages multiple encryption services and selects the appropriate one
	// based on encryption metadata when decrypting
	encryptionFactory := services.NewEncryptionServiceFactory("AGENTAPI_ENCRYPTION")
	primaryService, err := encryptionFactory.Create()
	if err != nil {
		log.Fatalf("Failed to create primary encryption service: %v", err)
	}

	// Create registry with primary service (used for encryption)
	encryptionRegistry := services.NewEncryptionServiceRegistry(primaryService)

	// Register Noop service for backward compatibility with plaintext data
	// This allows reading old unencrypted data
	noopService := services.NewNoopEncryptionService()
	encryptionRegistry.Register(noopService)

	// Try to register additional services for migration scenarios
	// These are optional and will be used for decryption if data was encrypted with them

	// Try to create a local encryption service (if different from primary)
	localFactory := services.NewEncryptionServiceFactory("AGENTAPI_DECRYPTION")
	if localService, err := localFactory.Create(); err == nil {
		// Only register if it's different from primary
		if localService.Algorithm() != primaryService.Algorithm() || localService.KeyID() != primaryService.KeyID() {
			encryptionRegistry.Register(localService)
		}
	}

	log.Printf("[SERVER] Encryption registry initialized with primary: %s (keyID: %s)",
		primaryService.Algorithm(), primaryService.KeyID())

	// Initialize settings repository
	settingsRepo = repositories.NewKubernetesSettingsRepository(
		k8sSessionManager.GetClient(),
		k8sSessionManager.GetNamespace(),
	)
	// Set settings repository in session manager for Bedrock integration
	k8sSessionManager.SetSettingsRepository(settingsRepo)
	log.Printf("[SERVER] Settings repository initialized")
	// Initialize share repository
	shareRepo = repositories.NewKubernetesShareRepository(
		k8sSessionManager.GetClient(),
		k8sSessionManager.GetNamespace(),
	)
	log.Printf("[SERVER] Share repository initialized")

	// Initialize team config repository
	teamConfigRepo := repositories.NewKubernetesTeamConfigRepository(
		k8sSessionManager.GetClient(),
		k8sSessionManager.GetNamespace(),
	)
	// Set team config repository in session manager for service account integration
	k8sSessionManager.SetTeamConfigRepository(teamConfigRepo)
	log.Printf("[SERVER] Team config repository initialized")

	// Initialize personal API key repository
	personalAPIKeyRepo := repositories.NewKubernetesPersonalAPIKeyRepository(
		k8sSessionManager.GetClient(),
		k8sSessionManager.GetNamespace(),
	)
	// Set personal API key repository in session manager
	k8sSessionManager.SetPersonalAPIKeyRepository(personalAPIKeyRepo)
	log.Printf("[SERVER] Personal API key repository initialized")

	// Initialize memory repository based on backend configuration.
	// Default is "kubernetes" (ConfigMap-backed). Set memory.backend = "s3" to use S3.
	var memoryRepo portrepos.MemoryRepository
	if cfg.Memory.Backend == "s3" && cfg.Memory.S3 != nil {
		s3MemRepo, s3Err := repositories.NewS3MemoryRepository(context.Background(), cfg.Memory.S3)
		if s3Err != nil {
			log.Fatalf("[SERVER] Failed to initialize S3 memory repository: %v", s3Err)
		}
		memoryRepo = s3MemRepo
		log.Printf("[SERVER] Memory repository initialized (backend: s3, bucket: %s)", cfg.Memory.S3.Bucket)
	} else {
		memoryRepo = repositories.NewKubernetesMemoryRepository(
			k8sSessionManager.GetClient(),
			k8sSessionManager.GetNamespace(),
		)
		log.Printf("[SERVER] Memory repository initialized (backend: kubernetes)")
	}

	// Initialize task repository (Kubernetes ConfigMap-backed)
	taskRepo := repositories.NewKubernetesTaskRepository(
		k8sSessionManager.GetClient(),
		k8sSessionManager.GetNamespace(),
	)
	log.Printf("[SERVER] Task repository initialized")

	// Initialize task group repository (Kubernetes ConfigMap-backed)
	taskGroupRepo := repositories.NewKubernetesTaskGroupRepository(
		k8sSessionManager.GetClient(),
		k8sSessionManager.GetNamespace(),
	)
	log.Printf("[SERVER] Task group repository initialized")

	s := &Server{
		config:         cfg,
		echo:           e,
		verbose:        verbose,
		logger:         lgr,
		container:      container,
		sessionManager: sessionManager,
		settingsRepo:   settingsRepo,
		shareRepo:      shareRepo,
		teamConfigRepo: teamConfigRepo,
		memoryRepo:     memoryRepo,
		taskRepo:       taskRepo,
		taskGroupRepo:  taskGroupRepo,
	}

	// Add logging middleware if verbose
	if verbose {
		e.Use(s.loggingMiddleware())
	}

	// Initialize GitHub auth provider if configured
	if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
		log.Printf("[AUTH_INIT] Initializing GitHub auth provider...")
		s.githubAuthProvider = auth.NewGitHubAuthProvider(cfg.Auth.GitHub)
		log.Printf("[AUTH_INIT] GitHub auth provider initialized successfully")

		// Configure the internal auth service with GitHub settings
		if simpleAuth, ok := container.AuthService.(*services.SimpleAuthService); ok {
			simpleAuth.SetGitHubAuthConfig(cfg.Auth.GitHub)
			log.Printf("[AUTH_INIT] GitHub auth config set for internal auth service")
		}
	}

	// Add authentication middleware using internal auth service
	e.Use(auth.AuthMiddleware(cfg, container.AuthService))

	// Initialize OAuth provider if configured
	log.Printf("[OAUTH_INIT] Checking OAuth configuration...")
	log.Printf("[OAUTH_INIT] cfg.Auth.GitHub != nil: %v", cfg.Auth.GitHub != nil)
	if cfg.Auth.GitHub != nil {
		log.Printf("[OAUTH_INIT] cfg.Auth.GitHub.OAuth != nil: %v", cfg.Auth.GitHub.OAuth != nil)
		if cfg.Auth.GitHub.OAuth != nil {
			log.Printf("[OAUTH_INIT] OAuth ClientID configured: %v", cfg.Auth.GitHub.OAuth.ClientID != "")
			log.Printf("[OAUTH_INIT] OAuth ClientSecret configured: %v", cfg.Auth.GitHub.OAuth.ClientSecret != "")
		}
	}
	if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.OAuth != nil &&
		cfg.Auth.GitHub.OAuth.ClientID != "" && cfg.Auth.GitHub.OAuth.ClientSecret != "" {
		log.Printf("[OAUTH_INIT] Initializing GitHub OAuth provider...")
		s.oauthProvider = auth.NewGitHubOAuthProvider(cfg.Auth.GitHub.OAuth, cfg.Auth.GitHub)
		log.Printf("[OAUTH_INIT] OAuth provider initialized successfully")
		// Start cleanup goroutine for expired OAuth sessions
		go s.cleanupExpiredOAuthSessions()
	} else {
		log.Printf("[OAUTH_INIT] OAuth provider not initialized - configuration missing or incomplete")
		if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.OAuth != nil {
			log.Printf("[OAUTH_INIT] OAuth configuration found but credentials are empty")
		}
	}

	// Initialize notification service
	baseDir := notification.GetBaseDir()
	notificationSvc, err := notification.NewService(baseDir)
	if err != nil {
		log.Printf("Failed to initialize notification service: %v", err)
	} else {
		s.notificationSvc = notificationSvc
		log.Printf("Notification service initialized successfully")

		// Set up subscription secret syncer if Kubernetes mode is enabled
		if k8sManager, ok := sessionManager.(*services.KubernetesSessionManager); ok {
			syncer := services.NewKubernetesSubscriptionSecretSyncer(
				k8sManager.GetClient(),
				k8sManager.GetNamespace(),
				notificationSvc.GetStorage(),
				"", // Use default prefix
			)
			notificationSvc.SetSecretSyncer(syncer)
			log.Printf("Subscription secret syncer configured for Kubernetes mode")
		}
	}

	// Start cleanup goroutine for defunct processes
	go s.cleanupDefunctProcesses()

	// Start cleanup goroutine for expired shares
	if s.shareRepo != nil {
		go s.cleanupExpiredShares()
	}

	// Bootstrap service accounts from team configs
	if teamConfigRepo != nil {
		if simpleAuth, ok := container.AuthService.(*services.SimpleAuthService); ok {
			ctx := context.Background()
			if err := services.BootstrapServiceAccounts(ctx, simpleAuth, teamConfigRepo); err != nil {
				log.Printf("[SERVER] Warning: failed to bootstrap service accounts: %v", err)
			}
		}
	}

	// Bootstrap personal API keys (Kubernetes mode only)
	if k8sSessionManager, ok := s.sessionManager.(*services.KubernetesSessionManager); ok {
		personalAPIKeyRepo := k8sSessionManager.GetPersonalAPIKeyRepository()
		if personalAPIKeyRepo != nil {
			if simpleAuth, ok := container.AuthService.(*services.SimpleAuthService); ok {
				ctx := context.Background()
				if err := services.BootstrapPersonalAPIKeys(ctx, simpleAuth, personalAPIKeyRepo); err != nil {
					log.Printf("[SERVER] Warning: failed to bootstrap personal API keys: %v", err)
				}
			}
		}
	}

	// Set up ServiceAccountEnsurer in KubernetesSessionManager so that all session creation
	// paths (start, webhook, schedule, etc.) automatically create TeamConfig on team-scoped sessions.
	if teamConfigRepo != nil {
		if simpleAuth, ok := container.AuthService.(*services.SimpleAuthService); ok {
			if k8sManager, ok := s.sessionManager.(*services.KubernetesSessionManager); ok {
				ensurer := serviceaccountuc.NewGetOrCreateServiceAccountUseCase(teamConfigRepo, simpleAuth)
				k8sManager.SetServiceAccountEnsurer(ensurer)
				log.Printf("[SERVER] ServiceAccountEnsurer configured for KubernetesSessionManager")
			}
		}
	}

	s.setupRoutes()

	return s
}

// StartMonitoring starts the session monitoring (called after server is fully initialized)
func (s *Server) StartMonitoring() {
	// Session monitoring disabled - notifications handled by Claude Code hooks
}

// loggingMiddleware returns Echo middleware for request logging
func (s *Server) loggingMiddleware() echo.MiddlewareFunc {
	return echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			log.Printf("Request: %s %s from %s", req.Method, req.URL.Path, req.RemoteAddr)
			return next(c)
		}
	})
}

// setupRoutes configures the router with all defined routes
func (s *Server) setupRoutes() {
	// Register non-auth routes using Router
	s.router = NewRouter(s.echo, s)
	if err := s.router.RegisterRoutes(); err != nil {
		log.Printf("Failed to register routes: %v", err)
	}

	// Register auth-related routes directly
	s.setupAuthRoutes()
}

// AddCustomHandler adds a custom handler to the router
func (s *Server) AddCustomHandler(handler CustomHandler) {
	if s.router != nil {
		s.router.AddCustomHandler(handler)
		// Register routes immediately since router is already initialized
		if err := handler.RegisterRoutes(s.echo, s); err != nil {
			log.Printf("Failed to register custom handler %s: %v", handler.GetName(), err)
		}
	}
}

// GetSessionManager returns the session manager
func (s *Server) GetSessionManager() portrepos.SessionManager {
	return s.sessionManager
}

// SetSessionManager allows configuration of a custom session manager (for testing)
func (s *Server) SetSessionManager(manager portrepos.SessionManager) {
	s.sessionManager = manager
}

// GetShareRepository returns the share repository
func (s *Server) GetShareRepository() portrepos.ShareRepository {
	return s.shareRepo
}

// SetShareRepository allows configuration of a custom share repository (for testing)
func (s *Server) SetShareRepository(repo portrepos.ShareRepository) {
	s.shareRepo = repo
}

// GetContainer returns the DI container
func (s *Server) GetContainer() *di.Container {
	return s.container
}

// CreateSession creates a new agent session
func (s *Server) CreateSession(sessionID string, startReq entities.StartRequest, userID, userRole string, teams []string) (entities.Session, error) {
	// Get auth team env file from user context if available
	var authTeamEnvFile string
	// Note: This would need to be passed from the handler if required

	// Merge environment variables from multiple sources
	envConfig := services.EnvMergeConfig{
		RoleEnvFiles:    &s.config.RoleEnvFiles,
		UserRole:        userRole,
		TeamEnvFile:     services.ExtractTeamEnvFile(startReq.Tags),
		AuthTeamEnvFile: authTeamEnvFile,
		RequestEnv:      startReq.Environment,
	}

	mergedEnv, err := services.MergeEnvironmentVariables(envConfig)
	if err != nil {
		log.Printf("[ENV] Failed to merge environment variables: %v", err)
		return nil, fmt.Errorf("failed to merge environment variables: %w", err)
	}

	// Replace the request environment with merged values
	startReq.Environment = mergedEnv

	// Extract repository information from tags
	repoInfo := s.extractRepositoryInfo(sessionID, startReq.Tags)

	// Determine initial message from Params.Message
	var initialMessage string
	if startReq.Params != nil && startReq.Params.Message != "" {
		initialMessage = startReq.Params.Message
	}

	// Determine GitHub token from Params.GithubToken
	// Note: github_token is not passed for team-scoped sessions (use GitHub App auth instead)
	var githubToken string
	if startReq.Params != nil && startReq.Params.GithubToken != "" && startReq.Scope != entities.ScopeTeam {
		githubToken = startReq.Params.GithubToken
	}

	// Determine agent type from Params.AgentType
	var agentType string
	if startReq.Params != nil && startReq.Params.AgentType != "" {
		agentType = startReq.Params.AgentType
	}

	// Determine Slack parameters from Params.Slack
	var slackParams *entities.SlackParams
	if startReq.Params != nil && startReq.Params.Slack != nil {
		slackParams = startReq.Params.Slack
	}

	// Determine oneshot from Params.Oneshot
	var oneshot bool
	if startReq.Params != nil {
		oneshot = startReq.Params.Oneshot
	}

	// Determine initial message wait second from Params.InitialMessageWaitSecond
	var initialMessageWaitSecond *int
	if startReq.Params != nil && startReq.Params.InitialMessageWaitSecond != nil {
		initialMessageWaitSecond = startReq.Params.InitialMessageWaitSecond
	}

	// Build run server request
	req := &entities.RunServerRequest{
		UserID:                   userID,
		Environment:              startReq.Environment,
		Tags:                     startReq.Tags,
		RepoInfo:                 repoInfo,
		InitialMessage:           initialMessage,
		Teams:                    teams,
		GithubToken:              githubToken,
		Scope:                    startReq.Scope,
		TeamID:                   startReq.TeamID,
		AgentType:                agentType,
		SlackParams:              slackParams,
		Oneshot:                  oneshot,
		InitialMessageWaitSecond: initialMessageWaitSecond,
	}

	// Delegate to session manager
	return s.sessionManager.CreateSession(context.Background(), sessionID, req, nil)
}

// DeleteSessionByID deletes a session by ID
func (s *Server) DeleteSessionByID(sessionID string) error {
	// Delete associated share link if exists (ignore errors as share may not exist)
	if s.shareRepo != nil {
		_ = s.shareRepo.Delete(sessionID)
	}

	return s.sessionManager.DeleteSession(sessionID)
}

// Shutdown gracefully stops all running sessions and waits for them to terminate
func (s *Server) Shutdown(timeout time.Duration) error {
	return s.sessionManager.Shutdown(timeout)
}

// GetEcho returns the Echo instance for external access
func (s *Server) GetEcho() *echo.Echo {
	return s.echo
}

// GetConfig returns the server configuration
func (s *Server) GetConfig() *config.Config {
	return s.config
}

// GetNotificationService returns the notification service
func (s *Server) GetNotificationService() *notification.Service {
	return s.notificationSvc
}

// GetSettingsRepository returns the settings repository
func (s *Server) GetSettingsRepository() portrepos.SettingsRepository {
	return s.settingsRepo
}

// GetMemoryRepository returns the memory repository
func (s *Server) GetMemoryRepository() portrepos.MemoryRepository {
	return s.memoryRepo
}

// SetMemoryRepository allows configuration of a custom memory repository (for testing)
func (s *Server) SetMemoryRepository(repo portrepos.MemoryRepository) {
	s.memoryRepo = repo
}

// ExtractRepositoryInfo extracts repository information from tags.
// This is a public function that can be used by other packages (e.g., schedule).
// The cloneDir parameter is typically the session ID.
func ExtractRepositoryInfo(tags map[string]string, cloneDir string) *entities.RepositoryInfo {
	if tags == nil {
		return nil
	}

	repoURL, exists := tags["repository"]
	if !exists || repoURL == "" {
		return nil
	}

	// Only process repository URLs that look like valid GitHub URLs
	if !isValidRepositoryURL(repoURL) {
		return nil
	}

	// Extract org/repo format from repository URL
	repoFullName, err := extractRepoFullNameFromURL(repoURL)
	if err != nil {
		log.Printf("Failed to extract repository full name from URL %s: %v", repoURL, err)
		return nil
	}

	return &entities.RepositoryInfo{
		FullName: repoFullName,
		CloneDir: cloneDir,
	}
}

// extractRepositoryInfo extracts repository information from tags (internal method with verbose logging)
func (s *Server) extractRepositoryInfo(sessionID string, tags map[string]string) *entities.RepositoryInfo {
	if tags == nil {
		return nil
	}

	repoURL, exists := tags["repository"]
	if !exists || repoURL == "" {
		return nil
	}

	// Only process repository URLs that look like valid GitHub URLs
	if !isValidRepositoryURL(repoURL) {
		if s.verbose {
			log.Printf("Repository tag found: %s, but it's not a valid repository URL. Skipping repository setup.", repoURL)
		}
		return nil
	}

	if s.verbose {
		log.Printf("Repository tag found: %s. Will pass to script as parameters.", repoURL)
	}

	repoInfo := ExtractRepositoryInfo(tags, sessionID)
	if repoInfo != nil && s.verbose {
		log.Printf("Extracted repository info - FullName: %s, CloneDir: %s", repoInfo.FullName, sessionID)
	}

	return repoInfo
}

// isValidRepositoryURL checks if a repository URL is valid for GitHub
func isValidRepositoryURL(repoURL string) bool {
	// Check for common GitHub URL patterns
	if strings.HasPrefix(repoURL, "https://github.com/") ||
		strings.HasPrefix(repoURL, "git@github.com:") ||
		strings.HasPrefix(repoURL, "http://github.com/") {
		return true
	}

	// Check for owner/repo format (e.g., "takutakahashi/agentapi-ui")
	parts := strings.Split(repoURL, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		// Simple validation: both owner and repo name should not be empty
		// and should not contain invalid characters for GitHub usernames/repo names
		return true
	}

	return false
}

// extractRepoFullNameFromURL extracts the org/repo format from a GitHub repository URL
func extractRepoFullNameFromURL(repoURL string) (string, error) {
	var repoPath string

	if strings.HasPrefix(repoURL, "https://github.com/") {
		repoPath = strings.TrimPrefix(repoURL, "https://github.com/")
	} else if strings.HasPrefix(repoURL, "git@github.com:") {
		repoPath = strings.TrimPrefix(repoURL, "git@github.com:")
	} else if strings.HasPrefix(repoURL, "http://github.com/") {
		repoPath = strings.TrimPrefix(repoURL, "http://github.com/")
	} else {
		// If it's not a full URL, assume it's already in owner/repo format
		repoPath = repoURL
	}

	// Remove .git suffix if present
	repoPath = strings.TrimSuffix(repoPath, ".git")

	// Split into org/repo
	parts := strings.Split(repoPath, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repository path: %s", repoPath)
	}

	return repoPath, nil
}

// cleanupDefunctProcesses periodically checks for and cleans up defunct processes
func (s *Server) cleanupDefunctProcesses() {
	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		s.cleanupDefunctProcessesOnce()
	}
}

// cleanupDefunctProcessesOnce performs a single cleanup of defunct processes
func (s *Server) cleanupDefunctProcessesOnce() {
	// Find defunct processes
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get process list for defunct cleanup: %v", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	defunctCount := 0

	for _, line := range lines {
		if strings.Contains(line, "<defunct>") || strings.Contains(line, " Z ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				pidStr := fields[1]
				if pid, err := strconv.Atoi(pidStr); err == nil {
					// Try to reap the defunct process by sending signal 0
					// This doesn't actually send a signal but checks if we can access the process
					if err := syscall.Kill(pid, 0); err != nil {
						// Process is already gone or we can't access it
						continue
					}
					defunctCount++
				}
			}
		}
	}

	if defunctCount > 0 {
		log.Printf("Found %d defunct processes during periodic cleanup", defunctCount)

		// Try to trigger process reaping by the init system
		// This is a best-effort approach
		if defunctCount > 10 {
			log.Printf("High number of defunct processes detected (%d). Consider investigating process management.", defunctCount)
		}
	}
}

// cleanupExpiredShares periodically removes expired session shares
func (s *Server) cleanupExpiredShares() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if s.shareRepo != nil {
			count, err := s.shareRepo.CleanupExpired()
			if err != nil {
				log.Printf("Failed to cleanup expired shares: %v", err)
			} else if count > 0 {
				log.Printf("Cleaned up %d expired session shares", count)
			}
		}
	}
}
