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
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
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
		fmt.Println("  setup-gh - Setup GitHub authentication using gh CLI")
		fmt.Println("Use 'agentapi-proxy helpers --help' for more information about available subcommands.")
	},
}

var setupClaudeCodeCmd = &cobra.Command{
	Use:   "setup-claude-code",
	Short: "Setup Claude Code configuration",
	Long:  "Creates Claude Code configuration directory and settings file at ~/.claude/settings.json",
	Run:   runSetupClaudeCode,
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Claude configuration (alias for setup-claude-code)",
	Long:  "Creates Claude Code configuration directory and settings file at ~/.claude/settings.json, and merges config/claude.json into ~/.claude.json",
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

var setupGHCmd = &cobra.Command{
	Use:   "setup-gh",
	Short: "Setup GitHub authentication using gh CLI",
	Long: `Setup GitHub authentication using gh CLI with comprehensive support.

This command provides complete GitHub authentication setup including:
- GitHub App installation token generation
- Personal access token authentication  
- GitHub Enterprise Server support
- Automatic installation ID discovery
- gh CLI authentication and git setup

Environment variables supported:
- GITHUB_TOKEN or GITHUB_PERSONAL_ACCESS_TOKEN: Personal access token
- GITHUB_APP_ID: GitHub App ID for app authentication
- GITHUB_INSTALLATION_ID: Installation ID (optional, auto-discovered if not provided)
- GITHUB_APP_PEM_PATH: Path to GitHub App private key file
- GITHUB_APP_PEM: GitHub App private key content (alternative to file)
- GITHUB_API: GitHub API URL for Enterprise Server (e.g., https://github.enterprise.com/api/v3)
- GITHUB_REPO_FULLNAME: Repository full name for installation ID discovery

Usage:
  agentapi-proxy helpers setup-gh --repo-fullname owner/repo`,
	RunE: runSetupGH,
}

var outputPath string
var userID string
var role string
var permissions []string
var expiryDays int
var keyPrefix string

// setup-gh command flags
var setupGHRepoFullName string
var githubAppID string
var githubInstallationID string
var githubAppPEMPath string
var githubAppPEM string
var githubAPI string
var githubToken string
var githubPersonalAccessToken string

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

	// setup-gh command flags
	setupGHCmd.Flags().StringVar(&setupGHRepoFullName, "repo-fullname", "", "Repository full name (owner/repo) for installation ID discovery")
	setupGHCmd.Flags().StringVar(&githubAppID, "github-app-id", "", "GitHub App ID (can also be set via GITHUB_APP_ID env var)")
	setupGHCmd.Flags().StringVar(&githubInstallationID, "github-installation-id", "", "GitHub Installation ID (optional, auto-discovered if not provided)")
	setupGHCmd.Flags().StringVar(&githubAppPEMPath, "github-app-pem-path", "", "Path to GitHub App private key file (can also be set via GITHUB_APP_PEM_PATH env var)")
	setupGHCmd.Flags().StringVar(&githubAppPEM, "github-app-pem", "", "GitHub App private key content (can also be set via GITHUB_APP_PEM env var)")
	setupGHCmd.Flags().StringVar(&githubAPI, "github-api", "", "GitHub API URL for Enterprise Server (can also be set via GITHUB_API env var)")
	setupGHCmd.Flags().StringVar(&githubToken, "github-token", "", "GitHub personal access token (can also be set via GITHUB_TOKEN env var)")
	setupGHCmd.Flags().StringVar(&githubPersonalAccessToken, "github-personal-access-token", "", "GitHub personal access token (can also be set via GITHUB_PERSONAL_ACCESS_TOKEN env var)")

	HelpersCmd.AddCommand(setupClaudeCodeCmd)
	HelpersCmd.AddCommand(initCmd)
	HelpersCmd.AddCommand(generateTokenCmd)
	HelpersCmd.AddCommand(setupGHCmd)
}

// RunSetupClaudeCode is exported for use in other packages
func RunSetupClaudeCode() error {
	return setupClaudeCodeInternal()
}

func runSetupClaudeCode(cmd *cobra.Command, args []string) {
	if err := setupClaudeCodeInternal(); err != nil {
		fmt.Printf("Error: %v\n", err)
		log.Printf("Fatal error: %v", err)
		os.Exit(1)
	}
}

func setupClaudeCodeInternal() error {
	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Create .claude directory
	claudeConfigDir := filepath.Join(homeDir, ".claude")
	if err := os.MkdirAll(claudeConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", claudeConfigDir, err)
	}

	// Validate that the embedded JSON is valid
	var tempSettings interface{}
	if err := json.Unmarshal([]byte(claudeCodeSettings), &tempSettings); err != nil {
		return fmt.Errorf("invalid embedded settings JSON: %w", err)
	}

	// Write settings.json file
	settingsPath := filepath.Join(claudeConfigDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(claudeCodeSettings), 0644); err != nil {
		return fmt.Errorf("failed to write settings file %s: %w", settingsPath, err)
	}

	// Merge config/claude.json into ~/.claude.json
	if err := mergeClaudeConfig(); err != nil {
		log.Printf("Warning: Failed to merge claude config: %v", err)
		// Don't return error, just warn
	}

	claudeSettingsMap := map[string]string{
		"hasTrustDialogAccepted":        "true",
		"hasCompletedProjectOnboarding": "true",
		"dontCrawlDirectory":            "true",
	}

	for key, value := range claudeSettingsMap {
		claudeCmd := exec.Command("claude", "config", "set", key, value)
		if err := claudeCmd.Run(); err != nil {
			log.Printf("Warning: Failed to set Claude config for key '%s': %v", key, err)
			// Don't return error for claude config, just warn
		}
	}

	log.Printf("Successfully created Claude Code configuration at %s", settingsPath)
	return nil
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
	addMcpServersCmd.Flags().String("claude-dir", "", "Claude configuration directory (defaults to ~/.claude)")
	HelpersCmd.AddCommand(addMcpServersCmd)
}

// RunAddMcpServers is exported for use in other packages
func RunAddMcpServers(configFlag string) error {
	return addMcpServersInternal(configFlag, "")
}

func runAddMcpServers(cmd *cobra.Command, args []string) {
	configFlag, _ := cmd.Flags().GetString("config")
	claudeDirFlag, _ := cmd.Flags().GetString("claude-dir")

	if err := addMcpServersInternal(configFlag, claudeDirFlag); err != nil {
		log.Fatalf("failed to add MCP servers: %v", err)
	}
}

func addMcpServersInternal(configFlag, claudeDirFlag string) error {
	// Get Claude directory
	claudeDir := claudeDirFlag
	if claudeDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		claudeDir = filepath.Join(homeDir, ".claude")
	}

	if configFlag == "" {
		return fmt.Errorf("config flag is required")
	}

	// Decode base64 configuration
	configJson, err := decodeBase64(configFlag)
	if err != nil {
		return fmt.Errorf("failed to decode base64 config: %w", err)
	}

	// Parse MCP server configurations
	var mcpConfigs []MCPServerConfig
	if err := json.Unmarshal([]byte(configJson), &mcpConfigs); err != nil {
		return fmt.Errorf("failed to parse MCP configuration: %w", err)
	}

	log.Printf("Adding %d MCP servers to Claude configuration", len(mcpConfigs))

	// Add each MCP server using claude command
	for _, mcpConfig := range mcpConfigs {
		if err := addMcpServer(claudeDir, mcpConfig); err != nil {
			log.Printf("Warning: Failed to add MCP server %s: %v", mcpConfig.Name, err)
			// Continue with other servers even if one fails
		} else {
			log.Printf("Successfully added MCP server: %s", mcpConfig.Name)
		}
	}

	return nil
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

// runSetupGH runs the setup-gh command
func runSetupGH(cmd *cobra.Command, args []string) error {
	// Set environment variables from flags if provided
	if err := setGitHubEnvFromFlags(); err != nil {
		return fmt.Errorf("failed to set environment variables: %w", err)
	}

	// Use repo full name from flag or environment
	repo := setupGHRepoFullName
	if repo == "" {
		repo = os.Getenv("GITHUB_REPO_FULLNAME")
	}

	// If no repo specified, try to auto-detect from git remote
	if repo == "" {
		if autoRepo := getGitHubRepoFullName(); autoRepo != "" {
			repo = autoRepo
			fmt.Printf("Auto-detected repository: %s\n", repo)
		}
	}

	// Call the startup package function
	if err := startup.SetupGitHubAuth(repo); err != nil {
		return fmt.Errorf("failed to setup GitHub authentication: %w", err)
	}

	fmt.Println("GitHub authentication setup completed successfully!")
	return nil
}

// setGitHubEnvFromFlags sets environment variables from command line flags
func setGitHubEnvFromFlags() error {
	// Map of flag variables to environment variable names
	envMappings := map[string]string{
		githubAppID:               "GITHUB_APP_ID",
		githubInstallationID:      "GITHUB_INSTALLATION_ID",
		githubAppPEMPath:          "GITHUB_APP_PEM_PATH",
		githubAppPEM:              "GITHUB_APP_PEM",
		githubAPI:                 "GITHUB_API",
		githubToken:               "GITHUB_TOKEN",
		githubPersonalAccessToken: "GITHUB_PERSONAL_ACCESS_TOKEN",
		setupGHRepoFullName:       "GITHUB_REPO_FULLNAME",
	}

	// Set environment variables from flags if provided
	for flagValue, envName := range envMappings {
		if flagValue != "" {
			if err := os.Setenv(envName, flagValue); err != nil {
				return fmt.Errorf("failed to set %s: %w", envName, err)
			}
			log.Printf("Set %s from flag", envName)
		}
	}

	return nil
}

// getGitHubRepoFullName extracts repository full name from git remote
func getGitHubRepoFullName() string {
	// Try to get the current working directory first
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Use git to get the remote origin URL
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	remoteURL := strings.TrimSpace(string(output))
	return extractRepoFullNameFromURL(remoteURL)
}

// extractRepoFullNameFromURL extracts owner/repo from various Git URL formats
func extractRepoFullNameFromURL(url string) string {
	// Handle SSH URLs: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		path := strings.TrimPrefix(url, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		return path
	}

	// Handle HTTPS URLs: https://github.com/owner/repo.git
	if strings.HasPrefix(url, "https://github.com/") {
		path := strings.TrimPrefix(url, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		return path
	}

	// Handle git protocol URLs: git://github.com/owner/repo.git
	if strings.HasPrefix(url, "git://github.com/") {
		path := strings.TrimPrefix(url, "git://github.com/")
		path = strings.TrimSuffix(path, ".git")
		return path
	}

	return ""
}
