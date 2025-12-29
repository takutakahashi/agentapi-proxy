package startup

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// SyncOptions contains options for the sync command
type SyncOptions struct {
	SettingsFile    string // Path to the mounted settings.json from Settings Secret
	OutputDir       string // Home directory (generates ~/.claude.json and ~/.claude/)
	MarketplacesDir string // Directory to clone marketplace repositories
	CredentialsFile string // Path to the mounted credentials.json from Credentials Secret (optional)
}

// settingsJSON represents the structure of settings.json from Settings Secret
type settingsJSON struct {
	Name         string                      `json:"name"`
	Bedrock      *bedrockJSON                `json:"bedrock,omitempty"`
	MCPServers   map[string]*mcpServerJSON   `json:"mcp_servers,omitempty"`
	Marketplaces map[string]*marketplaceJSON `json:"marketplaces,omitempty"`
	CreatedAt    string                      `json:"created_at"`
	UpdatedAt    string                      `json:"updated_at"`
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
	URL            string   `json:"url"`
	EnabledPlugins []string `json:"enabled_plugins,omitempty"`
}

// marketplaceSource represents the source configuration for extraKnownMarketplaces
// Source must be one of: 'url' | 'github' | 'git' | 'npm' | 'file' | 'directory'
type marketplaceSource struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

// extraKnownMarketplace represents an entry in extraKnownMarketplaces
type extraKnownMarketplace struct {
	Source marketplaceSource `json:"source"`
}

// Sync synchronizes settings from Settings Secret to Claude configuration files
func Sync(opts SyncOptions) error {
	log.Printf("[SYNC] Starting sync with settings file: %s, output dir: %s, marketplaces dir: %s",
		opts.SettingsFile, opts.OutputDir, opts.MarketplacesDir)

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

// syncMarketplaces clones marketplace repositories and generates settings.json
func syncMarketplaces(opts SyncOptions, settings *settingsJSON) error {
	// Create directories
	claudeDir := filepath.Join(opts.OutputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	if err := os.MkdirAll(opts.MarketplacesDir, 0755); err != nil {
		return fmt.Errorf("failed to create marketplaces directory: %w", err)
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

	// Process marketplaces if available
	if settings != nil && len(settings.Marketplaces) > 0 {
		extraKnownMarketplaces := make(map[string]extraKnownMarketplace)
		enabledPlugins := make(map[string]struct{})

		for name, marketplace := range settings.Marketplaces {
			if marketplace.URL == "" {
				log.Printf("[SYNC] Skipping marketplace %s: no URL configured", name)
				continue
			}

			targetDir := filepath.Join(opts.MarketplacesDir, name)

			// Clone repository
			log.Printf("[SYNC] Cloning marketplace %s from %s to %s", name, marketplace.URL, targetDir)
			if err := cloneMarketplace(marketplace.URL, targetDir); err != nil {
				log.Printf("[SYNC] Warning: failed to clone marketplace %s: %v", name, err)
				continue
			}

			// Add to extraKnownMarketplaces
			extraKnownMarketplaces[name] = extraKnownMarketplace{
				Source: marketplaceSource{
					Source: "directory",
					Path:   targetDir,
				},
			}

			// Add enabled plugins with marketplace qualifier (as object keys)
			for _, plugin := range marketplace.EnabledPlugins {
				enabledPlugins[fmt.Sprintf("%s@%s", plugin, name)] = struct{}{}
			}

			log.Printf("[SYNC] Successfully cloned marketplace %s", name)
		}

		if len(extraKnownMarketplaces) > 0 {
			settingsContent["extraKnownMarketplaces"] = extraKnownMarketplaces
		}

		if len(enabledPlugins) > 0 {
			settingsContent["enabledPlugins"] = enabledPlugins
		}
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
