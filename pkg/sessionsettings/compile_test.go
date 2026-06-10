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

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: envFile,
		StartupPath: startupFile,
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Verify all files were created
	assert.FileExists(t, filepath.Join(outputDir, ".claude.json"))
	assert.FileExists(t, filepath.Join(outputDir, ".claude/settings.json"))
	assert.FileExists(t, envFile)
	assert.FileExists(t, startupFile)

	// Verify mcpServers is written into claude.json
	claudeJSONPath := filepath.Join(outputDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	require.NoError(t, err)
	var claudeJSON map[string]interface{}
	err = json.Unmarshal(data, &claudeJSON)
	require.NoError(t, err)
	assert.Contains(t, claudeJSON, "mcpServers")
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

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: envFile,
		StartupPath: startupFile,
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Claude files should still be created with defaults
	assert.FileExists(t, filepath.Join(outputDir, ".claude.json"))
	assert.FileExists(t, filepath.Join(outputDir, ".claude/settings.json"))

	// Empty env file should be created
	assert.FileExists(t, envFile)

	// No mcpServers key when no servers configured
	claudeJSONPath := filepath.Join(outputDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	require.NoError(t, err)
	var claudeJSON map[string]interface{}
	err = json.Unmarshal(data, &claudeJSON)
	require.NoError(t, err)
	assert.NotContains(t, claudeJSON, "mcpServers")
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
		InputPath:   inputPath,
		OutputDir:   outputDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
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
	assert.Equal(t, true, claudeJSON["hasTrustDialogAccepted"])
	assert.Equal(t, true, claudeJSON["hasCompletedProjectOnboarding"])
	assert.Equal(t, true, claudeJSON["dontCrawlDirectory"])
	assertClaudeProjectTrusted(t, claudeJSON, "/home/agentapi/workdir")
	assertClaudeProjectTrusted(t, claudeJSON, "/home/agentapi/workdir/repo")

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
		InputPath:   inputPath,
		OutputDir:   tmpDir,
		EnvFilePath: envFile,
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
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
		InputPath:   inputPath,
		OutputDir:   tmpDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: startupFile,
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

func TestCompile_MCPServersInClaudeJSON(t *testing.T) {
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

	opts := CompileOptions{
		InputPath:   inputPath,
		OutputDir:   tmpDir,
		EnvFilePath: filepath.Join(tmpDir, "env"),
		StartupPath: filepath.Join(tmpDir, "startup.sh"),
	}

	err = Compile(opts)
	require.NoError(t, err)

	// Read and verify .claude.json contains mcpServers
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	require.NoError(t, err)

	var claudeJSON map[string]interface{}
	err = json.Unmarshal(data, &claudeJSON)
	require.NoError(t, err)

	// Verify mcpServers is embedded in claude.json
	assert.Contains(t, claudeJSON, "mcpServers")
	servers := claudeJSON["mcpServers"].(map[string]interface{})
	assert.Len(t, servers, 2)
	assert.Contains(t, servers, "server1")
	assert.Contains(t, servers, "server2")

	// Verify onboarding flags are still present
	assert.Equal(t, true, claudeJSON["hasCompletedOnboarding"])
	assert.Equal(t, true, claudeJSON["bypassPermissionsModeAccepted"])
	assertClaudeProjectTrusted(t, claudeJSON, "/home/agentapi/workdir")
}

func TestCompile_AutoUpdatesChannelStable(t *testing.T) {
	t.Run("default settings has autoUpdatesChannel stable", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-autoupdates-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Minimal settings with no settingsJSON configured
		settings := &SessionSettings{
			Session: SessionMeta{
				ID:     "test-autoupdate",
				UserID: "user-autoupdate",
				Scope:  "user",
			},
		}

		inputPath := filepath.Join(tmpDir, "settings.yaml")
		yamlData, err := MarshalYAML(settings)
		require.NoError(t, err)
		err = os.WriteFile(inputPath, yamlData, 0644)
		require.NoError(t, err)

		outputDir := filepath.Join(tmpDir, "output")
		opts := CompileOptions{
			InputPath:   inputPath,
			OutputDir:   outputDir,
			EnvFilePath: filepath.Join(tmpDir, "env"),
			StartupPath: filepath.Join(tmpDir, "startup.sh"),
		}

		err = Compile(opts)
		require.NoError(t, err)

		// Read and verify settings.json
		settingsPath := filepath.Join(outputDir, ".claude/settings.json")
		data, err := os.ReadFile(settingsPath)
		require.NoError(t, err)

		var settingsJSON map[string]interface{}
		err = json.Unmarshal(data, &settingsJSON)
		require.NoError(t, err)

		assert.Equal(t, "stable", settingsJSON["autoUpdatesChannel"])
		assertClaudePermissionsBypass(t, settingsJSON)
	})

	t.Run("custom settingsJSON also has autoUpdatesChannel stable", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-autoupdates-custom-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Settings with custom settingsJSON
		settings := &SessionSettings{
			Session: SessionMeta{
				ID:     "test-autoupdate-custom",
				UserID: "user-autoupdate-custom",
				Scope:  "user",
			},
			Claude: ClaudeConfig{
				SettingsJSON: map[string]interface{}{
					"settings": map[string]interface{}{
						"mcp.enabled": true,
					},
					"customKey": "customValue",
				},
			},
		}

		inputPath := filepath.Join(tmpDir, "settings.yaml")
		yamlData, err := MarshalYAML(settings)
		require.NoError(t, err)
		err = os.WriteFile(inputPath, yamlData, 0644)
		require.NoError(t, err)

		outputDir := filepath.Join(tmpDir, "output")
		opts := CompileOptions{
			InputPath:   inputPath,
			OutputDir:   outputDir,
			EnvFilePath: filepath.Join(tmpDir, "env"),
			StartupPath: filepath.Join(tmpDir, "startup.sh"),
		}

		err = Compile(opts)
		require.NoError(t, err)

		// Read and verify settings.json
		settingsPath := filepath.Join(outputDir, ".claude/settings.json")
		data, err := os.ReadFile(settingsPath)
		require.NoError(t, err)

		var settingsJSON map[string]interface{}
		err = json.Unmarshal(data, &settingsJSON)
		require.NoError(t, err)

		// autoUpdatesChannel should always be "stable"
		assert.Equal(t, "stable", settingsJSON["autoUpdatesChannel"])
		assertClaudePermissionsBypass(t, settingsJSON)
		// Custom key should be preserved
		assert.Equal(t, "customValue", settingsJSON["customKey"])
	})
}

func assertClaudeProjectTrusted(t *testing.T, claudeJSON map[string]interface{}, dir string) {
	t.Helper()
	projects, ok := claudeJSON["projects"].(map[string]interface{})
	require.True(t, ok, "projects should exist")
	project, ok := projects[dir].(map[string]interface{})
	require.True(t, ok, "project %s should exist", dir)
	assert.Equal(t, true, project["hasTrustDialogAccepted"])
	assert.Equal(t, true, project["hasCompletedProjectOnboarding"])
}

func assertClaudePermissionsBypass(t *testing.T, settingsJSON map[string]interface{}) {
	t.Helper()
	permissions, ok := settingsJSON["permissions"].(map[string]interface{})
	require.True(t, ok, "permissions should exist")
	assert.Equal(t, "bypassPermissions", permissions["defaultMode"])
	assert.Equal(t, true, permissions["skipDangerousModePermissionPrompt"])
}

func TestCompile_CodexConfigTOML(t *testing.T) {
	t.Run("writes config.toml when CodexConfigTOML is set", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-codex-config-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		settings := &SessionSettings{
			Session: SessionMeta{
				ID:        "test-codex",
				UserID:    "user-codex",
				Scope:     "user",
				AgentType: "codex-acp",
			},
			Codex: CodexConfig{
				ConfigTOML: "approval-mode = \"full-auto\"\nsandbox_mode = \"danger-full-access\"\n",
			},
		}

		inputPath := filepath.Join(tmpDir, "settings.yaml")
		yamlData, err := MarshalYAML(settings)
		require.NoError(t, err)
		err = os.WriteFile(inputPath, yamlData, 0644)
		require.NoError(t, err)

		outputDir := filepath.Join(tmpDir, "output")
		opts := CompileOptions{
			InputPath:   inputPath,
			OutputDir:   outputDir,
			EnvFilePath: filepath.Join(tmpDir, "env"),
			StartupPath: filepath.Join(tmpDir, "startup.sh"),
		}

		err = Compile(opts)
		require.NoError(t, err)

		configPath := filepath.Join(outputDir, ".codex/config.toml")
		assert.FileExists(t, configPath)

		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, "approval-mode = \"full-auto\"\nsandbox_mode = \"danger-full-access\"\n", string(data))
	})

	t.Run("skips config.toml when CodexConfigTOML is empty", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-codex-config-empty-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		settings := &SessionSettings{
			Session: SessionMeta{
				ID:     "test-no-codex",
				UserID: "user-no-codex",
				Scope:  "user",
			},
		}

		inputPath := filepath.Join(tmpDir, "settings.yaml")
		yamlData, err := MarshalYAML(settings)
		require.NoError(t, err)
		err = os.WriteFile(inputPath, yamlData, 0644)
		require.NoError(t, err)

		outputDir := filepath.Join(tmpDir, "output")
		opts := CompileOptions{
			InputPath:   inputPath,
			OutputDir:   outputDir,
			EnvFilePath: filepath.Join(tmpDir, "env"),
			StartupPath: filepath.Join(tmpDir, "startup.sh"),
		}

		err = Compile(opts)
		require.NoError(t, err)

		assert.NoFileExists(t, filepath.Join(outputDir, ".codex/config.toml"))
	})
}

func TestCompile_CodexMCPServers(t *testing.T) {
	t.Run("appends mcp_servers to config.toml", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-codex-mcp-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		settings := &SessionSettings{
			Session: SessionMeta{
				ID:        "test-codex-mcp",
				UserID:    "user-codex-mcp",
				Scope:     "user",
				AgentType: "codex-agentapi",
			},
			Codex: CodexConfig{
				MCPServers: map[string]interface{}{
					"github": map[string]interface{}{
						"type":    "stdio",
						"command": "npx",
						"args":    []interface{}{"-y", "@modelcontextprotocol/server-github"},
						"env":     map[string]interface{}{"GITHUB_TOKEN": "ghp_xxx"},
					},
					"slack": map[string]interface{}{
						"type": "http",
						"url":  "https://mcp.example.com/slack",
						"env":  map[string]interface{}{"SLACK_TOKEN": "xoxb-token"},
					},
				},
			},
		}

		inputPath := filepath.Join(tmpDir, "settings.yaml")
		yamlData, err := MarshalYAML(settings)
		require.NoError(t, err)
		err = os.WriteFile(inputPath, yamlData, 0644)
		require.NoError(t, err)

		outputDir := filepath.Join(tmpDir, "output")
		opts := CompileOptions{
			InputPath:   inputPath,
			OutputDir:   outputDir,
			EnvFilePath: filepath.Join(tmpDir, "env"),
			StartupPath: filepath.Join(tmpDir, "startup.sh"),
		}

		err = Compile(opts)
		require.NoError(t, err)

		configPath := filepath.Join(outputDir, ".codex/config.toml")
		assert.FileExists(t, configPath)

		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		content := string(data)

		// Both servers should appear as [mcp_servers.<name>] nested-table entries.
		assert.Contains(t, content, "[mcp_servers.github]")
		assert.Contains(t, content, `type = "stdio"`)
		assert.Contains(t, content, `command = "npx"`)
		assert.Contains(t, content, "[mcp_servers.slack]")
		assert.Contains(t, content, `type = "http"`)
		assert.Contains(t, content, `url = "https://mcp.example.com/slack"`)
		assert.Contains(t, content, "GITHUB_TOKEN")
		assert.NotContains(t, content, "SLACK_TOKEN")
	})

	t.Run("omits env for streamable_http mcp_servers", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-codex-mcp-streamable-http-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		settings := &SessionSettings{
			Session: SessionMeta{
				ID:        "test-codex-mcp-streamable-http",
				UserID:    "user-codex-mcp-streamable-http",
				Scope:     "user",
				AgentType: "codex-acp",
			},
			Codex: CodexConfig{
				MCPServers: map[string]interface{}{
					"github": map[string]interface{}{
						"type": "streamable_http",
						"url":  "https://api.githubcopilot.com/mcp",
						"env":  map[string]interface{}{"GITHUB_TOKEN": "$GITHUB_TOKEN"},
					},
				},
			},
		}

		inputPath := filepath.Join(tmpDir, "settings.yaml")
		yamlData, err := MarshalYAML(settings)
		require.NoError(t, err)
		err = os.WriteFile(inputPath, yamlData, 0644)
		require.NoError(t, err)

		outputDir := filepath.Join(tmpDir, "output")
		opts := CompileOptions{
			InputPath:   inputPath,
			OutputDir:   outputDir,
			EnvFilePath: filepath.Join(tmpDir, "env"),
			StartupPath: filepath.Join(tmpDir, "startup.sh"),
		}

		err = Compile(opts)
		require.NoError(t, err)

		configPath := filepath.Join(outputDir, ".codex/config.toml")
		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		content := string(data)

		assert.Contains(t, content, "[mcp_servers.github]")
		assert.Contains(t, content, `type = "streamable_http"`)
		assert.Contains(t, content, `url = "https://api.githubcopilot.com/mcp"`)
		assert.NotContains(t, content, "GITHUB_TOKEN")
		assert.NotContains(t, content, "env =")
	})

	t.Run("appends mcp_servers after existing ConfigTOML", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-codex-mcp-append-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		settings := &SessionSettings{
			Session: SessionMeta{
				ID:        "test-codex-mcp-append",
				UserID:    "user-codex-mcp-append",
				Scope:     "user",
				AgentType: "codex-acp",
			},
			Codex: CodexConfig{
				ConfigTOML: "approval-mode = \"full-auto\"\nsandbox_mode = \"danger-full-access\"\n",
				MCPServers: map[string]interface{}{
					"myhub": map[string]interface{}{
						"type": "http",
						"url":  "https://myhub.example.com/mcp",
					},
				},
			},
		}

		inputPath := filepath.Join(tmpDir, "settings.yaml")
		yamlData, err := MarshalYAML(settings)
		require.NoError(t, err)
		err = os.WriteFile(inputPath, yamlData, 0644)
		require.NoError(t, err)

		outputDir := filepath.Join(tmpDir, "output")
		opts := CompileOptions{
			InputPath:   inputPath,
			OutputDir:   outputDir,
			EnvFilePath: filepath.Join(tmpDir, "env"),
			StartupPath: filepath.Join(tmpDir, "startup.sh"),
		}

		err = Compile(opts)
		require.NoError(t, err)

		configPath := filepath.Join(outputDir, ".codex/config.toml")
		data, err := os.ReadFile(configPath)
		require.NoError(t, err)
		content := string(data)

		// Base config should be present.
		assert.Contains(t, content, "approval-mode")
		assert.Contains(t, content, "sandbox_mode")
		// MCP server should be appended after the base config.
		assert.Contains(t, content, "[mcp_servers.myhub]")
		assert.Contains(t, content, `url = "https://myhub.example.com/mcp"`)
		// The base config should appear BEFORE the mcp_servers section.
		assert.Less(t, strings.Index(content, "approval-mode"), strings.Index(content, "[mcp_servers."))
	})

	t.Run("skips when MCPServers is empty", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "compile-codex-mcp-empty-*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		settings := &SessionSettings{
			Session: SessionMeta{
				ID:     "test-no-mcp",
				UserID: "user-no-mcp",
				Scope:  "user",
			},
		}

		inputPath := filepath.Join(tmpDir, "settings.yaml")
		yamlData, err := MarshalYAML(settings)
		require.NoError(t, err)
		err = os.WriteFile(inputPath, yamlData, 0644)
		require.NoError(t, err)

		outputDir := filepath.Join(tmpDir, "output")
		opts := CompileOptions{
			InputPath:   inputPath,
			OutputDir:   outputDir,
			EnvFilePath: filepath.Join(tmpDir, "env"),
			StartupPath: filepath.Join(tmpDir, "startup.sh"),
		}

		err = Compile(opts)
		require.NoError(t, err)

		// No config.toml should be created when neither ConfigTOML nor MCPServers is set.
		assert.NoFileExists(t, filepath.Join(outputDir, ".codex/config.toml"))
	})
}

func TestCompile_MissingInput(t *testing.T) {
	opts := CompileOptions{
		InputPath:   "/nonexistent/settings.yaml",
		OutputDir:   "/tmp",
		EnvFilePath: "/tmp/env",
		StartupPath: "/tmp/startup.sh",
	}

	err := Compile(opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read settings file")
}

func TestDefaultCompileOptions(t *testing.T) {
	opts := DefaultCompileOptions()

	assert.Equal(t, "/session-settings/settings.yaml", opts.InputPath)
	assert.Equal(t, "/home/agentapi", opts.OutputDir)
	assert.Equal(t, "/home/agentapi/.session/env", opts.EnvFilePath)
	assert.Equal(t, "/home/agentapi/.session/startup.sh", opts.StartupPath)
}
