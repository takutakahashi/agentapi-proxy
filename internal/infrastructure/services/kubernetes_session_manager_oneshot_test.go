package services

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func newTestManagerForOneshot(t *testing.T) *KubernetesSessionManager {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}
	k8sClient := fake.NewSimpleClientset(ns)
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
	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	manager.namespace = "test-ns"
	return manager
}

// TestBuildSessionSettings_OneshotHookInjected verifies that when req.Oneshot is true,
// the Stop hook for delete-session is included in the compiled settings.json.
func TestBuildSessionSettings_OneshotHookInjected(t *testing.T) {
	manager := newTestManagerForOneshot(t)
	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "test-user"},
		"agentapi-session-test-session",
		"agentapi-session-test-session-svc",
		"agentapi-session-test-session-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)

	// Create the oneshot settings secret (as done in CreateSession)
	if err := manager.createOneshotSettingsSecret(context.Background(), session); err != nil {
		t.Fatalf("Failed to create oneshot settings secret: %v", err)
	}

	req := &entities.RunServerRequest{
		UserID:  "test-user",
		Scope:   entities.ScopeUser,
		Oneshot: true,
	}

	settings, buildErr := manager.buildSessionSettings(context.Background(), session, req, nil, true)
	if buildErr != nil {
		t.Fatalf("buildSessionSettings() error = %v", buildErr)
	}
	if settings == nil {
		t.Fatal("Expected non-nil settings")
	}

	hooksRaw, ok := settings.Claude.SettingsJSON["hooks"]
	if !ok {
		t.Fatal("Expected 'hooks' key in SettingsJSON")
	}

	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected hooks to be map[string]interface{}, got %T", hooksRaw)
	}

	stopHooks, ok := hooksMap["Stop"]
	if !ok {
		t.Fatal("Expected 'Stop' hook in hooks map")
	}

	stopList, ok := stopHooks.([]interface{})
	if !ok {
		t.Fatalf("Expected Stop hooks to be []interface{}, got %T", stopHooks)
	}

	if len(stopList) == 0 {
		t.Fatal("Expected at least one Stop hook entry")
	}

	// Find the delete-session command among the entries
	found := false
	for _, entry := range stopList {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		hooksInner, ok := entryMap["hooks"].([]interface{})
		if !ok {
			continue
		}
		for _, h := range hooksInner {
			hMap, ok := h.(map[string]interface{})
			if !ok {
				continue
			}
			cmd, _ := hMap["command"].(string)
			if cmd == "agentapi-proxy client delete-session --confirm" {
				found = true
			}
		}
	}
	if !found {
		// Also check as []map[string]interface{} (for in-memory non-JSON-roundtripped maps)
		for _, entry := range stopList {
			entryMap, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			hooksInnerRaw := entryMap["hooks"]
			// Try both []interface{} and []map[string]interface{}
			if inner, ok := hooksInnerRaw.([]map[string]interface{}); ok {
				for _, h := range inner {
					cmd, _ := h["command"].(string)
					if cmd == "agentapi-proxy client delete-session --confirm" {
						found = true
					}
				}
			}
		}
	}

	if !found {
		t.Errorf("Expected delete-session command in Stop hooks, got: %+v", stopList)
	}
}

// TestOneshotSecretCreatedWithCorrectFormat verifies the oneshot settings secret
// has the correct JSON format that settingspatch can parse.
func TestOneshotSecretCreatedWithCorrectFormat(t *testing.T) {
	manager := newTestManagerForOneshot(t)
	session := NewKubernetesSession(
		"test-session2",
		&entities.RunServerRequest{UserID: "test-user"},
		"agentapi-session-test-session2",
		"agentapi-session-test-session2-svc",
		"agentapi-session-test-session2-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)

	if err := manager.createOneshotSettingsSecret(context.Background(), session); err != nil {
		t.Fatalf("Failed to create oneshot settings secret: %v", err)
	}

	// Read back the secret and verify its content
	secretName := "agentapi-session-test-session2-svc-oneshot-settings"
	secret, err := manager.client.CoreV1().Secrets("test-ns").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get oneshot settings secret: %v", err)
	}

	data, ok := secret.Data["settings.json"]
	if !ok {
		t.Fatal("Expected 'settings.json' key in secret data")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse settings.json: %v", err)
	}

	hooks, ok := parsed["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected 'hooks' in settings.json")
	}

	stop, ok := hooks["Stop"].([]interface{})
	if !ok {
		t.Fatalf("Expected 'Stop' in hooks as array, got %T", hooks["Stop"])
	}

	if len(stop) == 0 {
		t.Fatal("Expected at least one Stop hook entry")
	}

	entry, ok := stop[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected first Stop entry to be map, got %T", stop[0])
	}

	innerHooks, ok := entry["hooks"].([]interface{})
	if !ok {
		t.Fatalf("Expected 'hooks' in first Stop entry, got %T", entry["hooks"])
	}

	if len(innerHooks) == 0 {
		t.Fatal("Expected at least one inner hook")
	}

	hook, ok := innerHooks[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected first inner hook to be map, got %T", innerHooks[0])
	}

	if hook["command"] != "agentapi-proxy client delete-session --confirm" {
		t.Errorf("Expected delete-session command, got: %v", hook["command"])
	}
}
