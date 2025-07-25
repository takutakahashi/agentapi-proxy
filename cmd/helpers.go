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

	"github.com/SherClockHolmes/webpush-go"
	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/startup"
)

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
		fmt.Println("  send-notification - Send push notifications to registered subscriptions")
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
- Automatic repository detection from git remote

AUTHENTICATION METHODS (one of the following is required):

Method 1: Personal Access Token
- GITHUB_TOKEN: GitHub personal access token
- GITHUB_PERSONAL_ACCESS_TOKEN: Alternative env var for personal access token

Method 2: GitHub App Authentication
- GITHUB_APP_ID: GitHub App ID (required)
- GITHUB_APP_PEM_PATH: Path to GitHub App private key file (required)
- GITHUB_APP_PEM: GitHub App private key content (alternative to file path)
- GITHUB_INSTALLATION_ID: Installation ID (optional, auto-discovered if not provided)

Optional settings:
- GITHUB_API: GitHub API URL for Enterprise Server (e.g., https://github.enterprise.com/api/v3)
- GITHUB_REPO_FULLNAME: Repository full name for installation ID discovery (auto-detected from git remote if not provided)

Usage:
  agentapi-proxy helpers setup-gh                    # Auto-detect repository from git remote
  agentapi-proxy helpers setup-gh --repo-fullname owner/repo
  
Examples:
  # Using personal access token
  export GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx
  agentapi-proxy helpers setup-gh
  
  # Using GitHub App
  export GITHUB_APP_ID=123456
  export GITHUB_APP_PEM_PATH=/path/to/private-key.pem
  agentapi-proxy helpers setup-gh`,
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

	// send-notification command flags
	sendNotificationCmd.Flags().StringVar(&notifyUserID, "user-id", "", "Target specific user ID")
	sendNotificationCmd.Flags().StringVar(&notifyUserType, "user-type", "", "Target specific user type (github, api_key)")
	sendNotificationCmd.Flags().StringVar(&notifyUsername, "username", "", "Target specific username")
	sendNotificationCmd.Flags().StringVar(&notifySessionID, "session-id", "", "Target users related to specific session ID")
	sendNotificationCmd.Flags().StringVar(&notifyTitle, "title", "", "Notification title (required)")
	sendNotificationCmd.Flags().StringVar(&notifyBody, "body", "", "Notification body (required)")
	sendNotificationCmd.Flags().StringVar(&notifyURL, "url", "", "Notification click URL")
	sendNotificationCmd.Flags().StringVar(&notifyIcon, "icon", "/icon-192x192.png", "Notification icon URL")
	sendNotificationCmd.Flags().StringVar(&notifyBadge, "badge", "", "Notification badge URL")
	sendNotificationCmd.Flags().IntVar(&notifyTTL, "ttl", 86400, "Notification TTL in seconds")
	sendNotificationCmd.Flags().StringVar(&notifyUrgency, "urgency", "normal", "Notification urgency (low, normal, high)")
	sendNotificationCmd.Flags().BoolVar(&notifyDryRun, "dry-run", false, "Show target subscriptions without sending")
	sendNotificationCmd.Flags().BoolVarP(&notifyVerbose, "verbose", "v", false, "Verbose output")
	sendNotificationCmd.Flags().StringVar(&notifyConfigPath, "config", "", "Configuration file path")

	if err := sendNotificationCmd.MarkFlagRequired("title"); err != nil {
		panic(err)
	}
	if err := sendNotificationCmd.MarkFlagRequired("body"); err != nil {
		panic(err)
	}

	HelpersCmd.AddCommand(setupClaudeCodeCmd)
	HelpersCmd.AddCommand(initCmd)
	HelpersCmd.AddCommand(generateTokenCmd)
	HelpersCmd.AddCommand(setupGHCmd)
	HelpersCmd.AddCommand(sendNotificationCmd)
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
		log.Printf("Failed to get current working directory: %v", err)
		return ""
	}

	// Use git to get the remote origin URL
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get git remote origin URL: %v", err)
		return ""
	}

	remoteURL := strings.TrimSpace(string(output))
	log.Printf("Git remote origin URL: %s", remoteURL)

	repoFullName := extractRepoFullNameFromURL(remoteURL)
	if repoFullName != "" {
		log.Printf("Extracted repository full name: %s", repoFullName)
	} else {
		log.Printf("Failed to extract repository full name from URL: %s", remoteURL)
	}

	return repoFullName
}

// extractRepoFullNameFromURL extracts owner/repo from various Git URL formats
func extractRepoFullNameFromURL(url string) string {
	// Handle SSH URLs: git@hostname:owner/repo.git
	if strings.Contains(url, "@") && strings.Contains(url, ":") && !strings.Contains(url, "://") {
		// SSH format: git@hostname:owner/repo.git
		parts := strings.Split(url, ":")
		if len(parts) >= 2 {
			path := strings.Join(parts[1:], ":")
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	// Handle HTTPS URLs: https://hostname/owner/repo.git
	if strings.HasPrefix(url, "https://") {
		// Remove https:// prefix
		withoutProtocol := strings.TrimPrefix(url, "https://")

		// Handle URLs with authentication token: token@hostname/owner/repo.git
		if strings.Contains(withoutProtocol, "@") {
			parts := strings.Split(withoutProtocol, "@")
			if len(parts) >= 2 {
				withoutProtocol = strings.Join(parts[1:], "@")
			}
		}

		// Extract path after hostname: hostname/owner/repo.git -> owner/repo.git
		pathParts := strings.Split(withoutProtocol, "/")
		if len(pathParts) >= 3 {
			// Join owner/repo parts
			path := strings.Join(pathParts[1:], "/")
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	// Handle HTTP URLs: http://hostname/owner/repo.git
	if strings.HasPrefix(url, "http://") {
		// Remove http:// prefix
		withoutProtocol := strings.TrimPrefix(url, "http://")

		// Handle URLs with authentication token: token@hostname/owner/repo.git
		if strings.Contains(withoutProtocol, "@") {
			parts := strings.Split(withoutProtocol, "@")
			if len(parts) >= 2 {
				withoutProtocol = strings.Join(parts[1:], "@")
			}
		}

		// Extract path after hostname: hostname/owner/repo.git -> owner/repo.git
		pathParts := strings.Split(withoutProtocol, "/")
		if len(pathParts) >= 3 {
			// Join owner/repo parts
			path := strings.Join(pathParts[1:], "/")
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	// Handle git protocol URLs: git://hostname/owner/repo.git
	if strings.HasPrefix(url, "git://") {
		// Remove git:// prefix
		withoutProtocol := strings.TrimPrefix(url, "git://")

		// Extract path after hostname: hostname/owner/repo.git -> owner/repo.git
		pathParts := strings.Split(withoutProtocol, "/")
		if len(pathParts) >= 3 {
			// Join owner/repo parts
			path := strings.Join(pathParts[1:], "/")
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	return ""
}

var sendNotificationCmd = &cobra.Command{
	Use:   "send-notification",
	Short: "Send push notifications to registered subscriptions",
	Long: `Send push notifications to registered subscriptions based on filtering criteria.

This command allows you to send push notifications to users who have registered
for push notifications through the subscription system. You can target specific
users, user types, sessions, or use other filtering criteria.

The command requires VAPID configuration to be set via environment variables:
- VAPID_PUBLIC_KEY: VAPID public key for web push authentication
- VAPID_PRIVATE_KEY: VAPID private key for web push authentication  
- VAPID_CONTACT_EMAIL: Contact email for VAPID authentication

Examples:
  # Send to specific user
  agentapi-proxy helpers send-notification --user-id "user123" --title "Hello" --body "Test message"
  
  # Send to all users in a session
  agentapi-proxy helpers send-notification --session-id "session456" --title "Session Update" --body "Status changed"
  
  # Send to all GitHub users
  agentapi-proxy helpers send-notification --user-type "github" --title "Announcement" --body "New feature available"
  
  # Dry run to see who would receive the notification
  agentapi-proxy helpers send-notification --user-id "user123" --title "Test" --body "Test" --dry-run`,
	RunE: runSendNotification,
}

// Send notification command flags
var (
	notifyUserID     string
	notifyUserType   string
	notifyUsername   string
	notifySessionID  string
	notifyTitle      string
	notifyBody       string
	notifyURL        string
	notifyIcon       string
	notifyBadge      string
	notifyTTL        int
	notifyUrgency    string
	notifyDryRun     bool
	notifyVerbose    bool
	notifyConfigPath string
)

type NotificationSubscription struct {
	ID                string            `json:"id"`
	UserID            string            `json:"user_id"`
	UserType          string            `json:"user_type"`
	Username          string            `json:"username"`
	Endpoint          string            `json:"endpoint"`
	Keys              map[string]string `json:"keys"`
	SessionIDs        []string          `json:"session_ids"`
	NotificationTypes []string          `json:"notification_types"`
	CreatedAt         time.Time         `json:"created_at"`
	Active            bool              `json:"active"`
}

type NotificationHistory struct {
	ID             string                 `json:"id"`
	UserID         string                 `json:"user_id"`
	SubscriptionID string                 `json:"subscription_id"`
	Title          string                 `json:"title"`
	Body           string                 `json:"body"`
	Type           string                 `json:"type"`
	SessionID      string                 `json:"session_id"`
	Data           map[string]interface{} `json:"data"`
	SentAt         time.Time              `json:"sent_at"`
	Delivered      bool                   `json:"delivered"`
	Clicked        bool                   `json:"clicked"`
	ErrorMessage   *string                `json:"error_message"`
}

func runSendNotification(cmd *cobra.Command, args []string) error {
	// Validate VAPID configuration
	vapidPublicKey := os.Getenv("VAPID_PUBLIC_KEY")
	vapidPrivateKey := os.Getenv("VAPID_PRIVATE_KEY")
	vapidContactEmail := os.Getenv("VAPID_CONTACT_EMAIL")

	if vapidPublicKey == "" || vapidPrivateKey == "" || vapidContactEmail == "" {
		return fmt.Errorf("VAPID configuration required: set VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY, and VAPID_CONTACT_EMAIL environment variables")
	}

	// Validate urgency
	if notifyUrgency != "low" && notifyUrgency != "normal" && notifyUrgency != "high" {
		return fmt.Errorf("invalid urgency: %s. Must be one of: low, normal, high", notifyUrgency)
	}

	// Get all subscriptions that match the criteria
	subscriptions, err := getMatchingSubscriptions()
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	if len(subscriptions) == 0 {
		fmt.Println("No subscriptions found matching the specified criteria")
		return nil
	}

	if notifyVerbose {
		fmt.Printf("Found %d matching subscriptions:\n", len(subscriptions))
		for _, sub := range subscriptions {
			fmt.Printf("- %s (%s): %s\n", sub.UserID, sub.UserType, sub.Username)
		}
		fmt.Println()
	}

	if notifyDryRun {
		fmt.Printf("Would send notifications to %d subscriptions:\n", len(subscriptions))
		for _, sub := range subscriptions {
			fmt.Printf("- %s (%s): %s\n", sub.UserID, sub.UserType, sub.Username)
		}
		return nil
	}

	// Send notifications
	results, err := sendNotifications(subscriptions, vapidPublicKey, vapidPrivateKey, vapidContactEmail)
	if err != nil {
		return fmt.Errorf("failed to send notifications: %w", err)
	}

	// Display results
	successful := 0
	failed := 0
	for _, result := range results {
		if result.Error == nil {
			successful++
			if notifyVerbose {
				fmt.Printf("✓ %s (%s): delivered\n", result.Subscription.UserID, result.Subscription.UserType)
			}
		} else {
			failed++
			fmt.Printf("✗ %s (%s): failed - %v\n", result.Subscription.UserID, result.Subscription.UserType, result.Error)
		}
	}

	fmt.Printf("\nSuccessfully sent notifications to %d subscriptions\n", successful)
	if failed > 0 {
		fmt.Printf("Failed to send to %d subscriptions\n", failed)
	}

	return nil
}

type NotificationResult struct {
	Subscription NotificationSubscription
	Error        error
}

func getMatchingSubscriptions() ([]NotificationSubscription, error) {
	var allSubscriptions []NotificationSubscription

	// Get base directory for user data
	baseDir := os.Getenv("USERHOME_BASEDIR")
	if baseDir == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		baseDir = filepath.Join(homeDir, ".agentapi-proxy")
	}
	baseDir = filepath.Join(baseDir, "myclaudes")

	// Check if base directory exists
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return allSubscriptions, nil
	}

	// Read all user directories
	userDirs, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read user directories: %w", err)
	}

	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		userID := userDir.Name()
		subscriptions, err := getSubscriptionsForUser(userID)
		if err != nil {
			if notifyVerbose {
				fmt.Printf("Warning: failed to read subscriptions for user %s: %v\n", userID, err)
			}
			continue
		}

		// Filter subscriptions based on criteria
		for _, sub := range subscriptions {
			if matchesFilter(sub) {
				allSubscriptions = append(allSubscriptions, sub)
			}
		}
	}

	return allSubscriptions, nil
}

func getSubscriptionsForUser(userID string) ([]NotificationSubscription, error) {
	// Get base directory for user data using same logic as SetupUserHome
	baseDir := os.Getenv("USERHOME_BASEDIR")
	if baseDir == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		baseDir = filepath.Join(homeDir, ".agentapi-proxy")
	}

	subscriptionsFile := filepath.Join(baseDir, "myclaudes", userID, "notifications", "subscriptions.json")

	if _, err := os.Stat(subscriptionsFile); os.IsNotExist(err) {
		return []NotificationSubscription{}, nil
	}

	file, err := os.Open(subscriptionsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open subscriptions file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	var allSubscriptions []NotificationSubscription
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&allSubscriptions); err != nil {
		// If decode fails, return empty slice
		return []NotificationSubscription{}, nil
	}

	// Filter active subscriptions
	var subscriptions []NotificationSubscription
	for _, sub := range allSubscriptions {
		if sub.Active {
			subscriptions = append(subscriptions, sub)
		}
	}

	return subscriptions, nil
}

func matchesFilter(sub NotificationSubscription) bool {
	// If user-id is specified, it must match
	if notifyUserID != "" && sub.UserID != notifyUserID {
		return false
	}

	// If user-type is specified, it must match
	if notifyUserType != "" && sub.UserType != notifyUserType {
		return false
	}

	// If username is specified, it must match
	if notifyUsername != "" && sub.Username != notifyUsername {
		return false
	}

	// If session-id is specified, user must be subscribed to that session
	if notifySessionID != "" {
		// Empty session_ids means subscribed to all sessions
		if len(sub.SessionIDs) == 0 {
			return true
		}
		// Check if the specified session is in the user's session list
		for _, sessionID := range sub.SessionIDs {
			if sessionID == notifySessionID {
				return true
			}
		}
		return false
	}

	return true
}

func sendNotifications(subscriptions []NotificationSubscription, vapidPublicKey, vapidPrivateKey, vapidContactEmail string) ([]NotificationResult, error) {
	var results []NotificationResult

	for _, sub := range subscriptions {
		result := NotificationResult{Subscription: sub}

		// Create notification payload
		payload := map[string]interface{}{
			"title": notifyTitle,
			"body":  notifyBody,
			"icon":  notifyIcon,
			"data": map[string]interface{}{
				"url": notifyURL,
			},
		}

		if notifyBadge != "" {
			payload["badge"] = notifyBadge
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			result.Error = fmt.Errorf("failed to marshal payload: %w", err)
			results = append(results, result)
			continue
		}

		// Create webpush subscription
		webpushSub := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.Keys["p256dh"],
				Auth:   sub.Keys["auth"],
			},
		}

		// Create webpush options
		options := &webpush.Options{
			Subscriber:      vapidContactEmail,
			VAPIDPublicKey:  vapidPublicKey,
			VAPIDPrivateKey: vapidPrivateKey,
			TTL:             notifyTTL,
		}

		switch notifyUrgency {
		case "low":
			options.Urgency = webpush.UrgencyLow
		case "high":
			options.Urgency = webpush.UrgencyHigh
		default:
			options.Urgency = webpush.UrgencyNormal
		}

		// Send notification
		resp, err := webpush.SendNotification(payloadBytes, webpushSub, options)
		if err != nil {
			result.Error = fmt.Errorf("failed to send notification: %w", err)
		} else if resp.StatusCode >= 400 {
			result.Error = fmt.Errorf("notification rejected with status %d", resp.StatusCode)
		}

		if resp != nil {
			if err := resp.Body.Close(); err != nil && notifyVerbose {
				fmt.Printf("Warning: failed to close response body: %v\n", err)
			}
		}

		results = append(results, result)

		// Save to history
		if err := saveNotificationHistory(sub, result.Error == nil, result.Error); err != nil && notifyVerbose {
			fmt.Printf("Warning: failed to save notification history: %v\n", err)
		}
	}

	return results, nil
}

func saveNotificationHistory(sub NotificationSubscription, delivered bool, sendError error) error {
	// Get base directory for user data using same logic as SetupUserHome
	baseDir := os.Getenv("USERHOME_BASEDIR")
	if baseDir == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		baseDir = filepath.Join(homeDir, ".agentapi-proxy")
	}

	historyFile := filepath.Join(baseDir, "myclaudes", sub.UserID, "notifications", "history.jsonl")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(historyFile), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	history := NotificationHistory{
		ID:             generateNotificationID(),
		UserID:         sub.UserID,
		SubscriptionID: sub.ID,
		Title:          notifyTitle,
		Body:           notifyBody,
		Type:           "manual", // Sent via command line
		SessionID:      notifySessionID,
		Data: map[string]interface{}{
			"url":     notifyURL,
			"icon":    notifyIcon,
			"badge":   notifyBadge,
			"ttl":     notifyTTL,
			"urgency": notifyUrgency,
		},
		SentAt:    time.Now(),
		Delivered: delivered,
		Clicked:   false, // Will be updated when clicked
	}

	if sendError != nil {
		errorMsg := sendError.Error()
		history.ErrorMessage = &errorMsg
	}

	// Append to history file
	file, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open history file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close history file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(file)
	return encoder.Encode(history)
}

func generateNotificationID() string {
	// Generate a simple notification ID based on timestamp and random bytes
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to timestamp-only ID if random generation fails
		return fmt.Sprintf("notif_%d", time.Now().Unix())
	}
	return fmt.Sprintf("notif_%d_%x", time.Now().Unix(), randomBytes)
}
