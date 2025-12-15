// Package mcp provides utilities for managing MCP (Model Context Protocol) server configurations.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// MCPConfig represents the structure of MCP configuration
type MCPConfig struct {
	MCPServers map[string]MCPServer `json:"mcpServers"`
}

// MCPServer represents a single MCP server configuration
type MCPServer struct {
	Type    string            `json:"type"`              // "http", "stdio", "sse"
	URL     string            `json:"url,omitempty"`     // for http/sse
	Command string            `json:"command,omitempty"` // for stdio
	Args    []string          `json:"args,omitempty"`    // for stdio
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"` // for http/sse
}

// MergeOptions configures the merge behavior
type MergeOptions struct {
	ExpandEnv bool
	Verbose   bool
	Logger    func(format string, args ...interface{})
}

// MergeConfigs merges multiple MCP config directories into a single config.
// Later directories take precedence over earlier ones (last wins).
func MergeConfigs(inputDirs []string, opts MergeOptions) (*MCPConfig, error) {
	result := &MCPConfig{
		MCPServers: make(map[string]MCPServer),
	}

	log := opts.Logger
	if log == nil {
		log = func(format string, args ...interface{}) {}
	}

	for _, dir := range inputDirs {
		if dir == "" {
			continue
		}

		if err := mergeDir(result, dir, opts, log); err != nil {
			// Directory not found is not an error (Optional)
			if os.IsNotExist(err) {
				log("[MCP] Directory not found, skipping: %s", dir)
				continue
			}
			return nil, fmt.Errorf("failed to merge directory %s: %w", dir, err)
		}
	}

	return result, nil
}

// mergeDir merges all JSON files in a directory into the result
func mergeDir(result *MCPConfig, dir string, opts MergeOptions, log func(string, ...interface{})) error {
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
		if err := mergeFile(result, filePath, opts, log); err != nil {
			return fmt.Errorf("failed to merge file %s: %w", filePath, err)
		}
	}

	return nil
}

// mergeFile merges a single JSON file into the result
func mergeFile(result *MCPConfig, filePath string, opts MergeOptions, log func(string, ...interface{})) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// Expand environment variables if requested
	if opts.ExpandEnv {
		data = []byte(ExpandEnvVars(string(data)))
	}

	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Merge servers (later files override earlier ones)
	for name, server := range config.MCPServers {
		if _, exists := result.MCPServers[name]; exists {
			log("[MCP] Overriding server: %s (from %s)", name, filePath)
		} else {
			log("[MCP] Adding server: %s (from %s)", name, filePath)
		}
		result.MCPServers[name] = server
	}

	return nil
}

// envVarPattern matches ${VAR} and ${VAR:-default} patterns
var envVarPattern = regexp.MustCompile(`\$\{([^}:]+)(:-([^}]*))?\}`)

// ExpandEnvVars expands ${VAR} and ${VAR:-default} patterns in a string
func ExpandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		submatch := envVarPattern.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}

		varName := submatch[1]
		hasDefault := len(submatch) >= 3 && submatch[2] != "" // Check if :- is present
		defaultVal := ""
		if len(submatch) >= 4 {
			defaultVal = submatch[3]
		}

		if val := os.Getenv(varName); val != "" {
			return val
		}
		if hasDefault {
			return defaultVal // Return default value (may be empty)
		}
		return match // Keep original if no value and no default specified
	})
}

// WriteConfig writes the merged config to a file
func WriteConfig(config *MCPConfig, outputPath string) error {
	// Ensure output directory exists
	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
	}

	data, err := json.MarshalIndent(config, "", "  ")
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
