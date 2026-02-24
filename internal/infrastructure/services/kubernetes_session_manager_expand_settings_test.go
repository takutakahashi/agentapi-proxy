package services

import (
	"testing"
)

func TestExpandSettingsToEnv_BedrockAuthMode(t *testing.T) {
	cfg := &agentapiSettingsJSON{
		AuthMode: "bedrock",
		Bedrock: &agentapiBedrockJSON{
			Model:           "anthropic.claude-3-sonnet-20240229-v1:0",
			AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			RoleARN:         "arn:aws:iam::123456789012:role/MyRole",
			Profile:         "my-profile",
		},
	}

	env := expandSettingsToEnv(cfg)

	if env["CLAUDE_CODE_USE_BEDROCK"] != "1" {
		t.Errorf("expected CLAUDE_CODE_USE_BEDROCK=1, got %q", env["CLAUDE_CODE_USE_BEDROCK"])
	}
	if env["AWS_ACCESS_KEY_ID"] != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("expected AWS_ACCESS_KEY_ID to be set, got %q", env["AWS_ACCESS_KEY_ID"])
	}
	if env["AWS_SECRET_ACCESS_KEY"] != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("expected AWS_SECRET_ACCESS_KEY to be set, got %q", env["AWS_SECRET_ACCESS_KEY"])
	}
	if env["AWS_ROLE_ARN"] != "arn:aws:iam::123456789012:role/MyRole" {
		t.Errorf("expected AWS_ROLE_ARN to be set, got %q", env["AWS_ROLE_ARN"])
	}
	if env["AWS_PROFILE"] != "my-profile" {
		t.Errorf("expected AWS_PROFILE to be set, got %q", env["AWS_PROFILE"])
	}
	if env["ANTHROPIC_MODEL"] != "anthropic.claude-3-sonnet-20240229-v1:0" {
		t.Errorf("expected ANTHROPIC_MODEL to be set, got %q", env["ANTHROPIC_MODEL"])
	}
}

func TestExpandSettingsToEnv_OAuthAuthMode(t *testing.T) {
	cfg := &agentapiSettingsJSON{
		AuthMode: "oauth",
	}

	env := expandSettingsToEnv(cfg)

	if env["CLAUDE_CODE_USE_BEDROCK"] != "0" {
		t.Errorf("expected CLAUDE_CODE_USE_BEDROCK=0, got %q", env["CLAUDE_CODE_USE_BEDROCK"])
	}
	if env["AWS_ACCESS_KEY_ID"] != "" {
		t.Errorf("expected AWS_ACCESS_KEY_ID to be cleared, got %q", env["AWS_ACCESS_KEY_ID"])
	}
	if env["AWS_SECRET_ACCESS_KEY"] != "" {
		t.Errorf("expected AWS_SECRET_ACCESS_KEY to be cleared, got %q", env["AWS_SECRET_ACCESS_KEY"])
	}
}

// TestExpandSettingsToEnv_EmptyAuthMode_DoesNotClearBedrockVars verifies that when
// auth_mode is empty (unset), the function does NOT output any auth-related keys,
// so that a team's bedrock settings are not overwritten when user settings have no auth_mode.
func TestExpandSettingsToEnv_EmptyAuthMode_DoesNotClearBedrockVars(t *testing.T) {
	cfg := &agentapiSettingsJSON{
		AuthMode: "", // not set
	}

	env := expandSettingsToEnv(cfg)

	// None of the auth-related keys should be present when auth_mode is empty.
	authKeys := []string{
		"CLAUDE_CODE_USE_BEDROCK",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_ROLE_ARN",
		"AWS_PROFILE",
		"ANTHROPIC_MODEL",
	}
	for _, key := range authKeys {
		if _, ok := env[key]; ok {
			t.Errorf("expected key %q to be absent when auth_mode is empty, but it was set to %q", key, env[key])
		}
	}
}

// TestExpandSettingsToEnv_MergeTeamBedrockWithEmptyUserAuthMode verifies the real-world scenario:
// team settings have bedrock enabled, user settings have no auth_mode.
// The merged env map should retain bedrock credentials from team settings.
func TestExpandSettingsToEnv_MergeTeamBedrockWithEmptyUserAuthMode(t *testing.T) {
	teamCfg := &agentapiSettingsJSON{
		AuthMode: "bedrock",
		Bedrock: &agentapiBedrockJSON{
			Model:           "anthropic.claude-3-sonnet-20240229-v1:0",
			AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		},
	}
	userCfg := &agentapiSettingsJSON{
		AuthMode: "", // user has no auth_mode set
		EnvVars:  map[string]string{"MY_VAR": "hello"},
	}

	// Simulate the merge: team first, then user (user may override)
	env := make(map[string]string)
	for k, v := range expandSettingsToEnv(teamCfg) {
		env[k] = v
	}
	for k, v := range expandSettingsToEnv(userCfg) {
		env[k] = v
	}

	// Bedrock credentials from team should survive
	if env["CLAUDE_CODE_USE_BEDROCK"] != "1" {
		t.Errorf("expected CLAUDE_CODE_USE_BEDROCK=1 after merge, got %q", env["CLAUDE_CODE_USE_BEDROCK"])
	}
	if env["AWS_ACCESS_KEY_ID"] != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("expected AWS_ACCESS_KEY_ID to be preserved after merge, got %q", env["AWS_ACCESS_KEY_ID"])
	}
	if env["AWS_SECRET_ACCESS_KEY"] != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("expected AWS_SECRET_ACCESS_KEY to be preserved after merge, got %q", env["AWS_SECRET_ACCESS_KEY"])
	}

	// User's own env vars should also be present
	if env["MY_VAR"] != "hello" {
		t.Errorf("expected MY_VAR=hello, got %q", env["MY_VAR"])
	}
}
