package sessionsettings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMarshalYAML_FullSettings(t *testing.T) {
	settings := &SessionSettings{
		Session: SessionMeta{
			ID:        "test-session-123",
			UserID:    "user-456",
			Scope:     "user",
			TeamID:    "org/team",
			AgentType: "claude-agentapi",
			Oneshot:   true,
			Teams:     []string{"org/team1", "org/team2"},
		},
		Env: map[string]string{
			"AGENTAPI_PORT":       "9000",
			"AGENTAPI_SESSION_ID": "test-session-123",
			"HOME":                "/home/agentapi",
		},
		EnvFromSecrets: []EnvFromSecret{
			{Name: "github-secret", Optional: true},
			{Name: "agent-env-user456", Optional: true},
		},
		Claude: ClaudeConfig{
			ClaudeJSON: map[string]interface{}{
				"hasCompletedOnboarding":        true,
				"bypassPermissionsModeAccepted": true,
			},
			SettingsJSON: map[string]interface{}{
				"settings": map[string]interface{}{
					"mcp.enabled": true,
				},
			},
			MCPServers: map[string]interface{}{
				"test-server": map[string]interface{}{
					"type": "http",
					"url":  "https://example.com/mcp",
				},
			},
		},
		Repository: &RepositoryConfig{
			FullName: "org/repo",
			CloneDir: "/home/agentapi/workdir/repo",
		},
		InitialMessage: "Hello, start working",
		WebhookPayload: `{"action":"opened"}`,
		Startup: StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--port", "9000"},
		},
		Github: &GithubConfig{
			Token:            "ghp_test",
			ConfigSecretName: "github-config",
		},
	}

	data, err := MarshalYAML(settings)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Unmarshal to verify round-trip
	var unmarshaled SessionSettings
	err = UnmarshalYAML(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, settings.Session.ID, unmarshaled.Session.ID)
	assert.Equal(t, settings.Session.UserID, unmarshaled.Session.UserID)
	assert.Equal(t, settings.Session.Scope, unmarshaled.Session.Scope)
	assert.Equal(t, settings.Session.TeamID, unmarshaled.Session.TeamID)
	assert.Equal(t, settings.Session.AgentType, unmarshaled.Session.AgentType)
	assert.Equal(t, settings.Session.Oneshot, unmarshaled.Session.Oneshot)
	assert.Equal(t, settings.Session.Teams, unmarshaled.Session.Teams)

	assert.Equal(t, settings.Env["AGENTAPI_PORT"], unmarshaled.Env["AGENTAPI_PORT"])
	assert.Len(t, unmarshaled.EnvFromSecrets, 2)
	assert.Equal(t, "github-secret", unmarshaled.EnvFromSecrets[0].Name)

	assert.NotNil(t, unmarshaled.Repository)
	assert.Equal(t, "org/repo", unmarshaled.Repository.FullName)

	assert.Equal(t, "Hello, start working", unmarshaled.InitialMessage)
	assert.Equal(t, `{"action":"opened"}`, unmarshaled.WebhookPayload)

	assert.NotNil(t, unmarshaled.Github)
	assert.Equal(t, "ghp_test", unmarshaled.Github.Token)
}

func TestMarshalYAML_MinimalSettings(t *testing.T) {
	settings := &SessionSettings{
		Session: SessionMeta{
			ID:     "minimal-session",
			UserID: "user-789",
			Scope:  "user",
		},
	}

	data, err := MarshalYAML(settings)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Verify omitempty works - optional fields should not appear
	yamlStr := string(data)
	assert.Contains(t, yamlStr, "id: minimal-session")
	assert.Contains(t, yamlStr, "user_id: user-789")
	assert.NotContains(t, yamlStr, "team_id")
	assert.NotContains(t, yamlStr, "agent_type")
	assert.NotContains(t, yamlStr, "initial_message")
	assert.NotContains(t, yamlStr, "repository")

	// Unmarshal to verify
	var unmarshaled SessionSettings
	err = UnmarshalYAML(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, "minimal-session", unmarshaled.Session.ID)
	assert.Equal(t, "user-789", unmarshaled.Session.UserID)
	assert.Nil(t, unmarshaled.Repository)
	assert.Nil(t, unmarshaled.Github)
	assert.Empty(t, unmarshaled.InitialMessage)
}

func TestLoadSettings_ValidFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sessionsettings-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	yamlContent := `session:
  id: test-id
  user_id: test-user
  scope: user
env:
  TEST_VAR: test-value
`
	filePath := filepath.Join(tmpDir, "settings.yaml")
	err = os.WriteFile(filePath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	settings, err := LoadSettings(filePath)
	require.NoError(t, err)
	assert.NotNil(t, settings)
	assert.Equal(t, "test-id", settings.Session.ID)
	assert.Equal(t, "test-user", settings.Session.UserID)
	assert.Equal(t, "test-value", settings.Env["TEST_VAR"])
}

func TestLoadSettings_FileNotFound(t *testing.T) {
	_, err := LoadSettings("/nonexistent/path/settings.yaml")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read settings file")
}

func TestLoadSettings_InvalidYAML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sessionsettings-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	invalidYAML := `this is not: valid: yaml: content`
	filePath := filepath.Join(tmpDir, "invalid.yaml")
	err = os.WriteFile(filePath, []byte(invalidYAML), 0644)
	require.NoError(t, err)

	_, err = LoadSettings(filePath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse settings YAML")
}

// Helper function for unmarshal tests
func UnmarshalYAML(data []byte, v interface{}) error {
	return yaml.Unmarshal(data, v)
}
