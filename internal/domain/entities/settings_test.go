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
