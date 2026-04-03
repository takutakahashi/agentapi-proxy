package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// OneshotCmd is the parent command for one-shot maintenance operations.
var OneshotCmd = &cobra.Command{
	Use:   "oneshot",
	Short: "One-shot maintenance commands (run once, then done)",
	Long:  "Collection of one-shot maintenance commands that are run once and then no longer needed.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Available oneshot commands:")
		fmt.Println("  migrate-credentials - Migrate legacy credential Secrets into agentapi-agent-files-* format")
		fmt.Println("")
		fmt.Println("Use 'agentapi-proxy oneshot <command> --help' for more information.")
	},
}
