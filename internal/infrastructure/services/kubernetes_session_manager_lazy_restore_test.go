package services

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func TestEnsureSessionWorkloadRecreatesMissingDeployment(t *testing.T) {
	pvcEnabled := true
	cfg := &config.Config{KubernetesSession: config.KubernetesSessionConfig{
		Namespace: "test-ns", Image: "test-image:latest", BasePort: 9000,
		PVCEnabled: &pvcEnabled, CPURequest: "100m", CPULimit: "1",
		MemoryRequest: "128Mi", MemoryLimit: "512Mi",
	}}
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-session-1-svc", Namespace: "test-ns"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-session-1-settings", Namespace: "test-ns"}},
	)
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatal(err)
	}
	request := &entities.RunServerRequest{UserID: "user-1", AgentType: "codex-acp"}
	session := NewKubernetesSession(
		"session-1", request, "agentapi-session-session-1",
		"agentapi-session-session-1-svc", "agentapi-session-session-1-pvc",
		"test-ns", 9000, nil, nil,
	)
	manager.sessions[session.id] = session

	got, restoring, err := manager.EnsureSessionWorkload(context.Background(), session.id)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !restoring {
		t.Fatalf("got session=%v restoring=%v", got, restoring)
	}
	if _, err := client.AppsV1().Deployments("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{}); err != nil {
		t.Fatalf("deployment was not recreated: %v", err)
	}
}
