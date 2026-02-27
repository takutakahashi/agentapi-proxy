package settingspatch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromJSON_DirectUnmarshal(t *testing.T) {
	t.Run("absent auth_mode becomes empty string (inherit)", func(t *testing.T) {
		data := []byte(`{"name":"test","auth_mode":""}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		assert.Equal(t, "", patch.AuthMode, "empty auth_mode should remain empty string")
	})

	t.Run("non-empty auth_mode is read directly", func(t *testing.T) {
		data := []byte(`{"name":"test","auth_mode":"bedrock"}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		assert.Equal(t, "bedrock", patch.AuthMode)
	})

	t.Run("enabled_plugins is read directly", func(t *testing.T) {
		data := []byte(`{"enabled_plugins":["plugin-a","plugin-b"]}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		assert.Equal(t, []string{"plugin-a", "plugin-b"}, patch.EnabledPlugins)
	})

	t.Run("env_vars string map is read directly", func(t *testing.T) {
		data := []byte(`{"env_vars":{"FOO":"bar","BAZ":"qux"}}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		assert.Equal(t, "bar", patch.EnvVars["FOO"])
		assert.Equal(t, "qux", patch.EnvVars["BAZ"])
	})

	t.Run("bedrock fields are read directly (enabled is preserved for round-trip)", func(t *testing.T) {
		data := []byte(`{"bedrock":{"enabled":true,"model":"claude-3","access_key_id":"AKIA"}}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		require.NotNil(t, patch.Bedrock)
		assert.True(t, patch.Bedrock.Enabled, "enabled:true must be preserved")
		assert.Equal(t, "claude-3", patch.Bedrock.Model)
		assert.Equal(t, "AKIA", patch.Bedrock.AccessKeyID)
		assert.Equal(t, "", patch.Bedrock.SecretAccessKey, "absent secret key should be empty string")
	})

	t.Run("mcp servers are read directly", func(t *testing.T) {
		data := []byte(`{"mcp_servers":{"my-server":{"type":"http","url":"http://localhost"}}}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		srv := patch.MCPServers["my-server"]
		require.NotNil(t, srv)
		assert.Equal(t, "http", srv.Type)
		assert.Equal(t, "http://localhost", srv.URL)
	})

	t.Run("full storage format round-trip", func(t *testing.T) {
		// Simulate what's stored in Kubernetes Secret today
		legacy := map[string]interface{}{
			"name":      "team-backend",
			"auth_mode": "bedrock",
			"bedrock": map[string]interface{}{
				"enabled":           true, // silently ignored
				"model":             "claude-3-5-sonnet-20241022",
				"access_key_id":     "AKIAIOSFODNN7EXAMPLE",
				"secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			},
			"mcp_servers": map[string]interface{}{
				"github": map[string]interface{}{
					"type": "http",
					"url":  "http://github-mcp:8080",
				},
			},
			"marketplaces": map[string]interface{}{
				"official": map[string]interface{}{
					"url": "https://github.com/org/plugins",
				},
			},
			"enabled_plugins": []string{"commit@official"},
			"env_vars": map[string]interface{}{
				"MY_CUSTOM_VAR": "my-value",
			},
		}

		data, err := json.Marshal(legacy)
		require.NoError(t, err)

		patch, err := FromJSON(data)
		require.NoError(t, err)

		assert.Equal(t, "bedrock", patch.AuthMode)
		assert.True(t, patch.Bedrock.Enabled, "enabled:true must be preserved in round-trip")
		assert.Equal(t, "claude-3-5-sonnet-20241022", patch.Bedrock.Model)
		assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", patch.Bedrock.AccessKeyID)
		assert.Equal(t, []string{"commit@official"}, patch.EnabledPlugins)
		assert.Equal(t, "my-value", patch.EnvVars["MY_CUSTOM_VAR"])
		assert.Equal(t, "http://github-mcp:8080", patch.MCPServers["github"].URL)
		assert.Equal(t, "https://github.com/org/plugins", patch.Marketplaces["official"].URL)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		_, err := FromJSON([]byte(`{invalid`))
		assert.Error(t, err)
	})
}
