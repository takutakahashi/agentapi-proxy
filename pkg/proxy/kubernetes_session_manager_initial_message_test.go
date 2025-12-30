package proxy

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func boolPtrForTest(b bool) *bool {
	return &b
}

func TestBuildInitialMessageSenderSidecar(t *testing.T) {
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:  true,
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

	t.Run("returns nil when no initial message and S3 sync disabled", func(t *testing.T) {
		session := &kubernetesSession{
			id:          "test-session",
			serviceName: "test-service",
			request: &RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "",
			},
		}

		sidecar := manager.buildInitialMessageSenderSidecar(session)
		if sidecar != nil {
			t.Error("Expected nil sidecar when no initial message and S3 sync disabled")
		}
	})

	t.Run("returns sidecar when initial message is provided", func(t *testing.T) {
		session := &kubernetesSession{
			id:          "test-session",
			serviceName: "test-service",
			request: &RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "Hello, this is the initial message",
			},
		}

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

		// Verify volume mounts (initial-message-state + initial-message)
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

	t.Run("returns sidecar when S3 sync is enabled without initial message", func(t *testing.T) {
		s3Cfg := &config.Config{
			KubernetesSession: config.KubernetesSessionConfig{
				Enabled:                 true,
				Image:                   "test-image:latest",
				BasePort:                9000,
				S3SyncEnabled:           true,
				S3SyncBucket:            "test-bucket",
				S3SyncRegion:            "us-east-1",
				S3SyncPrefix:            "sessions",
				S3SyncInterval:          60,
				S3SyncCredentialsSecret: "aws-credentials",
			},
		}
		s3Manager, err := NewKubernetesSessionManagerWithClient(s3Cfg, false, lgr, k8sClient)
		if err != nil {
			t.Fatalf("Failed to create manager: %v", err)
		}

		session := &kubernetesSession{
			id:          "test-session",
			serviceName: "test-service",
			request: &RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "",
			},
		}

		sidecar := s3Manager.buildInitialMessageSenderSidecar(session)
		if sidecar == nil {
			t.Fatal("Expected sidecar when S3 sync is enabled")
		}

		// Verify S3 sync environment variables
		envVars := make(map[string]string)
		for _, env := range sidecar.Env {
			envVars[env.Name] = env.Value
		}

		if envVars["S3_SYNC_ENABLED"] != "true" {
			t.Errorf("Expected S3_SYNC_ENABLED=true, got %s", envVars["S3_SYNC_ENABLED"])
		}
		if envVars["S3_BUCKET"] != "test-bucket" {
			t.Errorf("Expected S3_BUCKET=test-bucket, got %s", envVars["S3_BUCKET"])
		}
		if envVars["S3_REGION"] != "us-east-1" {
			t.Errorf("Expected S3_REGION=us-east-1, got %s", envVars["S3_REGION"])
		}
		if envVars["S3_PREFIX"] != "sessions" {
			t.Errorf("Expected S3_PREFIX=sessions, got %s", envVars["S3_PREFIX"])
		}
		if envVars["S3_SYNC_INTERVAL"] != "60" {
			t.Errorf("Expected S3_SYNC_INTERVAL=60, got %s", envVars["S3_SYNC_INTERVAL"])
		}
		if envVars["SESSION_ID"] != "test-session" {
			t.Errorf("Expected SESSION_ID=test-session, got %s", envVars["SESSION_ID"])
		}

		// Verify AWS credentials volume mount
		var hasAWSCredentials bool
		for _, vm := range sidecar.VolumeMounts {
			if vm.Name == "aws-credentials" {
				hasAWSCredentials = true
				if vm.MountPath != "/root/.aws" {
					t.Errorf("Expected aws-credentials mount path '/root/.aws', got %s", vm.MountPath)
				}
			}
		}
		if !hasAWSCredentials {
			t.Error("Expected aws-credentials volume mount")
		}
	})

	t.Run("returns sidecar with both initial message and S3 sync", func(t *testing.T) {
		s3Cfg := &config.Config{
			KubernetesSession: config.KubernetesSessionConfig{
				Enabled:                 true,
				Image:                   "test-image:latest",
				BasePort:                9000,
				S3SyncEnabled:           true,
				S3SyncBucket:            "test-bucket",
				S3SyncRegion:            "us-east-1",
				S3SyncPrefix:            "sessions",
				S3SyncInterval:          60,
				S3SyncCredentialsSecret: "aws-credentials",
			},
		}
		s3Manager, err := NewKubernetesSessionManagerWithClient(s3Cfg, false, lgr, k8sClient)
		if err != nil {
			t.Fatalf("Failed to create manager: %v", err)
		}

		session := &kubernetesSession{
			id:          "test-session",
			serviceName: "test-service",
			request: &RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "Hello, this is the initial message",
			},
		}

		sidecar := s3Manager.buildInitialMessageSenderSidecar(session)
		if sidecar == nil {
			t.Fatal("Expected sidecar when both initial message and S3 sync are enabled")
		}

		// Verify volume mounts (initial-message-state + initial-message + aws-credentials)
		if len(sidecar.VolumeMounts) != 3 {
			t.Errorf("Expected 3 volume mounts, got %d", len(sidecar.VolumeMounts))
		}

		// Verify all volume mounts are present
		volumeNames := make(map[string]bool)
		for _, vm := range sidecar.VolumeMounts {
			volumeNames[vm.Name] = true
		}
		if !volumeNames["initial-message-state"] {
			t.Error("Expected initial-message-state volume mount")
		}
		if !volumeNames["initial-message"] {
			t.Error("Expected initial-message volume mount")
		}
		if !volumeNames["aws-credentials"] {
			t.Error("Expected aws-credentials volume mount")
		}
	})
}

func TestCreateInitialMessageSecret(t *testing.T) {
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:   true,
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

	session := &kubernetesSession{
		id:          "test-session",
		serviceName: "agentapi-session-test-svc",
		request: &RunServerRequest{
			UserID: "test-user",
		},
	}

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
			Enabled:                true,
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
		session := &kubernetesSession{
			id:          "test-session",
			serviceName: "agentapi-session-test-svc",
			request: &RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "Hello, initial message",
			},
		}

		volumes := manager.buildVolumes(session, "claude-config-user")

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

	t.Run("does not include initial message volumes when message is empty and S3 sync disabled", func(t *testing.T) {
		session := &kubernetesSession{
			id:          "test-session",
			serviceName: "agentapi-session-test-svc",
			request: &RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "",
			},
		}

		volumes := manager.buildVolumes(session, "claude-config-user")

		// Look for initial-message volume
		for _, vol := range volumes {
			if vol.Name == "initial-message" {
				t.Error("initial-message volume should not be present when message is empty")
			}
			if vol.Name == "initial-message-state" {
				t.Error("initial-message-state volume should not be present when message is empty and S3 sync disabled")
			}
			if vol.Name == "aws-credentials" {
				t.Error("aws-credentials volume should not be present when S3 sync is disabled")
			}
		}
	})

	t.Run("includes S3 sync volumes when S3 sync is enabled", func(t *testing.T) {
		s3Cfg := &config.Config{
			KubernetesSession: config.KubernetesSessionConfig{
				Enabled:                 true,
				ClaudeConfigBaseSecret:  "claude-config-base",
				S3SyncEnabled:           true,
				S3SyncBucket:            "test-bucket",
				S3SyncRegion:            "us-east-1",
				S3SyncCredentialsSecret: "aws-credentials",
			},
		}
		s3Manager, err := NewKubernetesSessionManagerWithClient(s3Cfg, false, lgr, k8sClient)
		if err != nil {
			t.Fatalf("Failed to create manager: %v", err)
		}

		session := &kubernetesSession{
			id:          "test-session",
			serviceName: "agentapi-session-test-svc",
			request: &RunServerRequest{
				UserID:         "test-user",
				InitialMessage: "",
			},
		}

		volumes := s3Manager.buildVolumes(session, "claude-config-user")

		var hasInitialMessageState, hasAWSCredentials bool
		for _, vol := range volumes {
			if vol.Name == "initial-message-state" {
				hasInitialMessageState = true
			}
			if vol.Name == "aws-credentials" {
				hasAWSCredentials = true
				if vol.Secret == nil {
					t.Error("aws-credentials volume should be a Secret")
				} else if vol.Secret.SecretName != "aws-credentials" {
					t.Errorf("Expected secret name 'aws-credentials', got %s", vol.Secret.SecretName)
				}
			}
		}

		if !hasInitialMessageState {
			t.Error("Expected initial-message-state volume when S3 sync is enabled")
		}
		if !hasAWSCredentials {
			t.Error("Expected aws-credentials volume when S3 sync is enabled")
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
			Enabled:                true,
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

	req := &RunServerRequest{
		Port:           9000,
		UserID:         "test-user",
		InitialMessage: initialMessage,
	}

	session, err := manager.CreateSession(ctx, sessionID, req)
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
