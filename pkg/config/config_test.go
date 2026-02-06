package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// clearAGENTAPIEnvVars clears all AGENTAPI_ environment variables and returns a cleanup function
func clearAGENTAPIEnvVars(t *testing.T) {
	t.Helper()
	savedEnvVars := make(map[string]string)
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "AGENTAPI_") {
			parts := strings.SplitN(env, "=", 2)
			key := parts[0]
			savedEnvVars[key] = os.Getenv(key)
			_ = os.Unsetenv(key)
		}
	}
	t.Cleanup(func() {
		// Restore environment variables
		for key, value := range savedEnvVars {
			if value != "" {
				_ = os.Setenv(key, value)
			}
		}
	})
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	// Verify default auth config
	if config.Auth.Static == nil {
		t.Error("Auth.Static should be initialized by default")
	}
}

func TestLoadConfig(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	// Create a temporary config file
	tempConfig := &Config{
		Auth: AuthConfig{
			Static: &StaticAuthConfig{
				Enabled: false,
			},
		},
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

	// Compare auth config values (not pointer equality)
	if loadedConfig.Auth.Static != nil && loadedConfig.Auth.Static.Enabled {
		t.Error("Auth.Static should be disabled by default")
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
			Static: &StaticAuthConfig{
				Enabled: false,
			},
		},
	}

	_, valid := cfg.ValidateAPIKey("any-key")
	assert.False(t, valid)
}

func TestValidateAPIKey_ValidKey(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{
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
	clearAGENTAPIEnvVars(t)

	// Set up test environment variables
	_ = os.Setenv("TEST_CLIENT_ID", "github_client_123")
	_ = os.Setenv("TEST_CLIENT_SECRET", "github_secret_456")
	defer func() {
		_ = os.Unsetenv("TEST_CLIENT_ID")
		_ = os.Unsetenv("TEST_CLIENT_SECRET")
	}()

	// Create config with environment variable references
	configJSON := `{
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
	clearAGENTAPIEnvVars(t)

	// Create YAML config
	yamlConfig := `
auth:
  enabled: true
  github:
    enabled: true
    oauth:
      client_id: "yaml_client_id"
      client_secret: "yaml_client_secret"
      scope: "read:user read:org"
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
	if loadedConfig.Auth.GitHub == nil || loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("GitHub OAuth config should not be nil")
	}

	if loadedConfig.Auth.GitHub.OAuth.ClientID != "yaml_client_id" {
		t.Errorf("Expected ClientID to be 'yaml_client_id', got '%s'", loadedConfig.Auth.GitHub.OAuth.ClientID)
	}
}

func TestLoadConfigWithEnvironmentVariables(t *testing.T) {
	// Set up test environment variables (viper format)
	_ = os.Setenv("AGENTAPI_AUTH_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID", "env_client_id")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET", "env_client_secret")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_ID")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_OAUTH_CLIENT_SECRET")
	}()

	// Load config without specifying a file (should use env vars and defaults)
	loadedConfig, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify environment variables were loaded
	if loadedConfig.Auth.GitHub == nil || loadedConfig.Auth.GitHub.OAuth == nil {
		t.Fatal("GitHub OAuth config should not be nil")
	}

	// GitHub auth is considered enabled if OAuth config is present
	// (Auth.GitHub.Enabled was removed in favor of checking individual auth methods)

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
	clearAGENTAPIEnvVars(t)

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

func TestInitializeConfigStructsFromEnv_NoInitializationWhenConfigExists(t *testing.T) {
	clearAGENTAPIEnvVars(t)

	// Set up environment variables
	_ = os.Setenv("AGENTAPI_AUTH_STATIC_ENABLED", "true")
	_ = os.Setenv("AGENTAPI_AUTH_GITHUB_ENABLED", "true")

	defer func() {
		_ = os.Unsetenv("AGENTAPI_AUTH_STATIC_ENABLED")
		_ = os.Unsetenv("AGENTAPI_AUTH_GITHUB_ENABLED")
	}()

	// Create config with existing auth structures
	configJSON := `{
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
	clearAGENTAPIEnvVars(t)

	// Set up comprehensive environment variables
	envVars := map[string]string{
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

}
