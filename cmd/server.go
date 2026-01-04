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
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
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

	// Register schedule handlers (independent of worker status, but requires Kubernetes mode)
	registerScheduleHandlers(configData, proxyServer)

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

	// Create and register schedule handlers
	scheduleHandlers := schedule.NewHandlers(scheduleManager, proxyServer.GetSessionManager())
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
	)

	// Start leader worker in background
	go leaderWorker.Run(context.Background())

	log.Printf("[SCHEDULE_WORKER] Schedule worker started in namespace: %s", namespace)
	return leaderWorker
}
