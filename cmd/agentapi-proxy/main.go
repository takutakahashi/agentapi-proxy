package main

import (
	"log"
	"os"

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

var rootCmd = &cobra.Command{
	Use:   "agentapi-proxy",
	Short: "AgentAPI Proxy Server",
	Long:  "A reverse proxy server for AgentAPI that routes requests based on configuration",
	Run:   runProxy,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&port, "port", "p", "8080", "Port to listen on")
	rootCmd.PersistentFlags().StringVarP(&cfg, "config", "c", "config.json", "Configuration file path")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

	// Bind flags to viper
	if err := viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port")); err != nil {
		log.Printf("Failed to bind port flag: %v", err)
	}
	if err := viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config")); err != nil {
		log.Printf("Failed to bind config flag: %v", err)
	}
	if err := viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose")); err != nil {
		log.Printf("Failed to bind verbose flag: %v", err)
	}
}

func runProxy(cmd *cobra.Command, args []string) {
	if verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	config, err := config.LoadConfig(cfg)
	if err != nil {
		log.Printf("Failed to load config from %s, using defaults: %v", cfg, err)
		config = config.DefaultConfig()
	}

	proxyServer := proxy.NewProxy(config, verbose)

	log.Printf("Starting agentapi-proxy on port %s", port)
	if err := proxyServer.GetEcho().Start(":" + port); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}