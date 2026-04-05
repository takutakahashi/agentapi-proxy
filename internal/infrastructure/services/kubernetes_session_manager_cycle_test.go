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

	// Find the cycle hook command among the entries.
	// The message is no longer in the command (it's stored in CYCLE_ENABLED).
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
			if strings.Contains(cmd, "cycle") && strings.Contains(cmd, "--max-count 5") {
				// Must NOT contain the raw message text in the command line
				if strings.Contains(cmd, "Please continue the task") {
					t.Errorf("Cycle command must not contain the message text (should be in CYCLE_ENABLED): %s", cmd)
				}
				found = true
			}
		}
	}

	if !found {
		t.Errorf("Expected cycle command with --max-count 5 in Stop hooks, got: %+v", stopList)
	}

	// CYCLE_ENABLED must be present in settings.Files with the correct message content
	foundFile := false
	for _, f := range settings.Files {
		if f.Path == "/tmp/check/CYCLE_ENABLED" {
			if f.Content != "Please continue the task" {
				t.Errorf("Expected CYCLE_ENABLED content %q, got %q", "Please continue the task", f.Content)
			}
			foundFile = true
			break
		}
	}
	if !foundFile {
		t.Errorf("Expected /tmp/check/CYCLE_ENABLED in settings.Files, got: %+v", settings.Files)
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
			if strings.Contains(cmd, "cycle") {
				if strings.Contains(cmd, "--max-count") {
					t.Errorf("Expected no --max-count in command when CycleMaxCount=0, got: %s", cmd)
				}
				// Must NOT contain the message text in the command line
				if strings.Contains(cmd, "Keep going") {
					t.Errorf("Cycle command must not contain the message text: %s", cmd)
				}
				found = true
			}
		}
	}

	if !found {
		t.Errorf("Expected cycle command in Stop hooks, got: %+v", stopList)
	}

	// CYCLE_ENABLED must carry the message
	foundFile := false
	for _, f := range settings.Files {
		if f.Path == "/tmp/check/CYCLE_ENABLED" {
			if f.Content != "Keep going" {
				t.Errorf("Expected CYCLE_ENABLED content %q, got %q", "Keep going", f.Content)
			}
			foundFile = true
			break
		}
	}
	if !foundFile {
		t.Errorf("Expected /tmp/check/CYCLE_ENABLED in settings.Files, got: %+v", settings.Files)
	}
}

// TestBuildSessionSettings_CycleEnabledFileInjected verifies that /tmp/check/CYCLE_ENABLED
// is added to settings.Files when CycleMessage is set.
func TestBuildSessionSettings_CycleEnabledFileInjected(t *testing.T) {
	manager := newTestManagerForCycle(t)
	session := newTestSessionForCycle("test-user")

	req := &entities.RunServerRequest{
		UserID:       "test-user",
		CycleMessage: "Please continue the task",
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if settings == nil {
		t.Fatal("Expected non-nil settings")
	}

	found := false
	for _, f := range settings.Files {
		if f.Path == "/tmp/check/CYCLE_ENABLED" {
			if f.Content != "Please continue the task" {
				t.Errorf("Expected CYCLE_ENABLED content %q, got %q", "Please continue the task", f.Content)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected /tmp/check/CYCLE_ENABLED in settings.Files, got: %+v", settings.Files)
	}
}

// TestBuildSessionSettings_NoCycleEnabledFileWhenMessageEmpty verifies that
// /tmp/check/CYCLE_ENABLED is NOT added to settings.Files when CycleMessage is empty.
func TestBuildSessionSettings_NoCycleEnabledFileWhenMessageEmpty(t *testing.T) {
	manager := newTestManagerForCycle(t)
	session := newTestSessionForCycle("test-user")

	req := &entities.RunServerRequest{
		UserID:       "test-user",
		CycleMessage: "",
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if settings == nil {
		t.Fatal("Expected non-nil settings")
	}

	for _, f := range settings.Files {
		if f.Path == "/tmp/check/CYCLE_ENABLED" {
			t.Errorf("Unexpected /tmp/check/CYCLE_ENABLED in settings.Files when CycleMessage is empty")
		}
	}
}

// TestBuildSessionSettings_CycleHookAlwaysInjected verifies that the cycle Stop hook
// is injected even when CycleMessage is empty.  The hook is a no-op when
// /tmp/check/CYCLE_ENABLED does not exist, so registering it unconditionally allows
// cycling to be activated later without restarting the session.
func TestBuildSessionSettings_CycleHookAlwaysInjected(t *testing.T) {
	manager := newTestManagerForCycle(t)
	session := newTestSessionForCycle("test-user")

	req := &entities.RunServerRequest{
		UserID:        "test-user",
		CycleMessage:  "", // empty — hook must still be injected
		CycleMaxCount: 5,
	}

	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	if settings == nil {
		t.Fatal("Expected non-nil settings")
	}

	hooksRaw, ok := settings.Claude.SettingsJSON["hooks"]
	if !ok {
		t.Fatal("Expected 'hooks' key in SettingsJSON even when CycleMessage is empty")
	}

	hooksMap, ok := hooksRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected hooks to be map[string]interface{}, got %T", hooksRaw)
	}

	stopHooks, ok := hooksMap["Stop"]
	if !ok {
		t.Fatal("Expected 'Stop' hook in hooks map even when CycleMessage is empty")
	}

	stopList, ok := stopHooks.([]interface{})
	if !ok || len(stopList) == 0 {
		t.Fatal("Expected at least one Stop hook entry")
	}

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
			if strings.Contains(cmd, "cycle") && strings.Contains(cmd, "--max-count 5") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("Expected cycle command with --max-count 5 in Stop hooks even when CycleMessage is empty, got: %+v", stopList)
	}

	// CYCLE_ENABLED must NOT be added to settings.Files when CycleMessage is empty
	for _, f := range settings.Files {
		if f.Path == "/tmp/check/CYCLE_ENABLED" {
			t.Errorf("Unexpected /tmp/check/CYCLE_ENABLED in settings.Files when CycleMessage is empty")
		}
	}
}
