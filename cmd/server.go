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
	githubsyncmodule "github.com/takutakahashi/agentapi-proxy/internal/app/modules/githubsync"
	importexportmodule "github.com/takutakahashi/agentapi-proxy/internal/app/modules/importexport"
	mcpmodule "github.com/takutakahashi/agentapi-proxy/internal/app/modules/mcp"
	schedulemodule "github.com/takutakahashi/agentapi-proxy/internal/app/modules/schedule"
	sessionmodule "github.com/takutakahashi/agentapi-proxy/internal/app/modules/session"
	slackbotmodule "github.com/takutakahashi/agentapi-proxy/internal/app/modules/slackbot"
	stockmodule "github.com/takutakahashi/agentapi-proxy/internal/app/modules/stock"
	webhookmodule "github.com/takutakahashi/agentapi-proxy/internal/app/modules/webhook"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
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
	workerCtx, cancelWorkers := context.WithCancel(context.Background())

	// Start session monitoring after proxy is initialized
	proxyServer.StartMonitoring()

	// Start schedule worker if enabled
	var scheduleWorker *schedule.LeaderWorker
	if configData.ScheduleWorker.Enabled {
		scheduleWorker = schedulemodule.StartWorker(configData, proxyServer)
	}

	// Start Slackbot cleanup worker if enabled
	if configData.SlackbotCleanupWorker.Enabled {
		slackbotmodule.StartCleanupWorker(configData, proxyServer)
	}

	// Start stock inventory worker if enabled
	if configData.StockInventoryWorker.Enabled {
		stockmodule.StartWorker(configData, proxyServer)
	}

	// Start the leader-elected session allocator when Kubernetes sessions are active.
	sessionmodule.StartAllocator(configData, proxyServer)

	// Register schedule handlers (independent of worker status, but requires Kubernetes mode)
	schedulemodule.RegisterHandlers(configData, proxyServer)

	// Register webhook handlers (requires Kubernetes mode)
	webhookmodule.RegisterHandlers(configData, proxyServer)

	// Register import/export handlers (requires Kubernetes mode)
	importexportmodule.RegisterHandlers(configData, proxyServer)

	// Register GitHub sync handlers (requires Kubernetes mode)
	githubsyncmodule.RegisterHandlers(configData, proxyServer)

	// Register SlackBot handlers (requires Kubernetes mode)
	slackbotmodule.RegisterHandlers(configData, proxyServer)

	// Start Slack Socket Mode manager (requires Kubernetes mode)
	slackbotmodule.StartSocketManager(configData, proxyServer)

	// Register MCP handler
	mcpmodule.RegisterHandler(proxyServer)

	// Register session manager handler (small-cluster / forwarding mode)
	sessionmodule.RegisterManagerHandlers(configData, proxyServer)

	// Start outbound session manager allocator when configured.
	sessionmodule.StartManagerAllocator(workerCtx, configData, proxyServer)

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
	cancelWorkers()

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
