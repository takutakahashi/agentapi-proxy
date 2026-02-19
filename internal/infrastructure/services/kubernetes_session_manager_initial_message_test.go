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

		// Verify volume mounts
		if len(sidecar.VolumeMounts) != 2 {
			t.Errorf("Expected 2 volume mounts, got %d", len(sidecar.VolumeMounts))
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

func TestCreateInitialMessageSecret(t *testing.T) {
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace: "test-ns",
		},
	}
	lgr := logger.NewLogger()
	k8sClient := fake.NewSimpleClientset()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	manager.namespace = "test-ns"

	// Create the namespace first
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	}
	_, err = k8sClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}

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
	err = manager.createInitialMessageSecret(context.Background(), session, message)
	if err != nil {
		t.Fatalf("Failed to create initial message secret: %v", err)
	}

	// Verify the secret was created
	secretName := "agentapi-session-test-svc-initial-message"
	secret, err := k8sClient.CoreV1().Secrets("test-ns").Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Verify secret labels
	if secret.Labels["agentapi.proxy/session-id"] != "test-session" {
		t.Errorf("Expected session-id label 'test-session', got %s", secret.Labels["agentapi.proxy/session-id"])
	}
	if secret.Labels["agentapi.proxy/resource"] != "initial-message" {
		t.Errorf("Expected resource label 'initial-message', got %s", secret.Labels["agentapi.proxy/resource"])
	}

	// Verify secret data
	if string(secret.Data["message"]) != message {
		t.Errorf("Expected message '%s', got '%s'", message, string(secret.Data["message"]))
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

	t.Run("includes initial message volumes when message is provided", func(t *testing.T) {
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

		// Look for initial-message and initial-message-state volumes
		var hasInitialMessage, hasInitialMessageState bool
		for _, vol := range volumes {
			if vol.Name == "initial-message" {
				hasInitialMessage = true
				if vol.Secret == nil {
					t.Error("initial-message volume should be a Secret")
				} else if vol.Secret.SecretName != "agentapi-session-test-svc-initial-message" {
					t.Errorf("Expected secret name 'agentapi-session-test-svc-initial-message', got %s", vol.Secret.SecretName)
				}
			}
			if vol.Name == "initial-message-state" {
				hasInitialMessageState = true
				if vol.EmptyDir == nil {
					t.Error("initial-message-state volume should be an EmptyDir")
				}
			}
		}

		if !hasInitialMessage {
			t.Error("Expected initial-message volume to be present")
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
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	if session == nil {
		t.Fatal("Expected session to be created")
	}

	// Verify initial message secret was created
	secretName := fmt.Sprintf("agentapi-session-%s-svc-initial-message", sessionID)
	secret, err := k8sClient.CoreV1().Secrets(ns.Name).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get initial message secret: %v", err)
	}

	if string(secret.Data["message"]) != initialMessage {
		t.Errorf("Expected message '%s', got '%s'", initialMessage, string(secret.Data["message"]))
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

	// Verify volumes are present
	var hasInitialMessageVol, hasInitialMessageStateVol bool
	for _, vol := range podSpec.Volumes {
		if vol.Name == "initial-message" {
			hasInitialMessageVol = true
		}
		if vol.Name == "initial-message-state" {
			hasInitialMessageStateVol = true
		}
	}

	if !hasInitialMessageVol {
		t.Error("Expected initial-message volume in deployment")
	}
	if !hasInitialMessageStateVol {
		t.Error("Expected initial-message-state volume in deployment")
	}
}
