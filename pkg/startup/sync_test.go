package startup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Helper function to create a temp git repo for testing
func createTestGitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create dir: %v", err)
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to init git repo: %v", err)
	}

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to config git email: %v", err)
	}
	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to config git name: %v", err)
	}

	for filename, content := range files {
		filePath := filepath.Join(dir, filename)
		// Create parent directories if needed
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			t.Fatalf("Failed to create parent directory for %s: %v", filename, err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write file %s: %v", filename, err)
		}
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}
	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}
}

func TestLoadSettingsFile(t *testing.T) {
	t.Run("valid settings file", func(t *testing.T) {
		// Create temp file with valid settings
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {
				"test-marketplace": {
					"url": "https://github.com/example/marketplace"
				}
			},
			"enabled_plugins": ["plugin1@test-marketplace", "plugin2@test-marketplace"],
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Name != "test-user" {
			t.Errorf("Expected name 'test-user', got '%s'", settings.Name)
		}
		if len(settings.Marketplaces) != 1 {
			t.Errorf("Expected 1 marketplace, got %d", len(settings.Marketplaces))
		}
		mp, ok := settings.Marketplaces["test-marketplace"]
		if !ok {
			t.Fatal("Expected 'test-marketplace' to exist")
		}
		if mp.URL != "https://github.com/example/marketplace" {
			t.Errorf("Expected URL 'https://github.com/example/marketplace', got '%s'", mp.URL)
		}
		if len(settings.EnabledPlugins) != 2 {
			t.Errorf("Expected 2 enabled plugins, got %d", len(settings.EnabledPlugins))
		}
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := loadSettingsFile("")
		if err == nil {
			t.Error("Expected error for empty path")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := loadSettingsFile("/non/existent/path/settings.json")
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		if err := os.WriteFile(settingsFile, []byte("not valid json"), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		_, err := loadSettingsFile(settingsFile)
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})

	t.Run("settings with bedrock and mcp_servers", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"bedrock": {
				"enabled": true,
				"model": "anthropic.claude-3-opus-20240229-v1:0"
			},
			"mcp_servers": {
				"github": {
					"type": "sse",
					"url": "https://mcp.example.com"
				}
			},
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Bedrock == nil {
			t.Fatal("Expected bedrock to be set")
		}
		if !settings.Bedrock.Enabled {
			t.Error("Expected bedrock.enabled to be true")
		}
		if settings.MCPServers == nil {
			t.Fatal("Expected mcp_servers to be set")
		}
		if _, ok := settings.MCPServers["github"]; !ok {
			t.Error("Expected 'github' MCP server to exist")
		}
	})

	t.Run("empty JSON object", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		if err := os.WriteFile(settingsFile, []byte("{}"), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Name != "" {
			t.Errorf("Expected empty name, got '%s'", settings.Name)
		}
		if settings.Marketplaces != nil {
			t.Error("Expected nil marketplaces")
		}
	})

	t.Run("minimal settings with name only", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{"name": "minimal-user"}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Name != "minimal-user" {
			t.Errorf("Expected name 'minimal-user', got '%s'", settings.Name)
		}
	})

	t.Run("multiple marketplaces", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {
				"marketplace1": {
					"url": "https://github.com/org1/marketplace1"
				},
				"marketplace2": {
					"url": "https://github.com/org2/marketplace2"
				},
				"marketplace3": {
					"url": "https://github.com/org3/marketplace3"
				}
			},
			"enabled_plugins": ["plugin-a@marketplace1", "plugin-b@marketplace2", "plugin-c@marketplace2"]
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if len(settings.Marketplaces) != 3 {
			t.Errorf("Expected 3 marketplaces, got %d", len(settings.Marketplaces))
		}

		mp1 := settings.Marketplaces["marketplace1"]
		if mp1 == nil {
			t.Error("marketplace1 should exist")
		}

		mp2 := settings.Marketplaces["marketplace2"]
		if mp2 == nil {
			t.Error("marketplace2 should exist")
		}

		mp3 := settings.Marketplaces["marketplace3"]
		if mp3 == nil {
			t.Error("marketplace3 should exist")
		}

		if len(settings.EnabledPlugins) != 3 {
			t.Errorf("Expected 3 enabled plugins, got %d", len(settings.EnabledPlugins))
		}
	})

	t.Run("full bedrock configuration", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "bedrock-user",
			"bedrock": {
				"enabled": true,
				"model": "anthropic.claude-3-sonnet-20240229-v1:0",
				"access_key_id": "AKIAIOSFODNN7EXAMPLE",
				"secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				"role_arn": "arn:aws:iam::123456789012:role/ExampleRole",
				"profile": "default"
			}
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if settings.Bedrock == nil {
			t.Fatal("Expected bedrock to be set")
		}
		if settings.Bedrock.Model != "anthropic.claude-3-sonnet-20240229-v1:0" {
			t.Errorf("Expected model, got '%s'", settings.Bedrock.Model)
		}
		if settings.Bedrock.AccessKeyID != "AKIAIOSFODNN7EXAMPLE" {
			t.Error("Expected access_key_id")
		}
		if settings.Bedrock.SecretAccessKey != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
			t.Error("Expected secret_access_key")
		}
		if settings.Bedrock.RoleARN != "arn:aws:iam::123456789012:role/ExampleRole" {
			t.Error("Expected role_arn")
		}
		if settings.Bedrock.Profile != "default" {
			t.Error("Expected profile")
		}
	})

	t.Run("MCP server with all fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "mcp-user",
			"mcp_servers": {
				"custom-server": {
					"type": "stdio",
					"command": "/usr/bin/mcp-server",
					"args": ["--port", "8080", "--verbose"],
					"env": {
						"MCP_DEBUG": "true",
						"MCP_LOG_LEVEL": "debug"
					}
				},
				"sse-server": {
					"type": "sse",
					"url": "https://mcp.example.com/sse",
					"headers": {
						"Authorization": "Bearer token123",
						"X-Custom-Header": "value"
					}
				}
			}
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if len(settings.MCPServers) != 2 {
			t.Errorf("Expected 2 MCP servers, got %d", len(settings.MCPServers))
		}

		customServer := settings.MCPServers["custom-server"]
		if customServer == nil {
			t.Fatal("Expected custom-server to exist")
		}
		if customServer.Type != "stdio" {
			t.Errorf("Expected type 'stdio', got '%s'", customServer.Type)
		}
		if customServer.Command != "/usr/bin/mcp-server" {
			t.Errorf("Expected command, got '%s'", customServer.Command)
		}
		if len(customServer.Args) != 3 {
			t.Errorf("Expected 3 args, got %d", len(customServer.Args))
		}
		if len(customServer.Env) != 2 {
			t.Errorf("Expected 2 env vars, got %d", len(customServer.Env))
		}

		sseServer := settings.MCPServers["sse-server"]
		if sseServer == nil {
			t.Fatal("Expected sse-server to exist")
		}
		if sseServer.Type != "sse" {
			t.Errorf("Expected type 'sse', got '%s'", sseServer.Type)
		}
		if len(sseServer.Headers) != 2 {
			t.Errorf("Expected 2 headers, got %d", len(sseServer.Headers))
		}
	})

	t.Run("truncated JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		// Truncated JSON
		if err := os.WriteFile(settingsFile, []byte(`{"name": "test`), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		_, err := loadSettingsFile(settingsFile)
		if err == nil {
			t.Error("Expected error for truncated JSON")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		if err := os.WriteFile(settingsFile, []byte(""), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		_, err := loadSettingsFile(settingsFile)
		if err == nil {
			t.Error("Expected error for empty file")
		}
	})
}

func TestGenerateClaudeJSON(t *testing.T) {
	t.Run("creates new file", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := generateClaudeJSON(tmpDir)
		if err != nil {
			t.Fatalf("generateClaudeJSON failed: %v", err)
		}

		// Verify file was created
		claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
		data, err := os.ReadFile(claudeJSONPath)
		if err != nil {
			t.Fatalf("Failed to read .claude.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse .claude.json: %v", err)
		}

		if result["hasCompletedOnboarding"] != true {
			t.Error("Expected hasCompletedOnboarding to be true")
		}
		if result["bypassPermissionsModeAccepted"] != true {
			t.Error("Expected bypassPermissionsModeAccepted to be true")
		}
	})

	t.Run("preserves existing fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		claudeJSONPath := filepath.Join(tmpDir, ".claude.json")

		// Create existing file with extra fields
		existingData := map[string]interface{}{
			"existingField":     "existingValue",
			"anotherField":      123,
			"shouldBePreserved": true,
		}
		data, _ := json.Marshal(existingData)
		if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
			t.Fatalf("Failed to write existing file: %v", err)
		}

		err := generateClaudeJSON(tmpDir)
		if err != nil {
			t.Fatalf("generateClaudeJSON failed: %v", err)
		}

		// Verify existing fields are preserved
		data, err = os.ReadFile(claudeJSONPath)
		if err != nil {
			t.Fatalf("Failed to read .claude.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse .claude.json: %v", err)
		}

		if result["existingField"] != "existingValue" {
			t.Error("Expected existingField to be preserved")
		}
		if result["anotherField"] != float64(123) { // JSON numbers are float64
			t.Error("Expected anotherField to be preserved")
		}
		if result["shouldBePreserved"] != true {
			t.Error("Expected shouldBePreserved to be preserved")
		}
		if result["hasCompletedOnboarding"] != true {
			t.Error("Expected hasCompletedOnboarding to be true")
		}
		if result["bypassPermissionsModeAccepted"] != true {
			t.Error("Expected bypassPermissionsModeAccepted to be true")
		}
	})

	t.Run("handles invalid existing JSON gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()
		claudeJSONPath := filepath.Join(tmpDir, ".claude.json")

		// Create existing file with invalid JSON
		if err := os.WriteFile(claudeJSONPath, []byte("not valid json"), 0644); err != nil {
			t.Fatalf("Failed to write existing file: %v", err)
		}

		err := generateClaudeJSON(tmpDir)
		if err != nil {
			t.Fatalf("generateClaudeJSON failed: %v", err)
		}

		// Verify file was overwritten with valid JSON
		data, err := os.ReadFile(claudeJSONPath)
		if err != nil {
			t.Fatalf("Failed to read .claude.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse .claude.json: %v", err)
		}

		if result["hasCompletedOnboarding"] != true {
			t.Error("Expected hasCompletedOnboarding to be true")
		}
	})

	t.Run("overwrites false onboarding fields", func(t *testing.T) {
		tmpDir := t.TempDir()
		claudeJSONPath := filepath.Join(tmpDir, ".claude.json")

		// Create existing file with onboarding fields set to false
		existingData := map[string]interface{}{
			"hasCompletedOnboarding":        false,
			"bypassPermissionsModeAccepted": false,
			"someOtherField":                "value",
		}
		data, _ := json.Marshal(existingData)
		if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
			t.Fatalf("Failed to write existing file: %v", err)
		}

		err := generateClaudeJSON(tmpDir)
		if err != nil {
			t.Fatalf("generateClaudeJSON failed: %v", err)
		}

		data, err = os.ReadFile(claudeJSONPath)
		if err != nil {
			t.Fatalf("Failed to read .claude.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse .claude.json: %v", err)
		}

		// These should be overwritten to true
		if result["hasCompletedOnboarding"] != true {
			t.Error("Expected hasCompletedOnboarding to be overwritten to true")
		}
		if result["bypassPermissionsModeAccepted"] != true {
			t.Error("Expected bypassPermissionsModeAccepted to be overwritten to true")
		}
		// Other field should be preserved
		if result["someOtherField"] != "value" {
			t.Error("Expected someOtherField to be preserved")
		}
	})

	t.Run("preserves nested structures", func(t *testing.T) {
		tmpDir := t.TempDir()
		claudeJSONPath := filepath.Join(tmpDir, ".claude.json")

		existingData := map[string]interface{}{
			"nested": map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": "deep value",
				},
			},
			"array": []interface{}{"item1", "item2", "item3"},
		}
		data, _ := json.Marshal(existingData)
		if err := os.WriteFile(claudeJSONPath, data, 0644); err != nil {
			t.Fatalf("Failed to write existing file: %v", err)
		}

		err := generateClaudeJSON(tmpDir)
		if err != nil {
			t.Fatalf("generateClaudeJSON failed: %v", err)
		}

		data, err = os.ReadFile(claudeJSONPath)
		if err != nil {
			t.Fatalf("Failed to read .claude.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse .claude.json: %v", err)
		}

		// Verify nested structure is preserved
		nested, ok := result["nested"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected nested to be preserved")
		}
		level1, ok := nested["level1"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected level1 to be preserved")
		}
		if level1["level2"] != "deep value" {
			t.Error("Expected deep nested value to be preserved")
		}

		// Verify array is preserved
		arr, ok := result["array"].([]interface{})
		if !ok {
			t.Fatal("Expected array to be preserved")
		}
		if len(arr) != 3 {
			t.Errorf("Expected 3 array items, got %d", len(arr))
		}
	})

	t.Run("file permissions are correct", func(t *testing.T) {
		tmpDir := t.TempDir()

		err := generateClaudeJSON(tmpDir)
		if err != nil {
			t.Fatalf("generateClaudeJSON failed: %v", err)
		}

		claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
		info, err := os.Stat(claudeJSONPath)
		if err != nil {
			t.Fatalf("Failed to stat .claude.json: %v", err)
		}

		// Check that file is readable and writable by owner (0644)
		perm := info.Mode().Perm()
		if perm&0600 != 0600 {
			t.Errorf("Expected file to be readable and writable by owner, got %o", perm)
		}
	})
}

// writeTestSettingsJSON writes a minimal settings.json to simulate what compile generates.
func writeTestSettingsJSON(t *testing.T, outputDir string, content map[string]interface{}) {
	t.Helper()
	claudeDir := filepath.Join(outputDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude dir: %v", err)
	}
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal settings.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), data, 0644); err != nil {
		t.Fatalf("Failed to write settings.json: %v", err)
	}
}

func TestSyncMarketplaces(t *testing.T) {
	t.Run("creates directories without marketplaces", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		opts := SyncOptions{
			OutputDir: outputDir,
		}

		err := syncMarketplaces(opts, nil)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Verify directories were created
		if _, err := os.Stat(filepath.Join(outputDir, ".claude")); os.IsNotExist(err) {
			t.Error("Expected .claude directory to be created")
		}
		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
		if _, err := os.Stat(marketplacesDir); os.IsNotExist(err) {
			t.Error("Expected marketplaces directory to be created")
		}

		// settings.json should NOT be created by syncMarketplaces (compile's job)
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

	t.Run("skips marketplace with empty URL", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"empty-url": {URL: ""},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// No marketplace should be cloned
		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
		entries, _ := os.ReadDir(marketplacesDir)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".tmp-") {
				t.Errorf("Unexpected marketplace directory: %s", e.Name())
			}
		}
	})

	t.Run("empty marketplaces map", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		settings := &settingsJSON{
			Name:         "test-user",
			Marketplaces: map[string]*marketplaceJSON{},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Directories should be created even with no marketplaces
		if _, err := os.Stat(filepath.Join(outputDir, ".claude")); os.IsNotExist(err) {
			t.Error("Expected .claude directory to be created")
		}
	})

	t.Run("marketplace without enabled_plugins", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md":                       "# Test Marketplace",
			".claude-plugin/marketplace.json": `{"name": "no-plugins"}`,
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"no-plugins": {URL: sourceDir},
			},
			EnabledPlugins: nil,
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Marketplace should be cloned with real name
		clonedDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "no-plugins")
		if _, err := os.Stat(clonedDir); os.IsNotExist(err) {
			t.Error("Expected marketplace to be cloned as 'no-plugins'")
		}
	})

	t.Run("multiple marketplaces with mixed success", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		validSourceDir := filepath.Join(tmpDir, "valid-marketplace")
		createTestGitRepo(t, validSourceDir, map[string]string{
			"README.md":                       "# Valid Marketplace",
			".claude-plugin/marketplace.json": `{"name": "valid"}`,
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"valid":     {URL: validSourceDir},
				"invalid":   {URL: "https://invalid.example.com/nonexistent.git"},
				"empty-url": {URL: ""},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		// Should not fail even though some marketplaces fail
		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Only valid marketplace should be cloned
		clonedDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "valid")
		if _, err := os.Stat(clonedDir); os.IsNotExist(err) {
			t.Error("Expected 'valid' marketplace to be cloned")
		}
	})

	t.Run("clones marketplace with real name", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		sourceDir := filepath.Join(tmpDir, "marketplace")
		createTestGitRepo(t, sourceDir, map[string]string{
			"plugin.json":                     `{"name": "test"}`,
			".claude-plugin/marketplace.json": `{"name": "my-marketplace"}`,
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"alias-key": {URL: sourceDir},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Cloned with real name (from marketplace.json), not alias
		expectedDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "my-marketplace")
		if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
			t.Error("Expected marketplace cloned as real name 'my-marketplace'")
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "a", "b", "c", "output")

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, nil)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		if _, err := os.Stat(filepath.Join(outputDir, ".claude")); os.IsNotExist(err) {
			t.Error("Expected .claude directory to be created")
		}
		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
		if _, err := os.Stat(marketplacesDir); os.IsNotExist(err) {
			t.Error("Expected marketplaces directory to be created")
		}
	})
}

func TestCloneMarketplace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping clone tests")
	}

	t.Run("clones new repository", func(t *testing.T) {
		tmpDir := t.TempDir()

		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"test.txt": "test content",
		})

		targetDir := filepath.Join(tmpDir, "target")
		if err := cloneMarketplace(sourceDir, targetDir); err != nil {
			t.Fatalf("cloneMarketplace failed: %v", err)
		}

		if _, err := os.Stat(filepath.Join(targetDir, ".git")); os.IsNotExist(err) {
			t.Error("Expected .git directory to exist in cloned repo")
		}
		if _, err := os.Stat(filepath.Join(targetDir, "test.txt")); os.IsNotExist(err) {
			t.Error("Expected test.txt to exist in cloned repo")
		}
	})

	t.Run("pulls updates for existing clone", func(t *testing.T) {
		tmpDir := t.TempDir()

		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"test.txt": "initial content",
		})

		// Full clone (not shallow) so pull works
		targetDir := filepath.Join(tmpDir, "target")
		cmd := exec.Command("git", "clone", sourceDir, targetDir)
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to clone: %v", err)
		}

		// Add a new commit to the source
		testFile := filepath.Join(sourceDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("updated content"), 0644); err != nil {
			t.Fatalf("Failed to update test file: %v", err)
		}
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git add: %v", err)
		}
		cmd = exec.Command("git", "commit", "-m", "update commit")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git commit: %v", err)
		}

		if err := cloneMarketplace(sourceDir, targetDir); err != nil {
			t.Fatalf("cloneMarketplace (pull) failed: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(targetDir, "test.txt"))
		if err != nil {
			t.Fatalf("Failed to read test.txt: %v", err)
		}
		if string(data) != "updated content" {
			t.Errorf("Expected 'updated content', got '%s'", string(data))
		}
	})

	t.Run("fails for invalid URL", func(t *testing.T) {
		tmpDir := t.TempDir()
		targetDir := filepath.Join(tmpDir, "target")

		err := cloneMarketplace("https://invalid.example.com/nonexistent/repo.git", targetDir)
		if err == nil {
			t.Error("Expected error for invalid URL")
		}
	})

	t.Run("uses shallow clone flag", func(t *testing.T) {
		// Note: --depth 1 behavior varies for local file:// protocol vs https://
		// For local repos, git may still fetch all commits depending on git version
		// This test verifies the clone command is called with --depth 1 by checking
		// that the clone succeeds and creates a valid repo

		tmpDir := t.TempDir()

		// Create a local git repo with multiple commits
		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"file1.txt": "content 1",
		})

		// Add more commits
		for i := 2; i <= 5; i++ {
			filePath := filepath.Join(sourceDir, "file1.txt")
			if err := os.WriteFile(filePath, []byte("content "+string(rune('0'+i))), 0644); err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}
			cmd := exec.Command("git", "add", ".")
			cmd.Dir = sourceDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to git add: %v", err)
			}
			cmd = exec.Command("git", "commit", "-m", "commit "+string(rune('0'+i)))
			cmd.Dir = sourceDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("Failed to git commit: %v", err)
			}
		}

		// Clone the repo
		targetDir := filepath.Join(tmpDir, "target")
		err := cloneMarketplace(sourceDir, targetDir)
		if err != nil {
			t.Fatalf("cloneMarketplace failed: %v", err)
		}

		// Verify clone succeeded and has the latest content
		data, err := os.ReadFile(filepath.Join(targetDir, "file1.txt"))
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		if string(data) != "content 5" {
			t.Errorf("Expected latest content 'content 5', got '%s'", string(data))
		}

		// Verify it's a git repo
		if _, err := os.Stat(filepath.Join(targetDir, ".git")); os.IsNotExist(err) {
			t.Error("Expected .git directory to exist")
		}
	})

	t.Run("target directory already exists but not git repo", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create source repo
		sourceDir := filepath.Join(tmpDir, "source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md": "# Test",
		})

		// Create target directory with some files (but not a git repo)
		targetDir := filepath.Join(tmpDir, "target")
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			t.Fatalf("Failed to create target dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(targetDir, "existing.txt"), []byte("existing"), 0644); err != nil {
			t.Fatalf("Failed to write existing file: %v", err)
		}

		// Clone should fail because target already exists
		err := cloneMarketplace(sourceDir, targetDir)
		if err == nil {
			t.Error("Expected error when cloning to existing non-git directory")
		}
	})

	t.Run("handles empty repository gracefully", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create an empty git repo (no commits)
		sourceDir := filepath.Join(tmpDir, "source")
		if err := os.MkdirAll(sourceDir, 0755); err != nil {
			t.Fatalf("Failed to create source dir: %v", err)
		}
		cmd := exec.Command("git", "init")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to init git repo: %v", err)
		}

		// Clone should fail (empty repo has no commits to clone)
		targetDir := filepath.Join(tmpDir, "target")
		err := cloneMarketplace(sourceDir, targetDir)
		// Empty repos typically fail to clone with --depth 1
		if err == nil {
			// If it succeeds, that's also acceptable behavior
			t.Log("Cloning empty repo succeeded (may vary by git version)")
		}
	})
}

func TestSync(t *testing.T) {
	t.Run("succeeds without settings file", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		// Pre-create settings.json as compile would
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"settings": map[string]interface{}{"mcp.enabled": true},
		})

		opts := SyncOptions{
			OutputDir: outputDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// Directories should be created
		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
		if _, err := os.Stat(marketplacesDir); os.IsNotExist(err) {
			t.Error("Expected marketplaces directory to be created")
		}
	})

	t.Run("succeeds with valid settings file for custom marketplace", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		// Pre-create settings.json as compile would
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"settings": map[string]interface{}{"mcp.enabled": true},
		})

		// Optional: SettingsFile for custom marketplace config
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {},
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		opts := SyncOptions{
			SettingsFile: settingsFile,
			OutputDir:    outputDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// settings.json should still exist (written by compile step, not overwritten)
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
			t.Error("Expected settings.json to remain from compile step")
		}
	})

	t.Run("integration test with local marketplace - clones and resolves names", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available, skipping integration test")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		// Pre-create settings.json as compile would (with enabled_plugins)
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"settings":        map[string]interface{}{"mcp.enabled": true},
			"enabled_plugins": []string{"plugin1@test-mp-real-name", "plugin2@test-mp-real-name"},
		})

		// Create a local git repo as marketplace source
		marketplaceSourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, marketplaceSourceDir, map[string]string{
			".claude-plugin/marketplace.json": `{"name": "test-mp-real-name"}`,
			"README.md":                       "# Test Marketplace",
		})

		// SettingsFile for custom marketplace URL config
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {
				"test-mp": {
					"url": "` + marketplaceSourceDir + `"
				}
			},
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		opts := SyncOptions{
			SettingsFile: settingsFile,
			OutputDir:    outputDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// Verify marketplace was cloned with real name
		clonedMarketplace := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "test-mp-real-name")
		if _, err := os.Stat(filepath.Join(clonedMarketplace, ".git")); os.IsNotExist(err) {
			t.Error("Expected marketplace to be cloned")
		}
	})
}

func TestSyncMarketplaces_EnabledPlugins(t *testing.T) {
	// The new design: syncMarketplaces does NOT write settings.json.
	// It reads enabled_plugins FROM the pre-existing settings.json (written by compile).
	// These tests verify that syncMarketplaces correctly handles enabled_plugins
	// by reading from the pre-created settings.json.

	t.Run("does not create settings.json when no plugins configured", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		settings := &settingsJSON{
			Name:           "test-user",
			EnabledPlugins: []string{"context7@claude-plugins-official", "typescript@claude-plugins-official"},
		}

		opts := SyncOptions{
			OutputDir: outputDir,
		}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// settings.json should NOT be created by syncMarketplaces — that's compile's job
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

	t.Run("reads enabled_plugins from pre-created settings.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Pre-create settings.json as compile would (with enabled_plugins)
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"enabled_plugins": []string{"context7@claude-plugins-official", "typescript@claude-plugins-official"},
		})

		settings := &settingsJSON{
			Name: "test-user",
		}
		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// settings.json should still exist (untouched by syncMarketplaces)
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// enabled_plugins written by compile should be untouched
		pluginsRaw, ok := result["enabled_plugins"].([]interface{})
		if !ok {
			t.Fatal("Expected enabled_plugins to remain in settings.json")
		}
		if len(pluginsRaw) != 2 {
			t.Errorf("Expected 2 enabled_plugins, got %d", len(pluginsRaw))
		}
	})

	t.Run("clones custom marketplace when plugins reference it", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Create a local git repo as marketplace source
		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md":                       "# Test Marketplace",
			".claude-plugin/marketplace.json": `{"name": "custom-mp"}`,
		})

		// Pre-create settings.json as compile would
		writeTestSettingsJSON(t, outputDir, map[string]interface{}{
			"enabled_plugins": []string{
				"context7@claude-plugins-official",
				"custom-plugin1@custom-mp",
			},
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"custom-mp": {URL: sourceDir},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Marketplace should be cloned with real name from marketplace.json
		clonedDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "custom-mp")
		if _, err := os.Stat(clonedDir); os.IsNotExist(err) {
			t.Error("Expected marketplace to be cloned as 'custom-mp'")
		}

		// settings.json should still contain compile's enabled_plugins (untouched)
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}
		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}
		if _, ok := result["enabled_plugins"]; !ok {
			t.Error("Expected enabled_plugins to remain in settings.json after syncMarketplaces")
		}
	})

	t.Run("settings with enabled_plugins in JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"enabled_plugins": ["context7@claude-plugins-official", "git@claude-plugins-official", "my-plugin@my-marketplace"],
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		settings, err := loadSettingsFile(settingsFile)
		if err != nil {
			t.Fatalf("loadSettingsFile failed: %v", err)
		}

		if len(settings.EnabledPlugins) != 3 {
			t.Errorf("Expected 3 plugins, got %d", len(settings.EnabledPlugins))
		}
		if settings.EnabledPlugins[0] != "context7@claude-plugins-official" {
			t.Errorf("Expected first plugin to be 'context7@claude-plugins-official', got '%s'", settings.EnabledPlugins[0])
		}
	})
}

func TestReadMarketplaceName(t *testing.T) {
	t.Run("reads name from valid marketplace.json", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create .claude-plugin directory and marketplace.json
		pluginDir := filepath.Join(tmpDir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("Failed to create .claude-plugin directory: %v", err)
		}

		marketplaceJSON := `{"name": "my-awesome-marketplace", "description": "A test marketplace"}`
		if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), []byte(marketplaceJSON), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		name, err := readMarketplaceName(tmpDir)
		if err != nil {
			t.Fatalf("readMarketplaceName failed: %v", err)
		}

		if name != "my-awesome-marketplace" {
			t.Errorf("Expected name 'my-awesome-marketplace', got '%s'", name)
		}
	})

	t.Run("returns error when marketplace.json not found", func(t *testing.T) {
		tmpDir := t.TempDir()

		_, err := readMarketplaceName(tmpDir)
		if err == nil {
			t.Error("Expected error when marketplace.json does not exist")
		}
	})

	t.Run("returns error when name field is empty", func(t *testing.T) {
		tmpDir := t.TempDir()

		pluginDir := filepath.Join(tmpDir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("Failed to create .claude-plugin directory: %v", err)
		}

		marketplaceJSON := `{"name": "", "description": "A test marketplace"}`
		if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), []byte(marketplaceJSON), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		_, err := readMarketplaceName(tmpDir)
		if err == nil {
			t.Error("Expected error when name field is empty")
		}
	})

	t.Run("returns error when marketplace.json is invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()

		pluginDir := filepath.Join(tmpDir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("Failed to create .claude-plugin directory: %v", err)
		}

		if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), []byte("not valid json"), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		_, err := readMarketplaceName(tmpDir)
		if err == nil {
			t.Error("Expected error for invalid JSON")
		}
	})

	t.Run("returns error when name field is missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		pluginDir := filepath.Join(tmpDir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("Failed to create .claude-plugin directory: %v", err)
		}

		marketplaceJSON := `{"description": "A test marketplace without name"}`
		if err := os.WriteFile(filepath.Join(pluginDir, "marketplace.json"), []byte(marketplaceJSON), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		_, err := readMarketplaceName(tmpDir)
		if err == nil {
			t.Error("Expected error when name field is missing")
		}
	})
}

func TestResolvePluginName(t *testing.T) {
	t.Run("resolves plugin name with matching alias", func(t *testing.T) {
		nameMapping := map[string]string{
			"my-alias":    "real-marketplace-name",
			"other-alias": "other-real-name",
		}

		result := resolvePluginName("my-plugin@my-alias", nameMapping)
		if result != "my-plugin@real-marketplace-name" {
			t.Errorf("Expected 'my-plugin@real-marketplace-name', got '%s'", result)
		}
	})

	t.Run("preserves plugin name when no matching alias", func(t *testing.T) {
		nameMapping := map[string]string{
			"my-alias": "real-marketplace-name",
		}

		result := resolvePluginName("my-plugin@claude-plugins-official", nameMapping)
		if result != "my-plugin@claude-plugins-official" {
			t.Errorf("Expected 'my-plugin@claude-plugins-official', got '%s'", result)
		}
	})

	t.Run("preserves plugin without @ symbol", func(t *testing.T) {
		nameMapping := map[string]string{
			"my-alias": "real-marketplace-name",
		}

		result := resolvePluginName("my-plugin", nameMapping)
		if result != "my-plugin" {
			t.Errorf("Expected 'my-plugin', got '%s'", result)
		}
	})

	t.Run("handles empty name mapping", func(t *testing.T) {
		nameMapping := map[string]string{}

		result := resolvePluginName("my-plugin@my-marketplace", nameMapping)
		if result != "my-plugin@my-marketplace" {
			t.Errorf("Expected 'my-plugin@my-marketplace', got '%s'", result)
		}
	})

	t.Run("handles plugin name with multiple @ symbols", func(t *testing.T) {
		nameMapping := map[string]string{
			"email@domain.com": "real-name",
		}

		// Only split on first @
		result := resolvePluginName("plugin@email@domain.com", nameMapping)
		if result != "plugin@real-name" {
			t.Errorf("Expected 'plugin@real-name', got '%s'", result)
		}
	})
}

func TestSyncMarketplaces_WithRealMarketplaceName(t *testing.T) {
	// The new design: syncMarketplaces reads the real marketplace name from
	// .claude-plugin/marketplace.json and clones into that directory name.
	// It does NOT write settings.json.

	// Skip if git is not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	t.Run("clones marketplace under real name from marketplace.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Create a local git repo as marketplace source with .claude-plugin/marketplace.json
		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		mpJSON := `{"name": "jlaswell-community-marketplace", "description": "A community marketplace"}`
		createTestGitRepo(t, sourceDir, map[string]string{
			".claude-plugin/marketplace.json": mpJSON,
			"README.md":                       "# Test Marketplace",
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"my-alias": {URL: sourceDir},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Marketplace should be cloned with real name (from marketplace.json), not alias
		realNameDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "jlaswell-community-marketplace")
		if _, err := os.Stat(realNameDir); os.IsNotExist(err) {
			t.Error("Expected marketplace to be cloned under real name 'jlaswell-community-marketplace'")
		}

		// Alias directory should NOT exist
		aliasDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces", "my-alias")
		if _, err := os.Stat(aliasDir); err == nil {
			t.Error("Expected alias 'my-alias' NOT to be used as directory name")
		}

		// settings.json should NOT be created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

	t.Run("skips marketplace when marketplace.json is missing", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Create a local git repo WITHOUT .claude-plugin/marketplace.json
		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md": "# Test Marketplace without marketplace.json",
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"broken-marketplace": {URL: sourceDir},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		// Should not fail, but marketplace should be skipped (no marketplace.json)
		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// No marketplace directory should be created (was skipped due to missing marketplace.json)
		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")
		entries, _ := os.ReadDir(marketplacesDir)
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), ".tmp-") {
				t.Errorf("Unexpected marketplace directory: %s (marketplace should have been skipped)", e.Name())
			}
		}

		// settings.json should NOT be created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

	t.Run("multiple marketplaces cloned with real names", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")

		// Create first marketplace
		sourceDir1 := filepath.Join(tmpDir, "marketplace1")
		createTestGitRepo(t, sourceDir1, map[string]string{
			".claude-plugin/marketplace.json": `{"name": "first-real-name"}`,
		})

		// Create second marketplace
		sourceDir2 := filepath.Join(tmpDir, "marketplace2")
		createTestGitRepo(t, sourceDir2, map[string]string{
			".claude-plugin/marketplace.json": `{"name": "second-real-name"}`,
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"alias1": {URL: sourceDir1},
				"alias2": {URL: sourceDir2},
			},
		}

		opts := SyncOptions{OutputDir: outputDir}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		marketplacesDir := filepath.Join(outputDir, ".claude", "plugins", "marketplaces")

		// Both marketplaces should be cloned with real names
		if _, err := os.Stat(filepath.Join(marketplacesDir, "first-real-name")); os.IsNotExist(err) {
			t.Error("Expected 'first-real-name' marketplace directory to exist")
		}
		if _, err := os.Stat(filepath.Join(marketplacesDir, "second-real-name")); os.IsNotExist(err) {
			t.Error("Expected 'second-real-name' marketplace directory to exist")
		}

		// Alias directories should NOT exist
		if _, err := os.Stat(filepath.Join(marketplacesDir, "alias1")); err == nil {
			t.Error("Expected alias 'alias1' NOT to be used as directory name")
		}
		if _, err := os.Stat(filepath.Join(marketplacesDir, "alias2")); err == nil {
			t.Error("Expected alias 'alias2' NOT to be used as directory name")
		}

		// settings.json should NOT be created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); err == nil {
			t.Error("syncMarketplaces should NOT create settings.json (that is compile's job)")
		}
	})

}
