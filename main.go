package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	port    string
	config  string
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
	rootCmd.PersistentFlags().StringVarP(&config, "config", "c", "config.json", "Configuration file path")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")

	// Bind flags to viper
	viper.BindPFlag("port", rootCmd.PersistentFlags().Lookup("port"))
	viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func runProxy(cmd *cobra.Command, args []string) {
	if verbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	cfg, err := LoadConfig(config)
	if err != nil {
		log.Printf("Failed to load config from %s, using defaults: %v", config, err)
		cfg = DefaultConfig()
	}

	proxy := NewProxy(cfg, verbose)
	
	log.Printf("Starting agentapi-proxy on port %s", port)
	if err := proxy.GetEcho().Start(":" + port); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}