package services

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

type silentAllocationNotifier struct{}

func (silentAllocationNotifier) Notify(context.Context) error { return nil }

func (silentAllocationNotifier) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	ch := make(chan struct{})
	return ch, func() { close(ch) }, nil
}

func TestSubmitSessionAllocationPollsWhenNotificationIsMissed(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	cfg.KubernetesSession.BasePort = 9000
	cfg.KubernetesSession.PodStartTimeout = 3
	cfg.KubernetesSession.PVCEnabled = boolPtrForTest(false)

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}},
	))
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.SetSessionAllocatorEnabled(true)
	manager.SetSessionAllocationNotifier(silentAllocationNotifier{})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const sessionID = "poll-return-session"
	resultCh := make(chan entities.Session, 1)
	errCh := make(chan error, 1)
	go func() {
		sess, err := manager.CreateSession(ctx, sessionID, &entities.RunServerRequest{
			UserID: "test-user",
			Tags:   map[string]string{"source": "test"},
		}, nil)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- sess
	}()

	waitForAllocationSecret(t, manager, sessionID)

	_, err = manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentapi-session-" + sessionID + "-svc",
			Namespace: "test-ns",
			Labels: map[string]string{
				"agentapi.proxy/session-id": sessionID,
				"agentapi.proxy/user-id":    "test-user",
				"agentapi.proxy/scope":      string(entities.ScopeUser),
				"agentapi.proxy/tag-source": "test",
			},
			Annotations: map[string]string{
				"agentapi.proxy/created-at": time.Now().UTC().Format(time.RFC3339),
				"agentapi.proxy/updated-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 9000}},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create Service() error = %v", err)
	}

	if err := manager.CompleteSessionAllocation(ctx, sessionID, SessionAllocationResult{
		Status:             "assigned",
		AllocatedSessionID: sessionID,
	}); err != nil {
		t.Fatalf("CompleteSessionAllocation() error = %v", err)
	}

	select {
	case err := <-errCh:
		t.Fatalf("CreateSession() error = %v", err)
	case sess := <-resultCh:
		if sess.ID() != sessionID {
			t.Fatalf("session ID = %q, want %q", sess.ID(), sessionID)
		}
	case <-ctx.Done():
		t.Fatalf("CreateSession() did not return after allocation was assigned: %v", ctx.Err())
	}
}

func waitForAllocationSecret(t *testing.T, manager *KubernetesSessionManager, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := manager.getSessionAllocation(context.Background(), sessionID); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("allocation Secret for session %s was not created", sessionID)
}
