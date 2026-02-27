package settingspatch

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFromJSON_LegacyFormat(t *testing.T) {
	t.Run("empty auth_mode becomes nil AuthMode", func(t *testing.T) {
		data := []byte(`{"name":"test","auth_mode":""}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		assert.Nil(t, patch.AuthMode, "empty auth_mode should convert to nil")
	})

	t.Run("non-empty auth_mode is preserved", func(t *testing.T) {
		data := []byte(`{"name":"test","auth_mode":"bedrock"}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		assert.Equal(t, ptr("bedrock"), patch.AuthMode)
	})

	t.Run("enabled_plugins becomes AddPlugins", func(t *testing.T) {
		data := []byte(`{"enabled_plugins":["plugin-a","plugin-b"]}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		assert.Equal(t, []string{"plugin-a", "plugin-b"}, patch.AddPlugins)
		assert.Empty(t, patch.RemovePlugins)
	})

	t.Run("env_vars string map becomes *string map", func(t *testing.T) {
		data := []byte(`{"env_vars":{"FOO":"bar","BAZ":"qux"}}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		require.NotNil(t, patch.EnvVars["FOO"])
		assert.Equal(t, "bar", *patch.EnvVars["FOO"])
		require.NotNil(t, patch.EnvVars["BAZ"])
		assert.Equal(t, "qux", *patch.EnvVars["BAZ"])
	})

	t.Run("bedrock fields become pointers", func(t *testing.T) {
		data := []byte(`{"bedrock":{"enabled":true,"model":"claude-3","access_key_id":"AKIA"}}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		require.NotNil(t, patch.Bedrock)
		assert.Equal(t, ptr(true), patch.Bedrock.Enabled)
		assert.Equal(t, ptr("claude-3"), patch.Bedrock.Model)
		assert.Equal(t, ptr("AKIA"), patch.Bedrock.AccessKeyID)
		assert.Nil(t, patch.Bedrock.SecretAccessKey, "empty secret key should be nil")
	})

	t.Run("mcp servers are converted correctly", func(t *testing.T) {
		data := []byte(`{"mcp_servers":{"my-server":{"type":"http","url":"http://localhost"}}}`)
		patch, err := FromJSON(data)
		require.NoError(t, err)
		srv := patch.MCPServers["my-server"]
		require.NotNil(t, srv)
		assert.Equal(t, "http", srv.Type)
		assert.Equal(t, "http://localhost", srv.URL)
	})

	t.Run("full legacy settings.json round-trip", func(t *testing.T) {
		// Simulate what's stored in Kubernetes Secret today
		legacy := map[string]interface{}{
			"name":      "team-backend",
			"auth_mode": "bedrock",
			"bedrock": map[string]interface{}{
				"enabled":           true,
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

		assert.Equal(t, ptr("bedrock"), patch.AuthMode)
		assert.Equal(t, ptr("claude-3-5-sonnet-20241022"), patch.Bedrock.Model)
		assert.Equal(t, ptr("AKIAIOSFODNN7EXAMPLE"), patch.Bedrock.AccessKeyID)
		assert.Equal(t, []string{"commit@official"}, patch.AddPlugins)
		assert.Equal(t, ptr("my-value"), patch.EnvVars["MY_CUSTOM_VAR"])
		assert.Equal(t, "http://github-mcp:8080", patch.MCPServers["github"].URL)
		assert.Equal(t, "https://github.com/org/plugins", patch.Marketplaces["official"].URL)
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		_, err := FromJSON([]byte(`{invalid`))
		assert.Error(t, err)
	})
}
