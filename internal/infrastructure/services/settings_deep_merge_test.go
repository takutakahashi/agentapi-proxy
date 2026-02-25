package services

import (
	"testing"
)

// helper to build a simple agentapiSettingsJSON for test cases
func makeSettings(
	authMode string,
	oauthToken string,
	envVars map[string]string,
	mcpServers map[string]*agentapiMCPServerJSON,
	bedrock *agentapiBedrockJSON,
) *agentapiSettingsJSON {
	return &agentapiSettingsJSON{
		AuthMode:             authMode,
		ClaudeCodeOAuthToken: oauthToken,
		EnvVars:              envVars,
		MCPServers:           mcpServers,
		Bedrock:              bedrock,
	}
}

// --- deepMergeAgentapiSettings ---

func TestDeepMerge_EmptySlice(t *testing.T) {
	result := deepMergeAgentapiSettings(nil)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.AuthMode != "" {
		t.Errorf("unexpected AuthMode: %q", result.AuthMode)
	}
	if len(result.EnvVars) != 0 {
		t.Errorf("expected empty EnvVars, got %v", result.EnvVars)
	}
}

func TestDeepMerge_NilSourcesSkipped(t *testing.T) {
	src := makeSettings("bedrock", "", map[string]string{"A": "1"}, nil, nil)
	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{nil, src, nil})
	if result.AuthMode != "bedrock" {
		t.Errorf("expected AuthMode=bedrock, got %q", result.AuthMode)
	}
	if result.EnvVars["A"] != "1" {
		t.Errorf("expected EnvVars[A]=1, got %q", result.EnvVars["A"])
	}
}

func TestDeepMerge_SingleSource(t *testing.T) {
	src := makeSettings("oauth", "token123", map[string]string{"X": "x"}, nil, nil)
	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{src})
	if result.AuthMode != "oauth" {
		t.Errorf("expected oauth, got %q", result.AuthMode)
	}
	if result.ClaudeCodeOAuthToken != "token123" {
		t.Errorf("expected token123, got %q", result.ClaudeCodeOAuthToken)
	}
	if result.EnvVars["X"] != "x" {
		t.Errorf("expected x, got %q", result.EnvVars["X"])
	}
}

func TestDeepMerge_EnvVarsUnion(t *testing.T) {
	team := makeSettings("", "", map[string]string{"SHARED": "team", "TEAM_ONLY": "t"}, nil, nil)
	user := makeSettings("", "", map[string]string{"SHARED": "user", "USER_ONLY": "u"}, nil, nil)
	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})

	tests := map[string]string{
		"SHARED":    "user",  // user wins on conflict
		"TEAM_ONLY": "t",    // team-only key is preserved
		"USER_ONLY": "u",    // user-only key is added
	}
	for key, want := range tests {
		if got := result.EnvVars[key]; got != want {
			t.Errorf("EnvVars[%s]: want %q, got %q", key, want, got)
		}
	}
}

func TestDeepMerge_MCPServersUnion(t *testing.T) {
	serverA := &agentapiMCPServerJSON{Type: "http", URL: "https://team-a.example.com"}
	serverB := &agentapiMCPServerJSON{Type: "stdio", Command: "user-cmd"}
	sharedTeam := &agentapiMCPServerJSON{Type: "http", URL: "https://team-shared.example.com"}
	sharedUser := &agentapiMCPServerJSON{Type: "http", URL: "https://user-shared.example.com"}

	team := makeSettings("", "", nil, map[string]*agentapiMCPServerJSON{
		"server-a": serverA,
		"shared":   sharedTeam,
	}, nil)
	user := makeSettings("", "", nil, map[string]*agentapiMCPServerJSON{
		"server-b": serverB,
		"shared":   sharedUser,
	}, nil)

	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})

	if _, ok := result.MCPServers["server-a"]; !ok {
		t.Error("expected server-a to be present")
	}
	if _, ok := result.MCPServers["server-b"]; !ok {
		t.Error("expected server-b to be present")
	}
	if result.MCPServers["shared"].URL != "https://user-shared.example.com" {
		t.Errorf("expected user's shared URL, got %q", result.MCPServers["shared"].URL)
	}
}

func TestDeepMerge_AuthMode_EmptyDoesNotOverride(t *testing.T) {
	team := makeSettings("bedrock", "", nil, nil, nil)
	user := makeSettings("", "", nil, nil, nil) // user has no auth_mode set
	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})
	if result.AuthMode != "bedrock" {
		t.Errorf("expected team's bedrock to survive empty user override, got %q", result.AuthMode)
	}
}

func TestDeepMerge_AuthMode_UserWins(t *testing.T) {
	team := makeSettings("bedrock", "", nil, nil, nil)
	user := makeSettings("oauth", "mytoken", nil, nil, nil)
	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})
	if result.AuthMode != "oauth" {
		t.Errorf("expected oauth, got %q", result.AuthMode)
	}
	if result.ClaudeCodeOAuthToken != "mytoken" {
		t.Errorf("expected mytoken, got %q", result.ClaudeCodeOAuthToken)
	}
}

func TestDeepMerge_OAuthToken_EmptyDoesNotOverride(t *testing.T) {
	team := makeSettings("oauth", "team-token", nil, nil, nil)
	user := makeSettings("oauth", "", nil, nil, nil) // user has auth_mode oauth but no token
	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})
	if result.ClaudeCodeOAuthToken != "team-token" {
		t.Errorf("expected team token to survive, got %q", result.ClaudeCodeOAuthToken)
	}
}

func TestDeepMerge_OAuthToken_UserWins(t *testing.T) {
	team := makeSettings("oauth", "team-token", nil, nil, nil)
	user := makeSettings("oauth", "user-token", nil, nil, nil)
	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})
	if result.ClaudeCodeOAuthToken != "user-token" {
		t.Errorf("expected user-token, got %q", result.ClaudeCodeOAuthToken)
	}
}

func TestDeepMerge_Bedrock_NilHigherPriority(t *testing.T) {
	teamBedrock := &agentapiBedrockJSON{
		Enabled:         true,
		Model:           "claude-3",
		AccessKeyID:     "AKID",
		SecretAccessKey: "SECRET",
	}
	team := makeSettings("bedrock", "", nil, nil, teamBedrock)
	user := makeSettings("", "", nil, nil, nil) // user has no Bedrock block

	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})
	if result.Bedrock == nil {
		t.Fatal("expected team's Bedrock to be preserved")
	}
	if !result.Bedrock.Enabled {
		t.Error("expected Enabled=true")
	}
	if result.Bedrock.Model != "claude-3" {
		t.Errorf("expected claude-3, got %q", result.Bedrock.Model)
	}
	if result.Bedrock.AccessKeyID != "AKID" {
		t.Errorf("expected AKID, got %q", result.Bedrock.AccessKeyID)
	}
}

func TestDeepMerge_Bedrock_PartialOverride(t *testing.T) {
	// User overrides only the model; team credentials should be preserved.
	teamBedrock := &agentapiBedrockJSON{
		Enabled:         true,
		Model:           "claude-3",
		AccessKeyID:     "TEAM_AKID",
		SecretAccessKey: "TEAM_SECRET",
	}
	userBedrock := &agentapiBedrockJSON{
		Enabled: true,
		Model:   "claude-sonnet-4",
		// AccessKeyID and SecretAccessKey are empty — should keep team values
	}
	team := makeSettings("bedrock", "", nil, nil, teamBedrock)
	user := makeSettings("bedrock", "", nil, nil, userBedrock)

	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})
	if result.Bedrock == nil {
		t.Fatal("expected Bedrock to be non-nil")
	}
	if result.Bedrock.Model != "claude-sonnet-4" {
		t.Errorf("expected user model claude-sonnet-4, got %q", result.Bedrock.Model)
	}
	if result.Bedrock.AccessKeyID != "TEAM_AKID" {
		t.Errorf("expected team AKID to be preserved, got %q", result.Bedrock.AccessKeyID)
	}
	if result.Bedrock.SecretAccessKey != "TEAM_SECRET" {
		t.Errorf("expected team secret to be preserved, got %q", result.Bedrock.SecretAccessKey)
	}
}

func TestDeepMerge_Bedrock_EnabledFalseIsExplicit(t *testing.T) {
	// User sets Enabled=false — this is an explicit override even if string fields are empty.
	teamBedrock := &agentapiBedrockJSON{
		Enabled:     true,
		Model:       "claude-3",
		AccessKeyID: "AKID",
	}
	userBedrock := &agentapiBedrockJSON{
		Enabled: false, // explicit disable
		// other fields empty
	}
	team := makeSettings("bedrock", "", nil, nil, teamBedrock)
	user := makeSettings("bedrock", "", nil, nil, userBedrock)

	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{team, user})
	if result.Bedrock == nil {
		t.Fatal("expected Bedrock to be non-nil")
	}
	if result.Bedrock.Enabled {
		t.Error("expected Enabled=false (user's explicit override)")
	}
	// String fields from team should still be preserved (user's were empty)
	if result.Bedrock.Model != "claude-3" {
		t.Errorf("expected model=claude-3 from team, got %q", result.Bedrock.Model)
	}
	if result.Bedrock.AccessKeyID != "AKID" {
		t.Errorf("expected AKID from team, got %q", result.Bedrock.AccessKeyID)
	}
}

func TestDeepMerge_ThreeWay(t *testing.T) {
	// Simulate: base settings → team settings → user settings
	base := makeSettings("bedrock", "", map[string]string{"BASE": "b"}, nil, &agentapiBedrockJSON{
		Enabled:         true,
		Model:           "claude-base",
		AccessKeyID:     "BASE_AKID",
		SecretAccessKey: "BASE_SECRET",
	})
	team := makeSettings("bedrock", "", map[string]string{"BASE": "team-override", "TEAM": "t"}, nil, &agentapiBedrockJSON{
		Enabled: true,
		Model:   "claude-team",
		// AccessKeyID and SecretAccessKey inherit from base
	})
	user := makeSettings("oauth", "user-oauth-token", map[string]string{"USER": "u"}, nil, nil)

	result := deepMergeAgentapiSettings([]*agentapiSettingsJSON{base, team, user})

	if result.AuthMode != "oauth" {
		t.Errorf("expected oauth (user wins), got %q", result.AuthMode)
	}
	if result.ClaudeCodeOAuthToken != "user-oauth-token" {
		t.Errorf("expected user oauth token, got %q", result.ClaudeCodeOAuthToken)
	}
	if result.EnvVars["BASE"] != "team-override" {
		t.Errorf("expected team-override for BASE, got %q", result.EnvVars["BASE"])
	}
	if result.EnvVars["TEAM"] != "t" {
		t.Errorf("expected t for TEAM, got %q", result.EnvVars["TEAM"])
	}
	if result.EnvVars["USER"] != "u" {
		t.Errorf("expected u for USER, got %q", result.EnvVars["USER"])
	}
	// Bedrock: user has nil Bedrock, so team's Bedrock is kept
	if result.Bedrock == nil {
		t.Fatal("expected Bedrock to be non-nil (preserved from team)")
	}
	if result.Bedrock.Model != "claude-team" {
		t.Errorf("expected claude-team model, got %q", result.Bedrock.Model)
	}
	// base credentials should survive since team didn't set them
	if result.Bedrock.AccessKeyID != "BASE_AKID" {
		t.Errorf("expected BASE_AKID, got %q", result.Bedrock.AccessKeyID)
	}
	if result.Bedrock.SecretAccessKey != "BASE_SECRET" {
		t.Errorf("expected BASE_SECRET, got %q", result.Bedrock.SecretAccessKey)
	}
}
