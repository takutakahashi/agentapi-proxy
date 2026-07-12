package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

type fakeSettingsRepository struct {
	settings map[string]*entities.Settings
}

func (r *fakeSettingsRepository) Save(ctx context.Context, settings *entities.Settings) error {
	r.settings[settings.Name()] = settings
	return nil
}

func (r *fakeSettingsRepository) FindByName(ctx context.Context, name string) (*entities.Settings, error) {
	settings, ok := r.settings[name]
	if !ok {
		return nil, fmt.Errorf("settings not found: %s", name)
	}
	return settings, nil
}

func (r *fakeSettingsRepository) Delete(ctx context.Context, name string) error {
	delete(r.settings, name)
	return nil
}

func (r *fakeSettingsRepository) Exists(ctx context.Context, name string) (bool, error) {
	_, ok := r.settings[name]
	return ok, nil
}

func (r *fakeSettingsRepository) List(ctx context.Context) ([]*entities.Settings, error) {
	result := make([]*entities.Settings, 0, len(r.settings))
	for _, settings := range r.settings {
		result = append(result, settings)
	}
	return result, nil
}

func TestBuildSessionSettings_TeamSettingsUsesRepositoryEnvVars(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:     "test-ns",
			Image:         "test-image:latest",
			BasePort:      9000,
			PVCEnabled:    boolPtrForTest(false),
			CPURequest:    "100m",
			CPULimit:      "1",
			MemoryRequest: "128Mi",
			MemoryLimit:   "512Mi",
		},
	}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"

	teamSettings := entities.NewSettings("org/team-a")
	teamSettings.SetEnvVars(map[string]string{"SECRET_TOKEN": "decrypted-secret"})
	manager.SetSettingsRepository(&fakeSettingsRepository{
		settings: map[string]*entities.Settings{
			"org/team-a": teamSettings,
		},
	})

	// This is the shape written by KubernetesSettingsRepository when env vars are
	// encrypted. The session manager must not rely on this raw JSON for team/user
	// settings because SettingsPatch intentionally has no decryptor.
	_, err = k8sClient.CoreV1().Secrets("test-ns").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-settings-org-team-a", Namespace: "test-ns"},
		Data: map[string][]byte{
			"settings.json": []byte(`{
				"name": "org/team-a",
				"encrypted_env_vars": {
					"SECRET_TOKEN": {
						"v": "ciphertext",
						"alg": "aes-256-gcm",
						"kid": "sha256:test",
						"at": "2026-06-10T00:00:00Z",
						"ver": "v1"
					}
				}
			}`),
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("failed to create settings secret: %v", err)
	}

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "test-user"},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)
	req := &entities.RunServerRequest{
		UserID: "test-user",
		Scope:  entities.ScopeTeam,
		TeamID: "org/team-a",
		Environment: map[string]string{
			"PI_DEFAULT_PROVIDER": "ollama-cloud",
			"PI_DEFAULT_MODEL":    "glm-5:cloud",
		},
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if got := settings.Env["SECRET_TOKEN"]; got != "decrypted-secret" {
		t.Fatalf("SECRET_TOKEN = %q, want decrypted-secret", got)
	}
	if got := settings.Pi.SettingsJSON["defaultProvider"]; got != "ollama-cloud" {
		t.Fatalf("defaultProvider = %v", got)
	}
	if got := settings.Pi.SettingsJSON["defaultModel"]; got != "glm-5:cloud" {
		t.Fatalf("defaultModel = %v", got)
	}
}

func TestBuildSessionSettings_PiOllamaConfiguresCloudProvider(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:     "test-ns",
			Image:         "test-image:latest",
			BasePort:      9000,
			PVCEnabled:    boolPtrForTest(false),
			CPURequest:    "100m",
			CPULimit:      "1",
			MemoryRequest: "128Mi",
			MemoryLimit:   "512Mi",
		},
	}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "test-user"},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)
	req := &entities.RunServerRequest{
		UserID:    "test-user",
		AgentType: "pi-ollama",
		Environment: map[string]string{
			"OPENAI_API_KEY":            "openai-key",
			"PI_DEFAULT_PROVIDER":       "ollama-cloud",
			"PI_DEFAULT_MODEL":          "qwen3-coder",
			"PI_DEFAULT_THINKING_LEVEL": "high",
		},
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if _, ok := settings.Env["OLLAMA_API_KEY"]; ok {
		t.Fatalf("OLLAMA_API_KEY should not be synthesized")
	}
	if got := settings.Env["PI_ACP_PI_COMMAND"]; got != piOllamaCommandPath {
		t.Fatalf("PI_ACP_PI_COMMAND = %q", got)
	}
	if got := settings.Pi.SettingsJSON["defaultProvider"]; got != "ollama-cloud" {
		t.Fatalf("defaultProvider = %v", got)
	}
	if got := settings.Pi.SettingsJSON["defaultModel"]; got != "qwen3-coder" {
		t.Fatalf("defaultModel = %v", got)
	}
	if got := settings.Pi.SettingsJSON["defaultThinkingLevel"]; got != "high" {
		t.Fatalf("defaultThinkingLevel = %v", got)
	}
	if settings.Startup.PreScript == "" {
		t.Fatalf("expected pi-ollama startup pre-script")
	}
	if !strings.Contains(settings.Startup.PreScript, "node_modules/pi-ollama-cloud") {
		t.Fatalf("expected pi-ollama pre-script to skip install when package is baked into the image")
	}
	if !strings.Contains(settings.Startup.PreScript, "node_modules/pi-mcp-adapter") {
		t.Fatalf("expected pi-ollama pre-script to skip pi-mcp-adapter install when package is baked into the image")
	}
}

func TestBuildPiSettingsJSON(t *testing.T) {
	got := buildPiSettingsJSON(map[string]string{
		"PI_DEFAULT_PROVIDER":       "ollama-cloud",
		"PI_DEFAULT_MODEL":          "qwen3-coder",
		"PI_DEFAULT_THINKING_LEVEL": " high ",
		"UNRELATED":                 "ignored",
	})

	if got["defaultProvider"] != "ollama-cloud" {
		t.Fatalf("defaultProvider = %v", got["defaultProvider"])
	}
	if got["defaultModel"] != "qwen3-coder" {
		t.Fatalf("defaultModel = %v", got["defaultModel"])
	}
	if got["defaultThinkingLevel"] != "high" {
		t.Fatalf("defaultThinkingLevel = %v", got["defaultThinkingLevel"])
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 Pi settings, got %d", len(got))
	}
}

func TestBuildPiSettingsJSONSkipsEmptyValues(t *testing.T) {
	got := buildPiSettingsJSON(map[string]string{
		"PI_DEFAULT_PROVIDER": " ",
		"PI_DEFAULT_MODEL":    "qwen3-coder",
	})

	if len(got) != 1 || got["defaultModel"] != "qwen3-coder" {
		t.Fatalf("unexpected Pi settings: %#v", got)
	}
}
