package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/cmd"
)

var rootCmd = &cobra.Command{
	Use:   "agentapi-proxy",
	Short: "AgentAPI Proxy Server",
	Long:  "A reverse proxy server for AgentAPI that routes requests based on configuration",
}

func init() {
	rootCmd.AddCommand(cmd.ServerCmd)
	rootCmd.AddCommand(cmd.HelpersCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
