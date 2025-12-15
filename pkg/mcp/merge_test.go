package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeConfigs(t *testing.T) {
	// Create temp directories
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "base")
	teamDir := filepath.Join(tmpDir, "team")
	userDir := filepath.Join(tmpDir, "user")

	require.NoError(t, os.MkdirAll(baseDir, 0755))
	require.NoError(t, os.MkdirAll(teamDir, 0755))
	require.NoError(t, os.MkdirAll(userDir, 0755))

	// Base config
	baseConfig := `{
		"mcpServers": {
			"github": {"type": "http", "url": "https://base.example.com"},
			"sentry": {"type": "http", "url": "https://sentry.example.com"}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "servers.json"), []byte(baseConfig), 0644))

	// Team config (overrides github)
	teamConfig := `{
		"mcpServers": {
			"github": {"type": "http", "url": "https://team.example.com"},
			"db-tool": {"type": "stdio", "command": "npx", "args": ["db-tool"]}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(teamDir, "servers.json"), []byte(teamConfig), 0644))

	// User config (adds personal tool)
	userConfig := `{
		"mcpServers": {
			"my-tool": {"type": "stdio", "command": "/home/user/bin/my-tool"}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "servers.json"), []byte(userConfig), 0644))

	// Merge
	result, err := MergeConfigs([]string{baseDir, teamDir, userDir}, MergeOptions{})
	require.NoError(t, err)

	// Verify
	assert.Len(t, result.MCPServers, 4)
	assert.Equal(t, "https://team.example.com", result.MCPServers["github"].URL)    // Team override
	assert.Equal(t, "https://sentry.example.com", result.MCPServers["sentry"].URL)  // Base
	assert.Equal(t, "npx", result.MCPServers["db-tool"].Command)                    // Team
	assert.Equal(t, "/home/user/bin/my-tool", result.MCPServers["my-tool"].Command) // User
}

func TestMergeConfigsMultipleFilesPerDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple files in same directory
	file1 := `{"mcpServers": {"server-a": {"type": "http", "url": "https://a.example.com"}}}`
	file2 := `{"mcpServers": {"server-b": {"type": "http", "url": "https://b.example.com"}}}`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "01-a.json"), []byte(file1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "02-b.json"), []byte(file2), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	assert.Len(t, result.MCPServers, 2)
	assert.Equal(t, "https://a.example.com", result.MCPServers["server-a"].URL)
	assert.Equal(t, "https://b.example.com", result.MCPServers["server-b"].URL)
}

func TestMergeConfigsWithEnvExpansion(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{
		"mcpServers": {
			"api": {
				"type": "http",
				"url": "${API_URL:-https://default.example.com}",
				"headers": {
					"Authorization": "Bearer ${API_TOKEN}"
				}
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "api.json"), []byte(config), 0644))

	// Set env vars
	t.Setenv("API_URL", "https://custom.example.com")
	t.Setenv("API_TOKEN", "secret-token")

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{ExpandEnv: true})
	require.NoError(t, err)

	assert.Equal(t, "https://custom.example.com", result.MCPServers["api"].URL)
	assert.Equal(t, "Bearer secret-token", result.MCPServers["api"].Headers["Authorization"])
}

func TestMergeConfigsWithEnvDefault(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{
		"mcpServers": {
			"api": {
				"type": "http",
				"url": "${MISSING_VAR:-https://default.example.com}"
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "api.json"), []byte(config), 0644))

	// Ensure env var is not set
	_ = os.Unsetenv("MISSING_VAR")

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{ExpandEnv: true})
	require.NoError(t, err)

	assert.Equal(t, "https://default.example.com", result.MCPServers["api"].URL)
}

func TestMergeConfigsWithEnvNoDefault(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{
		"mcpServers": {
			"api": {
				"type": "http",
				"url": "${MISSING_VAR_NO_DEFAULT}"
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "api.json"), []byte(config), 0644))

	// Ensure env var is not set
	_ = os.Unsetenv("MISSING_VAR_NO_DEFAULT")

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{ExpandEnv: true})
	require.NoError(t, err)

	// Should keep original if no default
	assert.Equal(t, "${MISSING_VAR_NO_DEFAULT}", result.MCPServers["api"].URL)
}

func TestMergeConfigsMissingDir(t *testing.T) {
	// Missing directories should be skipped
	result, err := MergeConfigs([]string{"/nonexistent/dir"}, MergeOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.MCPServers)
}

func TestMergeConfigsEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.MCPServers)
}

func TestMergeConfigsEmptyInputDirs(t *testing.T) {
	result, err := MergeConfigs([]string{}, MergeOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.MCPServers)
}

func TestMergeConfigsSkipsNonJsonFiles(t *testing.T) {
	tmpDir := t.TempDir()

	jsonConfig := `{"mcpServers": {"server": {"type": "http", "url": "https://example.com"}}}`
	txtContent := `This is not JSON`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(jsonConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte(txtContent), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(txtContent), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	assert.Len(t, result.MCPServers, 1)
	assert.Equal(t, "https://example.com", result.MCPServers["server"].URL)
}

func TestMergeConfigsInvalidJson(t *testing.T) {
	tmpDir := t.TempDir()

	invalidJson := `{"mcpServers": {invalid json`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "invalid.json"), []byte(invalidJson), 0644))

	_, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestMergeConfigsSkipsSubdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	mainConfig := `{"mcpServers": {"main": {"type": "http", "url": "https://main.example.com"}}}`
	subConfig := `{"mcpServers": {"sub": {"type": "http", "url": "https://sub.example.com"}}}`

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "main.json"), []byte(mainConfig), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "sub.json"), []byte(subConfig), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	// Should only include main, not sub
	assert.Len(t, result.MCPServers, 1)
	assert.Equal(t, "https://main.example.com", result.MCPServers["main"].URL)
}

func TestMergeConfigsVerboseLogging(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{"mcpServers": {"server": {"type": "http", "url": "https://example.com"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644))

	var logs []string
	opts := MergeOptions{
		Verbose: true,
		Logger: func(format string, args ...interface{}) {
			logs = append(logs, format)
		},
	}

	_, err := MergeConfigs([]string{tmpDir}, opts)
	require.NoError(t, err)

	assert.NotEmpty(t, logs)
}

func TestMergeConfigsEmptyDirInList(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{"mcpServers": {"server": {"type": "http", "url": "https://example.com"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644))

	// Include empty string in dirs list
	result, err := MergeConfigs([]string{"", tmpDir, ""}, MergeOptions{})
	require.NoError(t, err)

	assert.Len(t, result.MCPServers, 1)
}

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("TEST_VAR", "value")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple var",
			input:    "${TEST_VAR}",
			expected: "value",
		},
		{
			name:     "var with default",
			input:    "${TEST_VAR:-default}",
			expected: "value",
		},
		{
			name:     "missing var with default",
			input:    "${MISSING:-default}",
			expected: "default",
		},
		{
			name:     "missing var no default",
			input:    "${MISSING}",
			expected: "${MISSING}",
		},
		{
			name:     "var with prefix and suffix",
			input:    "prefix-${TEST_VAR}-suffix",
			expected: "prefix-value-suffix",
		},
		{
			name:     "multiple vars",
			input:    "${TEST_VAR}/${TEST_VAR}",
			expected: "value/value",
		},
		{
			name:     "empty default",
			input:    "${MISSING:-}",
			expected: "",
		},
		{
			name:     "no var pattern",
			input:    "no vars here",
			expected: "no vars here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ExpandEnvVars(tt.input))
		})
	}
}

func TestWriteConfig(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "output", "merged.json")

	config := &MCPConfig{
		MCPServers: map[string]MCPServer{
			"test": {
				Type: "http",
				URL:  "https://example.com",
			},
		},
	}

	err := WriteConfig(config, outputPath)
	require.NoError(t, err)

	// Verify file was created
	assert.FileExists(t, outputPath)

	// Verify content
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	var result MCPConfig
	require.NoError(t, json.Unmarshal(data, &result))

	assert.Len(t, result.MCPServers, 1)
	assert.Equal(t, "https://example.com", result.MCPServers["test"].URL)
}

func TestWriteConfigCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "deep", "nested", "dir", "merged.json")

	config := &MCPConfig{
		MCPServers: map[string]MCPServer{},
	}

	err := WriteConfig(config, outputPath)
	require.NoError(t, err)

	assert.FileExists(t, outputPath)
}

func TestMergeAndWrite(t *testing.T) {
	tmpDir := t.TempDir()
	inputDir := filepath.Join(tmpDir, "input")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	config := `{"mcpServers": {"server": {"type": "http", "url": "https://example.com"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "config.json"), []byte(config), 0644))

	outputPath := filepath.Join(tmpDir, "output", "merged.json")

	err := MergeAndWrite([]string{inputDir}, outputPath, MergeOptions{})
	require.NoError(t, err)

	assert.FileExists(t, outputPath)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	var result MCPConfig
	require.NoError(t, json.Unmarshal(data, &result))

	assert.Len(t, result.MCPServers, 1)
	assert.Equal(t, "https://example.com", result.MCPServers["server"].URL)
}

func TestMCPServerAllFields(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{
		"mcpServers": {
			"http-server": {
				"type": "http",
				"url": "https://example.com/mcp",
				"headers": {
					"Authorization": "Bearer token",
					"X-Custom": "value"
				}
			},
			"stdio-server": {
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "@example/server"],
				"env": {
					"API_KEY": "secret",
					"DEBUG": "true"
				}
			},
			"sse-server": {
				"type": "sse",
				"url": "https://example.com/sse",
				"headers": {
					"Authorization": "Bearer token"
				}
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	// Verify HTTP server
	httpServer := result.MCPServers["http-server"]
	assert.Equal(t, "http", httpServer.Type)
	assert.Equal(t, "https://example.com/mcp", httpServer.URL)
	assert.Equal(t, "Bearer token", httpServer.Headers["Authorization"])
	assert.Equal(t, "value", httpServer.Headers["X-Custom"])

	// Verify Stdio server
	stdioServer := result.MCPServers["stdio-server"]
	assert.Equal(t, "stdio", stdioServer.Type)
	assert.Equal(t, "npx", stdioServer.Command)
	assert.Equal(t, []string{"-y", "@example/server"}, stdioServer.Args)
	assert.Equal(t, "secret", stdioServer.Env["API_KEY"])
	assert.Equal(t, "true", stdioServer.Env["DEBUG"])

	// Verify SSE server
	sseServer := result.MCPServers["sse-server"]
	assert.Equal(t, "sse", sseServer.Type)
	assert.Equal(t, "https://example.com/sse", sseServer.URL)
	assert.Equal(t, "Bearer token", sseServer.Headers["Authorization"])
}

func TestMergeConfigsOverrideOrder(t *testing.T) {
	tmpDir := t.TempDir()
	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")
	dir3 := filepath.Join(tmpDir, "dir3")

	require.NoError(t, os.MkdirAll(dir1, 0755))
	require.NoError(t, os.MkdirAll(dir2, 0755))
	require.NoError(t, os.MkdirAll(dir3, 0755))

	// Same server name in all directories with different URLs
	config1 := `{"mcpServers": {"server": {"type": "http", "url": "https://dir1.example.com"}}}`
	config2 := `{"mcpServers": {"server": {"type": "http", "url": "https://dir2.example.com"}}}`
	config3 := `{"mcpServers": {"server": {"type": "http", "url": "https://dir3.example.com"}}}`

	require.NoError(t, os.WriteFile(filepath.Join(dir1, "config.json"), []byte(config1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "config.json"), []byte(config2), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir3, "config.json"), []byte(config3), 0644))

	// dir3 should win (last in list)
	result, err := MergeConfigs([]string{dir1, dir2, dir3}, MergeOptions{})
	require.NoError(t, err)

	assert.Equal(t, "https://dir3.example.com", result.MCPServers["server"].URL)
}
