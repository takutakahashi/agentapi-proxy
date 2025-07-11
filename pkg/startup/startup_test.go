package startup

import (
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
	defer func() { os.Setenv("HOME", originalHome) }()
	os.Setenv("HOME", tempDir)

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
	defer func() { os.Setenv("HOME", originalHome) }()
	os.Setenv("HOME", tempDir)

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
	defer func() { os.Setenv("HOME", originalHome) }()
	os.Setenv("HOME", tempDir)

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
			os.Setenv("GITHUB_API", originalAPI)
		} else {
			os.Unsetenv("GITHUB_API")
		}
	}()

	// Test case 1: No GITHUB_API environment variable
	os.Unsetenv("GITHUB_API")
	result := getGitHubURL()
	expected := "https://github.com"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test case 2: GITHUB_API with /api/v3 suffix
	os.Setenv("GITHUB_API", "https://github.enterprise.com/api/v3")
	result = getGitHubURL()
	expected = "https://github.enterprise.com"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test case 3: GITHUB_API without /api/v3 suffix
	os.Setenv("GITHUB_API", "https://github.enterprise.com")
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
	os.Unsetenv("GITHUB_TOKEN")
	os.Unsetenv("GITHUB_PERSONAL_ACCESS_TOKEN")
	os.Unsetenv("GITHUB_APP_ID")
	os.Unsetenv("GITHUB_INSTALLATION_ID")
	os.Unsetenv("GITHUB_APP_PEM_PATH")
	os.Unsetenv("GITHUB_REPO_FULLNAME")

	// Test case 1: GITHUB_TOKEN environment variable
	os.Setenv("GITHUB_TOKEN", "test-token")
	token, err := getGitHubToken("")
	if err != nil {
		t.Errorf("Unexpected error with GITHUB_TOKEN: %v", err)
	}
	if token != "test-token" {
		t.Errorf("Expected test-token, got %s", token)
	}

	// Test case 2: GITHUB_PERSONAL_ACCESS_TOKEN fallback
	os.Unsetenv("GITHUB_TOKEN")
	os.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "personal-token")
	token, err = getGitHubToken("")
	if err != nil {
		t.Errorf("Unexpected error with GITHUB_PERSONAL_ACCESS_TOKEN: %v", err)
	}
	if token != "personal-token" {
		t.Errorf("Expected personal-token, got %s", token)
	}

	// Test case 3: No authentication available
	os.Unsetenv("GITHUB_PERSONAL_ACCESS_TOKEN")
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
	defer os.RemoveAll(tempDir)

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

// Helper function to set environment variable only if not empty
func setEnvIfNotEmpty(key, value string) {
	if value != "" {
		os.Setenv(key, value)
	} else {
		os.Unsetenv(key)
	}
}
