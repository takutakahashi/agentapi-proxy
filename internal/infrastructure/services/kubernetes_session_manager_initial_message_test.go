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
)

func boolPtrForTest(b bool) *bool {
	return &b
}

func TestBuildInitialMessageSenderSidecar(t *testing.T) {
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Image:    "test-image:latest",
			BasePort: 9000,
		},
	}
	lgr := logger.NewLogger()
	k8sClient := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	t.Run("returns nil when no initial message", func(t *testing.T) {
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "",
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil, // No webhook payload for test
		)

		sidecar := manager.buildInitialMessageSenderSidecar(session)
		if sidecar != nil {
			t.Error("Expected nil sidecar when no initial message")
		}
	})

	t.Run("returns sidecar when initial message is provided", func(t *testing.T) {
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "Hello, this is the initial message",
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil, // No webhook payload for test
		)

		sidecar := manager.buildInitialMessageSenderSidecar(session)
		if sidecar == nil {
			t.Fatal("Expected sidecar when initial message is provided")
		}

		if sidecar.Name != "initial-message-sender" {
			t.Errorf("Expected container name 'initial-message-sender', got %s", sidecar.Name)
		}

		if sidecar.Image != "test-image:latest" {
			t.Errorf("Expected image 'test-image:latest', got %s", sidecar.Image)
		}

		// Verify volume mounts: session-settings (read-only) and initial-message-state
		if len(sidecar.VolumeMounts) != 2 {
			t.Errorf("Expected 2 volume mounts, got %d", len(sidecar.VolumeMounts))
		}

		// Verify session-settings volume mount (initial message is stored here)
		var hasSessionSettings, hasInitialMessageState bool
		for _, vm := range sidecar.VolumeMounts {
			if vm.Name == "session-settings" {
				hasSessionSettings = true
				if vm.MountPath != "/session-settings" {
					t.Errorf("Expected session-settings mount at /session-settings, got %s", vm.MountPath)
				}
				if !vm.ReadOnly {
					t.Error("Expected session-settings volume mount to be read-only")
				}
			}
			if vm.Name == "initial-message-state" {
				hasInitialMessageState = true
				if vm.MountPath != "/initial-message-state" {
					t.Errorf("Expected initial-message-state mount at /initial-message-state, got %s", vm.MountPath)
				}
			}
		}
		if !hasSessionSettings {
			t.Error("Expected session-settings volume mount")
		}
		if !hasInitialMessageState {
			t.Error("Expected initial-message-state volume mount")
		}

		// Verify environment variables
		var portEnv *corev1.EnvVar
		for _, env := range sidecar.Env {
			if env.Name == "AGENTAPI_PORT" {
				portEnv = &env
				break
			}
		}
		if portEnv == nil {
			t.Error("Expected AGENTAPI_PORT environment variable")
		} else if portEnv.Value != "9000" {
			t.Errorf("Expected AGENTAPI_PORT=9000, got %s", portEnv.Value)
		}
	})
}

// TestInitialMessageInSettingsSecret verifies that the initial message is stored
// in the session-settings Secret under the "initial-message" key.
func TestInitialMessageInSettingsSecret(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	}
	k8sClient := fake.NewSimpleClientset(ns)
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:              "test-ns",
			Image:                  "test-image:latest",
			BasePort:               9000,
			PVCEnabled:             boolPtrForTest(false),
			ClaudeConfigBaseSecret: "claude-config-base",
			CPURequest:             "100m",
			CPULimit:               "1",
			MemoryRequest:          "128Mi",
			MemoryLimit:            "512Mi",
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
		nil, // No webhook payload for test
	)

	message := "This is a test initial message"
	req := &entities.RunServerRequest{
		UserID:         "test-user",
		InitialMessage: message,
	}

	err = manager.createSessionSettingsSecret(context.Background(), session, req, nil)
	if err != nil {
		t.Fatalf("Failed to create session settings secret: %v", err)
	}

	// Verify the settings secret was created and contains initial-message key
	// Secret name: agentapi-session-{session.id}-settings = agentapi-session-test-session-settings
	settingsSecretName := "agentapi-session-test-session-settings"
	secret, err := k8sClient.CoreV1().Secrets("test-ns").Get(context.Background(), settingsSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get settings secret: %v", err)
	}

	// Verify initial-message key in secret data
	if string(secret.Data["initial-message"]) != message {
		t.Errorf("Expected initial-message '%s', got '%s'", message, string(secret.Data["initial-message"]))
	}

	// Verify settings.yaml key also exists
	if _, ok := secret.Data["settings.yaml"]; !ok {
		t.Error("Expected settings.yaml key in secret data")
	}
}

func TestBuildVolumesWithInitialMessage(t *testing.T) {
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			ClaudeConfigBaseSecret: "claude-config-base",
		},
	}
	lgr := logger.NewLogger()
	k8sClient := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	t.Run("includes initial-message-state volume when message is provided", func(t *testing.T) {
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
			nil, // No webhook payload for test
		)

		volumes := manager.buildVolumes(session)

		// Look for initial-message-state volume (EmptyDir for tracking send state)
		// The initial message content is now in session-settings Secret, not a separate volume
		var hasInitialMessageState bool
		for _, vol := range volumes {
			if vol.Name == "initial-message" {
				t.Error("initial-message volume should NOT be present (content is in session-settings Secret)")
			}
			if vol.Name == "initial-message-state" {
				hasInitialMessageState = true
				if vol.EmptyDir == nil {
					t.Error("initial-message-state volume should be an EmptyDir")
				}
			}
		}

		if !hasInitialMessageState {
			t.Error("Expected initial-message-state volume to be present")
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
			nil, // No webhook payload for test
		)

		volumes := manager.buildVolumes(session)

		// Look for initial-message volume
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

func TestCreateSessionWithInitialMessage(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-initial-msg-ns",
		},
	}
	k8sClient := fake.NewSimpleClientset(ns)
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:              ns.Name,
			Image:                  "test-image:latest",
			BasePort:               9000,
			PVCEnabled:             boolPtrForTest(false),
			ClaudeConfigBaseSecret: "claude-config-base",
			CPURequest:             "100m",
			CPULimit:               "1",
			MemoryRequest:          "128Mi",
			MemoryLimit:            "512Mi",
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

	// Verify initial message is stored in the settings Secret under "initial-message" key
	// Settings secret name: agentapi-session-{id}-settings
	settingsSecretName := fmt.Sprintf("agentapi-session-%s-settings", sessionID)
	secret, err := k8sClient.CoreV1().Secrets(ns.Name).Get(ctx, settingsSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get settings secret: %v", err)
	}

	if string(secret.Data["initial-message"]) != initialMessage {
		t.Errorf("Expected initial-message '%s', got '%s'", initialMessage, string(secret.Data["initial-message"]))
	}

	// Verify the old per-session initial-message Secret does NOT exist
	oldSecretName := fmt.Sprintf("agentapi-session-%s-svc-initial-message", sessionID)
	_, err = k8sClient.CoreV1().Secrets(ns.Name).Get(ctx, oldSecretName, metav1.GetOptions{})
	if err == nil {
		t.Errorf("Old per-session initial-message Secret %s should not exist", oldSecretName)
	}

	// Verify deployment has initial-message-sender sidecar
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	podSpec := deployment.Spec.Template.Spec
	var hasSidecar bool
	for _, container := range podSpec.Containers {
		if container.Name == "initial-message-sender" {
			hasSidecar = true
			break
		}
	}

	if !hasSidecar {
		t.Error("Expected initial-message-sender sidecar in deployment")
	}

	// Verify initial-message-state volume is present (but NOT a separate initial-message Secret volume)
	var hasInitialMessageStateVol bool
	for _, vol := range podSpec.Volumes {
		if vol.Name == "initial-message" {
			t.Error("initial-message Secret volume should not be present (content is in session-settings)")
		}
		if vol.Name == "initial-message-state" {
			hasInitialMessageStateVol = true
		}
	}

	if !hasInitialMessageStateVol {
		t.Error("Expected initial-message-state volume in deployment")
	}
}
