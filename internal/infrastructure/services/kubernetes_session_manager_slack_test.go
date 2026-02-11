package services

import (
	"testing"

	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func TestBuildSlackSidecar(t *testing.T) {
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

	t.Run("returns nil when no Slack params", func(t *testing.T) {
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

	t.Run("returns sidecar when Slack params are provided", func(t *testing.T) {
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

		if sidecar.Image != "alpine:latest" {
			t.Errorf("Expected image 'alpine:latest', got %s", sidecar.Image)
		}

		// Verify environment variables
		envVars := make(map[string]string)
		for _, env := range sidecar.Env {
			envVars[env.Name] = env.Value
		}

		if channel, ok := envVars["SLACK_CHANNEL"]; !ok || channel != "C1234567890" {
			t.Errorf("Expected SLACK_CHANNEL=C1234567890, got %s", channel)
		}

		if threadTS, ok := envVars["SLACK_THREAD_TS"]; !ok || threadTS != "1234567890.123456" {
			t.Errorf("Expected SLACK_THREAD_TS=1234567890.123456, got %s", threadTS)
		}

		// Verify command
		if len(sidecar.Command) != 2 || sidecar.Command[0] != "/bin/sh" || sidecar.Command[1] != "-c" {
			t.Errorf("Expected command [/bin/sh -c], got %v", sidecar.Command)
		}

		if len(sidecar.Args) != 1 || sidecar.Args[0] != "sleep infinity" {
			t.Errorf("Expected args [sleep infinity], got %v", sidecar.Args)
		}
	})

	t.Run("returns sidecar when only channel is provided (thread_ts is optional)", func(t *testing.T) {
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
			envVars[env.Name] = env.Value
		}

		if channel, ok := envVars["SLACK_CHANNEL"]; !ok || channel != "C1234567890" {
			t.Errorf("Expected SLACK_CHANNEL=C1234567890, got %s", channel)
		}

		if threadTS, ok := envVars["SLACK_THREAD_TS"]; !ok || threadTS != "" {
			t.Errorf("Expected SLACK_THREAD_TS to be empty, got %s", threadTS)
		}
	})
}
