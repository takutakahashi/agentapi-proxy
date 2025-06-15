package cmd

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
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
		fmt.Println("  generate-token - Generate API keys for agentapi-proxy authentication")
		fmt.Println("Use 'agentapi-proxy helpers --help' for more information about available subcommands.")
	},
}

var setupClaudeCodeCmd = &cobra.Command{
	Use:   "setup-claude-code",
	Short: "Setup Claude Code configuration",
	Long:  "Creates Claude Code configuration directory and settings file at $CLAUDE_DIR/.claude/settings.json",
	Run:   runSetupClaudeCode,
}

var generateTokenCmd = &cobra.Command{
	Use:   "generate-token",
	Short: "Generate API keys for agentapi-proxy authentication",
	Long: `Generate API keys for agentapi-proxy authentication and save to a JSON file.

This command generates API keys for agentapi-proxy authentication and saves them to
a specified JSON file. If the file already exists, the new API key will be merged with existing content.

The generated API key includes:
- Unique API key with configurable prefix
- User ID and role assignment
- Permission configuration
- Creation and expiration timestamps

Usage:
  agentapi-proxy helpers generate-token --output-path /path/to/api_keys.json --user-id alice --role user`,
	RunE: runGenerateToken,
}

var outputPath string
var userID string
var role string
var permissions []string
var expiryDays int
var keyPrefix string

func init() {
	generateTokenCmd.Flags().StringVar(&outputPath, "output-path", "", "Path to JSON file where API keys will be saved (required)")
	generateTokenCmd.Flags().StringVar(&userID, "user-id", "", "User ID for the API key (required)")
	generateTokenCmd.Flags().StringVar(&role, "role", "user", "Role for the API key (admin, user, readonly)")
	generateTokenCmd.Flags().StringSliceVar(&permissions, "permissions", []string{"session:create", "session:list", "session:delete", "session:access"}, "Permissions for the API key")
	generateTokenCmd.Flags().IntVar(&expiryDays, "expiry-days", 365, "Number of days until the API key expires")
	generateTokenCmd.Flags().StringVar(&keyPrefix, "key-prefix", "ap", "Prefix for the generated API key")

	if err := generateTokenCmd.MarkFlagRequired("output-path"); err != nil {
		panic(err)
	}
	if err := generateTokenCmd.MarkFlagRequired("user-id"); err != nil {
		panic(err)
	}

	HelpersCmd.AddCommand(setupClaudeCodeCmd)
	HelpersCmd.AddCommand(generateTokenCmd)
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
	claudeSettingsMap := map[string]string{
		"hasTrustDialogAccepted":        "true",
		"hasCompletedProjectOnboarding": "true",
		"dontCrawlDirectory":            "true",
	}

	for key, value := range claudeSettingsMap {
		claudeCmd := exec.Command("claude", "config", "set", key, value)
		if err := claudeCmd.Run(); err != nil {
			fmt.Printf("Error setting Claude config: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Successfully created Claude Code configuration at %s\n", settingsPath)
}

type APIKeysFile struct {
	APIKeys []config.APIKey `json:"api_keys"`
}

func runGenerateToken(cmd *cobra.Command, args []string) error {
	// Validate role
	if role != "admin" && role != "user" && role != "readonly" {
		return fmt.Errorf("invalid role: %s. Must be one of: admin, user, readonly", role)
	}

	// Set default permissions based on role if not explicitly provided
	if !cmd.Flags().Changed("permissions") {
		switch role {
		case "admin":
			permissions = []string{"*"}
		case "user":
			permissions = []string{"session:create", "session:list", "session:delete", "session:access"}
		case "readonly":
			permissions = []string{"session:list"}
		}
	}

	// Generate random API key
	apiKey, err := generateAPIKey(userID, keyPrefix)
	if err != nil {
		return fmt.Errorf("failed to generate API key: %w", err)
	}

	// Create timestamps
	createdAt := time.Now()
	expiresAt := createdAt.AddDate(0, 0, expiryDays)

	// Create new API key entry
	newAPIKey := config.APIKey{
		Key:         apiKey,
		UserID:      userID,
		Role:        role,
		Permissions: permissions,
		CreatedAt:   createdAt.Format(time.RFC3339),
		ExpiresAt:   expiresAt.Format(time.RFC3339),
	}

	// Load existing API keys or create new structure
	var keysFile APIKeysFile
	if _, err := os.Stat(outputPath); err == nil {
		// File exists, load and merge
		data, err := os.ReadFile(outputPath)
		if err != nil {
			return fmt.Errorf("failed to read existing API keys file: %w", err)
		}

		if err := json.Unmarshal(data, &keysFile); err != nil {
			return fmt.Errorf("failed to parse existing API keys file: %w", err)
		}
	} else {
		// File doesn't exist, create new structure
		keysFile = APIKeysFile{
			APIKeys: []config.APIKey{},
		}
	}

	// Add new API key
	keysFile.APIKeys = append(keysFile.APIKeys, newAPIKey)

	// Save to file
	data, err := json.MarshalIndent(keysFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal API keys data: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write file
	if err := os.WriteFile(outputPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write API keys file: %w", err)
	}

	fmt.Printf("Successfully generated and saved API key to %s\n", outputPath)
	fmt.Printf("API Key: %s\n", apiKey)
	fmt.Printf("User ID: %s\n", userID)
	fmt.Printf("Role: %s\n", role)
	fmt.Printf("Permissions: %v\n", permissions)
	fmt.Printf("Expires at: %s\n", expiresAt.Format(time.RFC3339))
	return nil
}

func generateAPIKey(userID, prefix string) (string, error) {
	// Generate 16 random bytes
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Convert to hex string
	randomString := hex.EncodeToString(randomBytes)

	// Create API key with format: {prefix}_{role}_{userID}_{randomString}
	apiKey := fmt.Sprintf("%s_%s_%s", prefix, userID, randomString)

	return apiKey, nil
}
