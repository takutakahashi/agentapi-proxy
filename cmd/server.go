package cmd

import (
	"log"

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
		log.Printf("Failed to load config from %s, using defaults: %v", cfg, err)
		configData = config.DefaultConfig()
	}

	proxyServer := proxy.NewProxy(configData, verbose)

	log.Printf("Starting agentapi-proxy on port %s", port)
	if err := proxyServer.GetEcho().Start(":" + port); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
