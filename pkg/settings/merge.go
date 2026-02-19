// Package settings provides utilities for managing settings configurations.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SettingsConfig represents the structure of settings configuration for merging
type SettingsConfig struct {
	Marketplaces   map[string]MarketplaceConfig `json:"marketplaces,omitempty"`
	EnabledPlugins []string                     `json:"enabled_plugins,omitempty"`
	Hooks          map[string]interface{}       `json:"hooks,omitempty"`
}

// MarketplaceConfig represents a single marketplace configuration
type MarketplaceConfig struct {
	URL string `json:"url"`
}

// MergeOptions configures the merge behavior
type MergeOptions struct {
	Verbose bool
	Logger  func(format string, args ...interface{})
}

// MergeConfigs merges multiple settings config directories into a single config.
// Later directories take precedence over earlier ones (last wins) for marketplaces.
// EnabledPlugins are merged as a union (all unique plugins from all sources).
// Hooks are merged with later directories taking precedence (last wins).
func MergeConfigs(inputDirs []string, opts MergeOptions) (*SettingsConfig, error) {
	result := &SettingsConfig{
		Marketplaces:   make(map[string]MarketplaceConfig),
		EnabledPlugins: []string{},
		Hooks:          make(map[string]interface{}),
	}

	log := opts.Logger
	if log == nil {
		log = func(format string, args ...interface{}) {}
	}

	enabledPluginsSet := make(map[string]bool)

	for _, dir := range inputDirs {
		if dir == "" {
			continue
		}

		if err := mergeDir(result, dir, enabledPluginsSet, opts, log); err != nil {
			// Directory not found is not an error (Optional)
			if os.IsNotExist(err) {
				log("[SETTINGS] Directory not found, skipping: %s", dir)
				continue
			}
			return nil, fmt.Errorf("failed to merge directory %s: %w", dir, err)
		}
	}

	// Convert enabled plugins set to sorted slice
	for plugin := range enabledPluginsSet {
		result.EnabledPlugins = append(result.EnabledPlugins, plugin)
	}
	sort.Strings(result.EnabledPlugins)

	return result, nil
}

// mergeDir merges all JSON files in a directory into the result
func mergeDir(result *SettingsConfig, dir string, enabledPluginsSet map[string]bool, opts MergeOptions, log func(string, ...interface{})) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Sort files for deterministic order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		if err := mergeFile(result, filePath, enabledPluginsSet, opts, log); err != nil {
			return fmt.Errorf("failed to merge file %s: %w", filePath, err)
		}
	}

	return nil
}

// settingsJSON is the JSON representation of settings stored in Secret
// This matches the format used by kubernetes_settings_repository.go
type settingsJSON struct {
	Name           string                      `json:"name,omitempty"`
	Marketplaces   map[string]*marketplaceJSON `json:"marketplaces,omitempty"`
	EnabledPlugins []string                    `json:"enabled_plugins,omitempty"`
	Hooks          map[string]interface{}      `json:"hooks,omitempty"`
}

// marketplaceJSON is the JSON representation of a single marketplace
type marketplaceJSON struct {
	URL string `json:"url"`
}

// mergeFile merges a single JSON file into the result
func mergeFile(result *SettingsConfig, filePath string, enabledPluginsSet map[string]bool, opts MergeOptions, log func(string, ...interface{})) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var settings settingsJSON
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Merge marketplaces (later files override earlier ones)
	for name, marketplace := range settings.Marketplaces {
		if marketplace == nil {
			continue
		}
		if _, exists := result.Marketplaces[name]; exists {
			log("[SETTINGS] Overriding marketplace: %s (from %s)", name, filePath)
		} else {
			log("[SETTINGS] Adding marketplace: %s (from %s)", name, filePath)
		}
		result.Marketplaces[name] = MarketplaceConfig{URL: marketplace.URL}
	}

	// Merge enabled plugins (union of all)
	for _, plugin := range settings.EnabledPlugins {
		if plugin == "" {
			continue
		}
		if !enabledPluginsSet[plugin] {
			log("[SETTINGS] Adding enabled plugin: %s (from %s)", plugin, filePath)
			enabledPluginsSet[plugin] = true
		}
	}

	// Merge hooks (later files override earlier ones)
	if settings.Hooks != nil {
		for hookEvent, hookConfig := range settings.Hooks {
			if len(result.Hooks) > 0 && result.Hooks[hookEvent] != nil {
				log("[SETTINGS] Overriding hook: %s (from %s)", hookEvent, filePath)
			} else {
				log("[SETTINGS] Adding hook: %s (from %s)", hookEvent, filePath)
			}
			result.Hooks[hookEvent] = hookConfig
		}
	}

	return nil
}

// WriteConfig writes the merged config to a file
func WriteConfig(config *SettingsConfig, outputPath string) error {
	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	// Create output structure matching settings.json format
	output := settingsJSON{
		Name: "merged",
	}

	if len(config.Marketplaces) > 0 {
		output.Marketplaces = make(map[string]*marketplaceJSON)
		for name, marketplace := range config.Marketplaces {
			output.Marketplaces[name] = &marketplaceJSON{URL: marketplace.URL}
		}
	}

	if len(config.EnabledPlugins) > 0 {
		output.EnabledPlugins = config.EnabledPlugins
	}

	if len(config.Hooks) > 0 {
		output.Hooks = config.Hooks
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// MergeAndWrite is a convenience function that merges configs and writes to output
func MergeAndWrite(inputDirs []string, outputPath string, opts MergeOptions) error {
	config, err := MergeConfigs(inputDirs, opts)
	if err != nil {
		return err
	}

	return WriteConfig(config, outputPath)
}

// MergeInMemory merges a slice of SettingsConfig values in order (later overrides earlier)
// without reading from the filesystem. Returns nil if the input slice is empty.
func MergeInMemory(cfgs []SettingsConfig) *SettingsConfig {
	if len(cfgs) == 0 {
		return nil
	}

	result := &SettingsConfig{
		Marketplaces:   make(map[string]MarketplaceConfig),
		EnabledPlugins: []string{},
		Hooks:          make(map[string]interface{}),
	}
	enabledPluginsSet := make(map[string]bool)

	for _, cfg := range cfgs {
		for name, mp := range cfg.Marketplaces {
			result.Marketplaces[name] = mp
		}
		for _, plugin := range cfg.EnabledPlugins {
			if plugin != "" {
				enabledPluginsSet[plugin] = true
			}
		}
		for event, hook := range cfg.Hooks {
			result.Hooks[event] = hook
		}
	}

	for plugin := range enabledPluginsSet {
		result.EnabledPlugins = append(result.EnabledPlugins, plugin)
	}
	sort.Strings(result.EnabledPlugins)

	return result
}
