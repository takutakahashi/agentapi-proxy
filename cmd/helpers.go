package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

//go:embed claude_code_settings.json
var claudeCodeSettings string

var HelpersCmd = &cobra.Command{
	Use:   "helpers",
	Short: "Helper utilities for agentapi-proxy",
	Long:  "Collection of helper utilities and tools for working with agentapi-proxy",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Available helpers:")
		fmt.Println("  setup-claude-code - Setup Claude Code configuration")
		fmt.Println("Use 'agentapi-proxy helpers --help' for more information about available subcommands.")
	},
}

var setupClaudeCodeCmd = &cobra.Command{
	Use:   "setup-claude-code",
	Short: "Setup Claude Code configuration",
	Long:  "Creates Claude Code configuration directory and settings file at $CLAUDE_DIR/.claude/settings.json",
	Run:   runSetupClaudeCode,
}

func init() {
	HelpersCmd.AddCommand(setupClaudeCodeCmd)
}

func runSetupClaudeCode(cmd *cobra.Command, args []string) {
	claudeDir := os.Getenv("CLAUDE_DIR")
	if claudeDir == "" {
		fmt.Println("Error: CLAUDE_DIR environment variable is not set")
		os.Exit(1)
	}

	// Create .claude directory
	claudeConfigDir := filepath.Join(claudeDir, ".claude")
	if err := os.MkdirAll(claudeConfigDir, 0755); err != nil {
		fmt.Printf("Error creating directory %s: %v\n", claudeConfigDir, err)
		os.Exit(1)
	}

	// Validate that the embedded JSON is valid
	var tempSettings interface{}
	if err := json.Unmarshal([]byte(claudeCodeSettings), &tempSettings); err != nil {
		fmt.Printf("Error: Invalid embedded settings JSON: %v\n", err)
		os.Exit(1)
	}

	// Write settings.json file
	settingsPath := filepath.Join(claudeConfigDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(claudeCodeSettings), 0644); err != nil {
		fmt.Printf("Error writing settings file %s: %v\n", settingsPath, err)
		os.Exit(1)
	}

	fmt.Printf("Successfully created Claude Code configuration at %s\n", settingsPath)
}
