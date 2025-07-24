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
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxy"
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

	proxyServer := proxy.NewProxy(configData, verbose)

	// Start session monitoring after proxy is initialized
	proxyServer.StartMonitoring()

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
