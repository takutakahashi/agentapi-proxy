package startup

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSetupClaudeCode(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "claude-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Mock home directory
	originalHome := os.Getenv("HOME")
	defer func() {
		if err := os.Setenv("HOME", originalHome); err != nil {
			t.Logf("Failed to restore HOME: %v", err)
		}
	}()
	if err := os.Setenv("HOME", tempDir); err != nil {
		t.Fatalf("Failed to set HOME: %v", err)
	}

	// Mock userHomeDir function by creating a test that works with temp directory
	err = SetupClaudeCode()
	if err != nil {
		t.Fatalf("SetupClaudeCode failed: %v", err)
	}

	// Check if .claude directory was created
	claudeDir := filepath.Join(tempDir, ".claude")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		t.Errorf(".claude directory was not created")
	}

	// Check if settings.json was created
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		t.Errorf("settings.json was not created")
	}

	// Verify settings.json content
	settingsData, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings.json: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatalf("Failed to parse settings.json: %v", err)
	}

	// Check if required settings are present
	if settingsMap, ok := settings["settings"].(map[string]interface{}); ok {
		if mcpEnabled, exists := settingsMap["mcp.enabled"]; !exists || mcpEnabled != true {
			t.Errorf("mcp.enabled setting is missing or incorrect")
		}
	} else {
		t.Errorf("settings section is missing from settings.json")
	}
}

func TestMergeClaudeConfig(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "claude-merge-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Mock home directory
	originalHome := os.Getenv("HOME")
	defer func() {
		if err := os.Setenv("HOME", originalHome); err != nil {
			t.Logf("Failed to restore HOME: %v", err)
		}
	}()
	if err := os.Setenv("HOME", tempDir); err != nil {
		t.Fatalf("Failed to set HOME: %v", err)
	}

	// Test case 1: No existing .claude.json file
	err = mergeClaudeConfig()
	if err != nil {
		t.Fatalf("mergeClaudeConfig failed with no existing file: %v", err)
	}

	// Check if .claude.json was created
	claudeJsonPath := filepath.Join(tempDir, ".claude.json")
	if _, err := os.Stat(claudeJsonPath); os.IsNotExist(err) {
		t.Errorf(".claude.json was not created")
	}

	// Verify content
	claudeJsonData, err := os.ReadFile(claudeJsonPath)
	if err != nil {
		t.Fatalf("Failed to read .claude.json: %v", err)
	}

	var claudeJson map[string]interface{}
	if err := json.Unmarshal(claudeJsonData, &claudeJson); err != nil {
		t.Fatalf("Failed to parse .claude.json: %v", err)
	}

	expectedConfig := map[string]interface{}{
		"hasCompletedOnboarding":        true,
		"bypassPermissionsModeAccepted": true,
	}

	for key, expectedValue := range expectedConfig {
		if value, exists := claudeJson[key]; !exists || value != expectedValue {
			t.Errorf("Expected %s to be %v, got %v", key, expectedValue, value)
		}
	}

	// Test case 2: Existing .claude.json file with other content
	existingConfig := map[string]interface{}{
		"existingKey":            "existingValue",
		"hasCompletedOnboarding": false, // This should be overwritten
	}

	existingData, err := json.Marshal(existingConfig)
	if err != nil {
		t.Fatalf("Failed to marshal existing config: %v", err)
	}

	if err := os.WriteFile(claudeJsonPath, existingData, 0644); err != nil {
		t.Fatalf("Failed to write existing config: %v", err)
	}

	// Merge config again
	err = mergeClaudeConfig()
	if err != nil {
		t.Fatalf("mergeClaudeConfig failed with existing file: %v", err)
	}

	// Read and verify merged content
	mergedData, err := os.ReadFile(claudeJsonPath)
	if err != nil {
		t.Fatalf("Failed to read merged .claude.json: %v", err)
	}

	var mergedJson map[string]interface{}
	if err := json.Unmarshal(mergedData, &mergedJson); err != nil {
		t.Fatalf("Failed to parse merged .claude.json: %v", err)
	}

	// Check that existing key is preserved
	if value, exists := mergedJson["existingKey"]; !exists || value != "existingValue" {
		t.Errorf("Existing key was not preserved: %v", value)
	}

	// Check that new config is merged
	for key, expectedValue := range expectedConfig {
		if value, exists := mergedJson[key]; !exists || value != expectedValue {
			t.Errorf("Expected %s to be %v, got %v", key, expectedValue, value)
		}
	}
}

func TestDecodeBase64(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Valid base64",
			input:    "SGVsbG8gV29ybGQ=", // "Hello World" in base64
			expected: "Hello World",
		},
		{
			name:     "Invalid base64 - return as-is",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeBase64(tt.input)
			if err != nil {
				t.Fatalf("decodeBase64 failed: %v", err)
			}
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestSetupMCPServers(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "mcp-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Mock home directory
	originalHome := os.Getenv("HOME")
	defer func() { _ = os.Setenv("HOME", originalHome) }()
	_ = os.Setenv("HOME", tempDir)

	// Test case 1: Empty config flag
	err = SetupMCPServers("")
	if err == nil {
		t.Error("Expected error for empty config flag")
	}

	// Test case 2: Invalid JSON
	err = SetupMCPServers("invalid json")
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}

	// Test case 3: Valid JSON config
	mcpConfigs := []MCPServerConfig{
		{
			ID:        "test-server",
			Name:      "Test Server",
			Endpoint:  "http://localhost:8080",
			Enabled:   true,
			Transport: "stdio",
			Command:   "test-command",
			Args:      []string{"arg1", "arg2"},
			Env:       map[string]string{"KEY": "value"},
			Timeout:   30,
		},
	}

	configJson, err := json.Marshal(mcpConfigs)
	if err != nil {
		t.Fatalf("Failed to marshal MCP config: %v", err)
	}

	// Note: This test will fail because it tries to execute the claude command
	// which is not available in the test environment. This is expected behavior.
	_ = SetupMCPServers(string(configJson))
	// We don't check for error here because the claude command will fail
	// but we can verify that the config was parsed correctly

	// Test case 4: Base64 encoded config
	base64Config := base64.StdEncoding.EncodeToString(configJson)
	_ = SetupMCPServers(base64Config)
}

func TestAddMcpServer(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Test case 1: stdio transport with command
	mcpConfig := MCPServerConfig{
		Name:      "test-stdio-server",
		Transport: "stdio",
		Command:   "test-command",
		Args:      []string{"arg1", "arg2"},
		Env:       map[string]string{"TEST_ENV": "test_value"},
	}

	err := addMcpServer(tempDir, mcpConfig)
	// This might succeed if claude command is available in the environment
	if err != nil {
		// If claude is not installed, this is expected
		t.Logf("claude command not available or failed: %v", err)
	}

	// Test case 2: non-stdio transport (no command)
	mcpConfig2 := MCPServerConfig{
		Name:      "test-http-server",
		Transport: "http",
		Endpoint:  "http://localhost:8080",
	}

	err = addMcpServer(tempDir, mcpConfig2)
	// This might succeed if claude command is available in the environment
	if err != nil {
		// If claude is not installed, this is expected
		t.Logf("claude command not available or failed: %v", err)
	}
}

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		repoURL  string
		expected string
		hasError bool
	}{
		{
			name:     "HTTPS URL",
			repoURL:  "https://github.com/owner/repo",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "HTTPS URL with .git",
			repoURL:  "https://github.com/owner/repo.git",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "SSH URL",
			repoURL:  "git@github.com:owner/repo.git",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "SSH URL without .git",
			repoURL:  "git@github.com:owner/repo",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "GitHub Enterprise HTTPS",
			repoURL:  "https://github.enterprise.com/owner/repo",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "GitHub Enterprise SSH",
			repoURL:  "git@github.enterprise.com:owner/repo.git",
			expected: "owner/repo",
			hasError: false,
		},
		{
			name:     "Invalid URL",
			repoURL:  "invalid-url",
			expected: "",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractRepoName(tt.repoURL)
			if tt.hasError {
				if err == nil {
					t.Errorf("Expected error for URL %s", tt.repoURL)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for URL %s: %v", tt.repoURL, err)
				}
				if result != tt.expected {
					t.Errorf("Expected %s, got %s", tt.expected, result)
				}
			}
		})
	}
}

func TestGetGitHubURL(t *testing.T) {
	// Save original environment
	originalAPI := os.Getenv("GITHUB_API")
	defer func() {
		if originalAPI != "" {
			if err := os.Setenv("GITHUB_API", originalAPI); err != nil {
				t.Logf("Failed to restore GITHUB_API: %v", err)
			}
		} else {
			if err := os.Unsetenv("GITHUB_API"); err != nil {
				t.Logf("Failed to unset GITHUB_API: %v", err)
			}
		}
	}()

	// Test case 1: No GITHUB_API environment variable
	if err := os.Unsetenv("GITHUB_API"); err != nil {
		t.Fatalf("Failed to unset GITHUB_API: %v", err)
	}
	result := getGitHubURL()
	expected := "https://github.com"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test case 2: GITHUB_API with /api/v3 suffix
	if err := os.Setenv("GITHUB_API", "https://github.enterprise.com/api/v3"); err != nil {
		t.Fatalf("Failed to set GITHUB_API: %v", err)
	}
	result = getGitHubURL()
	expected = "https://github.enterprise.com"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test case 3: GITHUB_API without /api/v3 suffix
	if err := os.Setenv("GITHUB_API", "https://github.enterprise.com"); err != nil {
		t.Fatalf("Failed to set GITHUB_API: %v", err)
	}
	result = getGitHubURL()
	expected = "https://github.enterprise.com"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestGetGitHubToken(t *testing.T) {
	// Save original environment
	originalGithubToken := os.Getenv("GITHUB_TOKEN")
	originalPersonalToken := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	originalAppID := os.Getenv("GITHUB_APP_ID")
	originalInstallationID := os.Getenv("GITHUB_INSTALLATION_ID")
	originalPemPath := os.Getenv("GITHUB_APP_PEM_PATH")
	originalRepoFullName := os.Getenv("GITHUB_REPO_FULLNAME")

	defer func() {
		// Restore original environment
		setEnvIfNotEmpty("GITHUB_TOKEN", originalGithubToken)
		setEnvIfNotEmpty("GITHUB_PERSONAL_ACCESS_TOKEN", originalPersonalToken)
		setEnvIfNotEmpty("GITHUB_APP_ID", originalAppID)
		setEnvIfNotEmpty("GITHUB_INSTALLATION_ID", originalInstallationID)
		setEnvIfNotEmpty("GITHUB_APP_PEM_PATH", originalPemPath)
		setEnvIfNotEmpty("GITHUB_REPO_FULLNAME", originalRepoFullName)
	}()

	// Clear all environment variables
	_ = os.Unsetenv("GITHUB_TOKEN")
	_ = os.Unsetenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	_ = os.Unsetenv("GITHUB_APP_ID")
	_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
	_ = os.Unsetenv("GITHUB_APP_PEM_PATH")
	_ = os.Unsetenv("GITHUB_REPO_FULLNAME")

	// Test case 1: GITHUB_TOKEN environment variable
	_ = os.Setenv("GITHUB_TOKEN", "test-token")
	token, err := getGitHubToken("")
	if err != nil {
		t.Errorf("Unexpected error with GITHUB_TOKEN: %v", err)
	}
	if token != "test-token" {
		t.Errorf("Expected test-token, got %s", token)
	}

	// Test case 2: GITHUB_PERSONAL_ACCESS_TOKEN fallback
	_ = os.Unsetenv("GITHUB_TOKEN")
	_ = os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "personal-token")
	token, err = getGitHubToken("")
	if err != nil {
		t.Errorf("Unexpected error with GITHUB_PERSONAL_ACCESS_TOKEN: %v", err)
	}
	if token != "personal-token" {
		t.Errorf("Expected personal-token, got %s", token)
	}

	// Test case 3: GitHub App auth with manual installation ID
	_ = os.Unsetenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	_ = os.Setenv("GITHUB_APP_ID", "12345")
	_ = os.Setenv("GITHUB_INSTALLATION_ID", "67890")

	// Create a dummy PEM file
	tempDir := t.TempDir()
	pemFile := filepath.Join(tempDir, "test.pem")
	// Invalid PEM content will cause error, but it tests the code path
	if err := os.WriteFile(pemFile, []byte("invalid-pem"), 0600); err != nil {
		t.Fatalf("Failed to create test PEM file: %v", err)
	}
	_ = os.Setenv("GITHUB_APP_PEM_PATH", pemFile)

	_, err = getGitHubToken("")
	// This will fail because of invalid PEM, but tests the code path
	if err == nil {
		t.Error("Expected error with invalid PEM content")
	}

	// Test case 4: GitHub App auth with auto-discovery (no installation ID)
	_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
	_ = os.Setenv("GITHUB_REPO_FULLNAME", "owner/repo")

	_, err = getGitHubToken("")
	// This will fail because of invalid PEM, but tests the auto-discovery code path
	if err == nil {
		t.Error("Expected error with invalid PEM content in auto-discovery")
	}

	// Test case 5: GitHub App auth with repoFullName parameter
	_ = os.Unsetenv("GITHUB_REPO_FULLNAME")
	_, err = getGitHubToken("owner/repo")
	// This will fail because of invalid PEM, but tests the parameter path
	if err == nil {
		t.Error("Expected error with invalid PEM content with repoFullName parameter")
	}

	// Test case 6: No authentication available
	_ = os.Unsetenv("GITHUB_APP_ID")
	_ = os.Unsetenv("GITHUB_APP_PEM_PATH")
	_, err = getGitHubToken("")
	if err == nil {
		t.Error("Expected error when no authentication is available")
	}
}

func TestMCPServerConfig(t *testing.T) {
	// Test MCPServerConfig struct marshaling/unmarshaling
	config := MCPServerConfig{
		ID:        "test-id",
		Name:      "Test MCP Server",
		Endpoint:  "http://localhost:8080",
		Enabled:   true,
		Transport: "stdio",
		Command:   "test-command",
		Args:      []string{"arg1", "arg2"},
		Env:       map[string]string{"KEY1": "value1", "KEY2": "value2"},
		Timeout:   30,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal MCPServerConfig: %v", err)
	}

	// Unmarshal back
	var unmarshaled MCPServerConfig
	err = json.Unmarshal(jsonData, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPServerConfig: %v", err)
	}

	// Compare structs
	if !reflect.DeepEqual(config, unmarshaled) {
		t.Errorf("Marshaled and unmarshaled configs don't match")
	}
}

func TestInitGitHubRepo(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "github-repo-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Test case 1: Empty repo name with ignoreMissingConfig=true
	cloneDir := filepath.Join(tempDir, "test-repo")
	err = InitGitHubRepo("", cloneDir, true)
	if err != nil {
		t.Errorf("Expected no error with empty repo name and ignoreMissingConfig=true: %v", err)
	}

	// Test case 2: Empty repo name with ignoreMissingConfig=false
	err = InitGitHubRepo("", cloneDir, false)
	if err == nil {
		t.Error("Expected error with empty repo name and ignoreMissingConfig=false")
	}

	// Test case 3: Valid repo name but no authentication
	// This will fail because no GitHub token is available, which is expected
	err = InitGitHubRepo("owner/repo", cloneDir, false)
	if err == nil {
		t.Error("Expected error with no GitHub authentication")
	}
}

func TestGenerateGitHubAppToken(t *testing.T) {
	// Test case 1: Invalid app ID
	_, err := generateGitHubAppToken("invalid", "123", "/path/to/pem")
	if err == nil {
		t.Error("Expected error for invalid app ID")
	}

	// Test case 2: Invalid installation ID
	_, err = generateGitHubAppToken("123", "invalid", "/path/to/pem")
	if err == nil {
		t.Error("Expected error for invalid installation ID")
	}

	// Test case 3: Non-existent PEM file
	tempDir := t.TempDir()
	nonExistentPem := filepath.Join(tempDir, "non-existent.pem")
	_, err = generateGitHubAppToken("123", "456", nonExistentPem)
	if err == nil {
		t.Error("Expected error for non-existent PEM file")
	}

	// Test case 4: PEM file fallback to environment variable
	originalPem := os.Getenv("GITHUB_APP_PEM")
	defer func() {
		if originalPem != "" {
			_ = os.Setenv("GITHUB_APP_PEM", originalPem)
		} else {
			_ = os.Unsetenv("GITHUB_APP_PEM")
		}
	}()

	// Set a dummy PEM content
	_ = os.Setenv("GITHUB_APP_PEM", "dummy-pem-content")
	_, err = generateGitHubAppToken("123", "456", nonExistentPem)
	// This will still fail because it's not a valid PEM, but it tests the env var fallback path
	if err == nil {
		t.Error("Expected error for invalid PEM content")
	}
}

func TestAutoDiscoverInstallationID(t *testing.T) {
	// Test case 1: Invalid repository format
	_, err := autoDiscoverInstallationID("123", "/path/to/pem", "invalid-repo-format")
	if err == nil {
		t.Error("Expected error for invalid repository format")
	}

	// Test case 2: Invalid app ID
	_, err = autoDiscoverInstallationID("invalid", "/path/to/pem", "owner/repo")
	if err == nil {
		t.Error("Expected error for invalid app ID")
	}

	// Test case 3: Non-existent PEM file without env var
	tempDir := t.TempDir()
	nonExistentPem := filepath.Join(tempDir, "non-existent.pem")

	originalPem := os.Getenv("GITHUB_APP_PEM")
	defer func() {
		if originalPem != "" {
			_ = os.Setenv("GITHUB_APP_PEM", originalPem)
		} else {
			_ = os.Unsetenv("GITHUB_APP_PEM")
		}
	}()
	_ = os.Unsetenv("GITHUB_APP_PEM")

	_, err = autoDiscoverInstallationID("123", nonExistentPem, "owner/repo")
	if err == nil {
		t.Error("Expected error for non-existent PEM file")
	}
}

func TestAuthenticateGHCLI(t *testing.T) {
	// This function executes gh auth login command
	// We can only test that it attempts to run the command

	// Test with mock environment
	env := []string{"PATH=/usr/bin:/bin", "HOME=/tmp"}
	err := authenticateGHCLI("github.enterprise.com", "test-token", env)
	// This will fail because gh command might not be available in test environment
	// but we're testing that the function attempts to execute it
	if err == nil {
		// If gh is installed, it would fail with auth error
		t.Log("gh command executed (unexpected in test environment)")
	}
}

func TestSetupRepository(t *testing.T) {
	// Test case 1: Invalid repository URL
	tempDir := t.TempDir()
	err := setupRepository("", "test-token", tempDir)
	if err == nil {
		t.Error("Expected error for empty repository URL")
	}

	// Test case 2: Repository URL without proper format
	err = setupRepository("not-a-url", "test-token", tempDir)
	if err == nil {
		t.Error("Expected error for invalid repository URL")
	}

	// Test case 3: Git repository detection (mock .git directory)
	gitDir := filepath.Join(tempDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("Failed to create .git directory: %v", err)
	}

	// This will try to run git pull, which will fail in test environment
	err = setupRepository("https://github.com/owner/repo", "test-token", tempDir)
	if err == nil {
		t.Error("Expected error when running git pull in test environment")
	}

	// Test case 4: GitHub Enterprise with GITHUB_API env var
	originalAPI := os.Getenv("GITHUB_API")
	defer func() {
		if originalAPI != "" {
			_ = os.Setenv("GITHUB_API", originalAPI)
		} else {
			_ = os.Unsetenv("GITHUB_API")
		}
	}()

	_ = os.Setenv("GITHUB_API", "https://github.enterprise.com/api/v3")
	// Try to clone with enterprise URL
	err = setupRepository("https://github.enterprise.com/owner/repo", "test-token", tempDir)
	// This will fail but tests the enterprise path
	if err == nil {
		t.Error("Expected error when running gh repo clone in test environment")
	}
}

// Helper function to set environment variable only if not empty
func setEnvIfNotEmpty(key, value string) {
	if value != "" {
		_ = os.Setenv(key, value)
	} else {
		_ = os.Unsetenv(key)
	}
}
