package settingspatch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthMode_AllPatterns covers all auth_mode transition scenarios.
// These are the patterns that matter most for correct session configuration.
func TestAuthMode_AllPatterns(t *testing.T) {

	// Pattern 1: empty AuthMode — nothing should be set
	t.Run("1: empty AuthMode → no bedrock env vars", func(t *testing.T) {
		resolved := SettingsPatch{} // AuthMode is ""
		m, err := Materialize(resolved)
		require.NoError(t, err)

		_, hasBedrock := m.EnvVars["CLAUDE_CODE_USE_BEDROCK"]
		assert.False(t, hasBedrock, "CLAUDE_CODE_USE_BEDROCK must not be set")
		_, hasModel := m.EnvVars["ANTHROPIC_MODEL"]
		assert.False(t, hasModel, "ANTHROPIC_MODEL must not be set")
		_, hasKey := m.EnvVars["AWS_ACCESS_KEY_ID"]
		assert.False(t, hasKey, "AWS_ACCESS_KEY_ID must not be set")
	})

	// Pattern 2: bedrock + full credentials
	t.Run("2: bedrock + full credentials → all AWS vars set", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:           "claude-3-5-sonnet",
				AccessKeyID:     "AKIATEST",
				SecretAccessKey: "secretkey",
				RoleARN:         "arn:aws:iam::123:role/test",
				Profile:         "myprofile",
			},
		}
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "claude-3-5-sonnet", m.EnvVars["ANTHROPIC_MODEL"])
		assert.Equal(t, "AKIATEST", m.EnvVars["AWS_ACCESS_KEY_ID"])
		assert.Equal(t, "secretkey", m.EnvVars["AWS_SECRET_ACCESS_KEY"])
		assert.Equal(t, "arn:aws:iam::123:role/test", m.EnvVars["AWS_ROLE_ARN"])
		assert.Equal(t, "myprofile", m.EnvVars["AWS_PROFILE"])

		// OAuth token must NOT be set
		_, hasOAuth := m.EnvVars["CLAUDE_CODE_OAUTH_TOKEN"]
		assert.False(t, hasOAuth, "oauth token must not be set in bedrock mode")
	})

	// Pattern 3: bedrock without credentials → only USE_BEDROCK flag
	t.Run("3: bedrock without credentials → CLAUDE_CODE_USE_BEDROCK=1 only", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode: "bedrock",
			// No Bedrock struct = relies on external IAM / instance profile
		}
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		_, hasKey := m.EnvVars["AWS_ACCESS_KEY_ID"]
		assert.False(t, hasKey)
		_, hasSecret := m.EnvVars["AWS_SECRET_ACCESS_KEY"]
		assert.False(t, hasSecret)
		_, hasModel := m.EnvVars["ANTHROPIC_MODEL"]
		assert.False(t, hasModel)
	})

	// Pattern 4: oauth mode → USE_BEDROCK=0, all AWS vars wiped
	t.Run("4: oauth → CLAUDE_CODE_USE_BEDROCK=0, AWS vars cleared", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode:   "oauth",
			OAuthToken: "mytoken",
		}
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "0", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "mytoken", m.EnvVars["CLAUDE_CODE_OAUTH_TOKEN"])
		_, hasKey := m.EnvVars["AWS_ACCESS_KEY_ID"]
		assert.False(t, hasKey)
		_, hasSecret := m.EnvVars["AWS_SECRET_ACCESS_KEY"]
		assert.False(t, hasSecret)
		_, hasModel := m.EnvVars["ANTHROPIC_MODEL"]
		assert.False(t, hasModel)
	})

	// Pattern 5: oauth with leftover bedrock env vars in EnvVars map → aws vars cleared
	t.Run("5: oauth overrides leftover bedrock env vars", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode: "oauth",
			EnvVars: map[string]string{
				// These would survive if oauth didn't clean up
				"AWS_ACCESS_KEY_ID":     "stale-key",
				"AWS_SECRET_ACCESS_KEY": "stale-secret",
				"ANTHROPIC_MODEL":       "stale-model",
			},
		}
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "0", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		_, hasKey := m.EnvVars["AWS_ACCESS_KEY_ID"]
		assert.False(t, hasKey, "oauth must wipe AWS_ACCESS_KEY_ID even if set in EnvVars")
		_, hasSecret := m.EnvVars["AWS_SECRET_ACCESS_KEY"]
		assert.False(t, hasSecret, "oauth must wipe AWS_SECRET_ACCESS_KEY even if set in EnvVars")
		_, hasModel := m.EnvVars["ANTHROPIC_MODEL"]
		assert.False(t, hasModel, "oauth must wipe ANTHROPIC_MODEL even if set in EnvVars")
	})

	// Pattern 6: base=oauth, team=bedrock → team wins (bedrock)
	t.Run("6: base=oauth, team=bedrock → bedrock", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode:   "oauth",
			OAuthToken: "base-token",
		}
		team := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:       "claude-3",
				AccessKeyID: "AKIATEAM",
			},
		}

		resolved := Resolve(base, team)
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "claude-3", m.EnvVars["ANTHROPIC_MODEL"])
		assert.Equal(t, "AKIATEAM", m.EnvVars["AWS_ACCESS_KEY_ID"])
		// OAuth token should still be present (not cleared by bedrock mode) but bedrock wins
		// CLAUDE_CODE_OAUTH_TOKEN comes from OAuthToken field, which was set at base
		// After merge: base.OAuthToken=base-token, team doesn't override → still set
		assert.Equal(t, "base-token", m.EnvVars["CLAUDE_CODE_OAUTH_TOKEN"])
	})

	// Pattern 7: base=bedrock, team="", user="" → base bedrock inherited
	t.Run("7: base=bedrock, team=\"\", user=\"\" → bedrock inherited", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:       "claude-base",
				AccessKeyID: "AKIABASE",
			},
		}
		team := SettingsPatch{} // "" AuthMode = inherit
		user := SettingsPatch{} // "" AuthMode = inherit

		resolved := Resolve(base, team, user)
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "claude-base", m.EnvVars["ANTHROPIC_MODEL"])
		assert.Equal(t, "AKIABASE", m.EnvVars["AWS_ACCESS_KEY_ID"])
	})

	// Pattern 8: base=bedrock, user=oauth → user wins (oauth)
	t.Run("8: base=bedrock, user=oauth → oauth", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:       "claude-base",
				AccessKeyID: "AKIABASE",
			},
		}
		user := SettingsPatch{
			AuthMode:   "oauth",
			OAuthToken: "user-token",
		}

		resolved := Resolve(base, user)
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "0", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "user-token", m.EnvVars["CLAUDE_CODE_OAUTH_TOKEN"])
		// AWS vars must be cleared even though base had bedrock credentials
		_, hasKey := m.EnvVars["AWS_ACCESS_KEY_ID"]
		assert.False(t, hasKey, "oauth must clear AWS_ACCESS_KEY_ID from base bedrock")
		_, hasModel := m.EnvVars["ANTHROPIC_MODEL"]
		assert.False(t, hasModel, "oauth must clear ANTHROPIC_MODEL from base bedrock")
	})

	// Pattern 9: team=bedrock, user="" → team bedrock inherited ("" = inherit)
	t.Run("9: team=bedrock, user=\"\" → bedrock inherited (empty string semantics)", func(t *testing.T) {
		team := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:       "claude-team",
				AccessKeyID: "AKIATEAM",
			},
		}
		user := SettingsPatch{
			// AuthMode is "" → "not set by user" = inherit team's
		}

		resolved := Resolve(team, user)
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"],
			"empty user AuthMode must NOT override team's bedrock")
		assert.Equal(t, "claude-team", m.EnvVars["ANTHROPIC_MODEL"])
		assert.Equal(t, "AKIATEAM", m.EnvVars["AWS_ACCESS_KEY_ID"])
	})

	// Pattern 10: team=bedrock, user=oauth → user wins
	t.Run("10: team=bedrock, user=oauth → oauth (user always wins)", func(t *testing.T) {
		team := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:       "claude-team",
				AccessKeyID: "AKIATEAM",
			},
		}
		user := SettingsPatch{
			AuthMode:   "oauth",
			OAuthToken: "user-oauth-token",
		}

		resolved := Resolve(team, user)
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "0", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "user-oauth-token", m.EnvVars["CLAUDE_CODE_OAUTH_TOKEN"])
		_, hasKey := m.EnvVars["AWS_ACCESS_KEY_ID"]
		assert.False(t, hasKey, "user oauth must clear team's AWS_ACCESS_KEY_ID")
		_, hasModel := m.EnvVars["ANTHROPIC_MODEL"]
		assert.False(t, hasModel, "user oauth must clear team's ANTHROPIC_MODEL")
	})

	// Pattern 11: base=oauth, team=bedrock, user="" → team wins (bedrock)
	t.Run("11: base=oauth, team=bedrock, user=\"\" → bedrock (team overrides base)", func(t *testing.T) {
		base := SettingsPatch{
			AuthMode:   "oauth",
			OAuthToken: "base-oauth",
		}
		team := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:       "claude-team",
				AccessKeyID: "AKIATEAM",
			},
		}
		user := SettingsPatch{} // "" = inherit

		resolved := Resolve(base, team, user)
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "claude-team", m.EnvVars["ANTHROPIC_MODEL"])
		assert.Equal(t, "AKIATEAM", m.EnvVars["AWS_ACCESS_KEY_ID"])
	})

	// Pattern 12: user overrides only bedrock model, credentials from team
	t.Run("12: user overrides bedrock model only, team credentials inherited", func(t *testing.T) {
		team := SettingsPatch{
			AuthMode: "bedrock",
			Bedrock: &BedrockPatch{
				Model:           "claude-3",
				AccessKeyID:     "AKIATEAM",
				SecretAccessKey: "teamsecret",
			},
		}
		user := SettingsPatch{
			Bedrock: &BedrockPatch{
				Model: "claude-3-5", // only upgrade model
			},
		}

		resolved := Resolve(team, user)
		m, err := Materialize(resolved)
		require.NoError(t, err)

		assert.Equal(t, "1", m.EnvVars["CLAUDE_CODE_USE_BEDROCK"])
		assert.Equal(t, "claude-3-5", m.EnvVars["ANTHROPIC_MODEL"], "user model wins")
		assert.Equal(t, "AKIATEAM", m.EnvVars["AWS_ACCESS_KEY_ID"], "team credentials inherited")
		assert.Equal(t, "teamsecret", m.EnvVars["AWS_SECRET_ACCESS_KEY"], "team credentials inherited")
	})

	// Pattern 13: unknown auth_mode → ignored, no crash
	t.Run("13: unknown auth_mode → ignored, no crash", func(t *testing.T) {
		resolved := SettingsPatch{
			AuthMode: "kerberos", // unknown
		}
		m, err := Materialize(resolved)
		require.NoError(t, err)

		_, hasBedrock := m.EnvVars["CLAUDE_CODE_USE_BEDROCK"]
		assert.False(t, hasBedrock, "unknown auth_mode should not set any auth env vars")
	})
}
