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
	InputPath     string // Path to settings YAML (default: /session-settings/settings.yaml)
	OutputDir     string // Base output directory (default: /home/agentapi)
	EnvFilePath   string // Path for env file output (default: /home/agentapi/.session/env)
	StartupPath   string // Path for startup script (default: /home/agentapi/.session/startup.sh)
	MCPOutputPath string // Path for MCP config (default: /mcp-config/merged.json)
}

// DefaultCompileOptions returns the default compile options.
func DefaultCompileOptions() CompileOptions {
	return CompileOptions{
		InputPath:     "/session-settings/settings.yaml",
		OutputDir:     "/home/agentapi",
		EnvFilePath:   "/home/agentapi/.session/env",
		StartupPath:   "/home/agentapi/.session/startup.sh",
		MCPOutputPath: "/mcp-config/merged.json",
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

	// 2. Generate ~/.claude.json
	if err := generateClaudeJSON(opts.OutputDir, settings.Claude.ClaudeJSON); err != nil {
		return fmt.Errorf("failed to generate .claude.json: %w", err)
	}

	// 3. Generate ~/.claude/settings.json
	if err := generateSettingsJSON(opts.OutputDir, settings.Claude.SettingsJSON); err != nil {
		return fmt.Errorf("failed to generate settings.json: %w", err)
	}

	// 4. Generate MCP config
	if len(settings.Claude.MCPServers) > 0 {
		if err := generateMCPConfig(opts.MCPOutputPath, settings.Claude.MCPServers); err != nil {
			return fmt.Errorf("failed to generate MCP config: %w", err)
		}
	}

	// 5. Generate env file
	if err := generateEnvFile(opts.EnvFilePath, settings.Env); err != nil {
		return fmt.Errorf("failed to generate env file: %w", err)
	}

	// 6. Generate startup script
	if err := generateStartupScript(opts.StartupPath, settings.Startup); err != nil {
		return fmt.Errorf("failed to generate startup script: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Successfully compiled all configuration files")
	return nil
}

// generateClaudeJSON creates ~/.claude.json with onboarding settings.
// Mirrors the pattern from pkg/startup/sync.go generateClaudeJSON (lines 157-188).
func generateClaudeJSON(outputDir string, claudeJSON map[string]interface{}) error {
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

// generateMCPConfig creates /mcp-config/merged.json with {"mcpServers": {...}} wrapper.
// Mirrors the pattern from pkg/mcp/merge.go.
func generateMCPConfig(mcpOutputPath string, mcpServers map[string]interface{}) error {
	// Create parent directory if needed
	mcpDir := filepath.Dir(mcpOutputPath)
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		return fmt.Errorf("failed to create MCP config directory: %w", err)
	}

	// Wrap in mcpServers key
	wrapped := map[string]interface{}{
		"mcpServers": mcpServers,
	}

	data, err := json.MarshalIndent(wrapped, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	if err := os.WriteFile(mcpOutputPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write MCP config: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", mcpOutputPath)
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

	scriptContent := fmt.Sprintf(`#!/bin/sh
# Generated startup script for session
# DO NOT EDIT - this file is auto-generated by compile-settings

set -e

%s
`, commandLine)

	if err := os.WriteFile(startupPath, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write startup script: %w", err)
	}

	log.Printf("[COMPILE-SETTINGS] Generated %s", startupPath)
	return nil
}
