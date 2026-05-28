package githubsync

import (
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"gopkg.in/yaml.v3"
)

func TestSessionProfileToRecordEncryptsAllEnvironmentValues(t *testing.T) {
	dek := []byte("12345678901234567890123456789012")
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")

	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(map[string]string{
		"PUBLIC_VALUE": "not-secret",
		"API_TOKEN":    "secret-token",
	})
	cfg.SetParams(&entities.SessionParams{
		Message:      "hello",
		GithubToken:  "ghp_secret",
		AgentType:    "codex",
		ManagerID:    "manager-1",
		CycleMessage: "cycle-me",
		RepoFullName: "owner/repo",
		Slack: &entities.SlackParams{
			Channel:            "channel-1",
			ThreadTS:           "thread-1",
			BotTokenSecretName: "bot-secret",
			BotTokenSecretKey:  "bot-key",
		},
		Sandbox: &entities.SandboxParams{
			AllowedDomains: []string{"allowed.example.com"},
			DeniedDomains:  []string{"denied.example.com"},
		},
	})
	profile.SetConfig(cfg)

	rec, err := sessionProfileToRecord(profile, dek)
	if err != nil {
		t.Fatalf("sessionProfileToRecord returned error: %v", err)
	}

	for key, value := range rec.Environment {
		if !IsEncrypted(value) {
			t.Fatalf("environment value %s was not encrypted: %q", key, value)
		}
		plain, err := DecryptField(dek, value)
		if err != nil {
			t.Fatalf("failed to decrypt environment value %s: %v", key, err)
		}
		if plain != cfg.Environment()[key] {
			t.Fatalf("environment value %s decrypted to %q, want %q", key, plain, cfg.Environment()[key])
		}
	}

	if rec.Params == nil {
		t.Fatal("params were not exported")
	}
	if !IsEncrypted(rec.Params.GitHubToken) {
		t.Fatalf("github token was not encrypted: %q", rec.Params.GitHubToken)
	}
	plainToken, err := DecryptField(dek, rec.Params.GitHubToken)
	if err != nil {
		t.Fatalf("failed to decrypt github token: %v", err)
	}
	if plainToken != "ghp_secret" {
		t.Fatalf("github token decrypted to %q, want %q", plainToken, "ghp_secret")
	}

	data, err := yaml.Marshal(rec)
	if err != nil {
		t.Fatalf("failed to marshal record: %v", err)
	}
	rendered := string(data)
	for _, plaintext := range []string{
		"not-secret",
		"secret-token",
		"ghp_secret",
	} {
		if strings.Contains(rendered, plaintext) {
			t.Fatalf("exported YAML contains plaintext %q:\n%s", plaintext, rendered)
		}
	}
	for _, plaintext := range []string{
		"hello",
		"codex",
		"manager-1",
		"cycle-me",
		"owner/repo",
		"channel-1",
		"thread-1",
		"bot-secret",
		"bot-key",
		"allowed.example.com",
		"denied.example.com",
	} {
		if !strings.Contains(rendered, plaintext) {
			t.Fatalf("exported YAML should keep non-secret param %q in plaintext:\n%s", plaintext, rendered)
		}
	}
	if !strings.Contains(rendered, "github_token:") || strings.Contains(rendered, "GithubToken:") {
		t.Fatalf("exported YAML does not use snake_case github_token:\n%s", rendered)
	}
}

func TestSessionProfileToRecordFailsInsteadOfWritingPlaintextEnv(t *testing.T) {
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")

	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(map[string]string{
		"PUBLIC_VALUE": "not-secret",
	})
	profile.SetConfig(cfg)

	rec, err := sessionProfileToRecord(profile, []byte("bad-key"))
	if err == nil {
		t.Fatalf("sessionProfileToRecord succeeded with invalid key; record=%+v", rec)
	}
	if len(rec.Environment) > 0 {
		t.Fatalf("record contains environment after encryption failure: %+v", rec.Environment)
	}
}
