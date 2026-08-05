package settingspatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApply_ScalarFields(t *testing.T) {
	t.Run("empty higher fields inherit from base", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode:   "bedrock",
			OAuthToken: "base-token",
		}
		higher := SettingsPatch{} // all empty

		result := Apply(base, higher)
		assert.Equal(t, "bedrock", result.AuthMode)
		assert.Equal(t, "base-token", result.OAuthToken)
	})

	t.Run("non-empty higher fields override base", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode:   "bedrock",
			OAuthToken: "base-token",
		}
		higher := SettingsPatch{
			AuthMode:   "oauth",
			OAuthToken: "higher-token",
		}

		result := Apply(base, higher)
		assert.Equal(t, "oauth", result.AuthMode)
		assert.Equal(t, "higher-token", result.OAuthToken)
	})

	t.Run("base empty, higher non-empty", func(t *testing.T) {
		base := SettingsPatch{}
		higher := SettingsPatch{
			AuthMode: "oauth",
		}

		result := Apply(base, higher)
		assert.Equal(t, "oauth", result.AuthMode)
	})
}

func TestApply_BedrockPatch(t *testing.T) {
	t.Run("empty higher inherits base bedrock", func(t *testing.T) {
		base := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model: "claude-3",
			},
		}
		higher := SettingsPatch{}

		result := Apply(base, higher)
		assert.Equal(t, "claude-3", result.Bedrock.Model)
	})

	t.Run("higher overrides individual bedrock fields", func(t *testing.T) {
		base := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model:       "claude-3",
				AccessKeyID: "AKIABASE",
				Profile:     "default",
			},
		}
		higher := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model: "claude-3-5", // only override model
			},
		}

		result := Apply(base, higher)
		assert.Equal(t, "claude-3-5", result.Bedrock.Model)
		assert.Equal(t, "AKIABASE", result.Bedrock.AccessKeyID, "base AccessKeyID should be inherited")
		assert.Equal(t, "default", result.Bedrock.Profile, "base Profile should be inherited")
	})

	t.Run("base nil bedrock, higher sets it", func(t *testing.T) {
		base := SettingsPatch{}
		higher := SettingsPatch{
			Bedrock: &BedrockPatch{Model: "claude-3"},
		}

		result := Apply(base, higher)
		assert.NotNil(t, result.Bedrock)
		assert.Equal(t, "claude-3", result.Bedrock.Model)
	})
}

func TestApply_MapFields(t *testing.T) {
	t.Run("absent key in higher inherits from base", func(t *testing.T) {
		serverA := &MCPServerPatch{Type: "http", URL: "http://a"}
		base := SettingsPatch{
			MCPServers: map[string]*MCPServerPatch{"a": serverA},
		}
		higher := SettingsPatch{} // no MCPServers

		result := Apply(base, higher)
		assert.Equal(t, serverA, result.MCPServers["a"])
	})

	t.Run("non-nil value in higher overrides base", func(t *testing.T) {
		serverA1 := &MCPServerPatch{Type: "http", URL: "http://v1"}
		serverA2 := &MCPServerPatch{Type: "http", URL: "http://v2"}
		base := SettingsPatch{
			MCPServers: map[string]*MCPServerPatch{"a": serverA1},
		}
		higher := SettingsPatch{
			MCPServers: map[string]*MCPServerPatch{"a": serverA2},
		}

		result := Apply(base, higher)
		assert.Equal(t, "http://v2", result.MCPServers["a"].URL)
	})

	t.Run("nil value in higher deletes key from base", func(t *testing.T) {
		serverA := &MCPServerPatch{Type: "http", URL: "http://a"}
		base := SettingsPatch{
			MCPServers: map[string]*MCPServerPatch{"a": serverA},
		}
		higher := SettingsPatch{
			MCPServers: map[string]*MCPServerPatch{"a": nil}, // explicit delete
		}

		result := Apply(base, higher)
		_, exists := result.MCPServers["a"]
		assert.False(t, exists, "nil value should delete the key")
	})

	t.Run("env vars merge correctly (last wins)", func(t *testing.T) {
		base := SettingsPatch{
			EnvVars: map[string]string{
				"FOO": "foo",
				"BAR": "bar",
			},
		}
		higher := SettingsPatch{
			EnvVars: map[string]string{
				"BAR": "bar2", // override
				"BAZ": "baz",  // add
			},
		}

		result := Apply(base, higher)
		assert.Equal(t, "foo", result.EnvVars["FOO"], "FOO inherited from base")
		assert.Equal(t, "bar2", result.EnvVars["BAR"], "BAR overridden by higher")
		assert.Equal(t, "baz", result.EnvVars["BAZ"], "BAZ added by higher")
	})
}

func TestApply_Plugins(t *testing.T) {
	t.Run("plugins are accumulated via union", func(t *testing.T) {
		base := SettingsPatch{
			EnabledPlugins: []string{"plugin-a", "plugin-b"},
		}
		higher := SettingsPatch{
			EnabledPlugins: []string{"plugin-b", "plugin-c"}, // plugin-b is in both
		}

		result := Apply(base, higher)
		assert.Equal(t, []string{"plugin-a", "plugin-b", "plugin-c"}, result.EnabledPlugins)
	})
}

func TestResolve(t *testing.T) {
	t.Run("empty layers returns empty patch", func(t *testing.T) {
		result := Resolve()
		assert.Empty(t, result.AuthMode)
		assert.Empty(t, result.OAuthToken)
	})

	t.Run("single layer is returned as-is", func(t *testing.T) {
		layer := SettingsPatch{AuthMode: "oauth"}
		result := Resolve(layer)
		assert.Equal(t, "oauth", result.AuthMode)
	})

	t.Run("base → team → user priority chain", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode: "oauth",
			MCPServers: map[string]*MCPServerPatch{
				"base-server": {Type: "http", URL: "http://base"},
			},
			EnabledPlugins: []string{"plugin-base"},
		}
		team := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:       "claude-3",
				AccessKeyID: "AKIATEAM",
			},
			MCPServers: map[string]*MCPServerPatch{
				"team-server": {Type: "stdio", Command: "team-cmd"},
			},
			EnabledPlugins: []string{"plugin-team"},
		}
		user := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model: "claude-3-5", // only override model
			},
			MCPServers: map[string]*MCPServerPatch{
				"base-server": nil, // delete base server
			},
			EnabledPlugins: []string{"plugin-user"},
		}

		result := Resolve(base, team, user)

		assert.Equal(t, "bedrock", result.AuthMode)
		assert.Equal(t, "claude-3-5", result.Bedrock.Model)
		assert.Equal(t, "AKIATEAM", result.Bedrock.AccessKeyID)

		_, baseExists := result.MCPServers["base-server"]
		assert.False(t, baseExists)
		assert.NotNil(t, result.MCPServers["team-server"])

		assert.Equal(t, []string{"plugin-base", "plugin-team", "plugin-user"}, result.EnabledPlugins)
	})

	t.Run("auth_mode empty does not override lower layer (inherit semantics)", func(t *testing.T) {
		team := SettingsPatch{AuthMode: "bedrock"}
		user := SettingsPatch{} // AuthMode "" = inherit

		result := Resolve(team, user)
		assert.Equal(t, "bedrock", result.AuthMode, "empty user AuthMode should inherit team's bedrock")
	})

	t.Run("associativity holds", func(t *testing.T) {
		a := SettingsPatch{AuthMode: "bedrock", EnabledPlugins: []string{"p1"}}
		b := SettingsPatch{OAuthToken: "tok", EnabledPlugins: []string{"p2"}}
		c := SettingsPatch{AuthMode: "oauth", EnabledPlugins: []string{"p3"}}

		r1 := Apply(Apply(a, b), c)
		r2 := Resolve(a, b, c)

		assert.Equal(t, r1.AuthMode, r2.AuthMode)
		assert.Equal(t, r1.OAuthToken, r2.OAuthToken)
		assert.Equal(t, r1.EnabledPlugins, r2.EnabledPlugins)
	})
}
