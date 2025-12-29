package cmd

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/github"
	"github.com/takutakahashi/agentapi-proxy/pkg/mcp"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
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
		fmt.Println("  merge-mcp-config - Merge multiple MCP server configuration directories")
		fmt.Println("  sync - Sync Claude configuration from Settings Secret")
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
	return startup.SetupClaudeCode("")
}

func runSetupClaudeCode(cmd *cobra.Command, args []string) {
	if err := startup.SetupClaudeCode(""); err != nil {
		fmt.Printf("Error: %v\n", err)
		log.Printf("Fatal error: %v", err)
		os.Exit(1)
	}
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
	return startup.SetupMCPServers("", configFlag)
}

func runAddMcpServers(cmd *cobra.Command, args []string) {
	configFlag, _ := cmd.Flags().GetString("config")
	claudeDirFlag, _ := cmd.Flags().GetString("claude-dir")

	homeDir := claudeDirFlag
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			log.Fatalf("failed to get home directory: %v", err)
		}
	}

	if err := startup.SetupMCPServers(homeDir, configFlag); err != nil {
		log.Fatalf("failed to add MCP servers: %v", err)
	}
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
		if autoRepo := github.GetRepositoryFromGitRemote(); autoRepo != "" {
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

func runSendNotification(cmd *cobra.Command, args []string) error {
	// Extract session ID from working directory if not provided
	if notifySessionID == "" {
		cwd, err := os.Getwd()
		if err == nil {
			// Extract UUID pattern from path
			uuidRegex := regexp.MustCompile(`[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`)
			if match := uuidRegex.FindString(cwd); match != "" {
				notifySessionID = match
			}
		}
	}

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

	// Create CLI utilities instance
	cliUtils := notification.NewCLIUtils()

	// Get all subscriptions that match the criteria
	subscriptions, err := cliUtils.GetMatchingSubscriptions(notifyUserID, notifyUserType, notifyUsername, notifySessionID, notifyVerbose)
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
	results, err := cliUtils.SendNotifications(subscriptions, notifyTitle, notifyBody, notifyURL, notifyIcon, notifyBadge, notifyTTL, notifyUrgency, notifySessionID, vapidPublicKey, vapidPrivateKey, vapidContactEmail)
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

// merge-mcp-config command flags
var (
	mcpInputDirs    string
	mcpOutputPath   string
	mcpExpandEnv    bool
	mcpMergeVerbose bool
)

var mergeMCPConfigCmd = &cobra.Command{
	Use:   "merge-mcp-config",
	Short: "Merge multiple MCP server configuration directories",
	Long: `Merge multiple MCP server configuration directories into a single JSON file.

This command reads MCP server configurations from multiple directories and merges
them into a single configuration file. Later directories take precedence over
earlier ones, allowing for base/team/user layered configuration.

Each directory should contain one or more JSON files with the mcpServers structure:
{
  "mcpServers": {
    "server-name": {
      "type": "http",
      "url": "https://example.com/mcp"
    }
  }
}

Environment variables can be expanded in the configuration using ${VAR} or
${VAR:-default} syntax when --expand-env is specified.

Examples:
  # Merge base, team, and user configs
  agentapi-proxy helpers merge-mcp-config \
    --input-dirs /mcp-config/base,/mcp-config/team,/mcp-config/user \
    --output /mcp-config/merged.json

  # With environment variable expansion
  agentapi-proxy helpers merge-mcp-config \
    --input-dirs /mcp-config/base,/mcp-config/user \
    --output /mcp-config/merged.json \
    --expand-env`,
	RunE: runMergeMCPConfig,
}

func init() {
	mergeMCPConfigCmd.Flags().StringVar(&mcpInputDirs, "input-dirs", "",
		"Comma-separated list of input directories (in priority order, later takes precedence)")
	mergeMCPConfigCmd.Flags().StringVarP(&mcpOutputPath, "output", "o", "",
		"Output file path for merged configuration")
	mergeMCPConfigCmd.Flags().BoolVar(&mcpExpandEnv, "expand-env", false,
		"Expand ${VAR} and ${VAR:-default} environment variable patterns")
	mergeMCPConfigCmd.Flags().BoolVarP(&mcpMergeVerbose, "verbose", "v", false,
		"Verbose output")

	if err := mergeMCPConfigCmd.MarkFlagRequired("input-dirs"); err != nil {
		panic(err)
	}
	if err := mergeMCPConfigCmd.MarkFlagRequired("output"); err != nil {
		panic(err)
	}

	HelpersCmd.AddCommand(mergeMCPConfigCmd)
}

func runMergeMCPConfig(cmd *cobra.Command, args []string) error {
	dirs := strings.Split(mcpInputDirs, ",")

	// Trim whitespace
	for i := range dirs {
		dirs[i] = strings.TrimSpace(dirs[i])
	}

	opts := mcp.MergeOptions{
		ExpandEnv: mcpExpandEnv,
		Verbose:   mcpMergeVerbose,
	}

	if mcpMergeVerbose {
		opts.Logger = func(format string, args ...interface{}) {
			fmt.Printf(format+"\n", args...)
		}
	}

	config, err := mcp.MergeConfigs(dirs, opts)
	if err != nil {
		return fmt.Errorf("failed to merge configs: %w", err)
	}

	if err := mcp.WriteConfig(config, mcpOutputPath); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	serverCount := len(config.MCPServers)
	if mcpMergeVerbose {
		fmt.Printf("[MCP] Merged %d server(s) to %s\n", serverCount, mcpOutputPath)
	} else {
		fmt.Printf("Successfully merged %d MCP server(s) to %s\n", serverCount, mcpOutputPath)
	}

	return nil
}

// sync command flags
var (
	syncSettingsFile              string
	syncOutputDir                 string
	syncMarketplacesDir           string
	syncCredentialsFile           string
	syncClaudeMDFile              string
	syncNotificationSubscriptions string
	syncNotificationsDir          string
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync Claude configuration from Settings Secret",
	Long: `Sync Claude configuration from a mounted Settings Secret.

This command reads settings from a mounted Settings Secret and generates:
- ~/.claude.json with onboarding settings
- ~/.claude/settings.json with marketplace configuration
- ~/.claude/.credentials.json (if credentials file is provided)
- ~/.claude/CLAUDE.md (copied from Docker image)
- Notification subscriptions (if provided)

It also clones any configured marketplace repositories.

The settings file should be the mounted settings.json from the agentapi-settings-{user} Secret.
The credentials file should be the mounted credentials.json from the agentapi-agent-credentials-{user} Secret.

Examples:
  # Basic usage with defaults
  agentapi-proxy helpers sync

  # Specify all paths
  agentapi-proxy helpers sync \
    --settings-file /settings-config/settings.json \
    --output-dir /home/agentapi \
    --marketplaces-dir /marketplaces \
    --credentials-file /credentials-config/credentials.json \
    --notification-subscriptions /notification-subscriptions-source \
    --notifications-dir /notifications`,
	RunE: runSync,
}

func init() {
	syncCmd.Flags().StringVar(&syncSettingsFile, "settings-file", "/settings-config/settings.json",
		"Path to the mounted settings.json from Settings Secret")
	syncCmd.Flags().StringVar(&syncOutputDir, "output-dir", "",
		"Output directory (home directory, defaults to $HOME)")
	syncCmd.Flags().StringVar(&syncMarketplacesDir, "marketplaces-dir", "/marketplaces",
		"Directory to clone marketplace repositories")
	syncCmd.Flags().StringVar(&syncCredentialsFile, "credentials-file", "",
		"Path to the mounted credentials.json from Credentials Secret (optional)")
	syncCmd.Flags().StringVar(&syncClaudeMDFile, "claude-md-file", "",
		"Path to CLAUDE.md file to copy (optional, default: /tmp/config/CLAUDE.md)")
	syncCmd.Flags().StringVar(&syncNotificationSubscriptions, "notification-subscriptions", "",
		"Path to notification subscriptions directory (optional)")
	syncCmd.Flags().StringVar(&syncNotificationsDir, "notifications-dir", "",
		"Path to notifications output directory (optional)")

	HelpersCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	outputDir := syncOutputDir
	if outputDir == "" {
		var err error
		outputDir, err = os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	opts := startup.SyncOptions{
		SettingsFile:              syncSettingsFile,
		OutputDir:                 outputDir,
		MarketplacesDir:           syncMarketplacesDir,
		CredentialsFile:           syncCredentialsFile,
		ClaudeMDFile:              syncClaudeMDFile,
		NotificationSubscriptions: syncNotificationSubscriptions,
		NotificationsDir:          syncNotificationsDir,
	}

	if err := startup.Sync(opts); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	fmt.Println("Claude configuration sync completed successfully!")
	return nil
}
