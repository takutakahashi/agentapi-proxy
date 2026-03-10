package services

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func boolPtrForTest(b bool) *bool {
	return &b
}

// TestInitialMessageStoredInSettingsYAML verifies that the initial message is embedded
// inside settings.yaml (not as a separate "initial-message" key) when the settings
// Secret is created.
func TestInitialMessageStoredInSettingsYAML(t *testing.T) {
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

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{
			UserID: "test-user",
		},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)

	message := "This is a test initial message"
	req := &entities.RunServerRequest{
		UserID:         "test-user",
		InitialMessage: message,
	}

	// Build settings and create the Secret using the new function.
	settings := manager.buildSessionSettings(context.Background(), session, req, nil)
	err = manager.createSessionSettingsSecretFromSettings(context.Background(), session, req, settings)
	if err != nil {
		t.Fatalf("Failed to create session settings secret: %v", err)
	}

	settingsSecretName := "agentapi-session-test-session-settings"
	secret, err := k8sClient.CoreV1().Secrets("test-ns").Get(context.Background(), settingsSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get settings secret: %v", err)
	}

	// The initial message must be inside settings.yaml, not as a flat key.
	if _, ok := secret.Data["initial-message"]; ok {
		t.Error("initial-message flat key should NOT be present in the Secret (moved into settings.yaml)")
	}

	yamlData, ok := secret.Data["settings.yaml"]
	if !ok {
		t.Fatal("Expected settings.yaml key in secret data")
	}

	parsed, err := sessionsettings.LoadSettingsFromBytes(yamlData)
	if err != nil {
		t.Fatalf("Failed to parse settings.yaml: %v", err)
	}
	if parsed.InitialMessage != message {
		t.Errorf("Expected InitialMessage %q inside settings.yaml, got %q", message, parsed.InitialMessage)
	}
}

// TestBuildVolumesNoInitialMessageState verifies that the initial-message-state
// EmptyDir volume is no longer created (the sidecar has been replaced by agent-provisioner).
func TestBuildVolumesNoInitialMessageState(t *testing.T) {
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{},
	}
	lgr := logger.NewLogger()
	k8sClient := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	t.Run("does not include initial-message-state volume even when message is provided", func(t *testing.T) {
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "Hello, initial message",
			},
			"test-deploy",
			"agentapi-session-test-svc",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		volumes := manager.buildVolumes(session)

		for _, vol := range volumes {
			if vol.Name == "initial-message" {
				t.Error("initial-message volume should NOT be present")
			}
			if vol.Name == "initial-message-state" {
				t.Error("initial-message-state volume should NOT be present (sidecar removed, handled by agent-provisioner)")
			}
		}
	})

	t.Run("does not include initial message volumes when message is empty", func(t *testing.T) {
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "",
			},
			"test-deploy",
			"agentapi-session-test-svc",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		volumes := manager.buildVolumes(session)

		for _, vol := range volumes {
			if vol.Name == "initial-message" {
				t.Error("initial-message volume should not be present when message is empty")
			}
			if vol.Name == "initial-message-state" {
				t.Error("initial-message-state volume should not be present when message is empty")
			}
		}
	})
}

// TestCreateSessionWithInitialMessage verifies that a session created with an initial
// message uses agent-provisioner (no initial-message-sender sidecar), and that the
// initial message is embedded inside settings.yaml.
func TestCreateSessionWithInitialMessage(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-initial-msg-ns",
		},
	}
	k8sClient := fake.NewSimpleClientset(ns)
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:     ns.Name,
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

	ctx := context.Background()
	sessionID := "initial-msg-test"
	initialMessage := "Hello, this is the initial message for testing"

	req := &entities.RunServerRequest{
		UserID:         "test-user",
		InitialMessage: initialMessage,
	}

	session, err := manager.CreateSession(ctx, sessionID, req, nil)
	if err != nil {
		t.Fatalf("Failed to create session: %v\n", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	if session == nil {
		t.Fatal("Expected session to be created")
	}

	// Note: the settings Secret is NOT created immediately in CreateSession().
	// It is created asynchronously in watchSession() after successful provisioning.
	// Verify the settings Secret does NOT exist at this point (before provisioning).
	settingsSecretName := fmt.Sprintf("agentapi-session-%s-settings", sessionID)
	_, err = k8sClient.CoreV1().Secrets(ns.Name).Get(ctx, settingsSecretName, metav1.GetOptions{})
	if err == nil {
		t.Error("Settings secret should NOT exist before provisioning completes")
	}

	// Verify that the session stores the provision settings for later use.
	ks := session.(*KubernetesSession)
	if ks.ProvisionSettings() == nil {
		t.Error("Expected ProvisionSettings to be set on session after CreateSession")
	}
	if ks.ProvisionSettings().InitialMessage != initialMessage {
		t.Errorf("Expected InitialMessage %q in ProvisionSettings, got %q", initialMessage, ks.ProvisionSettings().InitialMessage)
	}

	// Verify deployment uses agent-provisioner (NOT initial-message-sender sidecar).
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	podSpec := deployment.Spec.Template.Spec
	for _, container := range podSpec.Containers {
		if container.Name == "initial-message-sender" {
			t.Error("initial-message-sender sidecar should NOT be present (replaced by agent-provisioner)")
		}
	}

	// Verify the main container uses agent-provisioner command.
	mainContainer := podSpec.Containers[0]
	if len(mainContainer.Command) == 0 || mainContainer.Command[0] != "agentapi-proxy" {
		t.Errorf("Expected main container command [agentapi-proxy], got %v", mainContainer.Command)
	}
	if len(mainContainer.Args) == 0 || mainContainer.Args[0] != "agent-provisioner" {
		t.Errorf("Expected main container args [agent-provisioner], got %v", mainContainer.Args)
	}

	// Verify provisioner port (9001) is exposed.
	var hasProvisionerPort bool
	for _, port := range mainContainer.Ports {
		if port.Name == "provisioner" && port.ContainerPort == provisionerPort {
			hasProvisionerPort = true
		}
	}
	if !hasProvisionerPort {
		t.Errorf("Expected provisioner port %d in container ports", provisionerPort)
	}

	// Verify no initial-message-state volume is present.
	for _, vol := range podSpec.Volumes {
		if vol.Name == "initial-message-state" {
			t.Error("initial-message-state volume should NOT be present (sidecar removed)")
		}
	}
}
