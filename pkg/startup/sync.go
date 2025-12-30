package startup

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SyncOptions contains options for the sync command
type SyncOptions struct {
	SettingsFile              string // Path to the mounted settings.json from Settings Secret
	OutputDir                 string // Home directory (generates ~/.claude.json and ~/.claude/)
	MarketplacesDir           string // Directory to clone marketplace repositories
	MarketplacesSettingsPath  string // Path prefix for marketplaces in settings.json (defaults to MarketplacesDir)
	CredentialsFile           string // Path to the mounted credentials.json from Credentials Secret (optional)
	ClaudeMDFile              string // Path to CLAUDE.md file to copy (optional, default: /tmp/config/CLAUDE.md)
	NotificationSubscriptions string // Path to notification subscriptions directory (optional)
	NotificationsDir          string // Path to notifications output directory (optional)
}

// settingsJSON represents the structure of settings.json from Settings Secret
type settingsJSON struct {
	Name           string                      `json:"name"`
	Bedrock        *bedrockJSON                `json:"bedrock,omitempty"`
	MCPServers     map[string]*mcpServerJSON   `json:"mcp_servers,omitempty"`
	Marketplaces   map[string]*marketplaceJSON `json:"marketplaces,omitempty"`
	EnabledPlugins []string                    `json:"enabled_plugins,omitempty"` // plugin@marketplace format
	CreatedAt      string                      `json:"created_at"`
	UpdatedAt      string                      `json:"updated_at"`
}

// bedrockJSON represents Bedrock settings (not used in sync, but part of structure)
type bedrockJSON struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// mcpServerJSON represents MCP server settings (not used in sync, but part of structure)
type mcpServerJSON struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// OfficialPluginsMarketplace is the marketplace identifier for official plugins
const OfficialPluginsMarketplace = "anthropics/claude-plugins-official"

// marketplaceJSON represents a marketplace configuration
type marketplaceJSON struct {
	URL string `json:"url"`
}

// marketplacePluginJSON represents .claude-plugin/marketplace.json in a marketplace repository
type marketplacePluginJSON struct {
	Name string `json:"name"`
}

// Sync synchronizes settings from Settings Secret to Claude configuration files
func Sync(opts SyncOptions) error {
	log.Printf("[SYNC] Starting sync with settings file: %s, output dir: %s, marketplaces dir: %s",
		opts.SettingsFile, opts.OutputDir, opts.MarketplacesDir)

	// Setup GitHub authentication first (for marketplace cloning)
	log.Printf("[SYNC] Setting up GitHub authentication")
	if err := SetupGitHubAuth(""); err != nil {
		// Warning only - continue even if auth setup fails (public repos may work)
		log.Printf("[SYNC] Warning: GitHub auth setup failed: %v", err)
	}

	// Load settings from file (optional - file may not exist)
	settings, err := loadSettingsFile(opts.SettingsFile)
	if err != nil {
		log.Printf("[SYNC] Settings file not found or invalid, using defaults: %v", err)
		settings = nil
	}

	// Generate ~/.claude.json
	if err := generateClaudeJSON(opts.OutputDir); err != nil {
		return fmt.Errorf("failed to generate .claude.json: %w", err)
	}

	// Clone marketplaces and generate ~/.claude/settings.json
	if err := syncMarketplaces(opts, settings); err != nil {
		return fmt.Errorf("failed to sync marketplaces: %w", err)
	}

	// Copy credentials.json if provided
	if opts.CredentialsFile != "" {
		if err := syncCredentials(opts.CredentialsFile, opts.OutputDir); err != nil {
			// Log warning but don't fail - credentials are optional
			log.Printf("[SYNC] Warning: failed to sync credentials: %v", err)
		}
	}

	// Copy CLAUDE.md if available
	claudeMDPath := opts.ClaudeMDFile
	if claudeMDPath == "" {
		claudeMDPath = "/tmp/config/CLAUDE.md" // Default path in Docker image
	}
	if err := syncClaudeMD(claudeMDPath, opts.OutputDir); err != nil {
		// Log warning but don't fail - CLAUDE.md is optional
		log.Printf("[SYNC] Warning: failed to sync CLAUDE.md: %v", err)
	}

	// Copy notification subscriptions if provided
	if opts.NotificationSubscriptions != "" && opts.NotificationsDir != "" {
		if err := syncNotificationSubscriptions(opts.NotificationSubscriptions, opts.NotificationsDir); err != nil {
			// Log warning but don't fail - notifications are optional
			log.Printf("[SYNC] Warning: failed to sync notification subscriptions: %v", err)
		}
	}

	log.Printf("[SYNC] Sync completed successfully")
	return nil
}

// loadSettingsFile loads settings from the mounted Settings Secret
func loadSettingsFile(settingsFile string) (*settingsJSON, error) {
	if settingsFile == "" {
		return nil, fmt.Errorf("settings file path is empty")
	}

	data, err := os.ReadFile(settingsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings file: %w", err)
	}

	var settings settingsJSON
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse settings JSON: %w", err)
	}

	log.Printf("[SYNC] Loaded settings for user: %s", settings.Name)
	return &settings, nil
}

// generateClaudeJSON generates ~/.claude.json with onboarding settings
func generateClaudeJSON(outputDir string) error {
	claudeJSONPath := filepath.Join(outputDir, ".claude.json")

	// Read existing file if present
	var existing map[string]interface{}
	if data, err := os.ReadFile(claudeJSONPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			log.Printf("[SYNC] Warning: failed to parse existing .claude.json: %v", err)
			existing = make(map[string]interface{})
		}
	} else {
		existing = make(map[string]interface{})
	}

	// Set required fields
	existing["hasCompletedOnboarding"] = true
	existing["bypassPermissionsModeAccepted"] = true

	// Write file
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal .claude.json: %w", err)
	}

	if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	log.Printf("[SYNC] Generated %s", claudeJSONPath)
	return nil
}

// syncMarketplaces clones marketplace repositories and registers them via claude command
func syncMarketplaces(opts SyncOptions, settings *settingsJSON) error {
	// Create directories
	claudeDir := filepath.Join(opts.OutputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	if err := os.MkdirAll(opts.MarketplacesDir, 0755); err != nil {
		return fmt.Errorf("failed to create marketplaces directory: %w", err)
	}

	// Determine the path prefix for claude command (may differ from clone directory)
	marketplacesSettingsPath := opts.MarketplacesSettingsPath
	if marketplacesSettingsPath == "" {
		marketplacesSettingsPath = opts.MarketplacesDir
	}

	// Add official plugins marketplace via claude command (no git clone needed)
	log.Printf("[SYNC] Adding official plugins marketplace: %s", OfficialPluginsMarketplace)
	if err := AddMarketplaceFunc(OfficialPluginsMarketplace); err != nil {
		log.Printf("[SYNC] Warning: failed to add official plugins marketplace: %v", err)
	} else {
		log.Printf("[SYNC] Successfully added official plugins marketplace")
	}

	// Build settings.json content
	settingsContent := make(map[string]interface{})

	// Add GITHUB_TOKEN to env if available
	if githubToken := os.Getenv("GITHUB_TOKEN"); githubToken != "" {
		settingsContent["env"] = map[string]string{
			"GITHUB_TOKEN": githubToken,
		}
	}

	// Enable MCP
	settingsContent["settings"] = map[string]interface{}{
		"mcp.enabled": true,
	}

	// nameMapping maps alias keys to real marketplace names
	nameMapping := make(map[string]string)

	// Process marketplaces if available
	if settings != nil && len(settings.Marketplaces) > 0 {
		for aliasKey, marketplace := range settings.Marketplaces {
			if marketplace.URL == "" {
				log.Printf("[SYNC] Skipping marketplace %s: no URL configured", aliasKey)
				continue
			}

			targetDir := filepath.Join(opts.MarketplacesDir, aliasKey)

			// Clone repository
			log.Printf("[SYNC] Cloning marketplace %s from %s to %s", aliasKey, marketplace.URL, targetDir)
			if err := cloneMarketplace(marketplace.URL, targetDir); err != nil {
				log.Printf("[SYNC] Warning: failed to clone marketplace %s: %v", aliasKey, err)
				continue
			}

			// Read the real marketplace name from .claude-plugin/marketplace.json
			realName, err := readMarketplaceName(targetDir)
			if err != nil {
				log.Printf("[SYNC] Error: failed to read marketplace name for %s: %v", aliasKey, err)
				continue
			}

			// Store mapping for plugin name resolution
			nameMapping[aliasKey] = realName

			// Add marketplace via claude command using the settings path
			settingsPath := filepath.Join(marketplacesSettingsPath, aliasKey)
			log.Printf("[SYNC] Adding marketplace %s via claude command: %s", aliasKey, settingsPath)
			if err := AddMarketplaceFunc(settingsPath); err != nil {
				log.Printf("[SYNC] Warning: failed to add marketplace %s: %v", aliasKey, err)
				continue
			}

			log.Printf("[SYNC] Successfully registered marketplace %s (alias: %s)", realName, aliasKey)
		}
	}

	// Process enabled plugins - resolve alias names to real marketplace names
	if settings != nil && len(settings.EnabledPlugins) > 0 {
		enabledPlugins := make(map[string]bool)
		for _, plugin := range settings.EnabledPlugins {
			resolvedPlugin := resolvePluginName(plugin, nameMapping)
			enabledPlugins[resolvedPlugin] = true
		}
		settingsContent["enabledPlugins"] = enabledPlugins
		log.Printf("[SYNC] Added %d enabled plugins (with resolved marketplace names)", len(settings.EnabledPlugins))
	}

	// Write settings.json
	settingsPath := filepath.Join(claudeDir, "settings.json")
	data, err := json.MarshalIndent(settingsContent, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings.json: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	log.Printf("[SYNC] Generated %s", settingsPath)
	return nil
}

// AddMarketplaceFunc is the function used to add marketplaces (can be overridden in tests)
var AddMarketplaceFunc = addMarketplaceDefault

// addMarketplaceDefault adds a marketplace using the claude CLI command
func addMarketplaceDefault(marketplacePath string) error {
	cmd := exec.Command("claude", "plugin", "marketplace", "add", marketplacePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("claude plugin marketplace add failed: %w, output: %s", err, string(output))
	}
	return nil
}

// cloneMarketplace clones a marketplace repository
func cloneMarketplace(url, targetDir string) error {
	// Check if already cloned
	if _, err := os.Stat(filepath.Join(targetDir, ".git")); err == nil {
		log.Printf("[SYNC] Marketplace already cloned at %s, pulling updates", targetDir)
		cmd := exec.Command("git", "pull")
		cmd.Dir = targetDir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git pull failed: %w, output: %s", err, string(output))
		}
		return nil
	}

	// Clone with shallow depth
	cmd := exec.Command("git", "clone", "--depth", "1", url, targetDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w, output: %s", err, string(output))
	}

	return nil
}

// readMarketplaceName reads the real marketplace name from .claude-plugin/marketplace.json
func readMarketplaceName(targetDir string) (string, error) {
	jsonPath := filepath.Join(targetDir, ".claude-plugin", "marketplace.json")

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return "", fmt.Errorf("failed to read marketplace.json: %w", err)
	}

	var mp marketplacePluginJSON
	if err := json.Unmarshal(data, &mp); err != nil {
		return "", fmt.Errorf("failed to parse marketplace.json: %w", err)
	}

	if mp.Name == "" {
		return "", fmt.Errorf("marketplace.json has empty name field")
	}

	return mp.Name, nil
}

// resolvePluginName replaces alias marketplace name with real name in plugin identifier
// plugin format: "plugin-name@marketplace-name"
func resolvePluginName(plugin string, nameMapping map[string]string) string {
	parts := strings.SplitN(plugin, "@", 2)
	if len(parts) != 2 {
		return plugin
	}

	pluginName := parts[0]
	marketplaceAlias := parts[1]

	if realName, ok := nameMapping[marketplaceAlias]; ok {
		return pluginName + "@" + realName
	}

	return plugin
}

// syncCredentials copies credentials.json from the mounted Secret to ~/.claude/.credentials.json
func syncCredentials(credentialsFile, outputDir string) error {
	// Check if source file exists
	if _, err := os.Stat(credentialsFile); os.IsNotExist(err) {
		log.Printf("[SYNC] Credentials file not found at %s, skipping", credentialsFile)
		return nil
	}

	// Read credentials file
	data, err := os.ReadFile(credentialsFile)
	if err != nil {
		return fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Validate that it's valid JSON
	var jsonCheck interface{}
	if err := json.Unmarshal(data, &jsonCheck); err != nil {
		return fmt.Errorf("credentials file is not valid JSON: %w", err)
	}

	// Create .claude directory if needed
	claudeDir := filepath.Join(outputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Write to destination
	destPath := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(destPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	log.Printf("[SYNC] Copied credentials to %s", destPath)
	return nil
}

// syncClaudeMD copies CLAUDE.md from the Docker image to ~/.claude/CLAUDE.md
func syncClaudeMD(claudeMDPath, outputDir string) error {
	// Check if source file exists
	if _, err := os.Stat(claudeMDPath); os.IsNotExist(err) {
		log.Printf("[SYNC] CLAUDE.md not found at %s, skipping", claudeMDPath)
		return nil
	}

	// Read CLAUDE.md file
	data, err := os.ReadFile(claudeMDPath)
	if err != nil {
		return fmt.Errorf("failed to read CLAUDE.md: %w", err)
	}

	// Create .claude directory if needed
	claudeDir := filepath.Join(outputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Write to destination
	destPath := filepath.Join(claudeDir, "CLAUDE.md")
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.md: %w", err)
	}

	log.Printf("[SYNC] Copied CLAUDE.md to %s", destPath)
	return nil
}

// syncNotificationSubscriptions copies notification subscriptions from mounted Secret to notifications directory
func syncNotificationSubscriptions(subscriptionsDir, notificationsDir string) error {
	// Check if source directory exists
	if _, err := os.Stat(subscriptionsDir); os.IsNotExist(err) {
		log.Printf("[SYNC] Notification subscriptions directory not found at %s, skipping", subscriptionsDir)
		return nil
	}

	// Create notifications directory if needed
	if err := os.MkdirAll(notificationsDir, 0755); err != nil {
		return fmt.Errorf("failed to create notifications directory: %w", err)
	}

	// Read source directory
	entries, err := os.ReadDir(subscriptionsDir)
	if err != nil {
		return fmt.Errorf("failed to read subscriptions directory: %w", err)
	}

	if len(entries) == 0 {
		log.Printf("[SYNC] No notification subscriptions found, skipping")
		return nil
	}

	// Copy each file (following symlinks since Secrets use symlinks)
	copiedCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		srcPath := filepath.Join(subscriptionsDir, entry.Name())
		destPath := filepath.Join(notificationsDir, entry.Name())

		// Read file (follows symlinks)
		data, err := os.ReadFile(srcPath)
		if err != nil {
			log.Printf("[SYNC] Warning: failed to read subscription file %s: %v", srcPath, err)
			continue
		}

		// Write to destination
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			log.Printf("[SYNC] Warning: failed to write subscription file %s: %v", destPath, err)
			continue
		}

		copiedCount++
	}

	log.Printf("[SYNC] Copied %d notification subscriptions to %s", copiedCount, notificationsDir)
	return nil
}
