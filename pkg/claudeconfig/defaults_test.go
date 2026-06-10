package claudeconfig

import "testing"

func TestEnsureClaudeJSONDefaults(t *testing.T) {
	config := EnsureClaudeJSONDefaults(map[string]interface{}{
		"custom": "value",
		"projects": map[string]interface{}{
			"/home/agentapi/workdir": map[string]interface{}{
				"allowedTools": []interface{}{"Bash(git *)"},
			},
		},
	})

	if config["custom"] != "value" {
		t.Fatalf("expected custom key to be preserved")
	}
	if config["hasCompletedOnboarding"] != true {
		t.Fatalf("expected hasCompletedOnboarding")
	}
	if config["bypassPermissionsModeAccepted"] != true {
		t.Fatalf("expected bypassPermissionsModeAccepted")
	}

	projects := config["projects"].(map[string]interface{})
	for _, dir := range TrustedProjectDirs {
		project, ok := projects[dir].(map[string]interface{})
		if !ok {
			t.Fatalf("expected project defaults for %s", dir)
		}
		if project["hasTrustDialogAccepted"] != true {
			t.Fatalf("expected %s to be trusted", dir)
		}
		if project["hasCompletedProjectOnboarding"] != true {
			t.Fatalf("expected %s project onboarding complete", dir)
		}
	}
}

func TestEnsureSettingsJSONDefaults(t *testing.T) {
	settings := EnsureSettingsJSONDefaults(map[string]interface{}{
		"custom": "value",
	})

	if settings["custom"] != "value" {
		t.Fatalf("expected custom key to be preserved")
	}

	permissions := settings["permissions"].(map[string]interface{})
	if permissions["defaultMode"] != "bypassPermissions" {
		t.Fatalf("expected defaultMode bypassPermissions")
	}
	if permissions["skipDangerousModePermissionPrompt"] != true {
		t.Fatalf("expected skipDangerousModePermissionPrompt")
	}
	if settings["skipDangerousModePermissionPrompt"] != true {
		t.Fatalf("expected top-level skipDangerousModePermissionPrompt")
	}
}
