package githubsync

import (
	"strings"
	"testing"

	importexport "github.com/takutakahashi/agentapi-proxy/pkg/import"
)

var testDEK = []byte("12345678901234567890123456789012") // 32-byte AES key

func buildTestResources() *importexport.TeamResources {
	return &importexport.TeamResources{
		Schedules: []importexport.ScheduleImport{
			{
				ID:   "sched-1",
				Name: "my-schedule",
				SessionConfig: importexport.SessionConfigImport{
					Environment: map[string]string{
						"API_TOKEN": "plain-token",
						"PUBLIC":    "public-value",
					},
					Params: &importexport.SessionParamsImport{
						GitHubToken: "ghp_plain",
					},
				},
			},
		},
		Webhooks: []importexport.WebhookImport{
			{
				ID:     "wh-1",
				Name:   "my-webhook",
				Secret: "webhook-secret",
				SessionConfig: &importexport.SessionConfigImport{
					Environment: map[string]string{"WH_ENV": "wh-value"},
				},
				Triggers: []importexport.WebhookTriggerImport{
					{
						Name: "trigger-1",
						SessionConfig: &importexport.SessionConfigImport{
							Environment: map[string]string{"TRIGGER_ENV": "trigger-value"},
						},
					},
				},
			},
		},
		Settings: &importexport.SettingsImport{
			ClaudeCodeOAuthToken: "oauth-token",
			EnvVars:              map[string]string{"MY_VAR": "my-value"},
			Bedrock: &importexport.BedrockSettingsImport{
				AccessKeyID:     "AKID123",
				SecretAccessKey: "secret-key",
			},
			MCPServers: map[string]*importexport.MCPServerImport{
				"mcp1": {
					Env:     map[string]string{"MCP_KEY": "mcp-secret"},
					Headers: map[string]string{"Authorization": "Bearer token"},
				},
			},
		},
		SessionProfiles: []importexport.SessionProfileImport{
			{
				ID:   "sp-1",
				Name: "default",
				Config: importexport.SessionProfileConfigImport{
					Environment: map[string]string{"SP_ENV": "sp-value"},
					Params: &importexport.SessionParamsImport{
						GitHubToken: "ghp_profile",
					},
				},
			},
		},
	}
}

func TestEncryptTaggedFields_EncryptsAllSensitiveFields(t *testing.T) {
	r := buildTestResources()

	if err := encryptTaggedFields(r, testDEK); err != nil {
		t.Fatalf("encryptTaggedFields: %v", err)
	}

	assertEncrypted := func(label, val string) {
		t.Helper()
		if !IsEncrypted(val) {
			t.Errorf("%s: expected encrypted value, got %q", label, val)
		}
	}
	assertMapEncrypted := func(label string, m map[string]string) {
		t.Helper()
		for k, v := range m {
			if !IsEncrypted(v) {
				t.Errorf("%s[%s]: expected encrypted value, got %q", label, k, v)
			}
		}
	}

	assertMapEncrypted("schedule env", r.Schedules[0].SessionConfig.Environment)
	assertEncrypted("schedule github_token", r.Schedules[0].SessionConfig.Params.GitHubToken)

	assertEncrypted("webhook secret", r.Webhooks[0].Secret)
	assertMapEncrypted("webhook env", r.Webhooks[0].SessionConfig.Environment)
	assertMapEncrypted("trigger env", r.Webhooks[0].Triggers[0].SessionConfig.Environment)

	assertEncrypted("oauth token", r.Settings.ClaudeCodeOAuthToken)
	assertMapEncrypted("settings env_vars", r.Settings.EnvVars)
	assertEncrypted("bedrock access_key_id", r.Settings.Bedrock.AccessKeyID)
	assertEncrypted("bedrock secret_access_key", r.Settings.Bedrock.SecretAccessKey)
	assertMapEncrypted("MCP env", r.Settings.MCPServers["mcp1"].Env)
	assertMapEncrypted("MCP headers", r.Settings.MCPServers["mcp1"].Headers)

	assertMapEncrypted("session profile env", r.SessionProfiles[0].Config.Environment)
	assertEncrypted("session profile github_token", r.SessionProfiles[0].Config.Params.GitHubToken)
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	original := buildTestResources()
	r := buildTestResources()

	if err := encryptTaggedFields(r, testDEK); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := decryptTaggedFields(r, testDEK); err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	assertEqual := func(label, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s: got %q, want %q", label, got, want)
		}
	}
	assertMapEqual := func(label string, got, want map[string]string) {
		t.Helper()
		for k, wv := range want {
			if got[k] != wv {
				t.Errorf("%s[%s]: got %q, want %q", label, k, got[k], wv)
			}
		}
	}

	assertMapEqual("schedule env", r.Schedules[0].SessionConfig.Environment, original.Schedules[0].SessionConfig.Environment)
	assertEqual("schedule github_token", r.Schedules[0].SessionConfig.Params.GitHubToken, original.Schedules[0].SessionConfig.Params.GitHubToken)

	assertEqual("webhook secret", r.Webhooks[0].Secret, original.Webhooks[0].Secret)
	assertMapEqual("webhook env", r.Webhooks[0].SessionConfig.Environment, original.Webhooks[0].SessionConfig.Environment)
	assertMapEqual("trigger env", r.Webhooks[0].Triggers[0].SessionConfig.Environment, original.Webhooks[0].Triggers[0].SessionConfig.Environment)

	assertEqual("oauth token", r.Settings.ClaudeCodeOAuthToken, original.Settings.ClaudeCodeOAuthToken)
	assertMapEqual("settings env_vars", r.Settings.EnvVars, original.Settings.EnvVars)
	assertEqual("bedrock access_key_id", r.Settings.Bedrock.AccessKeyID, original.Settings.Bedrock.AccessKeyID)
	assertEqual("bedrock secret_access_key", r.Settings.Bedrock.SecretAccessKey, original.Settings.Bedrock.SecretAccessKey)
	assertMapEqual("MCP env", r.Settings.MCPServers["mcp1"].Env, original.Settings.MCPServers["mcp1"].Env)
	assertMapEqual("MCP headers", r.Settings.MCPServers["mcp1"].Headers, original.Settings.MCPServers["mcp1"].Headers)

	assertMapEqual("session profile env", r.SessionProfiles[0].Config.Environment, original.SessionProfiles[0].Config.Environment)
	assertEqual("session profile github_token", r.SessionProfiles[0].Config.Params.GitHubToken, original.SessionProfiles[0].Config.Params.GitHubToken)
}

func TestClearCompanionFields(t *testing.T) {
	r := buildTestResources()
	// Populate companion fields to verify they get cleared.
	dummy := &importexport.EncryptedSecretData{Algorithm: "AES-256-GCM", Version: "v1"}
	r.Schedules[0].SessionConfig.EnvironmentEncrypted = map[string]*importexport.EncryptedSecretData{"API_TOKEN": dummy}
	r.Schedules[0].SessionConfig.Params.GitHubTokenEncrypted = dummy
	r.Webhooks[0].SecretEncrypted = dummy
	r.Webhooks[0].SessionConfig.EnvironmentEncrypted = map[string]*importexport.EncryptedSecretData{"WH_ENV": dummy}
	r.Webhooks[0].Triggers[0].SessionConfig.EnvironmentEncrypted = map[string]*importexport.EncryptedSecretData{"TRIGGER_ENV": dummy}
	r.Settings.ClaudeCodeOAuthTokenEncrypted = dummy
	r.Settings.EnvVarsEncrypted = map[string]*importexport.EncryptedSecretData{"MY_VAR": dummy}
	r.Settings.Bedrock.AccessKeyIDEncrypted = dummy
	r.Settings.Bedrock.SecretAccessKeyEncrypted = dummy
	r.Settings.MCPServers["mcp1"].EnvEncrypted = map[string]*importexport.EncryptedSecretData{"MCP_KEY": dummy}
	r.Settings.MCPServers["mcp1"].HeadersEncrypted = map[string]*importexport.EncryptedSecretData{"Authorization": dummy}
	r.SessionProfiles[0].Config.EnvironmentEncrypted = map[string]*importexport.EncryptedSecretData{"SP_ENV": dummy}
	r.SessionProfiles[0].Config.Params.GitHubTokenEncrypted = dummy

	clearCompanionFields(r)

	if r.Schedules[0].SessionConfig.EnvironmentEncrypted != nil {
		t.Error("schedule EnvironmentEncrypted should be nil")
	}
	if r.Schedules[0].SessionConfig.Params.GitHubTokenEncrypted != nil {
		t.Error("schedule GitHubTokenEncrypted should be nil")
	}
	if r.Webhooks[0].SecretEncrypted != nil {
		t.Error("webhook SecretEncrypted should be nil")
	}
	if r.Webhooks[0].SessionConfig.EnvironmentEncrypted != nil {
		t.Error("webhook SessionConfig.EnvironmentEncrypted should be nil")
	}
	if r.Webhooks[0].Triggers[0].SessionConfig.EnvironmentEncrypted != nil {
		t.Error("trigger EnvironmentEncrypted should be nil")
	}
	if r.Settings.ClaudeCodeOAuthTokenEncrypted != nil {
		t.Error("Settings.ClaudeCodeOAuthTokenEncrypted should be nil")
	}
	if r.Settings.EnvVarsEncrypted != nil {
		t.Error("Settings.EnvVarsEncrypted should be nil")
	}
	if r.Settings.Bedrock.AccessKeyIDEncrypted != nil {
		t.Error("Bedrock.AccessKeyIDEncrypted should be nil")
	}
	if r.Settings.Bedrock.SecretAccessKeyEncrypted != nil {
		t.Error("Bedrock.SecretAccessKeyEncrypted should be nil")
	}
	if r.Settings.MCPServers["mcp1"].EnvEncrypted != nil {
		t.Error("MCPServer.EnvEncrypted should be nil")
	}
	if r.Settings.MCPServers["mcp1"].HeadersEncrypted != nil {
		t.Error("MCPServer.HeadersEncrypted should be nil")
	}
	if r.SessionProfiles[0].Config.EnvironmentEncrypted != nil {
		t.Error("SessionProfile.EnvironmentEncrypted should be nil")
	}
	if r.SessionProfiles[0].Config.Params.GitHubTokenEncrypted != nil {
		t.Error("SessionProfile.Params.GitHubTokenEncrypted should be nil")
	}
}

func TestEncryptTaggedFields_IdempotentOnAlreadyEncryptedValues(t *testing.T) {
	r := buildTestResources()

	// Encrypt twice; values should not change on second pass.
	if err := encryptTaggedFields(r, testDEK); err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	snapshot := r.Settings.ClaudeCodeOAuthToken

	if err := encryptTaggedFields(r, testDEK); err != nil {
		t.Fatalf("second encrypt: %v", err)
	}
	if r.Settings.ClaudeCodeOAuthToken != snapshot {
		t.Errorf("double-encrypt changed the value: %q vs %q", r.Settings.ClaudeCodeOAuthToken, snapshot)
	}
}

func TestDecryptTaggedFields_PassthroughPlaintext(t *testing.T) {
	r := buildTestResources()

	// Decrypting plaintext should leave values unchanged.
	if err := decryptTaggedFields(r, testDEK); err != nil {
		t.Fatalf("decryptTaggedFields: %v", err)
	}
	if r.Settings.ClaudeCodeOAuthToken != "oauth-token" {
		t.Errorf("plaintext was modified: %q", r.Settings.ClaudeCodeOAuthToken)
	}
}

func TestEncryptTaggedFields_EmptyFieldsSkipped(t *testing.T) {
	r := &importexport.TeamResources{
		Settings: &importexport.SettingsImport{
			ClaudeCodeOAuthToken: "",
		},
	}
	if err := encryptTaggedFields(r, testDEK); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Settings.ClaudeCodeOAuthToken != "" {
		t.Errorf("empty field was modified: %q", r.Settings.ClaudeCodeOAuthToken)
	}
}

func TestEncryptedValuesHaveEncPrefix(t *testing.T) {
	r := buildTestResources()
	if err := encryptTaggedFields(r, testDEK); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !strings.HasPrefix(r.Settings.ClaudeCodeOAuthToken, encPrefix) {
		t.Errorf("encrypted value missing enc prefix: %q", r.Settings.ClaudeCodeOAuthToken)
	}
}
