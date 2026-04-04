package services

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func newTestManagerForCycle(t *testing.T) *KubernetesSessionManager {
	t.Helper()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	}
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

func newTestSessionForCycle(userID string) *KubernetesSession {
	return NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: userID},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)
}

// TestBuildSessionSettings_CycleHookInjected verifies that a Stop hook with the
// cycle command is injected into SettingsJSON when CycleMessage is set.
func TestBuildSessionSettings_CycleHookInjected(t *testing.T) {
	manager := newTestManagerForCycle(t)
	session := newTestSessionForCycle("test-user")

	req := &entities.RunServerRequest{
		UserID:        "test-user",
		CycleMessage:  "Please continue the task",
		CycleMaxCount: 5,
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
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

	// Find the cycle hook command among the entries
	found := false
	for _, entry := range stopList {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		hooksInner, ok := entryMap["hooks"].([]map[string]interface{})
		if !ok {
			continue
		}
		for _, h := range hooksInner {
			cmd, _ := h["command"].(string)
			if strings.Contains(cmd, "cycle") && strings.Contains(cmd, "Please continue the task") && strings.Contains(cmd, "--max-count 5") {
				found = true
			}
		}
	}

	if !found {
		t.Errorf("Expected cycle command in Stop hooks, got: %+v", stopList)
	}
}

// TestBuildSessionSettings_CycleHookWithoutMaxCount verifies that --max-count is
// omitted when CycleMaxCount is 0 (unlimited).
func TestBuildSessionSettings_CycleHookWithoutMaxCount(t *testing.T) {
	manager := newTestManagerForCycle(t)
	session := newTestSessionForCycle("test-user")

	req := &entities.RunServerRequest{
		UserID:        "test-user",
		CycleMessage:  "Keep going",
		CycleMaxCount: 0, // unlimited
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if settings == nil {
		t.Fatal("Expected non-nil settings")
	}

	hooksRaw, ok := settings.Claude.SettingsJSON["hooks"]
	if !ok {
		t.Fatal("Expected 'hooks' key in SettingsJSON")
	}
	hooksMap := hooksRaw.(map[string]interface{})
	stopList := hooksMap["Stop"].([]interface{})

	found := false
	for _, entry := range stopList {
		entryMap, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		hooksInner, ok := entryMap["hooks"].([]map[string]interface{})
		if !ok {
			continue
		}
		for _, h := range hooksInner {
			cmd, _ := h["command"].(string)
			if strings.Contains(cmd, "cycle") && strings.Contains(cmd, "Keep going") {
				if strings.Contains(cmd, "--max-count") {
					t.Errorf("Expected no --max-count in command when CycleMaxCount=0, got: %s", cmd)
				}
				found = true
			}
		}
	}

	if !found {
		t.Errorf("Expected cycle command in Stop hooks, got: %+v", stopList)
	}
}

// TestBuildSessionSettings_NoCycleHookWhenMessageEmpty verifies that no cycle hook
// is injected when CycleMessage is empty.
func TestBuildSessionSettings_NoCycleHookWhenMessageEmpty(t *testing.T) {
	manager := newTestManagerForCycle(t)
	session := newTestSessionForCycle("test-user")

	req := &entities.RunServerRequest{
		UserID:        "test-user",
		CycleMessage:  "",
		CycleMaxCount: 5,
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if settings == nil {
		t.Fatal("Expected non-nil settings")
	}

	// Either no hooks key, or hooks without Stop containing cycle command
	if hooksRaw, ok := settings.Claude.SettingsJSON["hooks"]; ok {
		if hooksMap, ok := hooksRaw.(map[string]interface{}); ok {
			if stopHooks, ok := hooksMap["Stop"]; ok {
				stopList, _ := stopHooks.([]interface{})
				for _, entry := range stopList {
					entryMap, ok := entry.(map[string]interface{})
					if !ok {
						continue
					}
					hooksInner, ok := entryMap["hooks"].([]map[string]interface{})
					if !ok {
						continue
					}
					for _, h := range hooksInner {
						cmd, _ := h["command"].(string)
						if strings.Contains(cmd, "cycle") {
							t.Errorf("Unexpected cycle command in Stop hooks when CycleMessage is empty: %s", cmd)
						}
					}
				}
			}
		}
	}
}
