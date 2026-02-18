package sessionsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompile_FullSettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-full-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create input settings
	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-123",
			UserID:    "user-456",
			Scope:     "user",
			AgentType: "claude-agentapi",
		},
		Env: map[string]string{
			"AGENTAPI_PORT":       "9000",
			"AGENTAPI_SESSION_ID": "test-123",
			"HOME":                "/home/agentapi",
		},
		Claude: ClaudeConfig{
			ClaudeJSON: map[string]interface{}{
				"testKey": "testValue",
			},
			SettingsJSON: map[string]interface{}{
				"settings": map[string]interface{}{
					"mcp.enabled": true,
				},
			},
			MCPServers: map[string]interface{}{
				"test-server": map[string]interface{}{
					"type": "http",
					"url":  "https://example.com",
				},
			},
		},
		Startup: StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--port", "9000"},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	envFile := filepath.Join(tmpDir, "env")
	startupFile := filepath.Join(tmpDir, "startup.sh")
	mcpFile := filepath.Join(tmpDir, "mcp-config.json")

	opts := CompileOptions{
		InputPath:     inputPath,
		OutputDir:     outputDir,
		EnvFilePath:   envFile,
		StartupPath:   startupFile,
		MCPOutputPath: mcpFile,
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Verify all files were created
	assert.FileExists(t, filepath.Join(outputDir, ".claude.json"))
	assert.FileExists(t, filepath.Join(outputDir, ".claude/settings.json"))
	assert.FileExists(t, envFile)
	assert.FileExists(t, startupFile)
	assert.FileExists(t, mcpFile)
}

func TestCompile_MinimalSettings(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-minimal-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Minimal settings - only required fields
	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "min-123",
			UserID: "user-789",
			Scope:  "user",
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := filepath.Join(tmpDir, "output")
	envFile := filepath.Join(tmpDir, "env")
	startupFile := filepath.Join(tmpDir, "startup.sh")
	mcpFile := filepath.Join(tmpDir, "mcp-config.json")

	opts := CompileOptions{
		InputPath:     inputPath,
		OutputDir:     outputDir,
		EnvFilePath:   envFile,
		StartupPath:   startupFile,
		MCPOutputPath: mcpFile,
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Claude files should still be created with defaults
	assert.FileExists(t, filepath.Join(outputDir, ".claude.json"))
	assert.FileExists(t, filepath.Join(outputDir, ".claude/settings.json"))

	// Empty env file should be created
	assert.FileExists(t, envFile)

	// MCP config should not be created (no servers)
	assert.NoFileExists(t, mcpFile)
}

func TestCompile_ClaudeJSON(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-claude-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-claude",
			UserID: "user-claude",
			Scope:  "user",
		},
		Claude: ClaudeConfig{
			ClaudeJSON: map[string]interface{}{
				"customKey": "customValue",
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	outputDir := tmpDir

	opts := CompileOptions{
		InputPath:     inputPath,
		OutputDir:     outputDir,
		EnvFilePath:   filepath.Join(tmpDir, "env"),
		StartupPath:   filepath.Join(tmpDir, "startup.sh"),
		MCPOutputPath: filepath.Join(tmpDir, "mcp.json"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Read and verify .claude.json
	claudeJSONPath := filepath.Join(outputDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	require.NoError(t, err)

	var claudeJSON map[string]interface{}
	err = json.Unmarshal(data, &claudeJSON)
	require.NoError(t, err)

	// Verify onboarding flags are always present
	assert.Equal(t, true, claudeJSON["hasCompletedOnboarding"])
	assert.Equal(t, true, claudeJSON["bypassPermissionsModeAccepted"])

	// Verify custom key is preserved
	assert.Equal(t, "customValue", claudeJSON["customKey"])
}

func TestCompile_EnvFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-env-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-env",
			UserID: "user-env",
			Scope:  "user",
		},
		Env: map[string]string{
			"Z_VAR":        "last",
			"A_VAR":        "first",
			"M_VAR":        "middle",
			"SPACED_VALUE": "value with spaces",
			"QUOTED_VALUE": `value with "quotes"`,
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	envFile := filepath.Join(tmpDir, "env")

	opts := CompileOptions{
		InputPath:     inputPath,
		OutputDir:     tmpDir,
		EnvFilePath:   envFile,
		StartupPath:   filepath.Join(tmpDir, "startup.sh"),
		MCPOutputPath: filepath.Join(tmpDir, "mcp.json"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Read env file
	data, err := os.ReadFile(envFile)
	require.NoError(t, err)

	content := string(data)
	lines := strings.Split(strings.TrimSpace(content), "\n")

	// Verify sorted order
	assert.True(t, strings.HasPrefix(lines[0], "A_VAR="))
	assert.True(t, strings.HasPrefix(lines[1], "M_VAR="))
	assert.Contains(t, content, "SPACED_VALUE=")
	assert.Contains(t, content, "Z_VAR=")

	// Verify values with spaces are quoted
	assert.Contains(t, content, `SPACED_VALUE="value with spaces"`)
}

func TestCompile_StartupScript(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-startup-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-startup",
			UserID: "user-startup",
			Scope:  "user",
		},
		Startup: StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--port", "9000", "--verbose"},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	startupFile := filepath.Join(tmpDir, "startup.sh")

	opts := CompileOptions{
		InputPath:     inputPath,
		OutputDir:     tmpDir,
		EnvFilePath:   filepath.Join(tmpDir, "env"),
		StartupPath:   startupFile,
		MCPOutputPath: filepath.Join(tmpDir, "mcp.json"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Read startup script
	data, err := os.ReadFile(startupFile)
	require.NoError(t, err)

	content := string(data)

	// Verify shebang
	assert.True(t, strings.HasPrefix(content, "#!/bin/sh"))

	// Verify command is present
	assert.Contains(t, content, "agentapi server")
	assert.Contains(t, content, "--port 9000")
	assert.Contains(t, content, "--verbose")

	// Verify file is executable
	info, err := os.Stat(startupFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestCompile_MCPConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "compile-mcp-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "test-mcp",
			UserID: "user-mcp",
			Scope:  "user",
		},
		Claude: ClaudeConfig{
			MCPServers: map[string]interface{}{
				"server1": map[string]interface{}{
					"type": "http",
					"url":  "https://server1.com",
				},
				"server2": map[string]interface{}{
					"type": "http",
					"url":  "https://server2.com",
				},
			},
		},
	}

	inputPath := filepath.Join(tmpDir, "settings.yaml")
	yamlData, err := MarshalYAML(settings)
	require.NoError(t, err)
	err = os.WriteFile(inputPath, yamlData, 0644)
	require.NoError(t, err)

	mcpFile := filepath.Join(tmpDir, "mcp-config.json")

	opts := CompileOptions{
		InputPath:     inputPath,
		OutputDir:     tmpDir,
		EnvFilePath:   filepath.Join(tmpDir, "env"),
		StartupPath:   filepath.Join(tmpDir, "startup.sh"),
		MCPOutputPath: mcpFile,
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Read and verify MCP config
	data, err := os.ReadFile(mcpFile)
	require.NoError(t, err)

	var mcpConfig map[string]interface{}
	err = json.Unmarshal(data, &mcpConfig)
	require.NoError(t, err)

	// Verify mcpServers wrapper
	assert.Contains(t, mcpConfig, "mcpServers")
	servers := mcpConfig["mcpServers"].(map[string]interface{})
	assert.Len(t, servers, 2)
	assert.Contains(t, servers, "server1")
	assert.Contains(t, servers, "server2")
}

func TestCompile_MissingInput(t *testing.T) {
	opts := CompileOptions{
		InputPath:     "/nonexistent/settings.yaml",
		OutputDir:     "/tmp",
		EnvFilePath:   "/tmp/env",
		StartupPath:   "/tmp/startup.sh",
		MCPOutputPath: "/tmp/mcp.json",
	}

	err := Compile(opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read settings file")
}

func TestDefaultCompileOptions(t *testing.T) {
	opts := DefaultCompileOptions()

	assert.Equal(t, "/session-settings/settings.yaml", opts.InputPath)
	assert.Equal(t, "/home/agentapi", opts.OutputDir)
	assert.Equal(t, "/session-settings/env", opts.EnvFilePath)
	assert.Equal(t, "/session-settings/startup.sh", opts.StartupPath)
	assert.Equal(t, "/home/agentapi/.mcp-config/merged.json", opts.MCPOutputPath)
}
