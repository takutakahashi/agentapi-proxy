package startup

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadSettingsFile(t *testing.T) {
	t.Run("valid settings file", func(t *testing.T) {
		// Create temp file with valid settings
		tmpDir := t.TempDir()
		settingsFile := filepath.Join(tmpDir, "settings.json")
		settingsData := `{
			"name": "test-user",
			"marketplaces": {
				"test-marketplace": {
					"url": "https://github.com/example/marketplace",
					"enabled_plugins": ["plugin1", "plugin2"]
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
		if len(mp.EnabledPlugins) != 2 {
			t.Errorf("Expected 2 enabled plugins, got %d", len(mp.EnabledPlugins))
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
					URL:            "",
					EnabledPlugins: []string{"plugin1"},
				},
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
					"url": "` + marketplaceSourceDir + `",
					"enabled_plugins": ["plugin1", "plugin2"]
				}
			},
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
		if source["source"] != "local" {
			t.Errorf("Expected source.source to be 'local', got '%v'", source["source"])
		}
		expectedPath := filepath.Join(marketplacesDir, "test-mp")
		if source["path"] != expectedPath {
			t.Errorf("Expected source.path to be '%s', got '%v'", expectedPath, source["path"])
		}

		// Check enabledPlugins
		enabledPlugins, ok := result["enabledPlugins"].([]interface{})
		if !ok {
			t.Fatal("Expected enabledPlugins to exist")
		}
		if len(enabledPlugins) != 2 {
			t.Errorf("Expected 2 enabled plugins, got %d", len(enabledPlugins))
		}
		// Check plugin format: plugin@marketplace
		expectedPlugins := map[string]bool{
			"plugin1@test-mp": true,
			"plugin2@test-mp": true,
		}
		for _, p := range enabledPlugins {
			plugin, ok := p.(string)
			if !ok {
				t.Errorf("Expected plugin to be string, got %T", p)
				continue
			}
			if !expectedPlugins[plugin] {
				t.Errorf("Unexpected plugin: %s", plugin)
			}
		}
	})
}
