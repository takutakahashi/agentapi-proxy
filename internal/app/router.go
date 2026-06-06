package app

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/personal_api_key"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/spec"
)

// Router handles route registration and management
type Router struct {
	echo     *echo.Echo
	server   *Server
	handlers *HandlerRegistry
}

// HandlerRegistry contains all handlers
type HandlerRegistry struct {
	notificationHandlers      *controllers.NotificationHandlers
	healthController          *controllers.HealthController
	sessionController         *controllers.SessionController
	acpController             *controllers.ACPController
	settingsController        *controllers.SettingsController
	credentialsController     *controllers.CredentialsController
	codexDeviceAuthController *controllers.CodexDeviceAuthController
	userController            *controllers.UserController
	shareController           *controllers.ShareController
	personalAPIKeyController  *controllers.PersonalAPIKeyController
	memoryController          *controllers.MemoryController
	sandboxPolicyController   *controllers.SandboxPolicyController
	taskController            *controllers.TaskController
	taskGroupController       *controllers.TaskGroupController
	fileController            *controllers.FileController
	sessionProfileController  *controllers.SessionProfileController
	provisionerController     *controllers.ProvisionerController
	customHandlers            []CustomHandler
}

// CustomHandler interface for adding custom routes
type CustomHandler interface {
	RegisterRoutes(e *echo.Echo, server *Server) error
	GetName() string
}

// NewRouter creates a new Router instance
func NewRouter(e *echo.Echo, server *Server) *Router {
	// Create settings controller
	var gitSyncKMSKeyARN, gitSyncAWSRegion string
	if cfg := server.GetConfig(); cfg != nil {
		gitSyncKMSKeyARN = cfg.GitSync.Encryption.KMSKeyARN
		gitSyncAWSRegion = cfg.GitSync.Encryption.AWSRegion
	}
	settingsController := controllers.NewSettingsController(server.settingsRepo, server.notificationSvc, gitSyncKMSKeyARN, gitSyncAWSRegion)

	// Create credentials controller
	var credentialsController *controllers.CredentialsController
	if server.credentialsRepo != nil {
		credentialsController = controllers.NewCredentialsController(server.credentialsRepo)
		log.Printf("[ROUTER] Credentials controller initialized")
	}

	// Create Codex device auth controller (requires credentials repo)
	var codexDeviceAuthController *controllers.CodexDeviceAuthController
	if server.credentialsRepo != nil {
		codexDeviceAuthController = controllers.NewCodexDeviceAuthController(server.credentialsRepo)
		log.Printf("[ROUTER] Codex device auth controller initialized")
	}

	// Create session controller with proper dependencies
	// server implements SessionManagerProvider interface via GetSessionManager()
	// Note: ServiceAccount creation for team-scoped sessions is now handled in
	// KubernetesSessionManager.CreateSession() via the injected ServiceAccountEnsurer.
	sessionController := controllers.NewSessionController(
		server, // Server implements SessionManagerProvider interface
		server, // Server implements SessionCreator interface
		controllers.WithSessionRouteRepository(server.GetSessionRouteRepository()),
		controllers.WithSettingsRepository(server.settingsRepo),
		controllers.WithSessionProfileRepository(server.sessionProfileRepo),
	)

	// Create share controller if share repository is available
	var shareController *controllers.ShareController
	if server.shareRepo != nil {
		shareController = controllers.NewShareController(
			server, // Server implements SessionManagerProvider interface
			server.shareRepo,
		)
		log.Printf("[ROUTER] Share controller initialized")
	}

	// Create personal API key controller if session manager is Kubernetes-based
	var personalAPIKeyController *controllers.PersonalAPIKeyController
	if k8sManager, ok := server.sessionManager.(*services.KubernetesSessionManager); ok {
		apiKeyRepo := repositories.NewKubernetesPersonalAPIKeyRepository(
			k8sManager.GetClient(),
			k8sManager.GetNamespace(),
		)
		getOrCreatePersonalAPIKeyUC := personal_api_key.NewGetOrCreatePersonalAPIKeyUseCase(apiKeyRepo)

		// Get auth service for loading API keys into memory
		var authService controllers.AuthServiceForPersonalAPIKey
		if simpleAuth, ok := server.container.AuthService.(*services.SimpleAuthService); ok {
			authService = simpleAuth
		}

		personalAPIKeyController = controllers.NewPersonalAPIKeyController(getOrCreatePersonalAPIKeyUC, authService)
		log.Printf("[ROUTER] Personal API key controller initialized")
	}

	// Create memory controller if memory repository is available
	var memoryController *controllers.MemoryController
	if server.memoryRepo != nil {
		memoryController = controllers.NewMemoryController(server.memoryRepo)
		log.Printf("[ROUTER] Memory controller initialized")
	}

	// Create sandbox policy controller if sandbox policy repository is available
	var sandboxPolicyController *controllers.SandboxPolicyController
	if server.sandboxPolicyRepo != nil {
		sandboxPolicyController = controllers.NewSandboxPolicyController(server.sandboxPolicyRepo, server.sandboxDomainRepo)
		log.Printf("[ROUTER] Sandbox policy controller initialized")
	}

	// Create task controller if task repository is available
	var taskController *controllers.TaskController
	if server.taskRepo != nil {
		taskController = controllers.NewTaskController(server.taskRepo)
		log.Printf("[ROUTER] Task controller initialized")
	}

	// Create task group controller if task group repository is available
	var taskGroupController *controllers.TaskGroupController
	if server.taskGroupRepo != nil {
		taskGroupController = controllers.NewTaskGroupController(server.taskGroupRepo)
		log.Printf("[ROUTER] Task group controller initialized")
	}

	// Create file controller if user file repository is available
	var fileController *controllers.FileController
	if server.userFileRepo != nil {
		fileController = controllers.NewFileController(server.userFileRepo)
		log.Printf("[ROUTER] File controller initialized")
	}

	// Create session profile controller if session profile repository is available
	var sessionProfileController *controllers.SessionProfileController
	if server.sessionProfileRepo != nil {
		sessionProfileController = controllers.NewSessionProfileController(server.sessionProfileRepo)
		log.Printf("[ROUTER] Session profile controller initialized")
	}

	var provisionerController *controllers.ProvisionerController
	if k8sManager, ok := server.sessionManager.(*services.KubernetesSessionManager); ok {
		provisionerController = controllers.NewProvisionerController(k8sManager)
		log.Printf("[ROUTER] Provisioner controller initialized")
	}

	acpController := controllers.NewACPController(server, server)

	return &Router{
		echo:   e,
		server: server,
		handlers: &HandlerRegistry{
			notificationHandlers:      controllers.NewNotificationHandlers(server.notificationSvc, server.sessionManager),
			healthController:          controllers.NewHealthController(),
			sessionController:         sessionController,
			acpController:             acpController,
			settingsController:        settingsController,
			credentialsController:     credentialsController,
			codexDeviceAuthController: codexDeviceAuthController,
			userController:            controllers.NewUserController(),
			shareController:           shareController,
			personalAPIKeyController:  personalAPIKeyController,
			memoryController:          memoryController,
			sandboxPolicyController:   sandboxPolicyController,
			taskController:            taskController,
			taskGroupController:       taskGroupController,
			fileController:            fileController,
			sessionProfileController:  sessionProfileController,
			provisionerController:     provisionerController,
			customHandlers:            make([]CustomHandler, 0),
		},
	}
}

// AddCustomHandler adds a custom handler to the registry
func (r *Router) AddCustomHandler(handler CustomHandler) {
	r.handlers.customHandlers = append(r.handlers.customHandlers, handler)
	log.Printf("Added custom handler: %s", handler.GetName())
}

// RegisterRoutes registers all routes
func (r *Router) RegisterRoutes() error {
	// Register core routes
	if err := r.registerCoreRoutes(); err != nil {
		return err
	}

	// Register conditional routes based on configuration
	if err := r.registerConditionalRoutes(); err != nil {
		return err
	}

	// Register custom handlers
	if err := r.registerCustomHandlers(); err != nil {
		return err
	}

	return nil
}

// registerCoreRoutes registers the core routes that are always available
func (r *Router) registerCoreRoutes() error {
	// Health check endpoint
	r.echo.GET("/health", r.handlers.healthController.HealthCheck)

	// Static file serving for /public/* (no authentication required)
	// Embedded from spec/openapi.json - independent of working directory
	// Must be registered before the /:sessionId/* catch-all route
	r.echo.StaticFS("/public", spec.FS())
	log.Printf("[ROUTES] Static file serving registered at /public/*")

	// ACP (Agent Client Protocol) JSON-RPC 2.0 endpoints
	log.Printf("[ROUTES] Registering ACP endpoints...")
	r.echo.POST("/acp", r.handlers.acpController.HandleRPC, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	r.echo.GET("/acp", r.handlers.acpController.HandleSessionSSE, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	r.echo.OPTIONS("/acp", func(c echo.Context) error {
		log.Printf("[ACP] OPTIONS /acp preflight: origin=%s", c.Request().Header.Get("Origin"))
		c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, Acp-Session-Id")
		c.Response().Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		return c.NoContent(http.StatusNoContent)
	})
	log.Printf("[ROUTES] ACP endpoints registered")

	// Session management routes
	log.Printf("[ROUTES] Registering session management endpoints...")
	r.echo.POST("/start", r.handlers.sessionController.StartSession)
	r.echo.GET("/search", r.handlers.sessionController.SearchSessions)
	r.echo.DELETE("/sessions/:sessionId", r.handlers.sessionController.DeleteSession)

	// Proxy-wide session status push endpoints (registered before /:sessionId/* catch-all)
	r.echo.GET("/sessions/status/stream", r.handlers.sessionController.StreamSessionsStatus)
	r.echo.GET("/sessions/status/wait", r.handlers.sessionController.WaitSessionsStatus)
	// Per-session message update long-poll endpoint (must be before /:sessionId/* catch-all)
	r.echo.GET("/sessions/:sessionId/messages/wait", r.handlers.sessionController.WaitSessionMessages)
	// Sandbox domain viewer (must be before /:sessionId/* catch-all)
	r.echo.GET("/sessions/:sessionId/sandbox-domains", r.handlers.sessionController.GetSessionSandboxDomains,
		auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	log.Printf("[ROUTES] Session status/message push endpoints registered (SSE + long-poll)")

	if r.handlers.provisionerController != nil {
		r.echo.POST("/internal/session-provisioners/connect", r.handlers.provisionerController.Connect)
		r.echo.GET("/internal/session-provisioners/:sessionId/provision-requests", r.handlers.provisionerController.GetProvisionRequest)
		r.echo.POST("/internal/session-provisioners/:sessionId/provision-requests/:requestId/status", r.handlers.provisionerController.UpdateProvisionRequestStatus)
		r.echo.GET("/internal/session-allocations/next", r.handlers.provisionerController.GetNextSessionAllocation)
		r.echo.POST("/internal/session-allocations/:sessionId/result", r.handlers.provisionerController.CompleteSessionAllocation)
		log.Printf("[ROUTES] Internal provisioner endpoints registered")
	}

	// Session sharing routes
	if r.handlers.shareController != nil {
		log.Printf("[ROUTES] Registering session sharing endpoints...")
		r.echo.POST("/sessions/:sessionId/share", r.handlers.shareController.CreateShare)
		r.echo.GET("/sessions/:sessionId/share", r.handlers.shareController.GetShare)
		r.echo.DELETE("/sessions/:sessionId/share", r.handlers.shareController.DeleteShare)
		// Add OPTIONS handler for session share endpoints (CORS preflight)
		r.echo.OPTIONS("/sessions/:sessionId/share", func(c echo.Context) error {
			c.Response().Header().Set("Access-Control-Allow-Origin", "*")
			c.Response().Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
			c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
			c.Response().Header().Set("Access-Control-Max-Age", "86400")
			return c.NoContent(http.StatusNoContent)
		})
		// Shared session access route (read-only)
		r.echo.Any("/s/:shareToken/*", r.handlers.shareController.RouteToSharedSession)
		r.echo.OPTIONS("/s/:shareToken/*", func(c echo.Context) error {
			c.Response().Header().Set("Access-Control-Allow-Origin", "*")
			c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
			c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
			c.Response().Header().Set("Access-Control-Max-Age", "86400")
			return c.NoContent(http.StatusNoContent)
		})
		log.Printf("[ROUTES] Session sharing endpoints registered")
	}

	// Session proxy route
	r.echo.Any("/:sessionId/*", r.handlers.sessionController.RouteToSession)
	log.Printf("[ROUTES] Session management endpoints registered")

	// Add explicit OPTIONS handler for DELETE endpoint to ensure CORS preflight works
	r.echo.OPTIONS("/sessions/:sessionId", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	// Add explicit OPTIONS handler for session proxy routes to ensure CORS preflight works
	r.echo.OPTIONS("/:sessionId/*", func(c echo.Context) error {
		// Set CORS headers for preflight
		c.Response().Header().Set("Access-Control-Allow-Origin", "*")
		c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
		c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
		c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
		c.Response().Header().Set("Access-Control-Max-Age", "86400")
		return c.NoContent(http.StatusNoContent)
	})

	return nil
}

// registerConditionalRoutes registers routes based on server configuration
func (r *Router) registerConditionalRoutes() error {
	// User info endpoint (requires authentication)
	log.Printf("[ROUTES] Registering user info endpoint...")
	r.echo.GET("/user/info", r.handlers.userController.GetUserInfo, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	log.Printf("[ROUTES] User info endpoint registered")

	// Add notification routes if service is available
	if r.server.notificationSvc != nil {
		log.Printf("[ROUTES] Registering notification endpoints...")
		// UI-compatible routes (proxied from agentapi-ui)
		r.echo.POST("/notification/subscribe", r.handlers.notificationHandlers.Subscribe, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/notification/subscribe", r.handlers.notificationHandlers.GetSubscriptions, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.DELETE("/notification/subscribe", r.handlers.notificationHandlers.DeleteSubscription, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))

		// Internal routes
		r.echo.POST("/notifications/webhook", r.handlers.notificationHandlers.Webhook)
		r.echo.GET("/notifications/history", r.handlers.notificationHandlers.GetHistory, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/notifications/send", r.handlers.notificationHandlers.SendNotification, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		log.Printf("[ROUTES] Notification endpoints registered")
	} else {
		log.Printf("[ROUTES] Notification service not available, skipping notification routes")
	}

	// Add settings routes if settings repository is available (Kubernetes mode only)
	if r.server.settingsRepo != nil && r.handlers.settingsController != nil {
		log.Printf("[ROUTES] Registering settings endpoints...")
		r.echo.GET("/settings/managers", r.handlers.settingsController.GetAvailableManagers, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/settings/:name", r.handlers.settingsController.GetSettings, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/settings/:name", r.handlers.settingsController.UpdateSettings, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/settings/:name", r.handlers.settingsController.DeleteSettings, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/settings/:name/sync", r.handlers.settingsController.DeleteGitSync, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Settings endpoints registered")
	} else {
		log.Printf("[ROUTES] Settings repository not available, skipping settings routes")
	}

	// Add credentials routes if credentials repository is available (Kubernetes mode only)
	if r.server.credentialsRepo != nil && r.handlers.credentialsController != nil {
		log.Printf("[ROUTES] Registering credentials endpoints...")
		r.echo.GET("/credentials/:name", r.handlers.credentialsController.GetCredentials, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/credentials/:name", r.handlers.credentialsController.UploadCredentials, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/credentials/:name", r.handlers.credentialsController.DeleteCredentials, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Credentials endpoints registered")
	} else {
		log.Printf("[ROUTES] Credentials repository not available, skipping credentials routes")
	}

	// Add Codex device auth routes (requires credentials repo)
	if r.handlers.codexDeviceAuthController != nil {
		log.Printf("[ROUTES] Registering Codex device auth endpoints...")
		r.echo.GET("/codex/device-auth/config", r.handlers.codexDeviceAuthController.GetConfig, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/codex/device-auth", r.handlers.codexDeviceAuthController.StartDeviceAuth, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/codex/device-auth/token", r.handlers.codexDeviceAuthController.PollDeviceAuth, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Codex device auth endpoints registered")
	}

	// Add personal API key routes if controller is available (Kubernetes mode only)
	if r.handlers.personalAPIKeyController != nil {
		log.Printf("[ROUTES] Registering personal API key endpoints...")
		r.echo.GET("/users/me/api-key", r.handlers.personalAPIKeyController.GetOrCreatePersonalAPIKey, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/users/me/api-key", r.handlers.personalAPIKeyController.GetOrCreatePersonalAPIKey, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		log.Printf("[ROUTES] Personal API key endpoints registered")
	} else {
		log.Printf("[ROUTES] Personal API key controller not available, skipping personal API key routes")
	}

	// Add memory routes if memory repository is available (Kubernetes mode only)
	if r.server.memoryRepo != nil && r.handlers.memoryController != nil {
		log.Printf("[ROUTES] Registering memory endpoints...")
		r.echo.POST("/memories", r.handlers.memoryController.CreateMemory, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/memories", r.handlers.memoryController.ListMemories, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/memories/:memoryId", r.handlers.memoryController.GetMemory, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/memories/:memoryId", r.handlers.memoryController.UpdateMemory, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/memories/:memoryId", r.handlers.memoryController.DeleteMemory, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Memory endpoints registered")
	} else {
		log.Printf("[ROUTES] Memory repository not available, skipping memory routes")
	}

	// Add sandbox policy routes if sandbox policy repository is available (Kubernetes mode only)
	if r.server.sandboxPolicyRepo != nil && r.handlers.sandboxPolicyController != nil {
		log.Printf("[ROUTES] Registering sandbox policy endpoints...")
		r.echo.POST("/sandbox-policies", r.handlers.sandboxPolicyController.CreateSandboxPolicy, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/sandbox-policies", r.handlers.sandboxPolicyController.ListSandboxPolicies, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/sandbox-policies/:id", r.handlers.sandboxPolicyController.GetSandboxPolicy, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/sandbox-policies/:id/domains", r.handlers.sandboxPolicyController.GetSandboxPolicyDomains, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/sandbox-policies/:id/domains/ignored", r.handlers.sandboxPolicyController.UpdateIgnoredDomains, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.PUT("/sandbox-policies/:id", r.handlers.sandboxPolicyController.UpdateSandboxPolicy, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/sandbox-policies/:id", r.handlers.sandboxPolicyController.DeleteSandboxPolicy, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Sandbox policy endpoints registered")
	} else {
		log.Printf("[ROUTES] Sandbox policy repository not available, skipping sandbox policy routes")
	}

	// Add task routes if task repository is available (Kubernetes mode only)
	if r.server.taskRepo != nil && r.handlers.taskController != nil {
		log.Printf("[ROUTES] Registering task endpoints...")
		r.echo.POST("/tasks", r.handlers.taskController.CreateTask, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/tasks", r.handlers.taskController.ListTasks, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/tasks/:taskId", r.handlers.taskController.GetTask, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/tasks/:taskId", r.handlers.taskController.UpdateTask, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/tasks/:taskId", r.handlers.taskController.DeleteTask, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Task endpoints registered")
	} else {
		log.Printf("[ROUTES] Task repository not available, skipping task routes")
	}

	// Add task group routes if task group repository is available (Kubernetes mode only)
	if r.server.taskGroupRepo != nil && r.handlers.taskGroupController != nil {
		log.Printf("[ROUTES] Registering task group endpoints...")
		r.echo.POST("/task-groups", r.handlers.taskGroupController.CreateTaskGroup, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/task-groups", r.handlers.taskGroupController.ListTaskGroups, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/task-groups/:groupId", r.handlers.taskGroupController.GetTaskGroup, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/task-groups/:groupId", r.handlers.taskGroupController.UpdateTaskGroup, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/task-groups/:groupId", r.handlers.taskGroupController.DeleteTaskGroup, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Task group endpoints registered")
	} else {
		log.Printf("[ROUTES] Task group repository not available, skipping task group routes")
	}

	// Add file routes if user file repository is available (Kubernetes mode only)
	if r.server.userFileRepo != nil && r.handlers.fileController != nil {
		log.Printf("[ROUTES] Registering user file endpoints...")
		r.echo.POST("/files", r.handlers.fileController.CreateFile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/files", r.handlers.fileController.ListFiles, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/files/:fileId", r.handlers.fileController.GetFile, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/files/:fileId", r.handlers.fileController.UpdateFile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/files/:fileId", r.handlers.fileController.DeleteFile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] User file endpoints registered")
	} else {
		log.Printf("[ROUTES] User file repository not available, skipping file routes")
	}

	// Add session profile routes if session profile repository is available (Kubernetes mode only)
	if r.server.sessionProfileRepo != nil && r.handlers.sessionProfileController != nil {
		log.Printf("[ROUTES] Registering session profile endpoints...")
		r.echo.POST("/session-profiles", r.handlers.sessionProfileController.CreateSessionProfile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/session-profiles", r.handlers.sessionProfileController.ListSessionProfiles, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/session-profiles/:id", r.handlers.sessionProfileController.GetSessionProfile, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/session-profiles/:id", r.handlers.sessionProfileController.UpdateSessionProfile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/session-profiles/:id", r.handlers.sessionProfileController.DeleteSessionProfile, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Session profile endpoints registered")
	} else {
		log.Printf("[ROUTES] Session profile repository not available, skipping session profile routes")
	}

	return nil
}

// registerCustomHandlers registers all custom handlers
func (r *Router) registerCustomHandlers() error {
	for _, handler := range r.handlers.customHandlers {
		log.Printf("[ROUTES] Registering custom handler: %s", handler.GetName())
		if err := handler.RegisterRoutes(r.echo, r.server); err != nil {
			log.Printf("[ROUTES] Failed to register custom handler %s: %v", handler.GetName(), err)
			return err
		}
		log.Printf("[ROUTES] Successfully registered custom handler: %s", handler.GetName())
	}

	return nil
}
