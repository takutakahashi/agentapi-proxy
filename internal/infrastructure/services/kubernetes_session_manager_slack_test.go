package services

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func TestBuildSlackSidecar(t *testing.T) {
	baseConfig := func(secretName string) *config.Config {
		return &config.Config{
			KubernetesSession: config.KubernetesSessionConfig{
				Image:                   "test-image:latest",
				BasePort:                9000,
				SlackBotTokenSecretName: secretName,
			},
		}
	}

	newManager := func(cfg *config.Config) *KubernetesSessionManager {
		lgr := logger.NewLogger()
		k8sClient := fake.NewSimpleClientset()
		manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
		if err != nil {
			t.Fatalf("Failed to create manager: %v", err)
		}
		return manager
	}

	t.Run("returns nil when no Slack params", func(t *testing.T) {
		manager := newManager(baseConfig("slack-secret"))
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID:      "test-user",
				SlackParams: nil,
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		sidecar := manager.buildSlackSidecar(session)
		if sidecar != nil {
			t.Error("Expected nil sidecar when no Slack params")
		}
	})

	t.Run("returns nil when Slack channel is empty", func(t *testing.T) {
		manager := newManager(baseConfig("slack-secret"))
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID: "test-user",
				SlackParams: &entities.SlackParams{
					Channel:  "",
					ThreadTS: "1234567890.123456",
				},
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		sidecar := manager.buildSlackSidecar(session)
		if sidecar != nil {
			t.Error("Expected nil sidecar when Slack channel is empty")
		}
	})

	t.Run("returns nil when bot token secret not configured", func(t *testing.T) {
		// No secret name configured
		manager := newManager(baseConfig(""))
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID: "test-user",
				SlackParams: &entities.SlackParams{
					Channel:  "C1234567890",
					ThreadTS: "1234567890.123456",
				},
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		sidecar := manager.buildSlackSidecar(session)
		if sidecar != nil {
			t.Error("Expected nil sidecar when bot token secret is not configured")
		}
	})

	t.Run("returns claude-posts sidecar when Slack params and secret are provided", func(t *testing.T) {
		manager := newManager(baseConfig("my-slack-secret"))
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID: "test-user",
				SlackParams: &entities.SlackParams{
					Channel:  "C1234567890",
					ThreadTS: "1234567890.123456",
				},
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		sidecar := manager.buildSlackSidecar(session)
		if sidecar == nil {
			t.Fatal("Expected sidecar when Slack params are provided")
		}

		if sidecar.Name != "slack-integration" {
			t.Errorf("Expected container name 'slack-integration', got %s", sidecar.Name)
		}

		if sidecar.Image != defaultSlackIntegrationImage {
			t.Errorf("Expected image '%s', got %s", defaultSlackIntegrationImage, sidecar.Image)
		}

		// Verify environment variables - build lookup maps
		plainEnvVars := make(map[string]string)
		secretEnvVars := make(map[string]*corev1.SecretKeySelector)
		for _, env := range sidecar.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				secretEnvVars[env.Name] = env.ValueFrom.SecretKeyRef
			} else {
				plainEnvVars[env.Name] = env.Value
			}
		}

		// SLACK_BOT_TOKEN must come from a SecretKeyRef
		botTokenRef, ok := secretEnvVars["SLACK_BOT_TOKEN"]
		if !ok {
			t.Error("Expected SLACK_BOT_TOKEN to use SecretKeyRef")
		} else {
			if botTokenRef.Name != "my-slack-secret" {
				t.Errorf("Expected secret name 'my-slack-secret', got %s", botTokenRef.Name)
			}
			if botTokenRef.Key != defaultSlackBotTokenSecretKey {
				t.Errorf("Expected secret key '%s', got %s", defaultSlackBotTokenSecretKey, botTokenRef.Key)
			}
		}

		// SLACK_CHANNEL_ID must be set to the channel value
		if channelID, ok := plainEnvVars["SLACK_CHANNEL_ID"]; !ok || channelID != "C1234567890" {
			t.Errorf("Expected SLACK_CHANNEL_ID=C1234567890, got %s", channelID)
		}

		if threadTS, ok := plainEnvVars["SLACK_THREAD_TS"]; !ok || threadTS != "1234567890.123456" {
			t.Errorf("Expected SLACK_THREAD_TS=1234567890.123456, got %s", threadTS)
		}

		// Verify command starts with /bin/sh -c
		if len(sidecar.Command) != 2 || sidecar.Command[0] != "/bin/sh" || sidecar.Command[1] != "-c" {
			t.Errorf("Expected command [/bin/sh -c], got %v", sidecar.Command)
		}

		// Verify args runs claude-posts with --file flag
		if len(sidecar.Args) != 1 {
			t.Fatalf("Expected exactly 1 arg, got %d", len(sidecar.Args))
		}
		if !strings.Contains(sidecar.Args[0], "claude-posts") {
			t.Errorf("Expected args to contain 'claude-posts', got %s", sidecar.Args[0])
		}
		if !strings.Contains(sidecar.Args[0], "--file /opt/claude-agentapi/history.jsonl") {
			t.Errorf("Expected args to contain '--file /opt/claude-agentapi/history.jsonl', got %s", sidecar.Args[0])
		}

		// Verify volume mount for claude-agentapi-history
		foundVolumeMount := false
		for _, vm := range sidecar.VolumeMounts {
			if vm.Name == "claude-agentapi-history" && vm.MountPath == "/opt/claude-agentapi" {
				foundVolumeMount = true
			}
		}
		if !foundVolumeMount {
			t.Error("Expected volume mount for claude-agentapi-history at /opt/claude-agentapi")
		}
	})

	t.Run("uses custom image when configured", func(t *testing.T) {
		cfg := &config.Config{
			KubernetesSession: config.KubernetesSessionConfig{
				Image:                   "test-image:latest",
				BasePort:                9000,
				SlackBotTokenSecretName: "slack-secret",
				SlackIntegrationImage:   "ghcr.io/takutakahashi/claude-posts:custom",
			},
		}
		manager := newManager(cfg)
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID: "test-user",
				SlackParams: &entities.SlackParams{
					Channel:  "C1234567890",
					ThreadTS: "1234567890.123456",
				},
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		sidecar := manager.buildSlackSidecar(session)
		if sidecar == nil {
			t.Fatal("Expected sidecar when Slack params are provided")
		}
		if sidecar.Image != "ghcr.io/takutakahashi/claude-posts:custom" {
			t.Errorf("Expected custom image, got %s", sidecar.Image)
		}
	})

	t.Run("uses custom bot token secret key when configured", func(t *testing.T) {
		cfg := &config.Config{
			KubernetesSession: config.KubernetesSessionConfig{
				Image:                   "test-image:latest",
				BasePort:                9000,
				SlackBotTokenSecretName: "slack-secret",
				SlackBotTokenSecretKey:  "slack-token",
			},
		}
		manager := newManager(cfg)
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID: "test-user",
				SlackParams: &entities.SlackParams{
					Channel:  "C1234567890",
					ThreadTS: "1234567890.123456",
				},
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		sidecar := manager.buildSlackSidecar(session)
		if sidecar == nil {
			t.Fatal("Expected sidecar when Slack params are provided")
		}
		for _, env := range sidecar.Env {
			if env.Name == "SLACK_BOT_TOKEN" {
				if env.ValueFrom.SecretKeyRef.Key != "slack-token" {
					t.Errorf("Expected secret key 'slack-token', got %s", env.ValueFrom.SecretKeyRef.Key)
				}
				return
			}
		}
		t.Error("SLACK_BOT_TOKEN env var not found")
	})

	t.Run("returns sidecar when only channel is provided (thread_ts is optional)", func(t *testing.T) {
		manager := newManager(baseConfig("slack-secret"))
		session := NewKubernetesSession(
			"test-session",
			&entities.RunServerRequest{
				UserID: "test-user",
				SlackParams: &entities.SlackParams{
					Channel:  "C1234567890",
					ThreadTS: "",
				},
			},
			"test-deploy",
			"test-service",
			"test-pvc",
			"test-ns",
			9000,
			nil,
			nil,
		)

		sidecar := manager.buildSlackSidecar(session)
		if sidecar == nil {
			t.Fatal("Expected sidecar when channel is provided")
		}

		envVars := make(map[string]string)
		for _, env := range sidecar.Env {
			if env.ValueFrom == nil {
				envVars[env.Name] = env.Value
			}
		}

		if channel, ok := envVars["SLACK_CHANNEL_ID"]; !ok || channel != "C1234567890" {
			t.Errorf("Expected SLACK_CHANNEL_ID=C1234567890, got %s", channel)
		}

		if threadTS, ok := envVars["SLACK_THREAD_TS"]; !ok || threadTS != "" {
			t.Errorf("Expected SLACK_THREAD_TS to be empty, got %s", threadTS)
		}
	})
}
