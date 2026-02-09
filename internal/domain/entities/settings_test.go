package entities

import (
	"testing"
	"time"
)

func TestNewBedrockSettings(t *testing.T) {
	bedrock := NewBedrockSettings(true)

	if !bedrock.Enabled() {
		t.Error("Expected Bedrock to be enabled")
	}
}

func TestBedrockSettings_Setters(t *testing.T) {
	bedrock := NewBedrockSettings(true)

	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	bedrock.SetRoleARN("arn:aws:iam::123456789012:role/MyRole")
	bedrock.SetProfile("my-profile")

	if bedrock.Model() != "anthropic.claude-sonnet-4-20250514-v1:0" {
		t.Errorf("Expected model 'anthropic.claude-sonnet-4-20250514-v1:0', got '%s'", bedrock.Model())
	}
	if bedrock.AccessKeyID() != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("Expected access key ID 'AKIAIOSFODNN7EXAMPLE', got '%s'", bedrock.AccessKeyID())
	}
	if bedrock.SecretAccessKey() != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("Expected secret access key to be set")
	}
	if bedrock.RoleARN() != "arn:aws:iam::123456789012:role/MyRole" {
		t.Errorf("Expected role ARN 'arn:aws:iam::123456789012:role/MyRole', got '%s'", bedrock.RoleARN())
	}
	if bedrock.Profile() != "my-profile" {
		t.Errorf("Expected profile 'my-profile', got '%s'", bedrock.Profile())
	}
}

func TestBedrockSettings_Validate(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		expectErr bool
	}{
		{
			name:      "disabled - valid",
			enabled:   false,
			expectErr: false,
		},
		{
			name:      "enabled - valid",
			enabled:   true,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bedrock := NewBedrockSettings(tt.enabled)
			err := bedrock.Validate()

			if tt.expectErr && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestNewSettings(t *testing.T) {
	settings := NewSettings("test-user")

	if settings.Name() != "test-user" {
		t.Errorf("Expected name 'test-user', got '%s'", settings.Name())
	}
	if settings.Bedrock() != nil {
		t.Error("Expected Bedrock to be nil initially")
	}
	if settings.CreatedAt().IsZero() {
		t.Error("Expected CreatedAt to be set")
	}
	if settings.UpdatedAt().IsZero() {
		t.Error("Expected UpdatedAt to be set")
	}
}

func TestSettings_SetBedrock(t *testing.T) {
	settings := NewSettings("test-user")
	originalUpdatedAt := settings.UpdatedAt()

	// Wait a bit to ensure time difference
	time.Sleep(time.Millisecond)

	bedrock := NewBedrockSettings(true)
	settings.SetBedrock(bedrock)

	if settings.Bedrock() == nil {
		t.Error("Expected Bedrock to be set")
	}
	if !settings.UpdatedAt().After(originalUpdatedAt) {
		t.Error("Expected UpdatedAt to be updated")
	}
}

func TestSettings_EnabledPlugins(t *testing.T) {
	settings := NewSettings("test-user")

	// Initially should be nil/empty
	if len(settings.EnabledPlugins()) != 0 {
		t.Error("Expected EnabledPlugins to be empty initially")
	}

	originalUpdatedAt := settings.UpdatedAt()

	// Wait a bit to ensure time difference
	time.Sleep(time.Millisecond)

	// Set plugins in plugin@marketplace format
	plugins := []string{"context7@claude-plugins-official", "typescript@claude-plugins-official", "my-plugin@my-marketplace"}
	settings.SetEnabledPlugins(plugins)

	// Verify plugins are set
	result := settings.EnabledPlugins()
	if len(result) != 3 {
		t.Errorf("Expected 3 plugins, got %d", len(result))
	}
	if result[0] != "context7@claude-plugins-official" || result[1] != "typescript@claude-plugins-official" || result[2] != "my-plugin@my-marketplace" {
		t.Errorf("Expected plugins in plugin@marketplace format, got %v", result)
	}

	// Verify UpdatedAt is updated
	if !settings.UpdatedAt().After(originalUpdatedAt) {
		t.Error("Expected UpdatedAt to be updated")
	}
}

func TestSettings_EnvVars(t *testing.T) {
	settings := NewSettings("test-user")

	// Initially should be nil/empty
	if len(settings.EnvVars()) != 0 {
		t.Error("Expected EnvVars to be empty initially")
	}
	if settings.EnvVarKeys() != nil {
		t.Error("Expected EnvVarKeys to be nil initially")
	}

	originalUpdatedAt := settings.UpdatedAt()

	// Wait a bit to ensure time difference
	time.Sleep(time.Millisecond)

	// Set environment variables
	envVars := map[string]string{
		"MY_API_KEY":  "secret-key",
		"DEBUG_MODE":  "true",
		"SERVICE_URL": "https://api.example.com",
		"ANOTHER_VAR": "value",
	}
	settings.SetEnvVars(envVars)

	// Verify env vars are set
	result := settings.EnvVars()
	if len(result) != 4 {
		t.Errorf("Expected 4 env vars, got %d", len(result))
	}
	if result["MY_API_KEY"] != "secret-key" {
		t.Errorf("Expected MY_API_KEY='secret-key', got '%s'", result["MY_API_KEY"])
	}
	if result["DEBUG_MODE"] != "true" {
		t.Errorf("Expected DEBUG_MODE='true', got '%s'", result["DEBUG_MODE"])
	}

	// Verify EnvVarKeys returns sorted keys
	keys := settings.EnvVarKeys()
	if len(keys) != 4 {
		t.Errorf("Expected 4 keys, got %d", len(keys))
	}
	// Verify keys are sorted
	expectedKeys := []string{"ANOTHER_VAR", "DEBUG_MODE", "MY_API_KEY", "SERVICE_URL"}
	for i, key := range keys {
		if key != expectedKeys[i] {
			t.Errorf("Expected key %d to be '%s', got '%s'", i, expectedKeys[i], key)
		}
	}

	// Verify UpdatedAt is updated
	if !settings.UpdatedAt().After(originalUpdatedAt) {
		t.Error("Expected UpdatedAt to be updated")
	}

	// Test setting nil env vars (should initialize as empty map)
	settings.SetEnvVars(nil)
	if settings.EnvVars() == nil {
		t.Error("Expected EnvVars to be initialized as empty map, not nil")
	}
	if len(settings.EnvVars()) != 0 {
		t.Error("Expected EnvVars to be empty after setting nil")
	}
}

func TestSettings_Validate(t *testing.T) {
	tests := []struct {
		name      string
		settings  *Settings
		expectErr bool
	}{
		{
			name:      "valid settings without bedrock",
			settings:  NewSettings("test-user"),
			expectErr: false,
		},
		{
			name: "valid settings with valid bedrock",
			settings: func() *Settings {
				s := NewSettings("test-user")
				s.SetBedrock(NewBedrockSettings(true))
				return s
			}(),
			expectErr: false,
		},
		{
			name: "invalid settings with empty name",
			settings: func() *Settings {
				s := &Settings{}
				return s
			}(),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.settings.Validate()

			if tt.expectErr && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}
