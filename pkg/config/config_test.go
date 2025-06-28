package config

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
