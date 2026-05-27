package sessionsettings

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// CompileOptions configures the compile-settings behavior.
type CompileOptions struct {
	InputPath   string // Path to settings YAML (default: /session-settings/settings.yaml)
	OutputDir   string // Base output directory (default: /home/agentapi)
	EnvFilePath string // Path for env file output (default: /session-settings/env)
	StartupPath string // Path for startup script (default: /session-settings/startup.sh)
}

// DefaultCompileOptions returns the default compile options.
func DefaultCompileOptions() CompileOptions {
	return CompileOptions{
		InputPath:   "/session-settings/settings.yaml",
		OutputDir:   "/home/agentapi",
		EnvFilePath: "/home/agentapi/.session/env",
		StartupPath: "/home/agentapi/.session/startup.sh",
	}
}

// Compile reads the settings YAML and generates all output files.
func Compile(opts CompileOptions) error {
	log.Printf("[COMPILE-SETTINGS] Reading settings from %s", opts.InputPath)

	// 1. Read and parse YAML
	data, err := os.ReadFile(opts.InputPath)
	if err != nil {
		return fmt.Errorf("failed to read settings file %s: %w", opts.InputPath, err)
	}

	var settings SessionSettings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("failed to parse settings YAML: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Loaded settings for session %s (user: %s)", settings.Session.ID, settings.Session.UserID)

	// 2. Generate ~/.claude.json (includes mcpServers if present)
	if err := generateClaudeJSON(opts.OutputDir, settings.Claude.ClaudeJSON, settings.Claude.MCPServers); err != nil {
		return fmt.Errorf("failed to generate .claude.json: %w", err)
	}

	// 3. Generate ~/.claude/settings.json
	if err := generateSettingsJSON(opts.OutputDir, settings.Claude.SettingsJSON); err != nil {
		return fmt.Errorf("failed to generate settings.json: %w", err)
	}

	// 3b. Generate ~/.codex/hooks.json (codex-acp sessions only)
	if err := generateCodexHooksJSON(opts.OutputDir, settings.Codex.HooksJSON); err != nil {
		return fmt.Errorf("failed to generate codex hooks.json: %w", err)
	}

	// 3c. Generate ~/.codex/config.toml (codex-acp sessions only)
	if err := generateCodexConfigTOML(opts.OutputDir, settings.Codex.ConfigTOML); err != nil {
		return fmt.Errorf("failed to generate codex config.toml: %w", err)
	}

	// 4. Generate env file
	if err := generateEnvFile(opts.EnvFilePath, settings.Env); err != nil {
		return fmt.Errorf("failed to generate env file: %w", err)
	}

	// 5. Generate startup script
	if err := generateStartupScript(opts.StartupPath, settings.Startup); err != nil {
		return fmt.Errorf("failed to generate startup script: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Successfully compiled all configuration files")
	return nil
}

// generateClaudeJSON creates ~/.claude.json with onboarding settings and MCP server configuration.
// Mirrors the pattern from pkg/startup/sync.go generateClaudeJSON (lines 157-188).
// mcpServers, if non-empty, is written to the "mcpServers" key so Claude Code can read it natively.
func generateClaudeJSON(outputDir string, claudeJSON map[string]interface{}, mcpServers map[string]interface{}) error {
	// Create output directory if needed
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	claudeJSONPath := filepath.Join(outputDir, ".claude.json")

	// Read existing file if present
	var existing map[string]interface{}
	if data, err := os.ReadFile(claudeJSONPath); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			log.Printf("[COMPILE-SETTINGS] Warning: failed to parse existing .claude.json: %v", err)
			existing = make(map[string]interface{})
		}
	} else {
		existing = make(map[string]interface{})
	}

	// Merge in provided values
	for k, v := range claudeJSON {
		existing[k] = v
	}

	// Always set required onboarding fields
	existing["hasCompletedOnboarding"] = true
	existing["bypassPermissionsModeAccepted"] = true

	// Write MCP servers directly into claude.json so Claude Code reads them natively
	if len(mcpServers) > 0 {
		existing["mcpServers"] = mcpServers
	}

	// Write file
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal .claude.json: %w", err)
	}

	if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write .claude.json: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", claudeJSONPath)
	return nil
}

// patchClaudeJSON reads the existing ~/.claude.json (written by Claude CLI or
// compile step) and ensures the required onboarding fields are set to true.
// This is called after syncExtra so that any Claude CLI invocations during
// marketplace/plugin setup cannot leave the file in a state that triggers
// the "Welcome to Claude Code" screen.
func patchClaudeJSON(outputDir string, extra map[string]interface{}) error {
	return generateClaudeJSON(outputDir, extra, nil)
}

// generateSettingsJSON creates ~/.claude/settings.json.
func generateSettingsJSON(outputDir string, settingsJSON map[string]interface{}) error {
	claudeDir := filepath.Join(outputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")

	// If no settings provided, create minimal empty settings
	if len(settingsJSON) == 0 {
		settingsJSON = map[string]interface{}{
			"settings": map[string]interface{}{
				"mcp.enabled": true,
			},
		}
	}

	// Always set autoUpdatesChannel to "stable" to prevent automatic updates
	// to non-stable channels in session containers.
	settingsJSON["autoUpdatesChannel"] = "stable"

	data, err := json.MarshalIndent(settingsJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings.json: %w", err)
	}

	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write settings.json: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", settingsPath)
	return nil
}

// generateEnvFile creates env file with sorted KEY=VALUE lines.
func generateEnvFile(envFilePath string, env map[string]string) error {
	// Create parent directory if needed
	envDir := filepath.Dir(envFilePath)
	if err := os.MkdirAll(envDir, 0755); err != nil {
		return fmt.Errorf("failed to create env file directory: %w", err)
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build lines
	var lines []string
	for _, k := range keys {
		v := env[k]
		// Quote value if it contains spaces or special characters
		if strings.ContainsAny(v, " \t\n\"'\\") {
			v = fmt.Sprintf(`"%s"`, strings.ReplaceAll(v, `"`, `\"`))
		}
		lines = append(lines, fmt.Sprintf("%s=%s", k, v))
	}

	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n" // Add trailing newline
	}

	if err := os.WriteFile(envFilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s (%d variables)", envFilePath, len(env))
	return nil
}

// managedSettingsPath is the well-known location for organisation-managed Claude Code policy.
// The same file is read by the Codex hook generator to keep both runtimes in sync.
const managedSettingsPath = "/etc/claude-code/managed-settings.json"

// generateCodexHooksJSON creates ~/.codex/hooks.json for Codex CLI hook configuration.
// Only written when hooksJSON is non-empty (i.e., for codex-acp sessions).
// Hooks from managed-settings.json are merged in so the same organisation-wide policy
// (UserPromptSubmit, Stop notifications, etc.) applies to Codex sessions too.
func generateCodexHooksJSON(outputDir string, hooksJSON map[string]interface{}) error {
	if len(hooksJSON) == 0 {
		return nil
	}

	// Merge hooks from the managed-settings policy file (mirrors Claude Code behaviour).
	hooksJSON = mergeCodexManagedHooks(hooksJSON)

	codexDir := filepath.Join(outputDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	hooksPath := filepath.Join(codexDir, "hooks.json")
	data, err := json.MarshalIndent(hooksJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal codex hooks.json: %w", err)
	}

	if err := os.WriteFile(hooksPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write codex hooks.json: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", hooksPath)
	return nil
}

// MergeCodexManagedHooks reads managed-settings.json and appends its hook entries
// to each matching event in hooksJSON. Unknown events are added as new entries.
// Returns the original hooksJSON unchanged if the file is absent or unreadable.
// Exported so callers outside this package (e.g. the session manager) can reuse the logic.
func MergeCodexManagedHooks(hooksJSON map[string]interface{}) map[string]interface{} {
	return mergeCodexManagedHooks(hooksJSON)
}

// mergeCodexManagedHooks is the unexported implementation; call MergeCodexManagedHooks externally.
func mergeCodexManagedHooks(hooksJSON map[string]interface{}) map[string]interface{} {
	data, err := os.ReadFile(managedSettingsPath)
	if err != nil {
		return hooksJSON // file absent — nothing to merge
	}

	var managed map[string]interface{}
	if err := json.Unmarshal(data, &managed); err != nil {
		log.Printf("[COMPILE-SETTINGS] Warning: failed to parse %s: %v", managedSettingsPath, err)
		return hooksJSON
	}

	managedHooks, ok := managed["hooks"].(map[string]interface{})
	if !ok || len(managedHooks) == 0 {
		return hooksJSON
	}

	// Work on a shallow copy so we don't mutate the caller's map.
	merged := make(map[string]interface{}, len(hooksJSON))
	for k, v := range hooksJSON {
		merged[k] = v
	}

	baseHooks, ok := merged["hooks"].(map[string]interface{})
	if !ok {
		baseHooks = make(map[string]interface{})
	}
	// Clone the inner hooks map too.
	innerHooks := make(map[string]interface{}, len(baseHooks))
	for k, v := range baseHooks {
		innerHooks[k] = v
	}

	for event, entries := range managedHooks {
		managedList, ok := entries.([]interface{})
		if !ok {
			continue
		}
		if existing, ok := innerHooks[event].([]interface{}); ok {
			innerHooks[event] = append(existing, managedList...)
		} else {
			innerHooks[event] = managedList
		}
	}

	merged["hooks"] = innerHooks
	log.Printf("[COMPILE-SETTINGS] Merged %d hook event(s) from %s into codex hooks.json", len(managedHooks), managedSettingsPath)
	return merged
}

// generateCodexConfigTOML creates ~/.codex/config.toml for Codex CLI configuration.
// Only written when configTOML is non-empty (i.e., for codex-acp sessions).
func generateCodexConfigTOML(outputDir string, configTOML string) error {
	if configTOML == "" {
		return nil
	}

	codexDir := filepath.Join(outputDir, ".codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		return fmt.Errorf("failed to create .codex directory: %w", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configTOML), 0644); err != nil {
		return fmt.Errorf("failed to write codex config.toml: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", configPath)
	return nil
}

// generateStartupScript creates executable shell script with the startup command.
func generateStartupScript(startupPath string, startup StartupConfig) error {
	// Create parent directory if needed
	startupDir := filepath.Dir(startupPath)
	if err := os.MkdirAll(startupDir, 0755); err != nil {
		return fmt.Errorf("failed to create startup script directory: %w", err)
	}

	// Build command line
	var commandLine string
	if len(startup.Command) > 0 {
		parts := append([]string{}, startup.Command...)
		parts = append(parts, startup.Args...)
		// Quote parts that contain spaces
		for i, part := range parts {
			if strings.ContainsAny(part, " \t\n") {
				parts[i] = fmt.Sprintf(`"%s"`, part)
			}
		}
		commandLine = strings.Join(parts, " ")
	} else {
		commandLine = "# No startup command configured"
	}

	var preScriptSection string
	if startup.PreScript != "" {
		preScriptSection = startup.PreScript + "\n\n"
	}

	scriptContent := fmt.Sprintf(`#!/bin/sh
# Generated startup script for session
# DO NOT EDIT - this file is auto-generated by compile-settings

set -e

%s%s
`, preScriptSection, commandLine)

	if err := os.WriteFile(startupPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write startup script: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", startupPath)
	return nil
}
