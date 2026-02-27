package settingspatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApply_ScalarFields(t *testing.T) {
	t.Run("nil higher fields inherit from base", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode:   ptr("bedrock"),
			OAuthToken: ptr("base-token"),
		}
		higher := SettingsPatch{} // all nil

		result := Apply(base, higher)
		assert.Equal(t, ptr("bedrock"), result.AuthMode)
		assert.Equal(t, ptr("base-token"), result.OAuthToken)
	})

	t.Run("non-nil higher fields override base", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode:   ptr("bedrock"),
			OAuthToken: ptr("base-token"),
		}
		higher := SettingsPatch{
			AuthMode:   ptr("oauth"),
			OAuthToken: ptr("higher-token"),
		}

		result := Apply(base, higher)
		assert.Equal(t, ptr("oauth"), result.AuthMode)
		assert.Equal(t, ptr("higher-token"), result.OAuthToken)
	})

	t.Run("base nil, higher non-nil", func(t *testing.T) {
		base := SettingsPatch{}
		higher := SettingsPatch{
			AuthMode: ptr("oauth"),
		}

		result := Apply(base, higher)
		assert.Equal(t, ptr("oauth"), result.AuthMode)
	})
}

func TestApply_BedrockPatch(t *testing.T) {
	t.Run("nil higher inherits base bedrock", func(t *testing.T) {
		base := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model: ptr("claude-3"),
			},
		}
		higher := SettingsPatch{}

		result := Apply(base, higher)
		assert.Equal(t, ptr("claude-3"), result.Bedrock.Model)
	})

	t.Run("higher overrides individual bedrock fields", func(t *testing.T) {
		base := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model:       ptr("claude-3"),
				AccessKeyID: ptr("AKIABASE"),
				Profile:     ptr("default"),
			},
		}
		higher := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model: ptr("claude-3-5"), // only override model
			},
		}

		result := Apply(base, higher)
		assert.Equal(t, ptr("claude-3-5"), result.Bedrock.Model)
		assert.Equal(t, ptr("AKIABASE"), result.Bedrock.AccessKeyID, "base AccessKeyID should be inherited")
		assert.Equal(t, ptr("default"), result.Bedrock.Profile, "base Profile should be inherited")
	})

	t.Run("base nil bedrock, higher sets it", func(t *testing.T) {
		base := SettingsPatch{}
		higher := SettingsPatch{
			Bedrock: &BedrockPatch{Model: ptr("claude-3")},
		}

		result := Apply(base, higher)
		assert.NotNil(t, result.Bedrock)
		assert.Equal(t, ptr("claude-3"), result.Bedrock.Model)
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

	t.Run("env vars merge correctly", func(t *testing.T) {
		base := SettingsPatch{
			EnvVars: map[string]*string{
				"FOO": ptr("foo"),
				"BAR": ptr("bar"),
			},
		}
		higher := SettingsPatch{
			EnvVars: map[string]*string{
				"BAR": ptr("bar2"), // override
				"BAZ": ptr("baz"),  // add
				"FOO": nil,         // delete
			},
		}

		result := Apply(base, higher)
		_, fooExists := result.EnvVars["FOO"]
		assert.False(t, fooExists, "FOO should be deleted")
		assert.Equal(t, ptr("bar2"), result.EnvVars["BAR"])
		assert.Equal(t, ptr("baz"), result.EnvVars["BAZ"])
	})
}

func TestApply_Plugins(t *testing.T) {
	t.Run("plugins are accumulated via union", func(t *testing.T) {
		base := SettingsPatch{
			AddPlugins: []string{"plugin-a", "plugin-b"},
		}
		higher := SettingsPatch{
			AddPlugins: []string{"plugin-b", "plugin-c"}, // plugin-b is in both
		}

		result := Apply(base, higher)
		assert.Equal(t, []string{"plugin-a", "plugin-b", "plugin-c"}, result.AddPlugins)
	})

	t.Run("remove plugins accumulated across layers", func(t *testing.T) {
		base := SettingsPatch{
			RemovePlugins: []string{"plugin-x"},
		}
		higher := SettingsPatch{
			RemovePlugins: []string{"plugin-y"},
		}

		result := Apply(base, higher)
		assert.ElementsMatch(t, []string{"plugin-x", "plugin-y"}, result.RemovePlugins)
	})
}

func TestResolve(t *testing.T) {
	t.Run("empty layers returns empty patch", func(t *testing.T) {
		result := Resolve()
		assert.Nil(t, result.AuthMode)
		assert.Nil(t, result.OAuthToken)
	})

	t.Run("single layer is returned as-is", func(t *testing.T) {
		layer := SettingsPatch{AuthMode: ptr("oauth")}
		result := Resolve(layer)
		assert.Equal(t, ptr("oauth"), result.AuthMode)
	})

	t.Run("base → team → user priority chain", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode: ptr("oauth"),
			MCPServers: map[string]*MCPServerPatch{
				"base-server": {Type: "http", URL: "http://base"},
			},
			AddPlugins: []string{"plugin-base"},
		}
		team := SettingsPatch{
			AuthMode: ptr("bedrock"),
			Bedrock: &BedrockPatch{
				Model:       ptr("claude-3"),
				AccessKeyID: ptr("AKIATEAM"),
			},
			MCPServers: map[string]*MCPServerPatch{
				"team-server": {Type: "stdio", Command: "team-cmd"},
			},
			AddPlugins: []string{"plugin-team"},
		}
		user := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model: ptr("claude-3-5"), // only override model
			},
			MCPServers: map[string]*MCPServerPatch{
				"base-server": nil, // delete base server
			},
			AddPlugins: []string{"plugin-user"},
		}

		result := Resolve(base, team, user)

		// AuthMode: team set bedrock, user didn't change it → bedrock
		assert.Equal(t, ptr("bedrock"), result.AuthMode)

		// Bedrock: team set AccessKeyID, user only changed model
		assert.Equal(t, ptr("claude-3-5"), result.Bedrock.Model)
		assert.Equal(t, ptr("AKIATEAM"), result.Bedrock.AccessKeyID, "team AccessKeyID should be inherited")

		// MCP servers: base-server deleted by user, team-server present
		_, baseExists := result.MCPServers["base-server"]
		assert.False(t, baseExists)
		assert.NotNil(t, result.MCPServers["team-server"])

		// Plugins: union of all
		assert.Equal(t, []string{"plugin-base", "plugin-team", "plugin-user"}, result.AddPlugins)
	})

	t.Run("auth_mode nil does not override lower layer (legacy behavior)", func(t *testing.T) {
		// team sets bedrock, user does not set auth_mode at all (nil)
		team := SettingsPatch{
			AuthMode: ptr("bedrock"),
		}
		user := SettingsPatch{
			// AuthMode is nil = "inherit from team"
		}

		result := Resolve(team, user)
		assert.Equal(t, ptr("bedrock"), result.AuthMode, "nil user AuthMode should inherit team's bedrock")
	})

	t.Run("associativity holds", func(t *testing.T) {
		a := SettingsPatch{AuthMode: ptr("bedrock"), AddPlugins: []string{"p1"}}
		b := SettingsPatch{OAuthToken: ptr("tok"), AddPlugins: []string{"p2"}}
		c := SettingsPatch{AuthMode: ptr("oauth"), AddPlugins: []string{"p3"}}

		r1 := Apply(Apply(a, b), c)
		r2 := Resolve(a, b, c)

		assert.Equal(t, r1.AuthMode, r2.AuthMode)
		assert.Equal(t, r1.OAuthToken, r2.OAuthToken)
		assert.Equal(t, r1.AddPlugins, r2.AddPlugins)
	})
}
