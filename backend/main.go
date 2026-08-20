package main

import (
	"log"
	"os"
	_ "time/tzdata" // embed IANA timezone database so time.LoadLocation works in containers without tzdata installed

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/cmd"
)

var rootCmd = &cobra.Command{
	Use:   "ccplant",
	Short: "AgentAPI Proxy Server",
	Long:  "A reverse proxy server for AgentAPI that routes requests based on configuration",
}

func init() {
	rootCmd.AddCommand(cmd.ServerCmd)
	rootCmd.AddCommand(cmd.WorkerCmd)
	rootCmd.AddCommand(cmd.SessionManagerCmd)
	rootCmd.AddCommand(cmd.HelpersCmd)
	rootCmd.AddCommand(cmd.ClientCmd)
	rootCmd.AddCommand(cmd.AgentProvisionerCmd)
	rootCmd.AddCommand(cmd.NativeSessionManagerCmd)
	rootCmd.AddCommand(cmd.NativeCmd)
	rootCmd.AddCommand(cmd.OneshotCmd)
	rootCmd.AddCommand(cmd.AcpServerCmd)
	rootCmd.AddCommand(cmd.DoctorCmd)
	rootCmd.AddCommand(cmd.ControlPlaneCmd)
	rootCmd.AddCommand(cmd.HelmCmd)
	rootCmd.AddCommand(cmd.KVStoreCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Printf("Fatal error executing command: %v", err)
		os.Exit(1)
	}
}
