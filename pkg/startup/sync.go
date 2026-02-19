package startup

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	github_pkg "github.com/takutakahashi/agentapi-proxy/pkg/github"
)

// SyncOptions contains options for the sync command
type SyncOptions struct {
	SettingsFile              string // Path to the mounted settings.json from Settings Secret
	OutputDir                 string // Home directory (generates ~/.claude.json and ~/.claude/)
	CredentialsFile           string // Path to the mounted credentials.json from Credentials Secret (optional)
	ClaudeMDFile              string // Path to CLAUDE.md file to copy (optional, default: /tmp/config/CLAUDE.md)
	NotificationSubscriptions string // Path to notification subscriptions directory (optional)
	NotificationsDir          string // Path to notifications output directory (optional)
	RegisterMarketplaces      bool   // Register cloned marketplaces using claude CLI
}

// settingsJSON represents the structure of settings.json from Settings Secret
type settingsJSON struct {
	Name           string                      `json:"name"`
	Bedrock        *bedrockJSON                `json:"bedrock,omitempty"`
	MCPServers     map[string]*mcpServerJSON   `json:"mcp_servers,omitempty"`
	Marketplaces   map[string]*marketplaceJSON `json:"marketplaces,omitempty"`
	EnabledPlugins []string                    `json:"enabled_plugins,omitempty"` // plugin@marketplace format
	Hooks          map[string]interface{}      `json:"hooks,omitempty"`
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

// marketplaceJSON represents a marketplace configuration
type marketplaceJSON struct {
	URL string `json:"url"`
}

// marketplacePluginJSON represents .claude-plugin/marketplace.json in a marketplace repository
type marketplacePluginJSON struct {
	Name string `json:"name"`
}

// Sync synchronizes settings from Settings Secret to Claude configuration files.
// It assumes ~/.claude.json and ~/.claude/settings.json have already been written
// by the compile step; it handles marketplace clone/register and plugin install only.
func Sync(opts SyncOptions) error {
	log.Printf("[SYNC] Starting sync: output dir: %s, register marketplaces: %v",
		opts.OutputDir, opts.RegisterMarketplaces)

	// Load custom marketplace config if provided (optional)
	settings, err := loadSettingsFile(opts.SettingsFile)
	if err != nil {
		// Normal when SettingsFile is empty or not needed
		settings = nil
	}

	// Register marketplaces and install plugins (reads enabled_plugins from
	// the already-written ~/.claude/settings.json)
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

// syncMarketplaces clones custom marketplace repositories and registers marketplaces
// and plugins via claude CLI. settings.json is assumed to already be written by
// the compile step; this function does NOT overwrite it.
func syncMarketplaces(opts SyncOptions, settings *settingsJSON) error {
	// Create directories
	claudeDir := filepath.Join(opts.OutputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	// Marketplaces are cloned to .claude/plugins/marketplaces/<marketplace-name>
	marketplacesDir := filepath.Join(claudeDir, "plugins", "marketplaces")
	if err := os.MkdirAll(marketplacesDir, 0755); err != nil {
		return fmt.Errorf("failed to create marketplaces directory: %w", err)
	}

	// nameMapping maps alias keys to real marketplace names (for custom marketplaces)
	nameMapping := make(map[string]string)

	// Clone custom marketplace repositories if configured
	if settings != nil && len(settings.Marketplaces) > 0 {
		for aliasKey, marketplace := range settings.Marketplaces {
			if marketplace.URL == "" {
				log.Printf("[SYNC] Skipping marketplace %s: no URL configured", aliasKey)
				continue
			}

			// Clone to a temporary directory first to read the real marketplace name
			tempDir := filepath.Join(marketplacesDir, ".tmp-"+aliasKey)

			log.Printf("[SYNC] Cloning marketplace %s from %s", aliasKey, marketplace.URL)
			if err := cloneMarketplace(marketplace.URL, tempDir); err != nil {
				log.Printf("[SYNC] Warning: failed to clone marketplace %s: %v", aliasKey, err)
				continue
			}

			// Read the real marketplace name from .claude-plugin/marketplace.json
			realName, err := readMarketplaceName(tempDir)
			if err != nil {
				log.Printf("[SYNC] Error: failed to read marketplace name for %s: %v", aliasKey, err)
				if removeErr := os.RemoveAll(tempDir); removeErr != nil {
					log.Printf("[SYNC] Warning: failed to remove temp dir %s: %v", tempDir, removeErr)
				}
				continue
			}

			// Rename to the real marketplace name
			targetDir := filepath.Join(marketplacesDir, realName)
			if err := os.Rename(tempDir, targetDir); err != nil {
				log.Printf("[SYNC] Error: failed to rename marketplace dir from %s to %s: %v", tempDir, targetDir, err)
				if removeErr := os.RemoveAll(tempDir); removeErr != nil {
					log.Printf("[SYNC] Warning: failed to remove temp dir %s: %v", tempDir, removeErr)
				}
				continue
			}

			nameMapping[aliasKey] = realName
			log.Printf("[SYNC] Cloned marketplace %s (alias: %s) at %s", realName, aliasKey, targetDir)
		}
	}

	if !opts.RegisterMarketplaces {
		return nil
	}

	// Register the official Anthropic marketplace
	if err := registerOfficialMarketplace(opts.OutputDir); err != nil {
		log.Printf("[SYNC] Warning: failed to register official marketplace: %v", err)
	}

	// Register custom cloned marketplaces
	if settings != nil && len(settings.Marketplaces) > 0 {
		if err := registerMarketplaces(opts.OutputDir, marketplacesDir); err != nil {
			log.Printf("[SYNC] Warning: failed to register marketplaces: %v", err)
		}
	}

	// Install enabled plugins.
	// enabled_plugins is read from the already-written ~/.claude/settings.json
	// (generated by the compile step) so we don't need to re-parse opts.SettingsFile.
	settingsPath := filepath.Join(claudeDir, "settings.json")
	enabledPlugins, err := readEnabledPluginsFromSettingsJSON(settingsPath)
	if err != nil {
		log.Printf("[SYNC] Warning: could not read enabled_plugins from %s: %v", settingsPath, err)
	}

	for _, plugin := range enabledPlugins {
		resolvedPlugin := resolvePluginName(plugin, nameMapping)
		if err := installPlugin(opts.OutputDir, resolvedPlugin); err != nil {
			log.Printf("[SYNC] Warning: failed to install plugin %s: %v", resolvedPlugin, err)
		}
	}

	return nil
}

// readEnabledPluginsFromSettingsJSON reads the enabled_plugins list from a
// settings.json file that was already written by the compile step.
func readEnabledPluginsFromSettingsJSON(settingsPath string) ([]string, error) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}

	var content struct {
		EnabledPlugins []string `json:"enabled_plugins"`
	}
	if err := json.Unmarshal(data, &content); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", settingsPath, err)
	}

	return content.EnabledPlugins, nil
}

// cloneMarketplace clones a marketplace repository.
// It extracts the repo fullname from the URL and sets up GitHub authentication
// via gh CLI (credential helper) before cloning, so private repositories can be accessed.
func cloneMarketplace(url, targetDir string) error {
	// Extract repo fullname from URL for GitHub App auth (Installation ID discovery)
	repoFullName := github_pkg.ParseRepositoryURL(url)
	if repoFullName != "" {
		log.Printf("[SYNC] Setting up GitHub authentication for marketplace repo: %s", repoFullName)
		if err := SetupGitHubAuth(repoFullName); err != nil {
			// Warning only - continue (public repos may work without auth)
			log.Printf("[SYNC] Warning: GitHub auth setup failed for %s: %v", repoFullName, err)
		}
	}

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

	// Clone with shallow depth; authentication is handled by gh credential helper
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

// officialMarketplace is the official Anthropic marketplace repository
const officialMarketplace = "anthropics/claude-plugins-official"

// claudeBinPath is the path to the claude CLI binary
const claudeBinPath = "/opt/claude/bin/claude"

// registerOfficialMarketplace registers the official Anthropic marketplace
func registerOfficialMarketplace(outputDir string) error {
	log.Printf("[SYNC] Registering official marketplace: %s", officialMarketplace)

	cmd := exec.Command(claudeBinPath, "plugin", "marketplace", "add", officialMarketplace)
	// Set HOME to outputDir so claude CLI writes to the correct location
	cmd.Env = append(os.Environ(), fmt.Sprintf("HOME=%s", outputDir))

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to register official marketplace: %w, output: %s", err, string(output))
	}

	log.Printf("[SYNC] Successfully registered official marketplace: %s", officialMarketplace)
	return nil
}

// registerMarketplaces uses claude CLI to register cloned marketplaces
func registerMarketplaces(outputDir string, marketplacesDir string) error {
	// Read marketplace directories
	entries, err := os.ReadDir(marketplacesDir)
	if err != nil {
		return fmt.Errorf("failed to read marketplaces directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip temp directories
		if strings.HasPrefix(entry.Name(), ".tmp-") {
			continue
		}

		marketplacePath := filepath.Join(marketplacesDir, entry.Name())

		log.Printf("[SYNC] Registering marketplace at %s", marketplacePath)

		cmd := exec.Command(claudeBinPath, "plugin", "marketplace", "add", marketplacePath)
		// Set HOME to outputDir so claude CLI writes to the correct location
		cmd.Env = append(os.Environ(), fmt.Sprintf("HOME=%s", outputDir))

		if output, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[SYNC] Warning: failed to register marketplace at %s: %v, output: %s",
				marketplacePath, err, string(output))
			continue
		}

		log.Printf("[SYNC] Successfully registered marketplace at %s", marketplacePath)
	}
	return nil
}

// installPlugin installs a plugin using claude CLI
// pluginIdentifier is in "plugin-name@marketplace-name" format
func installPlugin(outputDir string, pluginIdentifier string) error {
	log.Printf("[SYNC] Installing plugin: %s", pluginIdentifier)

	cmd := exec.Command(claudeBinPath, "plugin", "install", pluginIdentifier)
	// Set HOME to outputDir so claude CLI writes to the correct location
	cmd.Env = append(os.Environ(), fmt.Sprintf("HOME=%s", outputDir))

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install plugin %s: %w, output: %s", pluginIdentifier, err, string(output))
	}

	log.Printf("[SYNC] Successfully installed plugin: %s", pluginIdentifier)
	return nil
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

// SyncExtra copies credentials, CLAUDE.md, and notification subscriptions.
// It does NOT generate .claude.json / settings.json — that is handled by sessionsettings.Compile.
// This is called from the unified Setup init container.
func SyncExtra(opts SyncOptions) error {
	// Copy credentials.json if provided
	if opts.CredentialsFile != "" {
		if err := syncCredentials(opts.CredentialsFile, opts.OutputDir); err != nil {
			log.Printf("[SYNC-EXTRA] Warning: failed to sync credentials: %v", err)
		}
	}

	// Copy CLAUDE.md if available
	claudeMDPath := opts.ClaudeMDFile
	if claudeMDPath == "" {
		claudeMDPath = "/tmp/config/CLAUDE.md"
	}
	if err := syncClaudeMD(claudeMDPath, opts.OutputDir); err != nil {
		log.Printf("[SYNC-EXTRA] Warning: failed to sync CLAUDE.md: %v", err)
	}

	// Copy notification subscriptions if provided
	if opts.NotificationSubscriptions != "" && opts.NotificationsDir != "" {
		if err := syncNotificationSubscriptions(opts.NotificationSubscriptions, opts.NotificationsDir); err != nil {
			log.Printf("[SYNC-EXTRA] Warning: failed to sync notification subscriptions: %v", err)
		}
	}

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
