package settingspatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaterialize_AuthMode(t *testing.T) {
	t.Run("nil AuthMode: no auth env vars set", func(t *testing.T) {
		resolved := SettingsPatch{} // AuthMode is nil

		m, err := Materialize(resolved)
		require.NoError(t, err)
		_, ok := m.EnvVars["CLAUDE_CODE_USE_BEDROCK"]
		assert.False(t, ok, "should not set CLAUDE_CODE_USE_BEDROCK when AuthMode is nil")
	})

	t.Run("bedrock mode sets bedrock env vars", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode: ptr("bedrock"),
			Bedrock: &BedrockPatch{
				Model:           ptr("claude-3"),
				AccessKeyID:     ptr("AKIATEST"),
				SecretAccessKey: ptr("secret"),
				RoleARN:         ptr("arn:aws:iam::123:role/test"),
			},
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "claude-3", m.EnvVars["ANTHROPIC_MODEL"])
		assert.Equal(t, "AKIATEST", m.EnvVars["AWS_ACCESS_KEY_ID"])
		assert.Equal(t, "secret", m.EnvVars["AWS_SECRET_ACCESS_KEY"])
		assert.Equal(t, "arn:aws:iam::123:role/test", m.EnvVars["AWS_ROLE_ARN"])
	})

	t.Run("bedrock mode without credentials sets only CLAUDE_CODE_USE_BEDROCK", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode: ptr("bedrock"),
			// No Bedrock struct = credentials not set
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		_, hasKey := m.EnvVars["AWS_ACCESS_KEY_ID"]
		assert.False(t, hasKey, "should not set AWS credentials when Bedrock struct is nil")
	})

	t.Run("oauth mode clears all bedrock env vars", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode: ptr("oauth"),
			EnvVars: map[string]*string{
				"AWS_ACCESS_KEY_ID": ptr("leftover-key"), // would be overridden
			},
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.Equal(t, "0", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		_, hasKey := m.EnvVars["AWS_ACCESS_KEY_ID"]
		assert.False(t, hasKey, "oauth mode should clear AWS_ACCESS_KEY_ID")
		_, hasModel := m.EnvVars["ANTHROPIC_MODEL"]
		assert.False(t, hasModel, "oauth mode should clear ANTHROPIC_MODEL")
	})

	t.Run("oauth token is set when present", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode:   ptr("oauth"),
			OAuthToken: ptr("mytoken"),
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.Equal(t, "mytoken", m.EnvVars["CLAUDE_CODE_OAUTH_TOKEN"])
	})
}

func TestMaterialize_Plugins(t *testing.T) {
	t.Run("active plugins = add minus remove", func(t *testing.T) {
		resolved := Resolve(
			SettingsPatch{AddPlugins: []string{"plugin-a", "plugin-b", "plugin-c"}},
			SettingsPatch{RemovePlugins: []string{"plugin-b"}},
		)

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"plugin-a", "plugin-c"}, m.ActivePlugins)
	})

	t.Run("plugins appear in SettingsJSON", func(t *testing.T) {
		resolved := SettingsPatch{
			AddPlugins: []string{"commit@official"},
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		plugins, ok := m.SettingsJSON["enabled_plugins"]
		assert.True(t, ok)
		assert.Equal(t, []string{"commit@official"}, plugins)
	})
}

func TestMaterialize_Marketplaces(t *testing.T) {
	t.Run("marketplaces appear in SettingsJSON", func(t *testing.T) {
		resolved := SettingsPatch{
			Marketplaces: map[string]*MarketplacePatch{
				"official": {URL: "https://github.com/org/plugins"},
			},
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.NotNil(t, m.SettingsJSON["marketplaces"])
	})
}

func TestMaterialize_EnvVarValidation(t *testing.T) {
	t.Run("dangerous env vars are rejected", func(t *testing.T) {
		resolved := SettingsPatch{
			EnvVars: map[string]*string{
				"PATH":       ptr("/evil/path"),
				"SAFE_VAR":   ptr("safe-value"),
				"LD_PRELOAD": ptr("/evil/lib.so"),
			},
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.Equal(t, "safe-value", m.EnvVars["SAFE_VAR"])
		_, pathOk := m.EnvVars["PATH"]
		assert.False(t, pathOk, "PATH should be rejected")
		_, ldOk := m.EnvVars["LD_PRELOAD"]
		assert.False(t, ldOk, "LD_PRELOAD should be rejected")
	})

	t.Run("env vars with dangerous chars are rejected", func(t *testing.T) {
		resolved := SettingsPatch{
			EnvVars: map[string]*string{
				"SAFE_VAR":   ptr("safe"),
				"DANGER_VAR": ptr("value|with|pipes"),
			},
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.Equal(t, "safe", m.EnvVars["SAFE_VAR"])
		_, ok := m.EnvVars["DANGER_VAR"]
		assert.False(t, ok)
	})

	t.Run("deleted env vars (nil) are not output", func(t *testing.T) {
		resolved := SettingsPatch{
			EnvVars: map[string]*string{
				"FOO": nil, // explicitly deleted
			},
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		_, ok := m.EnvVars["FOO"]
		assert.False(t, ok)
	})
}

func TestMaterialize_MCPServers(t *testing.T) {
	t.Run("mcp servers are materialized", func(t *testing.T) {
		resolved := SettingsPatch{
			MCPServers: map[string]*MCPServerPatch{
				"my-server": {Type: "http", URL: "http://localhost:8080"},
			},
		}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.NotNil(t, m.MCPServers)
		assert.Contains(t, m.MCPServers, "my-server")
	})

	t.Run("no mcp servers: MCPServers is nil", func(t *testing.T) {
		resolved := SettingsPatch{}

		m, err := Materialize(resolved)
		require.NoError(t, err)
		assert.Nil(t, m.MCPServers)
	})
}

func TestMaterialize_FullPipeline(t *testing.T) {
	t.Run("base→team→user produces correct materialized settings", func(t *testing.T) {
		// base: oauth mode, base marketplace
		base := SettingsPatch{
			AuthMode:   ptr("oauth"),
			OAuthToken: ptr("base-token"),
			Marketplaces: map[string]*MarketplacePatch{
				"official": {URL: "https://github.com/official/plugins"},
			},
			AddPlugins: []string{"base-plugin"},
		}

		// team: switch to bedrock, add team MCP server
		team := SettingsPatch{
			AuthMode: ptr("bedrock"),
			Bedrock: &BedrockPatch{
				Model:           ptr("claude-3"),
				AccessKeyID:     ptr("AKIATEAM"),
				SecretAccessKey: ptr("teamsecret"),
			},
			MCPServers: map[string]*MCPServerPatch{
				"team-tool": {Type: "stdio", Command: "team-tool"},
			},
			AddPlugins: []string{"team-plugin"},
		}

		// user: only change model, add personal MCP server
		user := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model: ptr("claude-3-5"), // only upgrade model
			},
			MCPServers: map[string]*MCPServerPatch{
				"my-tool": {Type: "http", URL: "http://localhost:9090"},
			},
			AddPlugins: []string{"user-plugin"},
		}

		resolved := Resolve(base, team, user)
		m, err := Materialize(resolved)
		require.NoError(t, err)

		// Auth: team bedrock
		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		// Bedrock model: user's override
		assert.Equal(t, "claude-3-5", m.EnvVars["ANTHROPIC_MODEL"])
		// Bedrock credentials: team's
		assert.Equal(t, "AKIATEAM", m.EnvVars["AWS_ACCESS_KEY_ID"])
		// MCP servers: team-tool + my-tool
		assert.Contains(t, m.MCPServers, "team-tool")
		assert.Contains(t, m.MCPServers, "my-tool")
		// Plugins: union of all three
		assert.ElementsMatch(t, []string{"base-plugin", "team-plugin", "user-plugin"}, m.ActivePlugins)
		// Marketplace from base
		assert.NotNil(t, m.SettingsJSON["marketplaces"])
	})
}
