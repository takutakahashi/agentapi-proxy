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

func newWorkloadTestManager(t *testing.T, pvcEnabled bool) *KubernetesSessionManager {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}
	k8sClient := fake.NewSimpleClientset(ns)
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:     "test-ns",
			Image:         "test-image:latest",
			BasePort:      9000,
			PVCEnabled:    boolPtrForTest(pvcEnabled),
			CPURequest:    "100m",
			CPULimit:      "1",
			MemoryRequest: "128Mi",
			MemoryLimit:   "512Mi",
		},
	}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	return manager
}

func newWorkloadTestSession() *KubernetesSession {
	return NewKubernetesSession(
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
}

func TestCreateSessionWorkloadWithoutPVCUsesPodRestartPolicyNever(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	session := newWorkloadTestSession()

	if err := manager.createSessionWorkload(context.Background(), session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	pod, err := manager.client.CoreV1().Pods("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected pod to be created: %v", err)
	}
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("Expected restartPolicy Never, got %s", pod.Spec.RestartPolicy)
	}
	if pod.Spec.Volumes[0].PersistentVolumeClaim != nil {
		t.Fatal("Expected workdir volume to avoid PVC when PVC is disabled")
	}
	if pod.Spec.Volumes[0].EmptyDir == nil {
		t.Fatal("Expected workdir volume to use EmptyDir when PVC is disabled")
	}

	deployments, err := manager.client.AppsV1().Deployments("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list deployments: %v", err)
	}
	if len(deployments.Items) != 0 {
		t.Fatalf("Expected no deployments, got %d", len(deployments.Items))
	}
}

func TestCreateSessionWorkloadWithPVCUsesDeploymentRestartPolicyAlways(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	session := newWorkloadTestSession()

	if err := manager.createSessionWorkload(context.Background(), session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	deployment, err := manager.client.AppsV1().Deployments("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected deployment to be created: %v", err)
	}
	if deployment.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		t.Fatalf("Expected restartPolicy Always, got %s", deployment.Spec.Template.Spec.RestartPolicy)
	}
	if deployment.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim == nil {
		t.Fatal("Expected workdir volume to mount PVC when PVC is enabled")
	}

	pods, err := manager.client.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Fatalf("Expected no standalone pods, got %d", len(pods.Items))
	}
}
