package cmd

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
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
		fmt.Println("  init - Initialize Claude configuration (alias for setup-claude-code)")
		fmt.Println("Use 'agentapi-proxy helpers --help' for more information about available subcommands.")
	},
}

var setupClaudeCodeCmd = &cobra.Command{
	Use:   "setup-claude-code",
	Short: "Setup Claude Code configuration",
	Long:  "Creates Claude Code configuration directory and settings file at $CLAUDE_DIR/.claude/settings.json",
	Run:   runSetupClaudeCode,
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Claude configuration (alias for setup-claude-code)",
	Long:  "Creates Claude Code configuration directory and settings file at $CLAUDE_DIR/.claude/settings.json, and merges config/claude.json into ~/.claude.json",
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
	HelpersCmd.AddCommand(initCmd)
	HelpersCmd.AddCommand(generateTokenCmd)
}

func runSetupClaudeCode(cmd *cobra.Command, args []string) {
	claudeDir := os.Getenv("CLAUDE_DIR")
	if claudeDir == "" {
		fmt.Println("Error: CLAUDE_DIR environment variable is not set")
		log.Printf("Fatal error: CLAUDE_DIR environment variable is not set")
		os.Exit(1)
	}

	// Create .claude directory
	claudeConfigDir := filepath.Join(claudeDir, ".claude")
	if err := os.MkdirAll(claudeConfigDir, 0755); err != nil {
		fmt.Printf("Error creating directory %s: %v\n", claudeConfigDir, err)
		log.Printf("Fatal error creating directory %s: %v", claudeConfigDir, err)
		os.Exit(1)
	}

	// Validate that the embedded JSON is valid
	var tempSettings interface{}
	if err := json.Unmarshal([]byte(claudeCodeSettings), &tempSettings); err != nil {
		fmt.Printf("Error: Invalid embedded settings JSON: %v\n", err)
		log.Printf("Fatal error: Invalid embedded settings JSON: %v", err)
		os.Exit(1)
	}

	// Write settings.json file
	settingsPath := filepath.Join(claudeConfigDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(claudeCodeSettings), 0644); err != nil {
		fmt.Printf("Error writing settings file %s: %v\n", settingsPath, err)
		log.Printf("Fatal error writing settings file %s: %v", settingsPath, err)
		os.Exit(1)
	}

	// Merge config/claude.json into ~/.claude.json
	if err := mergeClaudeConfig(); err != nil {
		fmt.Printf("Warning: Failed to merge claude config: %v\n", err)
		// Don't exit on error, just warn
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
			log.Printf("Fatal error setting Claude config for key '%s': %v", key, err)
			os.Exit(1)
		}
	}

	fmt.Printf("Successfully created Claude Code configuration at %s\n", settingsPath)
}

func mergeClaudeConfig() error {
	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	targetPath := filepath.Join(homeDir, ".claude.json")

	// Define the configuration to merge as a map
	configToMerge := map[string]interface{}{
		"hasCompletedOnboarding":        true,
		"bypassPermissionsModeAccepted": true,
	}

	fmt.Printf("Using hardcoded configuration map\n")

	// Read existing ~/.claude.json if it exists
	var targetJSON map[string]interface{}
	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to read ~/.claude.json: %w", err)
		}
		// File doesn't exist, we'll create it
		targetJSON = make(map[string]interface{})
	} else {
		// Parse existing JSON
		if err := json.Unmarshal(targetData, &targetJSON); err != nil {
			return fmt.Errorf("failed to parse ~/.claude.json: %w", err)
		}
	}

	// Merge configuration map into target (config values override existing values)
	for key, value := range configToMerge {
		targetJSON[key] = value
	}

	// Write merged JSON back
	mergedData, err := json.MarshalIndent(targetJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged JSON: %w", err)
	}

	if err := os.WriteFile(targetPath, mergedData, 0644); err != nil {
		return fmt.Errorf("failed to write ~/.claude.json: %w", err)
	}

	fmt.Printf("Successfully merged claude config into %s\n", targetPath)
	return nil
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

// MCP Server configuration structure
type MCPServerConfig struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Endpoint  string            `json:"endpoint"`
	Enabled   bool              `json:"enabled"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Timeout   int               `json:"timeout,omitempty"`
}

var addMcpServersCmd = &cobra.Command{
	Use:   "add-mcp-servers",
	Short: "Add MCP servers to Claude configuration",
	Long:  "Add Model Context Protocol servers to Claude configuration from provided JSON configuration",
	Run:   runAddMcpServers,
}

func init() {
	addMcpServersCmd.Flags().String("config", "", "Base64 encoded JSON configuration for MCP servers")
	addMcpServersCmd.Flags().String("claude-dir", "", "Claude configuration directory (overrides CLAUDE_DIR env var)")
	HelpersCmd.AddCommand(addMcpServersCmd)
}

func runAddMcpServers(cmd *cobra.Command, args []string) {
	configFlag, _ := cmd.Flags().GetString("config")
	claudeDirFlag, _ := cmd.Flags().GetString("claude-dir")

	// Get Claude directory
	claudeDir := claudeDirFlag
	if claudeDir == "" {
		claudeDir = os.Getenv("CLAUDE_DIR")
	}
	if claudeDir == "" {
		claudeDir = "."
	}

	if configFlag == "" {
		log.Fatalf("config flag is required")
	}

	// Decode base64 configuration
	configJson, err := decodeBase64(configFlag)
	if err != nil {
		log.Fatalf("failed to decode base64 config: %v", err)
	}

	// Parse MCP server configurations
	var mcpConfigs []MCPServerConfig
	if err := json.Unmarshal([]byte(configJson), &mcpConfigs); err != nil {
		log.Fatalf("failed to parse MCP configuration: %v", err)
	}

	log.Printf("Adding %d MCP servers to Claude configuration", len(mcpConfigs))

	// Add each MCP server using claude command
	for _, mcpConfig := range mcpConfigs {
		if err := addMcpServer(claudeDir, mcpConfig); err != nil {
			log.Printf("Failed to add MCP server %s: %v", mcpConfig.Name, err)
			// Continue with other servers even if one fails
		} else {
			log.Printf("Successfully added MCP server: %s", mcpConfig.Name)
		}
	}
}

func decodeBase64(encoded string) (string, error) {
	// Try base64 decoding first
	decodedBase64, err := base64.StdEncoding.DecodeString(encoded)
	if err == nil {
		return string(decodedBase64), nil
	}

	// Try hex decoding if base64 fails
	decoded, err2 := hex.DecodeString(encoded)
	if err2 != nil {
		return "", fmt.Errorf("failed to decode as both base64 and hex: base64 error: %v, hex error: %v", err, err2)
	}
	return string(decoded), nil
}

func addMcpServer(claudeDir string, mcpConfig MCPServerConfig) error {
	// Build claude mcp add command
	args := []string{"mcp", "add", mcpConfig.Name}

	// Add command and args for stdio transport
	if mcpConfig.Transport == "stdio" && mcpConfig.Command != "" {
		args = append(args, mcpConfig.Command)
		if len(mcpConfig.Args) > 0 {
			args = append(args, mcpConfig.Args...)
		}
	}

	// Set environment variables
	env := os.Environ()
	if claudeDir != "" {
		env = append(env, fmt.Sprintf("CLAUDE_DIR=%s", claudeDir))
	}

	// Add custom environment variables from MCP config
	for key, value := range mcpConfig.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Execute claude command
	claudeCmd := exec.Command("claude", args...)
	claudeCmd.Env = env
	claudeCmd.Dir = claudeDir

	output, err := claudeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude command failed: %w, output: %s", err, string(output))
	}

	log.Printf("Claude mcp add output for %s: %s", mcpConfig.Name, string(output))
	return nil
}
