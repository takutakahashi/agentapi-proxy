package cmd

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSessionWorkloadExistsTreatsLivePodAsExisting(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-live", Namespace: "test-ns"},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})

	exists, err := sessionWorkloadExists(context.Background(), client, "test-ns", "agentapi-session-live")
	if err != nil {
		t.Fatalf("sessionWorkloadExists returned error: %v", err)
	}
	if !exists {
		t.Fatal("expected running pod to count as existing workload")
	}
}

func TestSessionWorkloadExistsTreatsTerminalPodsAsMissing(t *testing.T) {
	tests := []struct {
		name  string
		phase corev1.PodPhase
	}{
		{name: "succeeded", phase: corev1.PodSucceeded},
		{name: "failed", phase: corev1.PodFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-done", Namespace: "test-ns"},
				Status:     corev1.PodStatus{Phase: tt.phase},
			})

			exists, err := sessionWorkloadExists(context.Background(), client, "test-ns", "agentapi-session-done")
			if err != nil {
				t.Fatalf("sessionWorkloadExists returned error: %v", err)
			}
			if exists {
				t.Fatalf("expected %s pod to count as missing workload", tt.phase)
			}
		})
	}
}

func TestSessionWorkloadExistsTreatsDeploymentAsExisting(t *testing.T) {
	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-session-deploy", Namespace: "test-ns"},
	})

	exists, err := sessionWorkloadExists(context.Background(), client, "test-ns", "agentapi-session-deploy")
	if err != nil {
		t.Fatalf("sessionWorkloadExists returned error: %v", err)
	}
	if !exists {
		t.Fatal("expected deployment to count as existing workload")
	}
}
