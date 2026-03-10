package cmd

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	mcpiface "github.com/takutakahashi/agentapi-proxy/internal/interfaces/mcp"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	importexport "github.com/takutakahashi/agentapi-proxy/pkg/import"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
	"github.com/takutakahashi/agentapi-proxy/pkg/slackbot"
	slackbotcleanup "github.com/takutakahashi/agentapi-proxy/pkg/slackbot_cleanup"
	stock_inventory "github.com/takutakahashi/agentapi-proxy/pkg/stock_inventory"
	"github.com/takutakahashi/agentapi-proxy/pkg/webhook"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	port    string
	cfg     string
	verbose bool
)

var ServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the AgentAPI Proxy Server",
	Long:  "Start the reverse proxy server for AgentAPI that routes requests based on configuration",
	Run:   runProxy,
}

func init() {
	ServerCmd.Flags().StringVarP(&port, "port", "p", "8080", "Port to listen on")
	ServerCmd.Flags().StringVarP(&cfg, "config", "c", "config.json", "Configuration file path")
	ServerCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

	// Bind flags to viper
	if err := viper.BindPFlag("port", ServerCmd.Flags().Lookup("port")); err != nil {
		log.Printf("Failed to bind port flag: %v", err)
	}
	if err := viper.BindPFlag("config", ServerCmd.Flags().Lookup("config")); err != nil {
		log.Printf("Failed to bind config flag: %v", err)
	}
	if err := viper.BindPFlag("verbose", ServerCmd.Flags().Lookup("verbose")); err != nil {
		log.Printf("Failed to bind verbose flag: %v", err)
	}
}

func runProxy(cmd *cobra.Command, args []string) {
	if verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	configData, err := config.LoadConfig(cfg)
	if err != nil {
		log.Printf("Failed to load config from %s, trying to load from environment variables: %v", cfg, err)
		// Try to load configuration from environment variables
		var envErr error
		configData, envErr = config.LoadConfig("")
		if envErr != nil {
			log.Printf("Failed to load config from environment variables, using defaults: %v", envErr)
			configData = config.DefaultConfig()
		}
	}

	proxyServer := app.NewServer(configData, verbose)

	// Start session monitoring after proxy is initialized
	proxyServer.StartMonitoring()

	// Start schedule worker if enabled
	var scheduleWorker *schedule.LeaderWorker
	if configData.ScheduleWorker.Enabled {
		scheduleWorker = startScheduleWorker(configData, proxyServer)
	}

	// Start Slackbot cleanup worker if enabled
	if configData.SlackbotCleanupWorker.Enabled {
		startSlackbotCleanupWorker(configData, proxyServer)
	}

	// Start stock inventory worker if enabled
	if configData.StockInventoryWorker.Enabled {
		startStockInventoryWorker(configData, proxyServer)
	}

	// Register schedule handlers (independent of worker status, but requires Kubernetes mode)
	registerScheduleHandlers(configData, proxyServer)

	// Register webhook handlers (requires Kubernetes mode)
	registerWebhookHandlers(configData, proxyServer)

	// Register import/export handlers (requires Kubernetes mode)
	registerImportExportHandlers(configData, proxyServer)

	// Register SlackBot handlers (requires Kubernetes mode)
	registerSlackBotHandlers(configData, proxyServer)

	// Start Slack Socket Mode manager (requires Kubernetes mode)
	startSlackSocketManager(configData, proxyServer)

	// Register MCP handler
	registerMCPHandler(proxyServer, port)

	// Start server in a goroutine
	go func() {
		log.Printf("Starting agentapi-proxy on port %s", port)
		if err := proxyServer.GetEcho().Start(":" + port); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Shutdown signal received, shutting down gracefully...")

	// Stop schedule worker if running
	if scheduleWorker != nil {
		log.Printf("Stopping schedule worker...")
		scheduleWorker.Stop()
	}

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown the proxy and all sessions
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- proxyServer.Shutdown(25 * time.Second)
	}()

	// Shutdown the HTTP server
	serverShutdownDone := make(chan error, 1)
	go func() {
		serverShutdownDone <- proxyServer.GetEcho().Shutdown(ctx)
	}()

	// Wait for both shutdowns to complete
	var proxyErr, serverErr error
	for i := 0; i < 2; i++ {
		select {
		case err := <-shutdownDone:
			proxyErr = err
		case err := <-serverShutdownDone:
			serverErr = err
		case <-ctx.Done():
			log.Printf("Shutdown timeout reached")
			return
		}
	}

	if proxyErr != nil {
		log.Printf("Proxy shutdown error: %v", proxyErr)
	}
	if serverErr != nil {
		log.Printf("Server shutdown error: %v", serverErr)
	}

	log.Printf("Server shutdown complete")
}

// registerScheduleHandlers registers schedule REST API handlers
func registerScheduleHandlers(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[SCHEDULE_HANDLERS] Registering schedule handlers...")

	// Create Kubernetes client
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SCHEDULE_HANDLERS] Kubernetes config not available, skipping schedule handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SCHEDULE_HANDLERS] Failed to create Kubernetes client, skipping schedule handlers: %v", err)
		return
	}

	// Determine namespace
	namespace := configData.ScheduleWorker.Namespace
	if namespace == "" {
		namespace = configData.KubernetesSession.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}

	// Create schedule manager
	scheduleManager := schedule.NewKubernetesManager(client, namespace)

	// Run migration from legacy single-Secret format to individual Secrets
	if err := scheduleManager.MigrateFromLegacy(context.Background()); err != nil {
		log.Printf("[SCHEDULE_HANDLERS] Migration from legacy format failed: %v", err)
		// Continue even if migration fails - existing individual Secrets will still work
	}

	// Create and register schedule handlers
	scheduleHandlers := schedule.NewHandlers(scheduleManager, proxyServer.GetSessionManager(), proxyServer.GetMemoryRepository())
	proxyServer.AddCustomHandler(scheduleHandlers)

	log.Printf("[SCHEDULE_HANDLERS] Schedule handlers registered successfully")
}

// startScheduleWorker starts the schedule worker with leader election
func startScheduleWorker(configData *config.Config, proxyServer *app.Server) *schedule.LeaderWorker {
	log.Printf("[SCHEDULE_WORKER] Initializing schedule worker...")

	// Create Kubernetes client
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Kubernetes config not available, schedule worker disabled: %v", err)
		return nil
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Failed to create Kubernetes client, schedule worker disabled: %v", err)
		return nil
	}

	// Determine namespace
	namespace := configData.ScheduleWorker.Namespace
	if namespace == "" {
		namespace = configData.KubernetesSession.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}

	// Create schedule manager
	scheduleManager := schedule.NewKubernetesManager(client, namespace)

	// Parse worker config durations
	checkInterval, err := time.ParseDuration(configData.ScheduleWorker.CheckInterval)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Invalid check_interval, using default 30s: %v", err)
		checkInterval = 30 * time.Second
	}

	workerConfig := schedule.WorkerConfig{
		CheckInterval: checkInterval,
		Enabled:       true,
	}

	// Parse leader election config durations
	leaseDuration, err := time.ParseDuration(configData.ScheduleWorker.LeaseDuration)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Invalid lease_duration, using default 15s: %v", err)
		leaseDuration = 15 * time.Second
	}

	renewDeadline, err := time.ParseDuration(configData.ScheduleWorker.RenewDeadline)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Invalid renew_deadline, using default 10s: %v", err)
		renewDeadline = 10 * time.Second
	}

	retryPeriod, err := time.ParseDuration(configData.ScheduleWorker.RetryPeriod)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Invalid retry_period, using default 2s: %v", err)
		retryPeriod = 2 * time.Second
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		LeaseName:     "agentapi-schedule-worker",
		Namespace:     namespace,
	}

	// Create leader worker
	leaderWorker := schedule.NewLeaderWorker(
		scheduleManager,
		proxyServer.GetSessionManager(),
		client,
		workerConfig,
		electionConfig,
		proxyServer.GetMemoryRepository(),
	)

	// Start leader worker in background
	go leaderWorker.Run(context.Background())

	log.Printf("[SCHEDULE_WORKER] Schedule worker started in namespace: %s", namespace)
	return leaderWorker
}

// startSlackbotCleanupWorker starts the Slackbot session cleanup worker with leader election.
// It follows the same pattern as startScheduleWorker.
func startSlackbotCleanupWorker(configData *config.Config, proxyServer *app.Server) *slackbotcleanup.LeaderCleanupWorker {
	log.Printf("[SLACKBOT_CLEANUP] Initializing Slackbot cleanup worker...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SLACKBOT_CLEANUP] Kubernetes config not available, cleanup worker disabled: %v", err)
		return nil
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SLACKBOT_CLEANUP] Failed to create Kubernetes client, cleanup worker disabled: %v", err)
		return nil
	}

	namespace := configData.KubernetesSession.Namespace
	if namespace == "" {
		namespace = "default"
	}

	checkInterval, err := time.ParseDuration(configData.SlackbotCleanupWorker.CheckInterval)
	if err != nil || checkInterval <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid check_interval, using default 1h: %v", err)
		checkInterval = 1 * time.Hour
	}

	sessionTTL, err := time.ParseDuration(configData.SlackbotCleanupWorker.SessionTTL)
	if err != nil || sessionTTL <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid session_ttl, using default 72h: %v", err)
		sessionTTL = 72 * time.Hour
	}

	workerConfig := slackbotcleanup.CleanupWorkerConfig{
		CheckInterval: checkInterval,
		SessionTTL:    sessionTTL,
		Enabled:       true,
		DryRun:        configData.SlackbotCleanupWorker.DryRun,
	}

	leaseDuration, err := time.ParseDuration(configData.SlackbotCleanupWorker.LeaseDuration)
	if err != nil || leaseDuration <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid lease_duration, using default 15s: %v", err)
		leaseDuration = 15 * time.Second
	}

	renewDeadline, err := time.ParseDuration(configData.SlackbotCleanupWorker.RenewDeadline)
	if err != nil || renewDeadline <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid renew_deadline, using default 10s: %v", err)
		renewDeadline = 10 * time.Second
	}

	retryPeriod, err := time.ParseDuration(configData.SlackbotCleanupWorker.RetryPeriod)
	if err != nil || retryPeriod <= 0 {
		log.Printf("[SLACKBOT_CLEANUP] Invalid retry_period, using default 2s: %v", err)
		retryPeriod = 2 * time.Second
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		Namespace:     namespace,
		// LeaseName is overridden inside NewLeaderCleanupWorker to "agentapi-slackbot-cleanup-worker"
	}

	leaderCleanupWorker := slackbotcleanup.NewLeaderCleanupWorker(
		proxyServer.GetSessionManager(),
		client,
		namespace,
		workerConfig,
		electionConfig,
	)

	go leaderCleanupWorker.Run(context.Background())

	dryRunNote := ""
	if configData.SlackbotCleanupWorker.DryRun {
		dryRunNote = " [DRY-RUN]"
	}
	log.Printf("[SLACKBOT_CLEANUP] Slackbot cleanup worker started in namespace: %s (TTL: %v)%s", namespace, sessionTTL, dryRunNote)
	return leaderCleanupWorker
}

// startStockInventoryWorker starts the stock session inventory worker with leader election.
// It ensures a configurable number of pre-warmed stock sessions are always available.
func startStockInventoryWorker(configData *config.Config, proxyServer *app.Server) *stock_inventory.LeaderWorker {
	log.Printf("[STOCK_INVENTORY] Initializing stock inventory worker...")

	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Kubernetes config not available, stock inventory worker disabled: %v", err)
		return nil
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Failed to create Kubernetes client, stock inventory worker disabled: %v", err)
		return nil
	}

	namespace := configData.StockInventoryWorker.Namespace
	if namespace == "" {
		namespace = configData.KubernetesSession.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}

	// KubernetesSessionManager implements StockRepository.
	stockRepo, ok := proxyServer.GetSessionManager().(stock_inventory.StockRepository)
	if !ok {
		log.Printf("[STOCK_INVENTORY] Session manager does not implement StockRepository, stock inventory worker disabled")
		return nil
	}

	checkInterval, err := time.ParseDuration(configData.StockInventoryWorker.CheckInterval)
	if err != nil {
		log.Printf("[STOCK_INVENTORY] Invalid check_interval, using default 30s: %v", err)
		checkInterval = 30 * time.Second
	}

	targetCount := configData.StockInventoryWorker.TargetCount
	if targetCount <= 0 {
		targetCount = 2
	}

	workerConfig := stock_inventory.WorkerConfig{
		CheckInterval: checkInterval,
		TargetCount:   targetCount,
		Enabled:       true,
	}

	leaseDuration, err := time.ParseDuration(configData.StockInventoryWorker.LeaseDuration)
	if err != nil {
		leaseDuration = 15 * time.Second
	}
	renewDeadline, err := time.ParseDuration(configData.StockInventoryWorker.RenewDeadline)
	if err != nil {
		renewDeadline = 10 * time.Second
	}
	retryPeriod, err := time.ParseDuration(configData.StockInventoryWorker.RetryPeriod)
	if err != nil {
		retryPeriod = 2 * time.Second
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		LeaseName:     "agentapi-stock-inventory-worker",
		Namespace:     namespace,
	}

	leaderWorker := stock_inventory.NewLeaderWorker(stockRepo, client, workerConfig, electionConfig)

	go leaderWorker.Run(context.Background())

	log.Printf("[STOCK_INVENTORY] Stock inventory worker started in namespace: %s (target: %d)",
		namespace, targetCount)
	return leaderWorker
}

// registerWebhookHandlers registers webhook REST API handlers
func registerWebhookHandlers(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[WEBHOOK_HANDLERS] Registering webhook handlers...")

	// Create Kubernetes client
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[WEBHOOK_HANDLERS] Kubernetes config not available, skipping webhook handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[WEBHOOK_HANDLERS] Failed to create Kubernetes client, skipping webhook handlers: %v", err)
		return
	}

	// Determine namespace
	namespace := configData.ScheduleWorker.Namespace
	if namespace == "" {
		namespace = configData.KubernetesSession.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}

	// Create webhook repository (clean architecture)
	webhookRepo := repositories.NewKubernetesWebhookRepository(client, namespace)

	// Set default GitHub Enterprise host if configured
	if configData.Webhook.GitHubEnterpriseHost != "" {
		webhookRepo.SetDefaultGitHubEnterpriseHost(configData.Webhook.GitHubEnterpriseHost)
		log.Printf("[WEBHOOK_HANDLERS] Default GitHub Enterprise host configured: %s", configData.Webhook.GitHubEnterpriseHost)
	}

	// Create and register webhook handlers with baseURL from config
	webhookHandlers := webhook.NewHandlers(webhookRepo, proxyServer.GetSessionManager(), configData.Webhook.BaseURL, proxyServer.GetMemoryRepository())
	proxyServer.AddCustomHandler(webhookHandlers)

	if configData.Webhook.BaseURL != "" {
		log.Printf("[WEBHOOK_HANDLERS] Webhook base URL configured: %s", configData.Webhook.BaseURL)
	} else {
		log.Printf("[WEBHOOK_HANDLERS] Webhook base URL not configured, will auto-detect from request headers")
	}
	log.Printf("[WEBHOOK_HANDLERS] Webhook handlers registered successfully")
}

// registerImportExportHandlers registers import/export REST API handlers
func registerImportExportHandlers(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[IMPORT_EXPORT_HANDLERS] Registering import/export handlers...")

	// Create Kubernetes client
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[IMPORT_EXPORT_HANDLERS] Kubernetes config not available, skipping import/export handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[IMPORT_EXPORT_HANDLERS] Failed to create Kubernetes client, skipping import/export handlers: %v", err)
		return
	}

	// Determine namespace
	namespace := configData.ScheduleWorker.Namespace
	if namespace == "" {
		namespace = configData.KubernetesSession.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}

	// Create schedule manager
	scheduleManager := schedule.NewKubernetesManager(client, namespace)

	// Create webhook repository
	webhookRepo := repositories.NewKubernetesWebhookRepository(client, namespace)

	// Set default GitHub Enterprise host if configured
	if configData.Webhook.GitHubEnterpriseHost != "" {
		webhookRepo.SetDefaultGitHubEnterpriseHost(configData.Webhook.GitHubEnterpriseHost)
	}

	// Get settings repository from server
	settingsRepo := proxyServer.GetSettingsRepository()

	// Create encryption service for import/export
	encryptionFactory := services.NewEncryptionServiceFactory("AGENTAPI_ENCRYPTION")
	encryptionService, err := encryptionFactory.Create()
	if err != nil {
		log.Printf("[IMPORT_EXPORT_HANDLERS] Failed to create encryption service, using noop: %v", err)
		encryptionService = services.NewNoopEncryptionService()
	}
	log.Printf("[IMPORT_EXPORT_HANDLERS] Using encryption algorithm: %s", encryptionService.Algorithm())

	// Create and register import/export handlers
	importExportHandlers := importexport.NewHandlers(scheduleManager, webhookRepo, settingsRepo, encryptionService)
	proxyServer.AddCustomHandler(importExportHandlers)

	log.Printf("[IMPORT_EXPORT_HANDLERS] Import/export handlers registered successfully")
}

// registerSlackBotHandlers registers SlackBot management REST API handlers
func registerSlackBotHandlers(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[SLACKBOT_HANDLERS] Registering slackbot handlers...")

	// Create Kubernetes client
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SLACKBOT_HANDLERS] Kubernetes config not available, skipping slackbot handlers: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SLACKBOT_HANDLERS] Failed to create Kubernetes client, skipping slackbot handlers: %v", err)
		return
	}

	// Determine namespace
	namespace := configData.ScheduleWorker.Namespace
	if namespace == "" {
		namespace = configData.KubernetesSession.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}

	// Create SlackBot repository
	slackbotRepo := repositories.NewKubernetesSlackBotRepository(client, namespace)

	// Create and register SlackBot management handlers (no event reception - handled by Socket Mode)
	slackbotHandlers := slackbot.NewHandlers(slackbotRepo)
	proxyServer.AddCustomHandler(slackbotHandlers)

	log.Printf("[SLACKBOT_HANDLERS] SlackBot management handlers registered successfully")
}

// startSlackSocketManager starts the Slack Socket Mode manager with per-bot leader election
func startSlackSocketManager(configData *config.Config, proxyServer *app.Server) {
	log.Printf("[SOCKET_MANAGER] Initializing Slack Socket Mode manager...")

	// Create Kubernetes client
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		log.Printf("[SOCKET_MANAGER] Kubernetes config not available, skipping Socket Mode: %v", err)
		return
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Printf("[SOCKET_MANAGER] Failed to create Kubernetes client, skipping Socket Mode: %v", err)
		return
	}

	// Determine namespace
	namespace := configData.ScheduleWorker.Namespace
	if namespace == "" {
		namespace = configData.KubernetesSession.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}

	// Create dependencies
	slackbotRepo := repositories.NewKubernetesSlackBotRepository(client, namespace)
	channelResolver := services.NewSlackChannelResolver(client, namespace)

	eventHandler := controllers.NewSlackBotEventHandler(
		slackbotRepo,
		proxyServer.GetSessionManager(),
		configData.KubernetesSession.SlackBotTokenSecretName,
		configData.KubernetesSession.SlackBotTokenSecretKey,
		channelResolver,
		configData.Webhook.BaseURL,
		configData.Slack.DryRun,
		proxyServer.GetMemoryRepository(),
	)
	if configData.Slack.DryRun {
		log.Printf("[SOCKET_MANAGER] Slack dry-run mode enabled: session creation and Slack posts will be logged only")
	}

	// Resolve App token secret (defaults to SlackBotTokenSecretName if not set)
	appTokenSecretName := configData.Slack.AppTokenSecretName
	if appTokenSecretName == "" {
		appTokenSecretName = configData.KubernetesSession.SlackBotTokenSecretName
	}
	appTokenSecretKey := configData.Slack.AppTokenSecretKey
	if appTokenSecretKey == "" {
		appTokenSecretKey = "app-token"
	}

	// Parse leader election config durations
	leaseDuration, err := time.ParseDuration(configData.ScheduleWorker.LeaseDuration)
	if err != nil {
		leaseDuration = 15 * time.Second
	}
	renewDeadline, err := time.ParseDuration(configData.ScheduleWorker.RenewDeadline)
	if err != nil {
		renewDeadline = 10 * time.Second
	}
	retryPeriod, err := time.ParseDuration(configData.ScheduleWorker.RetryPeriod)
	if err != nil {
		retryPeriod = 2 * time.Second
	}

	electionConfig := schedule.LeaderElectionConfig{
		LeaseDuration: leaseDuration,
		RenewDeadline: renewDeadline,
		RetryPeriod:   retryPeriod,
		Namespace:     namespace,
	}

	managerConfig := slackbot.SlackSocketManagerConfig{
		DefaultAppTokenSecretName: appTokenSecretName,
		DefaultAppTokenSecretKey:  appTokenSecretKey,
		DefaultBotTokenSecretName: configData.KubernetesSession.SlackBotTokenSecretName,
		DefaultBotTokenSecretKey:  configData.KubernetesSession.SlackBotTokenSecretKey,
		LeaderElectionConfig:      electionConfig,
	}

	manager := slackbot.NewSlackSocketManager(
		client,
		namespace,
		slackbotRepo,
		eventHandler,
		channelResolver,
		managerConfig,
	)

	go manager.Run(context.Background())

	log.Printf("[SOCKET_MANAGER] Slack Socket Mode manager started in namespace: %s", namespace)
}

// registerMCPHandler registers MCP HTTP handler
func registerMCPHandler(proxyServer *app.Server, port string) {
	log.Printf("[MCP_HANDLER] Registering MCP handler...")

	// Create and register MCP handler with server dependencies
	mcpHandler := mcpiface.NewMCPHandler(proxyServer)
	proxyServer.AddCustomHandler(mcpHandler)

	log.Printf("[MCP_HANDLER] MCP handler registered successfully at /mcp")
}
