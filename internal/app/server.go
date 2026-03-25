package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
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
	config          *config.Config
	echo            *echo.Echo
	verbose         bool
	logger          *logger.Logger
	oauthProvider   *auth.GitHubOAuthProvider
	oauthSessions   sync.Map // sessionID -> OAuthSession
	notificationSvc *notification.Service
	container       *di.Container                  // Internal DI container
	sessionManager  portrepos.SessionManager       // Session lifecycle manager
	settingsRepo    portrepos.SettingsRepository   // Settings repository
	shareRepo       portrepos.ShareRepository      // Share repository for session sharing
	teamConfigRepo  portrepos.TeamConfigRepository // Team configuration repository
	memoryRepo      portrepos.MemoryRepository     // Memory repository
	taskRepo        portrepos.TaskRepository       // Task repository
	taskGroupRepo   portrepos.TaskGroupRepository  // Task group repository
	router          *Router                        // Router for custom handler registration
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
	// Supported backends: "kubernetes" (default), "s3", "external".
	var memoryRepo portrepos.MemoryRepository
	switch cfg.Memory.Backend {
	case "s3":
		if cfg.Memory.S3 == nil {
			log.Fatalf("[SERVER] Memory backend is 's3' but no S3 configuration provided")
		}
		s3MemRepo, s3Err := repositories.NewS3MemoryRepository(context.Background(), cfg.Memory.S3)
		if s3Err != nil {
			log.Fatalf("[SERVER] Failed to initialize S3 memory repository: %v", s3Err)
		}
		memoryRepo = s3MemRepo
		log.Printf("[SERVER] Memory repository initialized (backend: s3, bucket: %s)", cfg.Memory.S3.Bucket)
	case "external":
		if cfg.Memory.External == nil || cfg.Memory.External.URL == "" {
			log.Fatalf("[SERVER] Memory backend is 'external' but no external configuration provided (set AGENTAPI_MEMORY_EXTERNAL_URL)")
		}
		memoryRepo = repositories.NewExternalMemoryRepository(cfg.Memory.External, personalAPIKeyRepo, teamConfigRepo)
		log.Printf("[SERVER] Memory repository initialized (backend: external, url: %s)", cfg.Memory.External.URL)
	default:
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

	// Initialize GitHub auth provider if configured.
	// A single *GitHubAuthProvider instance is shared across all subsystems
	// (SimpleAuthService and GitHubOAuthProvider) so they use the same
	// in-memory teamCache and ConfigMap-backed teamMappingRepo.
	var githubAuthProvider *auth.GitHubAuthProvider
	if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
		log.Printf("[AUTH_INIT] Initializing GitHub auth provider...")
		githubAuthProvider = auth.NewGitHubAuthProvider(cfg.Auth.GitHub)

		// Inject ConfigMap-backed team mapping cache (1 user = 1 key in the ConfigMap)
		teamMappingRepo := repositories.NewKubernetesUserTeamMappingRepository(
			k8sSessionManager.GetClient(),
			k8sSessionManager.GetNamespace(),
		)
		githubAuthProvider.SetTeamMappingRepo(teamMappingRepo)
		log.Printf("[AUTH_INIT] GitHub auth provider initialized with ConfigMap team mapping cache")

		// Inject the shared provider into SimpleAuthService.
		if simpleAuth, ok := container.AuthService.(*services.SimpleAuthService); ok {
			simpleAuth.SetGitHubProvider(githubAuthProvider)
			simpleAuth.SetGitHubAuthConfig(cfg.Auth.GitHub)
			log.Printf("[AUTH_INIT] GitHub auth provider injected into internal auth service")
		}
	}

	// Add authentication middleware using internal auth service
	e.Use(auth.AuthMiddleware(cfg, container.AuthService))

	// Initialize OAuth provider if configured.
	// Reuses the shared githubAuthProvider so OAuth-authenticated users benefit from
	// the same teamCache and teamMappingRepo as token-based auth users.
	if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.OAuth != nil &&
		cfg.Auth.GitHub.OAuth.ClientID != "" && cfg.Auth.GitHub.OAuth.ClientSecret != "" {
		log.Printf("[OAUTH_INIT] Initializing GitHub OAuth provider...")
		s.oauthProvider = auth.NewGitHubOAuthProvider(cfg.Auth.GitHub.OAuth, githubAuthProvider)
		log.Printf("[OAUTH_INIT] OAuth provider initialized successfully")
		// Start cleanup goroutine for expired OAuth sessions
		go s.cleanupExpiredOAuthSessions()
	} else {
		log.Printf("[OAUTH_INIT] OAuth provider not initialized - configuration missing or incomplete")
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
			// Also use the syncer as the subscription reader so that push notifications
			// read subscriptions from Kubernetes Secrets rather than local file storage.
			// This is required in session pods where local storage is empty.
			notificationSvc.SetSubscriptionReader(syncer)
			// Use the syncer as the subscription writer so that all subscription mutations
			// go directly to the Kubernetes Secret, bypassing local file storage entirely.
			// This prevents subscription loss after pod restarts.
			notificationSvc.SetSubscriptionWriter(syncer)
			log.Printf("Subscription secret syncer configured for Kubernetes mode (read+write)")
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

	// Register memory dump handler on KubernetesSessionManager.
	// This ensures dumpSessionToMemory is called for every session deletion path
	// (HTTP DELETE, Slackbot cleanup, etc.) without requiring callers to know about it.
	if k8sManager, ok := sessionManager.(*services.KubernetesSessionManager); ok && memoryRepo != nil {
		k8sManager.AddSessionDeletedHandler(func(ctx context.Context, sess entities.Session) {
			ks, ok := sess.(*services.KubernetesSession)
			if !ok {
				return
			}
			req := ks.Request()
			if len(req.MemoryKey) == 0 {
				return
			}
			s.dumpSessionToMemory(sess.ID(), ks, req)
		})
		log.Printf("[SERVER] Memory dump handler registered for session deletion")
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
		MemoryKey:                startReq.MemoryKey,
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

	// Delete associated tasks for this session (cascade delete)
	if s.taskRepo != nil {
		ctx := context.Background()
		tasks, err := s.taskRepo.List(ctx, portrepos.TaskFilter{SessionID: sessionID})
		if err != nil {
			log.Printf("[SESSION] Warning: failed to list tasks for session %s: %v", sessionID, err)
		} else {
			for _, task := range tasks {
				if err := s.taskRepo.Delete(ctx, task.ID()); err != nil {
					log.Printf("[SESSION] Warning: failed to delete task %s for session %s: %v", task.ID(), sessionID, err)
				}
			}
			if len(tasks) > 0 {
				log.Printf("[SESSION] Deleted %d tasks associated with session %s", len(tasks), sessionID)
			}
		}
	}

	return s.sessionManager.DeleteSession(sessionID)
}

// dumpSessionToMemory fetches messages from the session, stores them as a draft memory,
// and creates an integration session to summarize and merge the draft into permanent memory.
// All errors are logged and non-fatal — session deletion continues regardless.
func (s *Server) dumpSessionToMemory(sessionID string, session *services.KubernetesSession, req *entities.RunServerRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. メッセージ取得
	messages, err := s.sessionManager.GetMessages(ctx, sessionID)
	if err != nil {
		log.Printf("[MEMORY_DUMP] Failed to get messages for session %s: %v", sessionID, err)
		return
	}
	if len(messages) == 0 {
		log.Printf("[MEMORY_DUMP] No messages to dump for session %s, skipping", sessionID)
		return
	}

	// 2. メッセージをフォーマット
	content := formatMessagesForDump(messages)

	// 3. ドラフトメモリのタグ = MemoryKey + draft=true
	tags := make(map[string]string, len(req.MemoryKey)+1)
	for k, v := range req.MemoryKey {
		tags[k] = v
	}
	tags["draft"] = "true"

	// 4. ドラフトメモリ保存
	memID := uuid.New().String()
	title := fmt.Sprintf("Draft: Session %s (%s)", sessionID[:8], time.Now().Format("2006-01-02 15:04"))
	memory := entities.NewMemoryWithTags(memID, title, content, req.Scope, req.UserID, req.TeamID, tags)
	if err := s.memoryRepo.Create(context.Background(), memory); err != nil {
		log.Printf("[MEMORY_DUMP] Failed to save draft memory for session %s: %v", sessionID, err)
		return
	}
	log.Printf("[MEMORY_DUMP] Saved draft memory %s for session %s", memID, sessionID)

	// 5. 統合セッション作成
	s.createMemoryIntegrationSession(req, memID)
}

// createMemoryIntegrationSession creates a hidden oneshot session whose task is to
// summarize the given draft memory and merge it into the permanent memories.
func (s *Server) createMemoryIntegrationSession(req *entities.RunServerRequest, draftMemoryID string) {
	// MemoryKey フラグ生成（決定的な順序）
	keys := make([]string, 0, len(req.MemoryKey))
	for k := range req.MemoryKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var tagFlagParts []string // for --tag k=v (used in memory list)
	var keyFlagParts []string // for --key k=v (used in memory upsert)
	for _, k := range keys {
		tagFlagParts = append(tagFlagParts, fmt.Sprintf("--tag %s=%s", k, req.MemoryKey[k]))
		keyFlagParts = append(keyFlagParts, fmt.Sprintf("--key %s=%s", k, req.MemoryKey[k]))
	}
	memTagFlags := strings.Join(tagFlagParts, " ")
	memKeyFlags := strings.Join(keyFlagParts, " ")

	scope := "user"
	if req.Scope == entities.ScopeTeam {
		scope = "team"
	}

	prompt := buildIntegrationPrompt(memTagFlags, memKeyFlags, scope, draftMemoryID)

	// 削除されたセッションの環境変数（AGENTAPI_KEY 等）を引き継ぐ
	env := make(map[string]string, len(req.Environment))
	for k, v := range req.Environment {
		env[k] = v
	}

	integrationReq := &entities.RunServerRequest{
		UserID:         req.UserID,
		Teams:          req.Teams,
		Scope:          req.Scope,
		TeamID:         req.TeamID,
		Tags:           map[string]string{"hidden": "true"},
		MemoryKey:      nil, // MemoryKey を渡さない: 統合セッション削除時に再ダンプが走るのを防ぐ
		InitialMessage: prompt,
		Oneshot:        true,
		Environment:    env,
	}

	newSessionID := uuid.New().String()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := s.sessionManager.CreateSession(ctx, newSessionID, integrationReq, nil); err != nil {
		log.Printf("[MEMORY_DUMP] Failed to create integration session: %v", err)
		return
	}
	log.Printf("[MEMORY_DUMP] Created integration session %s for memory consolidation (draft: %s)", newSessionID, draftMemoryID)
}

// formatMessagesForDump formats a slice of messages as a markdown conversation log.
func formatMessagesForDump(messages []portrepos.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		fmt.Fprintf(&sb, "## [%s] %s\n\n%s\n\n---\n\n",
			msg.Timestamp.Format("2006-01-02 15:04:05"), msg.Role, msg.Content)
	}
	return sb.String()
}

// buildIntegrationPrompt generates the initial message for the memory integration session.
// memTagFlags: space-joined "--tag k=v" flags for memory list
// memKeyFlags: space-joined "--key k=v" flags for memory upsert
func buildIntegrationPrompt(memTagFlags, memKeyFlags, scope, draftMemoryID string) string {
	return fmt.Sprintf(`あなたはメモリ統合エージェントです。以下のタスクを順番に実行してください。

## タスク

1. ドラフトメモリ（ID: %s）の内容を取得する:
   agentapi-proxy client memory get %s

2. 既存の永続メモリ一覧を取得する:
   agentapi-proxy client memory list %s --scope %s --exclude-tag draft=true
   （CLAUDE.md にすでに注入済みのメモリも参照してください）

3. ドラフトの内容を分析・要約し、既存メモリとの重複を避けながら統合する。
   統合した内容を /tmp/integrated_memory.md に保存する。

4. 統合した内容でメモリを作成または更新する:
   agentapi-proxy client memory upsert %s --scope %s --title "統合メモリ" --content-file /tmp/integrated_memory.md
   （既存メモリがある場合は更新、ない場合は新規作成）

5. 作業完了後、必ずドラフトメモリ（ID: %s）を削除する:
   agentapi-proxy client memory delete %s

重要: ステップ5のドラフトメモリ削除は必ず実行してください。すべての作業が完了したら、その旨を報告してください。`, draftMemoryID, draftMemoryID, memTagFlags, scope, memKeyFlags, scope, draftMemoryID, draftMemoryID)
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

// GetTaskRepository returns the task repository
func (s *Server) GetTaskRepository() portrepos.TaskRepository {
	return s.taskRepo
}

// GetTaskGroupRepository returns the task group repository
func (s *Server) GetTaskGroupRepository() portrepos.TaskGroupRepository {
	return s.taskGroupRepo
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
