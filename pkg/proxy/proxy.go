package proxy

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
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

// Proxy represents the HTTP proxy server
type Proxy struct {
	config             *config.Config
	echo               *echo.Echo
	verbose            bool
	logger             *logger.Logger
	oauthProvider      *auth.GitHubOAuthProvider
	githubAuthProvider *auth.GitHubAuthProvider
	oauthSessions      sync.Map // sessionID -> OAuthSession
	notificationSvc    *notification.Service
	container          *di.Container                // Internal DI container
	sessionManager     SessionManager               // Session lifecycle manager
	settingsRepo       portrepos.SettingsRepository // Settings repository
	shareRepo          ShareRepository              // Share repository for session sharing
	router             *Router                      // Router for custom handler registration
}

// NewProxy creates a new proxy instance
func NewProxy(cfg *config.Config, verbose bool) *Proxy {
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
				return firstSegment != "start" && firstSegment != "search" && firstSegment != "sessions" && firstSegment != "oauth" && firstSegment != "auth" && firstSegment != "notification" && firstSegment != "notifications"
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
	var shareRepo ShareRepository
	log.Printf("[PROXY] Initializing Kubernetes session manager")
	k8sSessionManager, err := NewKubernetesSessionManager(cfg, verbose, lgr)
	if err != nil {
		log.Fatalf("[PROXY] Failed to initialize Kubernetes session manager: %v", err)
	}
	sessionManager := SessionManager(k8sSessionManager)
	log.Printf("[PROXY] Kubernetes session manager initialized successfully")
	// Initialize settings repository
	settingsRepo = repositories.NewKubernetesSettingsRepository(
		k8sSessionManager.GetClient(),
		k8sSessionManager.GetNamespace(),
	)
	// Set settings repository in session manager for Bedrock integration
	k8sSessionManager.SetSettingsRepository(settingsRepo)
	log.Printf("[PROXY] Settings repository initialized")
	// Initialize share repository
	shareRepo = NewKubernetesShareRepository(
		k8sSessionManager.GetClient(),
		k8sSessionManager.GetNamespace(),
	)
	log.Printf("[PROXY] Share repository initialized")

	p := &Proxy{
		config:         cfg,
		echo:           e,
		verbose:        verbose,
		logger:         lgr,
		container:      container,
		sessionManager: sessionManager,
		settingsRepo:   settingsRepo,
		shareRepo:      shareRepo,
	}

	// Add logging middleware if verbose
	if verbose {
		e.Use(p.loggingMiddleware())
	}

	// Initialize GitHub auth provider if configured
	if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
		log.Printf("[AUTH_INIT] Initializing GitHub auth provider...")
		p.githubAuthProvider = auth.NewGitHubAuthProvider(cfg.Auth.GitHub)
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
		p.oauthProvider = auth.NewGitHubOAuthProvider(cfg.Auth.GitHub.OAuth, cfg.Auth.GitHub)
		log.Printf("[OAUTH_INIT] OAuth provider initialized successfully")
		// Start cleanup goroutine for expired OAuth sessions
		go p.cleanupExpiredOAuthSessions()
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
		p.notificationSvc = notificationSvc
		log.Printf("Notification service initialized successfully")

		// Set up subscription secret syncer if Kubernetes mode is enabled
		if k8sManager, ok := sessionManager.(*KubernetesSessionManager); ok {
			syncer := NewKubernetesSubscriptionSecretSyncer(
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
	go p.cleanupDefunctProcesses()

	// Start cleanup goroutine for expired shares
	if p.shareRepo != nil {
		go p.cleanupExpiredShares()
	}

	p.setupRoutes()

	return p
}

// StartMonitoring starts the session monitoring (called after proxy is fully initialized)
func (p *Proxy) StartMonitoring() {
	// Session monitoring disabled - notifications handled by Claude Code hooks
}

// loggingMiddleware returns Echo middleware for request logging
func (p *Proxy) loggingMiddleware() echo.MiddlewareFunc {
	return echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			log.Printf("Request: %s %s from %s", req.Method, req.URL.Path, req.RemoteAddr)
			return next(c)
		}
	})
}

// setupRoutes configures the router with all defined routes
func (p *Proxy) setupRoutes() {
	// Register non-auth routes using Router
	p.router = NewRouter(p.echo, p)
	if err := p.router.RegisterRoutes(); err != nil {
		log.Printf("Failed to register routes: %v", err)
	}

	// Register auth-related routes directly
	p.setupAuthRoutes()
}

// AddCustomHandler adds a custom handler to the router
func (p *Proxy) AddCustomHandler(handler CustomHandler) {
	if p.router != nil {
		p.router.AddCustomHandler(handler)
		// Register routes immediately since router is already initialized
		if err := handler.RegisterRoutes(p.echo, p); err != nil {
			log.Printf("Failed to register custom handler %s: %v", handler.GetName(), err)
		}
	}
}

// setupAuthRoutes registers authentication-related routes
func (p *Proxy) setupAuthRoutes() {
	// Add authentication info routes
	authInfoHandlers := NewAuthInfoHandlers(p.config)
	p.echo.GET("/auth/types", authInfoHandlers.GetAuthTypes)
	p.echo.GET("/auth/status", authInfoHandlers.GetAuthStatus)

	// Add OAuth routes if OAuth is configured
	log.Printf("[ROUTES] OAuth provider configured: %v", p.oauthProvider != nil)
	if p.oauthProvider != nil {
		log.Printf("[ROUTES] Registering OAuth endpoints...")
		// OAuth endpoints don't require existing authentication
		p.echo.POST("/oauth/authorize", p.handleOAuthLogin)
		p.echo.GET("/oauth/callback", p.handleOAuthCallback)
		p.echo.POST("/oauth/logout", p.handleOAuthLogout)
		p.echo.POST("/oauth/refresh", p.handleOAuthRefresh)
		log.Printf("[ROUTES] OAuth endpoints registered: /oauth/authorize, /oauth/callback, /oauth/logout, /oauth/refresh")
	} else {
		log.Printf("[ROUTES] OAuth endpoints not registered - OAuth provider not configured")
	}
}

// GetSessionManager returns the session manager
func (p *Proxy) GetSessionManager() SessionManager {
	return p.sessionManager
}

// SetSessionManager allows configuration of a custom session manager (for testing)
func (p *Proxy) SetSessionManager(manager SessionManager) {
	p.sessionManager = manager
}

// GetShareRepository returns the share repository
func (p *Proxy) GetShareRepository() ShareRepository {
	return p.shareRepo
}

// SetShareRepository allows configuration of a custom share repository (for testing)
func (p *Proxy) SetShareRepository(repo ShareRepository) {
	p.shareRepo = repo
}

// GetContainer returns the DI container
func (p *Proxy) GetContainer() *di.Container {
	return p.container
}

// CreateSession creates a new agent session
func (p *Proxy) CreateSession(sessionID string, startReq StartRequest, userID, userRole string, teams []string) (Session, error) {
	// Get auth team env file from user context if available
	var authTeamEnvFile string
	// Note: This would need to be passed from the handler if required

	// Merge environment variables from multiple sources
	envConfig := EnvMergeConfig{
		RoleEnvFiles:    &p.config.RoleEnvFiles,
		UserRole:        userRole,
		TeamEnvFile:     ExtractTeamEnvFile(startReq.Tags),
		AuthTeamEnvFile: authTeamEnvFile,
		RequestEnv:      startReq.Environment,
	}

	mergedEnv, err := MergeEnvironmentVariables(envConfig)
	if err != nil {
		log.Printf("[ENV] Failed to merge environment variables: %v", err)
		return nil, fmt.Errorf("failed to merge environment variables: %w", err)
	}

	// Replace the request environment with merged values
	startReq.Environment = mergedEnv

	// Extract repository information from tags
	repoInfo := p.extractRepositoryInfo(sessionID, startReq.Tags)

	// Determine initial message from Params.Message
	var initialMessage string
	if startReq.Params != nil && startReq.Params.Message != "" {
		initialMessage = startReq.Params.Message
	}

	// Determine GitHub token from Params.GithubToken
	var githubToken string
	if startReq.Params != nil && startReq.Params.GithubToken != "" {
		githubToken = startReq.Params.GithubToken
	}

	// Build run server request
	req := &RunServerRequest{
		UserID:         userID,
		Environment:    startReq.Environment,
		Tags:           startReq.Tags,
		RepoInfo:       repoInfo,
		InitialMessage: initialMessage,
		Teams:          teams,
		GithubToken:    githubToken,
		Scope:          startReq.Scope,
		TeamID:         startReq.TeamID,
	}

	// Delegate to session manager
	return p.sessionManager.CreateSession(context.Background(), sessionID, req)
}

// DeleteSessionByID deletes a session by ID
func (p *Proxy) DeleteSessionByID(sessionID string) error {
	// Delete associated share link if exists (ignore errors as share may not exist)
	if p.shareRepo != nil {
		_ = p.shareRepo.Delete(sessionID)
	}

	return p.sessionManager.DeleteSession(sessionID)
}

// Shutdown gracefully stops all running sessions and waits for them to terminate
func (p *Proxy) Shutdown(timeout time.Duration) error {
	return p.sessionManager.Shutdown(timeout)
}

// ExtractRepositoryInfo extracts repository information from tags.
// This is a public function that can be used by other packages (e.g., schedule).
// The cloneDir parameter is typically the session ID.
func ExtractRepositoryInfo(tags map[string]string, cloneDir string) *RepositoryInfo {
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

	return &RepositoryInfo{
		FullName: repoFullName,
		CloneDir: cloneDir,
	}
}

// extractRepositoryInfo extracts repository information from tags (internal method with verbose logging)
func (p *Proxy) extractRepositoryInfo(sessionID string, tags map[string]string) *RepositoryInfo {
	if tags == nil {
		return nil
	}

	repoURL, exists := tags["repository"]
	if !exists || repoURL == "" {
		return nil
	}

	// Only process repository URLs that look like valid GitHub URLs
	if !isValidRepositoryURL(repoURL) {
		if p.verbose {
			log.Printf("Repository tag found: %s, but it's not a valid repository URL. Skipping repository setup.", repoURL)
		}
		return nil
	}

	if p.verbose {
		log.Printf("Repository tag found: %s. Will pass to script as parameters.", repoURL)
	}

	repoInfo := ExtractRepositoryInfo(tags, sessionID)
	if repoInfo != nil && p.verbose {
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
func (p *Proxy) cleanupDefunctProcesses() {
	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		p.cleanupDefunctProcessesOnce()
	}
}

// cleanupDefunctProcessesOnce performs a single cleanup of defunct processes
func (p *Proxy) cleanupDefunctProcessesOnce() {
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
func (p *Proxy) cleanupExpiredShares() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if p.shareRepo != nil {
			count, err := p.shareRepo.CleanupExpired()
			if err != nil {
				log.Printf("Failed to cleanup expired shares: %v", err)
			} else if count > 0 {
				log.Printf("Cleaned up %d expired session shares", count)
			}
		}
	}
}

// GetEcho returns the Echo instance for external access
func (p *Proxy) GetEcho() *echo.Echo {
	return p.echo
}
