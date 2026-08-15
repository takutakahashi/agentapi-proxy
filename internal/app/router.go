package app

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	apitokenuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/api_token"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/personal_api_key"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/resource_transfer"
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
	notificationHandlers           *controllers.NotificationHandlers
	healthController               *controllers.HealthController
	sessionController              *controllers.SessionController
	acpController                  *controllers.ACPController
	settingsController             *controllers.SettingsController
	adminSettingsController        *controllers.AdminSettingsController
	googleOAuthController          *controllers.GoogleOAuthController
	credentialsController          *controllers.CredentialsController
	codexDeviceAuthController      *controllers.CodexDeviceAuthController
	userController                 *controllers.UserController
	shareController                *controllers.ShareController
	personalAPIKeyController       *controllers.PersonalAPIKeyController
	apiTokenController             *controllers.APITokenController
	memoryController               *controllers.MemoryController
	sandboxPolicyController        *controllers.SandboxPolicyController
	resourceTransferController     *controllers.ResourceTransferController
	fileController                 *controllers.FileController
	assetController                *controllers.AssetController
	sessionProfileController       *controllers.SessionProfileController
	provisionerController          *controllers.ProvisionerController
	externalAllocationController   *controllers.ProvisionerController
	workerControlController        *controllers.WorkerControlController
	sessionControlController       *controllers.SessionControlController
	sessionControlReaderController *controllers.SessionControlReaderController
	esmControlController           *controllers.ESMControlController
	sessionRuntimeController       *controllers.SessionRuntimeController
	sessionPoolController          *controllers.SessionPoolController
	usageController                *controllers.UsageController
	customHandlers                 []CustomHandler
}

// CustomHandler interface for adding custom routes
type CustomHandler interface {
	RegisterRoutes(e *echo.Echo) error
	GetName() string
}

// NewRouter creates a new Router instance
func NewRouter(e *echo.Echo, server *Server) *Router {
	// Create settings controller
	settingsController := controllers.NewSettingsController(server.settingsRepo, server.notificationSvc)
	settingsController.SetESMControlTunnel(server.esmControlTunnel)
	sessionPoolController := controllers.NewSessionPoolController(server.sessionRunnerStore, server.sessionRouteRepo)

	var apiKeyRepo *repositories.KubernetesPersonalAPIKeyRepository
	var adminSettingsController *controllers.AdminSettingsController
	if server.persistenceClient != nil {
		apiKeyRepo = repositories.NewKubernetesPersonalAPIKeyRepository(
			server.GetPersistenceClient(),
			server.namespace,
		)
		if server.kvStore != nil {
			adminSettingsController = controllers.NewAdminSettingsController(server.kvStore, server.namespace, server.GetConfig()).WithRuntimeConfigProvider(server.GetConfigProvider())
		}
	}

	var googleOAuthController *controllers.GoogleOAuthController
	if cfg := server.GetConfig(); cfg != nil {
		googleOAuthController = controllers.NewGoogleOAuthController(cfg.Scia, server.GetPersistenceClient(), server.namespace)
		if apiKeyRepo != nil {
			googleOAuthController.WithPersonalAPIKeyRepository(apiKeyRepo)
		}
	}

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
		controllers.WithESMControlTunnel(server.esmControlTunnel),
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
	if apiKeyRepo != nil {
		getOrCreatePersonalAPIKeyUC := personal_api_key.NewGetOrCreatePersonalAPIKeyUseCase(apiKeyRepo)

		// Get auth service for loading API keys into memory
		var authService controllers.AuthServiceForPersonalAPIKey
		if simpleAuth, ok := server.container.AuthService.(*services.SimpleAuthService); ok {
			authService = simpleAuth
		}

		personalAPIKeyController = controllers.NewPersonalAPIKeyController(getOrCreatePersonalAPIKeyUC, authService)
		log.Printf("[ROUTER] Personal API key controller initialized")
	}

	// Create the unified multi API token controller (list/create/get/delete)
	// when the new API token repository is available (Kubernetes mode only).
	var apiTokenController *controllers.APITokenController
	if server.apiTokenRepo != nil {
		var tokenAuthService apitokenuc.AuthService
		if simpleAuth, ok := server.container.AuthService.(*services.SimpleAuthService); ok {
			tokenAuthService = simpleAuth
		}
		createUC := apitokenuc.NewCreateAPITokenUseCase(server.apiTokenRepo, tokenAuthService)
		listUC := apitokenuc.NewListAPITokenUseCase(server.apiTokenRepo)
		getUC := apitokenuc.NewGetAPITokenUseCase(server.apiTokenRepo)
		deleteUC := apitokenuc.NewDeleteAPITokenUseCase(server.apiTokenRepo, tokenAuthService)
		apiTokenController = controllers.NewAPITokenController(createUC, listUC, getUC, deleteUC)
		log.Printf("[ROUTER] API token controller initialized")
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

	resourceTransferOptions := []resource_transfer.Option{
		resource_transfer.WithMemoryRepository(server.memoryRepo),
		resource_transfer.WithSessionProfileRepository(server.sessionProfileRepo),
		resource_transfer.WithSandboxPolicyRepository(server.sandboxPolicyRepo),
	}
	if server.persistenceClient != nil {
		client := server.GetPersistenceClient()
		namespace := server.namespace
		resourceTransferOptions = append(resourceTransferOptions,
			resource_transfer.WithWebhookRepository(repositories.NewKubernetesWebhookRepository(client, namespace)),
			resource_transfer.WithSlackBotRepository(repositories.NewKubernetesSlackBotRepository(client, namespace)),
		)
	}
	resourceTransferController := controllers.NewResourceTransferController(resource_transfer.New(resourceTransferOptions...))
	log.Printf("[ROUTER] Resource transfer controller initialized")

	// Create file controller if user file repository is available
	var fileController *controllers.FileController
	if server.userFileRepo != nil {
		fileController = controllers.NewFileController(server.userFileRepo)
		log.Printf("[ROUTER] File controller initialized")
	}

	var assetController *controllers.AssetController
	if server.assetStore != nil {
		assetController = controllers.NewAssetController(server.assetStore)
		log.Printf("[ROUTER] Asset controller initialized")
	}

	// Create session profile controller if session profile repository is available
	var sessionProfileController *controllers.SessionProfileController
	if server.sessionProfileRepo != nil {
		sessionProfileController = controllers.NewSessionProfileController(server.sessionProfileRepo)
		log.Printf("[ROUTER] Session profile controller initialized")
	}

	var provisionerController *controllers.ProvisionerController
	var externalAllocationController *controllers.ProvisionerController
	var workerControlController *controllers.WorkerControlController
	var sessionControlController *controllers.SessionControlController
	var sessionControlReaderController *controllers.SessionControlReaderController
	var esmControlController *controllers.ESMControlController
	var sessionRuntimeController *controllers.SessionRuntimeController
	if k8sManager, ok := server.sessionManager.(*services.KubernetesSessionManager); ok {
		provisionerController = controllers.NewProvisionerController(k8sManager, k8sManager, server.settingsRepo, server.sessionRouteRepo, server.sessionStateStore)
		if server.sessionControlStore != nil {
			sessionControlController = controllers.NewSessionControlController(server.sessionControlStore, k8sManager)
		}
		if server.esmControlStore != nil {
			esmControlController = controllers.NewESMControlController(server.esmControlStore, provisionerController)
			if server.sessionRouteRepo != nil {
				sessionRuntimeController = controllers.NewSessionRuntimeController(server.esmControlStore, server.sessionRouteRepo)
			}
		}
		log.Printf("[ROUTER] Provisioner controller initialized")
	}
	if queue, ok := server.sessionManager.(sessionallocation.Queue); ok {
		if provisionerController != nil {
			externalAllocationController = provisionerController
		} else {
			externalAllocationController = controllers.NewProvisionerController(nil, queue, server.settingsRepo, server.sessionRouteRepo)
		}
		if server.esmControlStore != nil {
			esmControlController = controllers.NewESMControlController(server.esmControlStore, externalAllocationController)
			if server.sessionRouteRepo != nil {
				sessionRuntimeController = controllers.NewSessionRuntimeController(server.esmControlStore, server.sessionRouteRepo)
			}
		}
	}
	if cfg := server.GetConfig(); cfg != nil && cfg.Worker.ControlAPIToken != "" {
		workerControlController = controllers.NewWorkerControlController(server.sessionManager, cfg.Worker.ControlAPIToken, server, server.sessionRouteRepo)
		log.Printf("[ROUTER] Worker control controller initialized")
	}
	if server.sessionControlStore != nil {
		sessionControlReaderController = controllers.NewSessionControlReaderController(server.sessionControlStore, server.sessionManager)
	}

	acpController := controllers.NewACPController(server, server, server.GetSessionRouteRepository())
	acpController.SetESMControlTunnel(server.esmControlTunnel)
	var usageController *controllers.UsageController
	if server.usageRepo != nil {
		usageController = controllers.NewUsageController(server.usageRepo, server.sessionManager)
	}

	return &Router{
		echo:   e,
		server: server,
		handlers: &HandlerRegistry{
			notificationHandlers:           controllers.NewNotificationHandlers(server.notificationSvc, server.sessionManager),
			healthController:               controllers.NewHealthController(),
			sessionController:              sessionController,
			acpController:                  acpController,
			settingsController:             settingsController,
			adminSettingsController:        adminSettingsController,
			googleOAuthController:          googleOAuthController,
			credentialsController:          credentialsController,
			codexDeviceAuthController:      codexDeviceAuthController,
			userController:                 controllers.NewUserController(),
			shareController:                shareController,
			personalAPIKeyController:       personalAPIKeyController,
			apiTokenController:             apiTokenController,
			memoryController:               memoryController,
			sandboxPolicyController:        sandboxPolicyController,
			resourceTransferController:     resourceTransferController,
			fileController:                 fileController,
			assetController:                assetController,
			sessionProfileController:       sessionProfileController,
			provisionerController:          provisionerController,
			externalAllocationController:   externalAllocationController,
			workerControlController:        workerControlController,
			sessionControlController:       sessionControlController,
			sessionControlReaderController: sessionControlReaderController,
			esmControlController:           esmControlController,
			sessionRuntimeController:       sessionRuntimeController,
			sessionPoolController:          sessionPoolController,
			usageController:                usageController,
			customHandlers:                 make([]CustomHandler, 0),
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

	r.registerSessionProxyRoutes()

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
	if r.handlers.usageController != nil {
		r.echo.POST("/internal/usage-events", r.handlers.usageController.Create,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/usage", r.handlers.usageController.Get,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/usage/export.parquet", r.handlers.usageController.ExportParquet,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/sessions/:sessionId/usage", r.handlers.usageController.GetSession,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	}
	r.echo.PATCH("/sessions/:sessionId/annotations", r.handlers.sessionController.UpdateSessionAnnotations)
	r.echo.POST("/sessions/:sessionId/resume", r.handlers.sessionController.ResumeSession)
	r.echo.DELETE("/sessions/:sessionId", r.handlers.sessionController.DeleteSession)
	if r.handlers.sessionPoolController != nil {
		r.echo.GET("/available-session-pools", r.handlers.sessionPoolController.ListAvailablePools,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/session-pool-preference", r.handlers.sessionPoolController.PutPreference,
			auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/internal/session-runners/register", r.handlers.sessionPoolController.RegisterRunner)
		r.echo.GET("/internal/session-runners/allocations/next", r.handlers.sessionPoolController.ClaimRunnerAllocation)
		r.echo.POST("/internal/session-runners/allocations/:sessionId/ack", r.handlers.sessionPoolController.AckRunnerAllocation)
		r.echo.POST("/internal/session-runners/allocations/:sessionId/fail", r.handlers.sessionPoolController.FailRunnerAllocation)
		r.echo.POST("/internal/session-managers/:id/heartbeat", r.handlers.sessionPoolController.HeartbeatManager)
	}

	// Proxy-wide session status push endpoints (registered before /:sessionId/* catch-all)
	r.echo.GET("/sessions/status/stream", r.handlers.sessionController.StreamSessionsStatus)
	r.echo.GET("/sessions/status/wait", r.handlers.sessionController.WaitSessionsStatus)
	// Per-session message update long-poll endpoint (must be before /:sessionId/* catch-all)
	r.echo.GET("/sessions/:sessionId/messages/wait", r.handlers.sessionController.WaitSessionMessages)
	// Sandbox domain viewer (must be before /:sessionId/* catch-all)
	r.echo.GET("/sessions/:sessionId/sandbox-domains", r.handlers.sessionController.GetSessionSandboxDomains,
		auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	log.Printf("[ROUTES] Session status/message push endpoints registered (SSE + long-poll)")

	if r.handlers.resourceTransferController != nil {
		log.Printf("[ROUTES] Registering resource transfer endpoint...")
		r.echo.POST("/resources/transfer", r.handlers.resourceTransferController.TransferResource, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/resources/*", func(c echo.Context) error {
			if c.Param("*") != "transfer" {
				return echo.NewHTTPError(http.StatusNotFound, "resource endpoint not found")
			}
			return r.handlers.resourceTransferController.TransferResource(c)
		}, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Resource transfer endpoint registered")
	}

	if r.handlers.provisionerController != nil {
		r.echo.POST("/internal/session-provisioners/connect", r.handlers.provisionerController.Connect)
		r.echo.GET("/internal/session-provisioners/:sessionId/provision-requests", r.handlers.provisionerController.GetProvisionRequest)
		r.echo.POST("/internal/session-provisioners/:sessionId/provision-requests/:requestId/status", r.handlers.provisionerController.UpdateProvisionRequestStatus)
		r.echo.GET("/internal/session-allocations/next", r.handlers.provisionerController.GetNextSessionAllocation)
		r.echo.POST("/internal/session-allocations/:sessionId/result", r.handlers.provisionerController.CompleteSessionAllocation)
		r.echo.PUT("/internal/session-state/:sessionId", r.handlers.provisionerController.SaveSessionState)
		r.echo.POST("/internal/session-state/:sessionId/suspend", r.handlers.provisionerController.ScheduleSessionSuspend)
		r.echo.GET("/internal/session-state/:sessionId", r.handlers.provisionerController.LoadSessionState)
		r.echo.POST("/internal/session-state/:sessionId/uploads", r.handlers.provisionerController.BeginSessionStateUpload)
		r.echo.GET("/internal/session-state/:sessionId/uploads/:uploadId/parts/:partNumber", r.handlers.provisionerController.PresignSessionStatePart)
		r.echo.POST("/internal/session-state/:sessionId/uploads/:uploadId/complete", r.handlers.provisionerController.CompleteSessionStateUpload)
		r.echo.DELETE("/internal/session-state/:sessionId/uploads/:uploadId", r.handlers.provisionerController.AbortSessionStateUpload)
		r.echo.GET("/internal/session-state/:sessionId/download-url", r.handlers.provisionerController.PresignSessionStateDownload)
		log.Printf("[ROUTES] Internal provisioner endpoints registered")
	}
	if r.handlers.externalAllocationController != nil {
		r.echo.GET("/internal/external-session-manager/allocations/next", r.handlers.externalAllocationController.GetNextExternalSessionAllocation)
		r.echo.GET("/internal/external-session-manager/runtime-profile", r.handlers.externalAllocationController.GetExternalSessionManagerRuntimeProfile)
		r.echo.POST("/internal/external-session-manager/allocations/:sessionId/result", r.handlers.externalAllocationController.CompleteExternalSessionAllocation)
		log.Printf("[ROUTES] External manager allocation endpoints registered")
	}
	if r.handlers.workerControlController != nil {
		r.echo.POST("/internal/worker/sessions/:sessionId", r.handlers.workerControlController.CreateSession)
		r.echo.GET("/internal/worker/sessions", r.handlers.workerControlController.ListSessions)
		r.echo.DELETE("/internal/worker/sessions/:sessionId", r.handlers.workerControlController.DeleteSession)
		r.echo.POST("/internal/worker/sessions/:sessionId/messages", r.handlers.workerControlController.SendMessage)
		r.echo.POST("/internal/worker/sessions/:sessionId/stop", r.handlers.workerControlController.StopAgent)
		r.echo.GET("/internal/worker/stock", r.handlers.workerControlController.Stock)
		r.echo.POST("/internal/worker/stock", r.handlers.workerControlController.Stock)
		r.echo.DELETE("/internal/worker/stock", r.handlers.workerControlController.Stock)
		log.Printf("[ROUTES] Isolated worker-control endpoints registered")
	}
	if r.handlers.sessionControlController != nil {
		r.echo.GET("/internal/session-control/:sessionId/commands", r.handlers.sessionControlController.WaitCommands)
		r.echo.POST("/internal/session-control/:sessionId/events", r.handlers.sessionControlController.AppendEvents)
		log.Printf("[ROUTES] Internal session control long-poll endpoints registered")
	}
	if r.handlers.sessionControlReaderController != nil {
		r.echo.GET("/sessions/:sessionId/control/events/wait", r.handlers.sessionControlReaderController.WaitEvents,
			auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
	}
	if r.handlers.esmControlController != nil {
		r.echo.GET("/internal/external-session-manager/control/commands", r.handlers.esmControlController.WaitCommands)
		r.echo.POST("/internal/external-session-manager/control/frames", r.handlers.esmControlController.AppendFrames)
		r.echo.GET("/internal/external-session-managers/:managerId/control/commands", r.handlers.esmControlController.WaitCommands)
		r.echo.POST("/internal/external-session-managers/:managerId/control/frames", r.handlers.esmControlController.AppendFrames)
		log.Printf("[ROUTES] Internal outbound ESM control endpoints registered")
	}
	if r.handlers.sessionRuntimeController != nil {
		r.echo.GET("/internal/session-runtime/:sessionId/requests", r.handlers.sessionRuntimeController.WaitRequests)
		r.echo.POST("/internal/session-runtime/:sessionId/frames", r.handlers.sessionRuntimeController.AppendFrames)
		log.Printf("[ROUTES] Direct Session Pod runtime endpoints registered")
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

	// Add explicit OPTIONS handler for DELETE endpoint to ensure CORS preflight works
	r.echo.OPTIONS("/sessions/:sessionId", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	log.Printf("[ROUTES] Session management endpoints registered")

	return nil
}

func (r *Router) registerSessionProxyRoutes() {
	// Session proxy route must be registered after all proxy-level routes so
	// paths such as /resources/transfer are not treated as session IDs.
	r.echo.Any("/:sessionId/*", r.handlers.sessionController.RouteToSession)

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
		r.echo.POST("/external-session-managers/registration-tokens", r.handlers.settingsController.IssueExternalSessionManagerEnrollmentToken, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/external-session-managers/enroll", r.handlers.settingsController.EnrollExternalSessionManager)
		r.echo.GET("/external-session-managers", r.handlers.settingsController.ListExternalSessionManagers, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/external-session-managers/:id", r.handlers.settingsController.GetExternalSessionManager, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PATCH("/external-session-managers/:id", r.handlers.settingsController.PatchExternalSessionManager, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/external-session-managers/:id", r.handlers.settingsController.DeleteExternalSessionManager, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/external-session-managers/:id/rotate-token", r.handlers.settingsController.RotateExternalSessionManagerToken, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.POST("/external-session-managers/:id/heartbeat", r.handlers.settingsController.HeartbeatExternalSessionManager)
		r.echo.GET("/settings/managers", r.handlers.settingsController.GetAvailableManagers, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.GET("/settings/:name", r.handlers.settingsController.GetSettings, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.PUT("/settings/:name", r.handlers.settingsController.UpdateSettings, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.DELETE("/settings/:name", r.handlers.settingsController.DeleteSettings, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Settings endpoints registered")
	} else {
		log.Printf("[ROUTES] Settings repository not available, skipping settings routes")
	}

	if r.handlers.adminSettingsController != nil {
		log.Printf("[ROUTES] Registering admin system settings endpoints...")
		r.echo.GET("/admin/system-settings", r.handlers.adminSettingsController.Get, auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService))
		r.echo.GET("/admin/system-settings/versions", r.handlers.adminSettingsController.ListVersions, auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService))
		r.echo.PUT("/admin/system-settings", r.handlers.adminSettingsController.Put, auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService))
	}
	if r.handlers.sessionPoolController != nil {
		adminOnly := auth.RequirePermission(entities.PermissionAdmin, r.server.container.AuthService)
		r.echo.POST("/admin/session-managers", r.handlers.sessionPoolController.CreateManager, adminOnly)
		r.echo.GET("/admin/session-managers", r.handlers.sessionPoolController.ListManagers, adminOnly)
		r.echo.PATCH("/admin/session-managers/:id", r.handlers.sessionPoolController.PatchManager, adminOnly)
		r.echo.POST("/admin/session-managers/:id/pools", r.handlers.sessionPoolController.CreatePool, adminOnly)
		r.echo.PATCH("/admin/session-managers/:id/pools/:pool", r.handlers.sessionPoolController.PatchPool, adminOnly)
		r.echo.GET("/admin/session-pools", r.handlers.sessionPoolController.ListPools, adminOnly)
		r.echo.POST("/admin/session-pools/:pool/bindings", r.handlers.sessionPoolController.CreateBinding, adminOnly)
		r.echo.GET("/admin/session-pools/:pool/bindings", r.handlers.sessionPoolController.ListBindings, adminOnly)
		r.echo.DELETE("/admin/session-pools/:pool/bindings/:bindingId", r.handlers.sessionPoolController.DeleteBinding, adminOnly)
	}

	if r.handlers.googleOAuthController != nil {
		log.Printf("[ROUTES] Registering scia integration endpoints...")
		r.echo.GET("/integrations", r.handlers.googleOAuthController.GetIntegrations, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/integrations/:id/authorization-url", r.handlers.googleOAuthController.CreateAuthorizationURL, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/integrations/:id/revoke", r.handlers.googleOAuthController.RevokeIntegration, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/integrations/google-oauth/status", r.handlers.googleOAuthController.GetStatus, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		log.Printf("[ROUTES] scia integration endpoints registered")
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

	// Unified multi API token endpoints (list/create/get/delete). Available
	// in Kubernetes mode only, where the API token repository is initialized.
	if r.handlers.apiTokenController != nil {
		log.Printf("[ROUTES] Registering API token endpoints...")
		r.echo.GET("/api-tokens", r.handlers.apiTokenController.List, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.POST("/api-tokens", r.handlers.apiTokenController.Create, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		r.echo.GET("/api-tokens/:tokenId", r.handlers.apiTokenController.Get, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		r.echo.DELETE("/api-tokens/:tokenId", r.handlers.apiTokenController.Delete, auth.RequirePermission(entities.PermissionSessionRead, r.server.container.AuthService))
		log.Printf("[ROUTES] API token endpoints registered")
	} else {
		log.Printf("[ROUTES] API token controller not available, skipping API token routes")
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

	// Add asset upload route if asset storage is available.
	if r.server.assetStore != nil && r.handlers.assetController != nil {
		log.Printf("[ROUTES] Registering asset endpoints...")
		r.echo.POST("/assets", r.handlers.assetController.CreateAsset, auth.RequirePermission(entities.PermissionSessionCreate, r.server.container.AuthService))
		log.Printf("[ROUTES] Asset endpoints registered")
	} else {
		log.Printf("[ROUTES] Asset store not available, skipping asset routes")
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
		if err := handler.RegisterRoutes(r.echo); err != nil {
			log.Printf("[ROUTES] Failed to register custom handler %s: %v", handler.GetName(), err)
			return err
		}
		log.Printf("[ROUTES] Successfully registered custom handler: %s", handler.GetName())
	}

	return nil
}
