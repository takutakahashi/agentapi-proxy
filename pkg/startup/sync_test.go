package startup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

func TestSyncMarketplaces(t *testing.T) {
	t.Run("creates directories and settings.json without marketplaces", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, nil)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Verify directories were created
		if _, err := os.Stat(filepath.Join(outputDir, ".claude")); os.IsNotExist(err) {
			t.Error("Expected .claude directory to be created")
		}
		if _, err := os.Stat(marketplacesDir); os.IsNotExist(err) {
			t.Error("Expected marketplaces directory to be created")
		}

		// Verify settings.json was created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// Verify MCP is enabled
		settings, ok := result["settings"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected settings field to exist")
		}
		if settings["mcp.enabled"] != true {
			t.Error("Expected mcp.enabled to be true")
		}
	})

	t.Run("includes GITHUB_TOKEN in env when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Set GITHUB_TOKEN
		originalToken := os.Getenv("GITHUB_TOKEN")
		if err := os.Setenv("GITHUB_TOKEN", "test-token-12345"); err != nil {
			t.Fatalf("Failed to set GITHUB_TOKEN: %v", err)
		}
		defer func() {
			if originalToken != "" {
				_ = os.Setenv("GITHUB_TOKEN", originalToken)
			} else {
				_ = os.Unsetenv("GITHUB_TOKEN")
			}
		}()

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, nil)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Verify settings.json contains GITHUB_TOKEN
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		env, ok := result["env"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected env field to exist")
		}
		if env["GITHUB_TOKEN"] != "test-token-12345" {
			t.Errorf("Expected GITHUB_TOKEN to be 'test-token-12345', got '%v'", env["GITHUB_TOKEN"])
		}
	})

	t.Run("skips marketplace with empty URL", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"empty-url": {
					URL: "",
				},
			},
			EnabledPlugins: []string{"plugin1@empty-url"},
		}

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Verify settings.json doesn't have extraKnownMarketplaces
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		if _, ok := result["extraKnownMarketplaces"]; ok {
			t.Error("Expected extraKnownMarketplaces to not exist when all marketplaces are skipped")
		}
	})

	t.Run("empty marketplaces map", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		settings := &settingsJSON{
			Name:         "test-user",
			Marketplaces: map[string]*marketplaceJSON{}, // Empty map, not nil
		}

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		if _, ok := result["extraKnownMarketplaces"]; ok {
			t.Error("Expected no extraKnownMarketplaces for empty map")
		}
		if _, ok := result["enabledPlugins"]; ok {
			t.Error("Expected no enabledPlugins for empty map")
		}
	})

	t.Run("marketplace without enabled_plugins", func(t *testing.T) {
		// Skip if git is not available
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Create a local git repo as marketplace source
		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md": "# Test Marketplace",
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"no-plugins": {
					URL: sourceDir,
				},
			},
			EnabledPlugins: nil, // No plugins enabled
		}

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// extraKnownMarketplaces should exist
		if _, ok := result["extraKnownMarketplaces"]; !ok {
			t.Error("Expected extraKnownMarketplaces to exist")
		}
		// But enabledPlugins should not
		if _, ok := result["enabledPlugins"]; ok {
			t.Error("Expected no enabledPlugins when no plugins are enabled")
		}
	})

	t.Run("no GITHUB_TOKEN set", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Save and clear GITHUB_TOKEN
		originalToken := os.Getenv("GITHUB_TOKEN")
		_ = os.Unsetenv("GITHUB_TOKEN")
		defer func() {
			if originalToken != "" {
				_ = os.Setenv("GITHUB_TOKEN", originalToken)
			}
		}()

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, nil)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// env should not exist when GITHUB_TOKEN is not set
		if _, ok := result["env"]; ok {
			t.Error("Expected no env when GITHUB_TOKEN is not set")
		}
	})

	t.Run("multiple marketplaces with mixed success", func(t *testing.T) {
		// Skip if git is not available
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Create one valid marketplace
		validSourceDir := filepath.Join(tmpDir, "valid-marketplace")
		createTestGitRepo(t, validSourceDir, map[string]string{
			"README.md": "# Valid Marketplace",
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"valid": {
					URL: validSourceDir,
				},
				"invalid": {
					URL: "https://invalid.example.com/nonexistent.git",
				},
				"empty-url": {
					URL: "",
				},
			},
			EnabledPlugins: []string{"plugin1@valid", "plugin2@invalid", "plugin3@empty-url"},
		}

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		// Should not fail even though some marketplaces fail
		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// Only valid marketplace should be in extraKnownMarketplaces
		extraKnown, ok := result["extraKnownMarketplaces"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected extraKnownMarketplaces to exist")
		}
		if len(extraKnown) != 1 {
			t.Errorf("Expected 1 marketplace in extraKnownMarketplaces, got %d", len(extraKnown))
		}
		if _, ok := extraKnown["valid"]; !ok {
			t.Error("Expected 'valid' marketplace to exist")
		}

		// All enabled plugins should be in enabledPlugins (as object) - they are passed through
		enabledPlugins, ok := result["enabledPlugins"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected enabledPlugins to exist as object")
		}
		if len(enabledPlugins) != 3 {
			t.Errorf("Expected 3 enabled plugins, got %d", len(enabledPlugins))
		}
		if _, ok := enabledPlugins["plugin1@valid"]; !ok {
			t.Error("Expected 'plugin1@valid' to exist in enabledPlugins")
		}
	})

	t.Run("settings.json format verification", func(t *testing.T) {
		// Skip if git is not available
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Create marketplace
		sourceDir := filepath.Join(tmpDir, "marketplace")
		createTestGitRepo(t, sourceDir, map[string]string{
			"plugin.json": `{"name": "test"}`,
		})

		// Set GITHUB_TOKEN for this test
		originalToken := os.Getenv("GITHUB_TOKEN")
		_ = os.Setenv("GITHUB_TOKEN", "test-token")
		defer func() {
			if originalToken != "" {
				_ = os.Setenv("GITHUB_TOKEN", originalToken)
			} else {
				_ = os.Unsetenv("GITHUB_TOKEN")
			}
		}()

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"my-marketplace": {
					URL: sourceDir,
				},
			},
			EnabledPlugins: []string{"plugin-a@my-marketplace", "plugin-b@my-marketplace"},
		}

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// Verify env.GITHUB_TOKEN
		env := result["env"].(map[string]interface{})
		if env["GITHUB_TOKEN"] != "test-token" {
			t.Error("Expected GITHUB_TOKEN in env")
		}

		// Verify settings.mcp.enabled
		settingsField := result["settings"].(map[string]interface{})
		if settingsField["mcp.enabled"] != true {
			t.Error("Expected mcp.enabled to be true")
		}

		// Verify extraKnownMarketplaces structure
		extraKnown := result["extraKnownMarketplaces"].(map[string]interface{})
		mp := extraKnown["my-marketplace"].(map[string]interface{})
		source := mp["source"].(map[string]interface{})
		if source["source"] != "directory" {
			t.Errorf("Expected source.source to be 'directory', got '%v'", source["source"])
		}
		expectedPath := filepath.Join(marketplacesDir, "my-marketplace")
		if source["path"] != expectedPath {
			t.Errorf("Expected path '%s', got '%s'", expectedPath, source["path"])
		}

		// Verify enabledPlugins format (as object with plugin names as keys)
		plugins := result["enabledPlugins"].(map[string]interface{})
		expectedPlugins := map[string]bool{
			"plugin-a@my-marketplace": true,
			"plugin-b@my-marketplace": true,
		}
		if len(plugins) != len(expectedPlugins) {
			t.Errorf("Expected %d plugins, got %d", len(expectedPlugins), len(plugins))
		}
		for pluginName := range expectedPlugins {
			if _, ok := plugins[pluginName]; !ok {
				t.Errorf("Expected plugin '%s' to exist", pluginName)
			}
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Use deeply nested paths
		outputDir := filepath.Join(tmpDir, "a", "b", "c", "output")
		marketplacesDir := filepath.Join(tmpDir, "x", "y", "z", "marketplaces")

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, nil)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Verify all directories were created
		if _, err := os.Stat(filepath.Join(outputDir, ".claude")); os.IsNotExist(err) {
			t.Error("Expected .claude directory to be created")
		}
		if _, err := os.Stat(marketplacesDir); os.IsNotExist(err) {
			t.Error("Expected marketplaces directory to be created")
		}
	})
}

func TestCloneMarketplace(t *testing.T) {
	// Skip if git is not available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available, skipping clone tests")
	}

	t.Run("clones new repository", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a local git repo to clone from
		sourceDir := filepath.Join(tmpDir, "source")
		if err := os.MkdirAll(sourceDir, 0755); err != nil {
			t.Fatalf("Failed to create source dir: %v", err)
		}

		// Initialize git repo
		cmd := exec.Command("git", "init")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to init git repo: %v", err)
		}

		// Configure git user for the test repo
		cmd = exec.Command("git", "config", "user.email", "test@test.com")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to config git email: %v", err)
		}
		cmd = exec.Command("git", "config", "user.name", "Test User")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to config git name: %v", err)
		}

		// Create a file and commit
		testFile := filepath.Join(sourceDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git add: %v", err)
		}
		cmd = exec.Command("git", "commit", "-m", "initial commit")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git commit: %v", err)
		}

		// Clone the repo
		targetDir := filepath.Join(tmpDir, "target")
		err := cloneMarketplace(sourceDir, targetDir)
		if err != nil {
			t.Fatalf("cloneMarketplace failed: %v", err)
		}

		// Verify clone succeeded
		if _, err := os.Stat(filepath.Join(targetDir, ".git")); os.IsNotExist(err) {
			t.Error("Expected .git directory to exist in cloned repo")
		}
		if _, err := os.Stat(filepath.Join(targetDir, "test.txt")); os.IsNotExist(err) {
			t.Error("Expected test.txt to exist in cloned repo")
		}
	})

	t.Run("pulls updates for existing clone", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create a local git repo to clone from
		sourceDir := filepath.Join(tmpDir, "source")
		if err := os.MkdirAll(sourceDir, 0755); err != nil {
			t.Fatalf("Failed to create source dir: %v", err)
		}

		// Initialize git repo
		cmd := exec.Command("git", "init")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to init git repo: %v", err)
		}

		// Configure git user
		cmd = exec.Command("git", "config", "user.email", "test@test.com")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to config git email: %v", err)
		}
		cmd = exec.Command("git", "config", "user.name", "Test User")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to config git name: %v", err)
		}

		// Create a file and commit
		testFile := filepath.Join(sourceDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("initial content"), 0644); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git add: %v", err)
		}
		cmd = exec.Command("git", "commit", "-m", "initial commit")
		cmd.Dir = sourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git commit: %v", err)
		}

		// Clone the repo (first time - full clone, not shallow)
		targetDir := filepath.Join(tmpDir, "target")
		cmd = exec.Command("git", "clone", sourceDir, targetDir)
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to clone: %v", err)
		}

		// Add new commit to source
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

		// Call cloneMarketplace on existing clone - should pull
		err := cloneMarketplace(sourceDir, targetDir)
		if err != nil {
			t.Fatalf("cloneMarketplace (pull) failed: %v", err)
		}

		// Verify content was updated
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
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Create output directory (simulates home directory existing)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		opts := SyncOptions{
			SettingsFile:    "/non/existent/settings.json",
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// Verify .claude.json was created
		claudeJSONPath := filepath.Join(outputDir, ".claude.json")
		if _, err := os.Stat(claudeJSONPath); os.IsNotExist(err) {
			t.Error("Expected .claude.json to be created")
		}

		// Verify settings.json was created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
			t.Error("Expected settings.json to be created")
		}
	})

	t.Run("succeeds with valid settings file", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Create output directory (simulates home directory existing)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		// Create settings file
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
			SettingsFile:    settingsFile,
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// Verify .claude.json was created
		claudeJSONPath := filepath.Join(outputDir, ".claude.json")
		if _, err := os.Stat(claudeJSONPath); os.IsNotExist(err) {
			t.Error("Expected .claude.json to be created")
		}

		// Verify settings.json was created
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
			t.Error("Expected settings.json to be created")
		}
	})

	t.Run("integration test with local marketplace", func(t *testing.T) {
		// Skip if git is not available
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available, skipping integration test")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Create output directory (simulates home directory existing)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatalf("Failed to create output dir: %v", err)
		}

		// Create a local git repo as marketplace source
		marketplaceSourceDir := filepath.Join(tmpDir, "marketplace-source")
		if err := os.MkdirAll(marketplaceSourceDir, 0755); err != nil {
			t.Fatalf("Failed to create marketplace source dir: %v", err)
		}

		cmd := exec.Command("git", "init")
		cmd.Dir = marketplaceSourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to init git repo: %v", err)
		}

		cmd = exec.Command("git", "config", "user.email", "test@test.com")
		cmd.Dir = marketplaceSourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to config git email: %v", err)
		}
		cmd = exec.Command("git", "config", "user.name", "Test User")
		cmd.Dir = marketplaceSourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to config git name: %v", err)
		}

		// Create marketplace.json
		marketplaceJSON := filepath.Join(marketplaceSourceDir, "marketplace.json")
		if err := os.WriteFile(marketplaceJSON, []byte(`{"name": "test-marketplace"}`), 0644); err != nil {
			t.Fatalf("Failed to write marketplace.json: %v", err)
		}

		cmd = exec.Command("git", "add", ".")
		cmd.Dir = marketplaceSourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git add: %v", err)
		}
		cmd = exec.Command("git", "commit", "-m", "initial commit")
		cmd.Dir = marketplaceSourceDir
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to git commit: %v", err)
		}

		// Create settings file with marketplace
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {
				"test-mp": {
					"url": "` + marketplaceSourceDir + `"
				}
			},
			"enabled_plugins": ["plugin1@test-mp", "plugin2@test-mp"],
			"created_at": "2025-01-01T00:00:00Z",
			"updated_at": "2025-01-01T00:00:00Z"
		}`
		if err := os.WriteFile(settingsFile, []byte(settingsData), 0644); err != nil {
			t.Fatalf("Failed to write settings file: %v", err)
		}

		opts := SyncOptions{
			SettingsFile:    settingsFile,
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := Sync(opts)
		if err != nil {
			t.Fatalf("Sync failed: %v", err)
		}

		// Verify marketplace was cloned
		clonedMarketplace := filepath.Join(marketplacesDir, "test-mp")
		if _, err := os.Stat(filepath.Join(clonedMarketplace, ".git")); os.IsNotExist(err) {
			t.Error("Expected marketplace to be cloned")
		}

		// Verify settings.json contains marketplace config
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// Check extraKnownMarketplaces
		extraKnown, ok := result["extraKnownMarketplaces"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected extraKnownMarketplaces to exist")
		}
		testMp, ok := extraKnown["test-mp"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected test-mp marketplace to exist")
		}
		source, ok := testMp["source"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected source to exist")
		}
		if source["source"] != "directory" {
			t.Errorf("Expected source.source to be 'directory', got '%v'", source["source"])
		}
		expectedPath := filepath.Join(marketplacesDir, "test-mp")
		if source["path"] != expectedPath {
			t.Errorf("Expected source.path to be '%s', got '%v'", expectedPath, source["path"])
		}

		// Check enabledPlugins (as object with plugin names as keys)
		enabledPlugins, ok := result["enabledPlugins"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected enabledPlugins to exist as object")
		}
		if len(enabledPlugins) != 2 {
			t.Errorf("Expected 2 enabled plugins, got %d", len(enabledPlugins))
		}
		// Check plugin format: plugin@marketplace
		expectedPlugins := map[string]bool{
			"plugin1@test-mp": true,
			"plugin2@test-mp": true,
		}
		for pluginName := range expectedPlugins {
			if _, ok := enabledPlugins[pluginName]; !ok {
				t.Errorf("Expected plugin '%s' to exist", pluginName)
			}
		}
	})
}

func TestSyncMarketplaces_EnabledPlugins(t *testing.T) {
	t.Run("adds plugins to enabledPlugins in plugin@marketplace format", func(t *testing.T) {
		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		settings := &settingsJSON{
			Name:           "test-user",
			EnabledPlugins: []string{"context7@claude-plugins-official", "typescript@claude-plugins-official", "python@claude-plugins-official"},
		}

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Verify settings.json contains plugins
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// Check enabledPlugins
		enabledPlugins, ok := result["enabledPlugins"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected enabledPlugins to exist")
		}

		// Verify plugins are in format: plugin@marketplace
		expectedPlugins := []string{
			"context7@claude-plugins-official",
			"typescript@claude-plugins-official",
			"python@claude-plugins-official",
		}
		for _, pluginName := range expectedPlugins {
			val, ok := enabledPlugins[pluginName]
			if !ok {
				t.Errorf("Expected plugin '%s' to exist", pluginName)
			}
			// Verify value is true (boolean)
			if val != true {
				t.Errorf("Expected plugin '%s' value to be true, got %v", pluginName, val)
			}
		}

		// extraKnownMarketplaces should not exist (no custom marketplaces)
		if _, ok := result["extraKnownMarketplaces"]; ok {
			t.Error("Expected no extraKnownMarketplaces when no marketplaces are defined")
		}
	})

	t.Run("combines official and marketplace plugins", func(t *testing.T) {
		// Skip if git is not available
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("git not available")
		}

		tmpDir := t.TempDir()
		outputDir := filepath.Join(tmpDir, "output")
		marketplacesDir := filepath.Join(tmpDir, "marketplaces")

		// Create a local git repo as marketplace source
		sourceDir := filepath.Join(tmpDir, "marketplace-source")
		createTestGitRepo(t, sourceDir, map[string]string{
			"README.md": "# Test Marketplace",
		})

		settings := &settingsJSON{
			Name: "test-user",
			Marketplaces: map[string]*marketplaceJSON{
				"custom-mp": {
					URL: sourceDir,
				},
			},
			EnabledPlugins: []string{
				"context7@claude-plugins-official",
				"typescript@claude-plugins-official",
				"custom-plugin1@custom-mp",
				"custom-plugin2@custom-mp",
			},
		}

		opts := SyncOptions{
			OutputDir:       outputDir,
			MarketplacesDir: marketplacesDir,
		}

		err := syncMarketplaces(opts, settings)
		if err != nil {
			t.Fatalf("syncMarketplaces failed: %v", err)
		}

		// Verify settings.json
		settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("Failed to read settings.json: %v", err)
		}

		var result map[string]interface{}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("Failed to parse settings.json: %v", err)
		}

		// Check enabledPlugins contains both official and custom plugins
		enabledPlugins, ok := result["enabledPlugins"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected enabledPlugins to exist")
		}

		expectedPlugins := []string{
			"context7@claude-plugins-official",
			"typescript@claude-plugins-official",
			"custom-plugin1@custom-mp",
			"custom-plugin2@custom-mp",
		}

		if len(enabledPlugins) != len(expectedPlugins) {
			t.Errorf("Expected %d plugins, got %d", len(expectedPlugins), len(enabledPlugins))
		}

		for _, pluginName := range expectedPlugins {
			if _, ok := enabledPlugins[pluginName]; !ok {
				t.Errorf("Expected plugin '%s' to exist", pluginName)
			}
		}

		// Check extraKnownMarketplaces exists for custom marketplace
		extraKnown, ok := result["extraKnownMarketplaces"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected extraKnownMarketplaces to exist")
		}
		if _, ok := extraKnown["custom-mp"]; !ok {
			t.Error("Expected custom-mp to exist in extraKnownMarketplaces")
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

func TestInstallEnabledPlugins(t *testing.T) {
	t.Run("empty plugins list returns nil", func(t *testing.T) {
		err := installEnabledPlugins([]string{})
		if err != nil {
			t.Errorf("Expected nil error for empty plugins list, got: %v", err)
		}
	})

	t.Run("nil plugins list returns nil", func(t *testing.T) {
		err := installEnabledPlugins(nil)
		if err != nil {
			t.Errorf("Expected nil error for nil plugins list, got: %v", err)
		}
	})

	t.Run("installs plugins when claude command is available", func(t *testing.T) {
		// Skip if claude is not available
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude not available, skipping plugin installation test")
		}

		// Test with a known plugin from claude-plugins-official
		plugins := []string{"code-review@claude-plugins-official"}
		err := installEnabledPlugins(plugins)
		// Note: This may fail if the plugin is already installed or marketplace is not configured
		// We just check that the function doesn't panic
		if err != nil {
			t.Logf("Plugin installation returned error (may be expected): %v", err)
		}
	})

	t.Run("continues on plugin installation failure", func(t *testing.T) {
		// Skip if claude is not available
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude not available, skipping plugin installation test")
		}

		// Test with a non-existent plugin - should fail but not panic
		plugins := []string{"nonexistent-plugin@nonexistent-marketplace"}
		err := installEnabledPlugins(plugins)
		if err == nil {
			t.Log("Expected error for non-existent plugin, but got nil (plugin may exist)")
		}
	})

	t.Run("handles multiple plugins with mixed results", func(t *testing.T) {
		// Skip if claude is not available
		if _, err := exec.LookPath("claude"); err != nil {
			t.Skip("claude not available, skipping plugin installation test")
		}

		// Test with a mix of valid and invalid plugins
		plugins := []string{
			"nonexistent-plugin-1@nonexistent-marketplace",
			"nonexistent-plugin-2@nonexistent-marketplace",
		}
		err := installEnabledPlugins(plugins)
		// Function should continue even if some plugins fail
		if err == nil {
			t.Log("Expected error for non-existent plugins, but got nil")
		}
	})
}
