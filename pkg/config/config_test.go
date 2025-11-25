package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// cleanupEnvironmentVars removes all AGENTAPI_ environment variables and returns a function to restore them
func cleanupEnvironmentVars() func() {
	var envVarsToRestore []string

	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "AGENTAPI_") {
			envVarsToRestore = append(envVarsToRestore, env)
			keyValue := strings.SplitN(env, "=", 2)
			if len(keyValue) == 2 {
				_ = os.Unsetenv(keyValue[0])
			}
		}
	}

	return func() {
		// Restore environment variables
		for _, env := range envVarsToRestore {
			keyValue := strings.SplitN(env, "=", 2)
			if len(keyValue) == 2 {
				_ = os.Setenv(keyValue[0], keyValue[1])
			}
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	if config.StartPort == 0 {
		t.Error("StartPort should not be zero")
	}

	expectedStartPort := 9000
	if config.StartPort != expectedStartPort {
		t.Errorf("Expected StartPort to be %d, got %d", expectedStartPort, config.StartPort)
	}

	// Test default EnableMultipleUsers value
	if config.EnableMultipleUsers != false {
		t.Errorf("Expected EnableMultipleUsers to be false, got %t", config.EnableMultipleUsers)
	}
}

func TestLoadConfig(t *testing.T) {
	// Clean up environment variables for this test
	restore := cleanupEnvironmentVars()
	defer restore()

	// Create a temporary config file
	tempConfig := &Config{
		StartPort: 8000,
	}

	configData, err := json.Marshal(tempConfig)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Write to temporary file
	tmpfile, err := os.CreateTemp("", "config*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.Write(configData); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Expected config after loading (with defaults applied)
	expectedConfig := &Config{
		StartPort: 8000,
		Auth: AuthConfig{
			Enabled: false,
			Static: &StaticAuthConfig{
				Enabled:    false,
				HeaderName: "X-API-Key",
				APIKeys:    []APIKey{},
				KeysFile:   "",
			},
			GitHub: &GitHubAuthConfig{
				Enabled:     false,
				BaseURL:     "https://api.github.com",
				TokenHeader: "Authorization",
				UserMapping: GitHubUserMapping{
					DefaultRole:        "",
					DefaultPermissions: []string{},
					TeamRoleMapping:    map[string]TeamRoleRule{},
				},
				OAuth: &GitHubOAuthConfig{
					ClientID:     "",
					ClientSecret: "",
					Scope:        "read:user read:org",
					BaseURL:      "",
				},
			},
		},
		Persistence: PersistenceConfig{
			Enabled:               false,
			Backend:               "file",
			FilePath:              "./sessions.json",
			SyncInterval:          30,
			EncryptSecrets:        true,
			SessionRecoveryMaxAge: 24,
		},
		EnableMultipleUsers: false, // Default value
	}

	// Compare basic fields
	if loadedConfig.StartPort != expectedConfig.StartPort {
		t.Errorf("StartPort mismatch: expected %d, got %d", expectedConfig.StartPort, loadedConfig.StartPort)
	}
	if loadedConfig.EnableMultipleUsers != expectedConfig.EnableMultipleUsers {
		t.Errorf("EnableMultipleUsers mismatch: expected %t, got %t", expectedConfig.EnableMultipleUsers, loadedConfig.EnableMultipleUsers)
	}

	// Compare persistence config
	if loadedConfig.Persistence != expectedConfig.Persistence {
		t.Errorf("Persistence config mismatch.\nExpected: %+v\nGot: %+v", expectedConfig.Persistence, loadedConfig.Persistence)
	}

	// Compare auth config values (not pointer equality)
	if loadedConfig.Auth.Enabled != expectedConfig.Auth.Enabled {
		t.Errorf("Auth.Enabled mismatch: expected %t, got %t", expectedConfig.Auth.Enabled, loadedConfig.Auth.Enabled)
	}

	// Verify static auth config is properly initialized with defaults
	if loadedConfig.Auth.Static == nil {
		t.Error("Auth.Static should not be nil")
	} else {
		if loadedConfig.Auth.Static.HeaderName != "X-API-Key" {
			t.Errorf("Auth.Static.HeaderName should be 'X-API-Key', got '%s'", loadedConfig.Auth.Static.HeaderName)
		}
		if loadedConfig.Auth.Static.Enabled != false {
			t.Errorf("Auth.Static.Enabled should be false, got %t", loadedConfig.Auth.Static.Enabled)
		}
	}

	// Verify GitHub auth config is properly initialized with defaults
	if loadedConfig.Auth.GitHub == nil {
		t.Error("Auth.GitHub should not be nil")
	} else {
		if loadedConfig.Auth.GitHub.BaseURL != "https://api.github.com" {
			t.Errorf("Auth.GitHub.BaseURL should be 'https://api.github.com', got '%s'", loadedConfig.Auth.GitHub.BaseURL)
		}
		if loadedConfig.Auth.GitHub.TokenHeader != "Authorization" {
			t.Errorf("Auth.GitHub.TokenHeader should be 'Authorization', got '%s'", loadedConfig.Auth.GitHub.TokenHeader)
		}
		if loadedConfig.Auth.GitHub.OAuth == nil {
			t.Error("Auth.GitHub.OAuth should not be nil")
		} else if loadedConfig.Auth.GitHub.OAuth.Scope != "read:user read:org" {
			t.Errorf("Auth.GitHub.OAuth.Scope should be 'read:user read:org', got '%s'", loadedConfig.Auth.GitHub.OAuth.Scope)
		}
	}
}

func TestLoadConfigNonexistentFile(t *testing.T) {
	_, err := LoadConfig("nonexistent-file.json")
	if err == nil {
		t.Error("LoadConfig should return error for nonexistent file")
	}
}

func TestLoadConfigInvalidJSON(t *testing.T) {
	// Create a temporary file with invalid JSON
	tmpfile, err := os.CreateTemp("", "invalid*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	invalidJSON := `{"invalid": json}`
	if _, err := tmpfile.WriteString(invalidJSON); err != nil {
		t.Fatalf("Failed to write invalid JSON: %v", err)
	}
	_ = tmpfile.Close()

	_, err = LoadConfig(tmpfile.Name())
	if err == nil {
		t.Error("LoadConfig should return error for invalid JSON")
	}
}

func TestValidateAPIKey_AuthDisabled(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{
			Enabled: false,
		},
	}

	_, valid := cfg.ValidateAPIKey("any-key")
	assert.False(t, valid)
}

func TestValidateAPIKey_ValidKey(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{
			Enabled: true,
			Static: &StaticAuthConfig{
				Enabled: true,
				APIKeys: []APIKey{
					{
						Key:         "valid-key",
						UserID:      "user1",
						Role:        "user",
						Permissions: []string{"session:create"},
						CreatedAt:   "2024-01-01T00:00:00Z",
					},
				},
			},
		},
	}

	apiKey, valid := cfg.ValidateAPIKey("valid-key")
	assert.True(t, valid)
	assert.NotNil(t, apiKey)
	assert.Equal(t, "user1", apiKey.UserID)
	assert.Equal(t, "user", apiKey.Role)
}

func TestValidateAPIKey_InvalidKey(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{
			Enabled: true,
			Static: &StaticAuthConfig{
				Enabled: true,
				APIKeys: []APIKey{
					{
						Key:    "valid-key",
						UserID: "user1",
					},
				},
			},
		},
	}

	_, valid := cfg.ValidateAPIKey("invalid-key")
	assert.False(t, valid)
}

func TestValidateAPIKey_ExpiredKey(t *testing.T) {
	// Create an expired key
	expiredTime := time.Now().Add(-24 * time.Hour).Format(time.RFC3339)

	cfg := &Config{
		Auth: AuthConfig{
			Enabled: true,
			Static: &StaticAuthConfig{
				Enabled: true,
				APIKeys: []APIKey{
					{
						Key:       "expired-key",
						UserID:    "user1",
						ExpiresAt: expiredTime,
					},
				},
			},
		},
	}

	_, valid := cfg.ValidateAPIKey("expired-key")
	assert.False(t, valid)
}

func TestAPIKey_HasPermission(t *testing.T) {
	apiKey := &APIKey{
		Permissions: []string{"session:create", "session:delete"},
	}

	assert.True(t, apiKey.HasPermission("session:create"))
	assert.True(t, apiKey.HasPermission("session:delete"))
	assert.False(t, apiKey.HasPermission("session:admin"))
}

func TestAPIKey_HasPermission_Wildcard(t *testing.T) {
	apiKey := &APIKey{
		Permissions: []string{"*"},
	}

	assert.True(t, apiKey.HasPermission("session:create"))
	assert.True(t, apiKey.HasPermission("session:delete"))
	assert.True(t, apiKey.HasPermission("any:permission"))
}

func TestLoadConfig_EnableMultipleUsers(t *testing.T) {
	// Clean up environment variables for this test
	restore := cleanupEnvironmentVars()
	defer restore()

	// Test with EnableMultipleUsers enabled
	tempConfig := &Config{
		StartPort:           8000,
		EnableMultipleUsers: true,
	}

	configData, err := json.Marshal(tempConfig)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Write to temporary file
	tmpfile, err := os.CreateTemp("", "config*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.Write(configData); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !loadedConfig.EnableMultipleUsers {
		t.Errorf("Expected EnableMultipleUsers to be true, got %t", loadedConfig.EnableMultipleUsers)
	}
}

func TestExpandEnvVars(t *testing.T) {
	// Set up test environment variables
	_ = os.Setenv("TEST_VAR", "test_value")
	_ = os.Setenv("CLIENT_ID", "my_client_id")
	defer func() {
		_ = os.Unsetenv("TEST_VAR")
		_ = os.Unsetenv("CLIENT_ID")
	}()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple variable expansion",
			input:    "${TEST_VAR}",
			expected: "test_value",
		},
		{
			name:     "Variable in string",
			input:    "prefix_${TEST_VAR}_suffix",
			expected: "prefix_test_value_suffix",
		},
		{
			name:     "Multiple variables",
			input:    "${TEST_VAR}_${CLIENT_ID}",
			expected: "test_value_my_client_id",
		},
		{
			name:     "Non-existent variable",
			input:    "${NON_EXISTENT_VAR}",
			expected: "${NON_EXISTENT_VAR}",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "No variables",
			input:    "plain_string",
			expected: "plain_string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := expandEnvVars(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnvVars(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoadConfigWithEnvVarExpansion(t *testing.T) {
	// Clean up environment variables for this test
	restore := cleanupEnvironmentVars()
	defer restore()

	// Set up test environment variables
	_ = os.Setenv("TEST_CLIENT_ID", "github_client_123")
	_ = os.Setenv("TEST_CLIENT_SECRET", "github_secret_456")
	defer func() {
		_ = os.Unsetenv("TEST_CLIENT_ID")
		_ = os.Unsetenv("TEST_CLIENT_SECRET")
	}()

	// Create config with environment variable references
	configJSON := `{
		"start_port": 8000,
		"auth": {
			"enabled": true,
			"github": {
				"enabled": true,
				"oauth": {
					"client_id": "${TEST_CLIENT_ID}",
					"client_secret": "${TEST_CLIENT_SECRET}",
					"scope": "read:user read:org"
				}
			}
		}
	}`

	// Write to temporary file
	tmpfile, err := os.CreateTemp("", "config*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.WriteString(configJSON); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify environment variables were expanded
	if loadedConfig.Auth.GitHub == nil || loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("GitHub OAuth config should not be nil")
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientID != "github_client_123" {
		t.Errorf("Expected ClientID to be 'github_client_123', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientID)
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientSecret != "github_secret_456" {
		t.Errorf("Expected ClientSecret to be 'github_secret_456', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientSecret)
	}
}

func TestLoadConfigWithYAML(t *testing.T) {
	// Clean up environment variables for this test
	restore := cleanupEnvironmentVars()
	defer restore()

	// Create YAML config
	yamlConfig := `
start_port: 8000
auth:
  enabled: true
  github:
    enabled: true
    oauth:
      client_id: "yaml_client_id"
      client_secret: "yaml_client_secret"
      scope: "read:user read:org"
persistence:
  enabled: true
  backend: "file"
  file_path: "./test_sessions.json"
enable_multiple_users: true
`

	// Write to temporary YAML file
	tmpfile, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.WriteString(yamlConfig); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify YAML was loaded correctly
	if loadedConfig.StartPort != 8000 {
		t.Errorf("Expected StartPort to be 8000, got %d", loadedConfig.StartPort)
	}

	if !loadedConfig.EnableMultipleUsers {
		t.Errorf("Expected EnableMultipleUsers to be true, got %t", loadedConfig.EnableMultipleUsers)
	}

	if !loadedConfig.Persistence.Enabled {
		t.Errorf("Expected Persistence.Enabled to be true, got %t", loadedConfig.Persistence.Enabled)
	}

	if loadedConfig.Persistence.FilePath != "./test_sessions.json" {
		t.Errorf("Expected Persistence.FilePath to be './test_sessions.json', got '%s'", loadedConfig.Persistence.FilePath)
	}

	if loadedConfig.Auth.GitHub == nil || loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("GitHub OAuth config should not be nil")
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientID != "yaml_client_id" {
		t.Errorf("Expected ClientID to be 'yaml_client_id', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientID)
	}
}

func TestLoadConfigWithEnvironmentVariables(t *testing.T) {
	// Set up test environment variables (viper format)
	_ = os.Setenv("AGENTAPI_START_PORT", "9999")
	_ = os.Setenv("AGENTAPI_AUTH_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID", "env_client_id")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET", "env_client_secret")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_BACKEND", "sqlite")
	_ = os.Setenv("AGENTAPI_ENABLE_MULTIPLE_USERS", "true")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_START_PORT")
		_ = os.Unsetenv("AGENTAPI_AUTH_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_ENABLED")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_BACKEND")
		_ = os.Unsetenv("AGENTAPI_ENABLE_MULTIPLE_USERS")
	}()

	// Load config without specifying a file (should use env vars and defaults)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify environment variables were loaded
	if loadedConfig.StartPort != 9999 {
		t.Errorf("Expected StartPort to be 9999, got %d", loadedConfig.StartPort)
	}

	if !loadedConfig.Auth.Enabled {
		t.Errorf("Expected Auth.Enabled to be true, got %t", loadedConfig.Auth.Enabled)
	}

	if !loadedConfig.EnableMultipleUsers {
		t.Errorf("Expected EnableMultipleUsers to be true, got %t", loadedConfig.EnableMultipleUsers)
	}

	if !loadedConfig.Persistence.Enabled {
		t.Errorf("Expected Persistence.Enabled to be true, got %t", loadedConfig.Persistence.Enabled)
	}

	if loadedConfig.Persistence.Backend != "sqlite" {
		t.Errorf("Expected Persistence.Backend to be 'sqlite', got '%s'", loadedConfig.Persistence.Backend)
	}

	if loadedConfig.Auth.GitHub == nil || loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("GitHub OAuth config should not be nil")
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientID != "env_client_id" {
		t.Errorf("Expected ClientID to be 'env_client_id', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientID)
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientSecret != "env_client_secret" {
		t.Errorf("Expected ClientSecret to be 'env_client_secret', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientSecret)
	}
}

func TestInitializeConfigStructsFromEnv_StaticAuth(t *testing.T) {
	// Set up test environment variables for static auth
	_ = os.Setenv("AGENTAPI_AUTH_STATIC_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_STATIC_HEADER_NAME", "X-Custom-Key")
	_ = os.Setenv("AGENTAPI_AUTH_STATIC_KEYS_FILE", "/path/to/keys.json")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_STATIC_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_STATIC_HEADER_NAME")
		_ = os.Unsetenv("AGENTAPI_AUTH_STATIC_KEYS_FILE")
	}()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify Static auth config was initialized from environment variables
	if loadedConfig.Auth.Static == nil {
		t.Fatal("Auth.Static should not be nil when environment variables are set")
	}

	assert.True(t, loadedConfig.Auth.Static.Enabled)
	assert.Equal(t, "X-Custom-Key", loadedConfig.Auth.Static.HeaderName)
	assert.Equal(t, "/path/to/keys.json", loadedConfig.Auth.Static.KeysFile)
	assert.Empty(t, loadedConfig.Auth.Static.APIKeys) // Should be empty initially
}

func TestInitializeConfigStructsFromEnv_GitHubAuth(t *testing.T) {
	// Clean up environment variables for this test
	restore := cleanupEnvironmentVars()
	defer restore()

	// Set up test environment variables for GitHub auth
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_BASE_URL", "https://github.company.com/api/v3")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_TOKEN_HEADER", "X-GitHub-Token")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE", "developer")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_BASE_URL")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_TOKEN_HEADER")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE")
	}()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify GitHub auth config was initialized from environment variables
	if loadedConfig.Auth.GitHub == nil {
		t.Fatal("Auth.GitHub should not be nil when environment variables are set")
	}

	assert.True(t, loadedConfig.Auth.GitHub.Enabled)
	assert.Equal(t, "https://github.company.com/api/v3", loadedConfig.Auth.GitHub.BaseURL)
	assert.Equal(t, "X-GitHub-Token", loadedConfig.Auth.GitHub.TokenHeader)
	assert.Equal(t, "developer", loadedConfig.Auth.GitHub.UserMapping.DefaultRole)
}

func TestInitializeConfigStructsFromEnv_GitHubOAuth(t *testing.T) {
	// First set up GitHub auth to exist
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID", "oauth_client_123")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET", "oauth_secret_456")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE", "read:user read:org repo")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_BASE_URL", "https://github.company.com")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_BASE_URL")
	}()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify GitHub OAuth config was initialized from environment variables
	if loadedConfig.Auth.GitHub == nil {
		t.Fatal("Auth.GitHub should not be nil")
	}
	if loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("Auth.GitHub.OAuth should not be nil when environment variables are set")
	}

	assert.Equal(t, "oauth_client_123", loadedConfig.Auth.GitHub.OAuth.ClientID)
	assert.Equal(t, "oauth_secret_456", loadedConfig.Auth.GitHub.OAuth.ClientSecret)
	assert.Equal(t, "read:user read:org repo", loadedConfig.Auth.GitHub.OAuth.Scope)
	assert.Equal(t, "https://github.company.com", loadedConfig.Auth.GitHub.OAuth.BaseURL)
}

func TestInitializeConfigStructsFromEnv_Persistence(t *testing.T) {
	// Set up test environment variables for persistence
	_ = os.Setenv("AGENTAPI_PERSISTENCE_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_BACKEND", "postgres")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_FILE_PATH", "/custom/path/sessions.json")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_SYNC_INTERVAL_SECONDS", "60")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_ENABLED")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_BACKEND")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_FILE_PATH")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_SYNC_INTERVAL_SECONDS")
	}()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify Persistence config was initialized from environment variables
	assert.True(t, loadedConfig.Persistence.Enabled)
	assert.Equal(t, "postgres", loadedConfig.Persistence.Backend)
	assert.Equal(t, "/custom/path/sessions.json", loadedConfig.Persistence.FilePath)
	assert.Equal(t, 60, loadedConfig.Persistence.SyncInterval)
}

func TestInitializeConfigStructsFromEnv_S3Persistence(t *testing.T) {
	// Set up test environment variables for S3 persistence
	_ = os.Setenv("AGENTAPI_PERSISTENCE_S3_BUCKET", "my-test-bucket")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_S3_REGION", "us-west-2")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_S3_PREFIX", "agentapi/sessions/")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_S3_ENDPOINT", "https://s3.amazonaws.com")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_S3_ACCESS_KEY", "test_access_key")
	_ = os.Setenv("AGENTAPI_PERSISTENCE_S3_SECRET_KEY", "test_secret_key")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_S3_BUCKET")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_S3_REGION")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_S3_PREFIX")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_S3_ENDPOINT")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_S3_ACCESS_KEY")
		_ = os.Unsetenv("AGENTAPI_PERSISTENCE_S3_SECRET_KEY")
	}()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify S3 Persistence config was initialized from environment variables
	assert.Equal(t, "my-test-bucket", loadedConfig.Persistence.S3Bucket)
	assert.Equal(t, "us-west-2", loadedConfig.Persistence.S3Region)
	assert.Equal(t, "agentapi/sessions/", loadedConfig.Persistence.S3Prefix)
	assert.Equal(t, "https://s3.amazonaws.com", loadedConfig.Persistence.S3Endpoint)
	assert.Equal(t, "test_access_key", loadedConfig.Persistence.S3AccessKey)
	assert.Equal(t, "test_secret_key", loadedConfig.Persistence.S3SecretKey)
}

func TestInitializeConfigStructsFromEnv_NoInitializationWhenConfigExists(t *testing.T) {
	// Clean up environment variables for this test
	restore := cleanupEnvironmentVars()
	defer restore()

	// Set up environment variables
	_ = os.Setenv("AGENTAPI_AUTH_STATIC_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_STATIC_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_ENABLED")
	}()

	// Create config with existing auth structures
	configJSON := `{
		"start_port": 8000,
		"auth": {
			"enabled": false,
			"static": {
				"enabled": false,
				"header_name": "X-Existing-Key"
			},
			"github": {
				"enabled": false,
				"base_url": "https://existing.github.com"
			}
		}
	}`

	// Write to temporary file
	tmpfile, err := os.CreateTemp("", "config*.json")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpfile.Name()) }()

	if _, err := tmpfile.WriteString(configJSON); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	_ = tmpfile.Close()

	// Load the config
	loadedConfig, err := LoadConfig(tmpfile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify existing config structures were NOT overwritten by environment variables
	// But environment variables still affect enabled flags due to viper's automatic env handling
	assert.True(t, loadedConfig.Auth.Static.Enabled)                                 // Environment variable takes precedence
	assert.Equal(t, "X-Existing-Key", loadedConfig.Auth.Static.HeaderName)           // Should remain as configured
	assert.True(t, loadedConfig.Auth.GitHub.Enabled)                                 // Environment variable takes precedence
	assert.Equal(t, "https://existing.github.com", loadedConfig.Auth.GitHub.BaseURL) // Should remain as configured
}

func TestInitializeConfigStructsFromEnv_PartialEnvironmentVariables(t *testing.T) {
	// Clean up environment variables for this test
	restore := cleanupEnvironmentVars()
	defer restore()

	// Set up only some environment variables
	_ = os.Setenv("AGENTAPI_AUTH_STATIC_HEADER_NAME", "X-Partial-Key")
	// Note: Not setting AGENTAPI_AUTH_STATIC_ENABLED

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_STATIC_HEADER_NAME")
	}()

	// Load config without file (should initialize from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify Static auth config was still initialized due to header_name being set
	if loadedConfig.Auth.Static == nil {
		t.Fatal("Auth.Static should not be nil when any environment variable is set")
	}

	assert.False(t, loadedConfig.Auth.Static.Enabled)                     // Should be false (default)
	assert.Equal(t, "X-Partial-Key", loadedConfig.Auth.Static.HeaderName) // Should be from env var
}

func TestInitializeConfigStructsFromEnv_AllSettingsFromEnvironment(t *testing.T) {
	// Clean up environment variables for this test
	restore := cleanupEnvironmentVars()
	defer restore()

	// Set up comprehensive environment variables
	envVars := map[string]string{
		"AGENTAPI_START_PORT":                            "7777",
		"AGENTAPI_AUTH_ENABLED":                          "true",
		"AGENTAPI_AUTH_STATIC_ENABLED":                   "true",
		"AGENTAPI_AUTH_STATIC_HEADER_NAME":               "X-Full-Test-Key",
		"AGENTAPI_AUTH_STATIC_KEYS_FILE":                 "/full/test/keys.json",
		"AGENTAPI_AUTH_GITHUB_ENABLED":                   "true",
		"AGENTAPI_AUTH_GITHUB_BASE_URL":                  "https://full.test.github.com/api/v3",
		"AGENTAPI_AUTH_GITHUB_TOKEN_HEADER":              "X-Full-GitHub-Token",
		"AGENTAPI_AUTH_GITHUB_USER_MAPPING_DEFAULT_ROLE": "full-tester",
		"AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID":           "full_client_123",
		"AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET":       "full_secret_456",
		"AGENTAPI_AUTH_GITHUB_OAUTH_SCOPE":               "read:user read:org admin:repo",
		"AGENTAPI_AUTH_GITHUB_OAUTH_BASE_URL":            "https://full.test.github.com",
		"AGENTAPI_PERSISTENCE_ENABLED":                   "true",
		"AGENTAPI_PERSISTENCE_BACKEND":                   "s3",
		"AGENTAPI_PERSISTENCE_FILE_PATH":                 "/full/test/sessions.json",
		"AGENTAPI_PERSISTENCE_SYNC_INTERVAL_SECONDS":     "120",
		"AGENTAPI_PERSISTENCE_S3_BUCKET":                 "full-test-bucket",
		"AGENTAPI_PERSISTENCE_S3_REGION":                 "eu-central-1",
		"AGENTAPI_PERSISTENCE_S3_PREFIX":                 "full-test/sessions/",
		"AGENTAPI_PERSISTENCE_S3_ENDPOINT":               "https://full.test.s3.endpoint.com",
		"AGENTAPI_PERSISTENCE_S3_ACCESS_KEY":             "full_test_access",
		"AGENTAPI_PERSISTENCE_S3_SECRET_KEY":             "full_test_secret",
		"AGENTAPI_ENABLE_MULTIPLE_USERS":                 "true",
	}

	// Set all environment variables
	for key, value := range envVars {
		_ = os.Setenv(key, value)
	}

	// Clean up environment variables
	defer func() {
		for key := range envVars {
			_ = os.Unsetenv(key)
		}
	}()

	// Load config without file (should initialize everything from env vars)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify all settings were loaded from environment variables
	assert.Equal(t, 7777, loadedConfig.StartPort)
	assert.True(t, loadedConfig.Auth.Enabled)
	assert.True(t, loadedConfig.EnableMultipleUsers)

	// Static auth verification
	if assert.NotNil(t, loadedConfig.Auth.Static) {
		assert.True(t, loadedConfig.Auth.Static.Enabled)
		assert.Equal(t, "X-Full-Test-Key", loadedConfig.Auth.Static.HeaderName)
		assert.Equal(t, "/full/test/keys.json", loadedConfig.Auth.Static.KeysFile)
	}

	// GitHub auth verification
	if assert.NotNil(t, loadedConfig.Auth.GitHub) {
		assert.True(t, loadedConfig.Auth.GitHub.Enabled)
		assert.Equal(t, "https://full.test.github.com/api/v3", loadedConfig.Auth.GitHub.BaseURL)
		assert.Equal(t, "X-Full-GitHub-Token", loadedConfig.Auth.GitHub.TokenHeader)
		assert.Equal(t, "full-tester", loadedConfig.Auth.GitHub.UserMapping.DefaultRole)

		// GitHub OAuth verification
		if assert.NotNil(t, loadedConfig.Auth.GitHub.OAuth) {
			assert.Equal(t, "full_client_123", loadedConfig.Auth.GitHub.OAuth.ClientID)
			assert.Equal(t, "full_secret_456", loadedConfig.Auth.GitHub.OAuth.ClientSecret)
			assert.Equal(t, "read:user read:org admin:repo", loadedConfig.Auth.GitHub.OAuth.Scope)
			assert.Equal(t, "https://full.test.github.com", loadedConfig.Auth.GitHub.OAuth.BaseURL)
		}
	}

	// Persistence verification
	assert.True(t, loadedConfig.Persistence.Enabled)
	assert.Equal(t, "s3", loadedConfig.Persistence.Backend)
	assert.Equal(t, "/full/test/sessions.json", loadedConfig.Persistence.FilePath)
	assert.Equal(t, 120, loadedConfig.Persistence.SyncInterval)
	assert.Equal(t, "full-test-bucket", loadedConfig.Persistence.S3Bucket)
	assert.Equal(t, "eu-central-1", loadedConfig.Persistence.S3Region)
	assert.Equal(t, "full-test/sessions/", loadedConfig.Persistence.S3Prefix)
	assert.Equal(t, "https://full.test.s3.endpoint.com", loadedConfig.Persistence.S3Endpoint)
	assert.Equal(t, "full_test_access", loadedConfig.Persistence.S3AccessKey)
	assert.Equal(t, "full_test_secret", loadedConfig.Persistence.S3SecretKey)
}
