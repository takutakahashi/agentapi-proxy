package services

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCreateSessionWithAllocatorReturnsAfterSubmittingAllocation(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"
	cfg.KubernetesSession.PodStartTimeout = 30

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.SetSessionAllocatorEnabled(true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	session, err := manager.CreateSession(ctx, "test-session", &entities.RunServerRequest{
		UserID: "test-user",
		Scope:  entities.ScopeUser,
		Tags:   map[string]string{"purpose": "test"},
	}, nil)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.ID() != "test-session" {
		t.Fatalf("session.ID() = %q, want test-session", session.ID())
	}
	if session.Status() != "creating" {
		t.Fatalf("session.Status() = %q, want creating", session.Status())
	}

	sec, err := manager.client.CoreV1().Secrets("test-ns").Get(context.Background(), sessionAllocationSecretName("test-session"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected allocation Secret to be created: %v", err)
	}
	if got := sec.Labels["agentapi.proxy/session-allocation-status"]; got != "pending" {
		t.Fatalf("allocation status label = %q, want pending", got)
	}
}

func TestExternalSessionAllocationIsClaimedOnlyByManager(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}

	req := &entities.RunServerRequest{UserID: "test-user", Scope: entities.ScopeUser}
	settings := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{UserID: "test-user", Scope: string(entities.ScopeUser)},
	}
	if err := manager.SubmitExternalSessionAllocation(context.Background(), "manager-a", "test-session", settings, req); err != nil {
		t.Fatalf("SubmitExternalSessionAllocation() error = %v", err)
	}

	if _, ok, err := manager.NextSessionAllocation(context.Background(), 0); err != nil || ok {
		t.Fatalf("NextSessionAllocation() = ok=%t err=%v, want ok=false err=nil", ok, err)
	}

	if _, ok, err := manager.NextExternalSessionAllocation(context.Background(), "manager-b", 0); err != nil || ok {
		t.Fatalf("NextExternalSessionAllocation(manager-b) = ok=%t err=%v, want ok=false err=nil", ok, err)
	}

	allocation, ok, err := manager.NextExternalSessionAllocation(context.Background(), "manager-a", 0)
	if err != nil {
		t.Fatalf("NextExternalSessionAllocation(manager-a) error = %v", err)
	}
	if !ok {
		t.Fatalf("NextExternalSessionAllocation(manager-a) ok=false, want true")
	}
	if allocation.SessionID != "test-session" {
		t.Fatalf("allocation.SessionID = %q, want test-session", allocation.SessionID)
	}
	if allocation.ProvisionSettings == nil {
		t.Fatalf("allocation.ProvisionSettings is nil")
	}
}

func TestCompleteSessionAllocationDeletesAllocationSecret(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}

	if err := manager.saveSessionAllocation(context.Background(), &SessionAllocationRequest{
		SessionID: "test-session",
		Request:   &entities.RunServerRequest{UserID: "test-user"},
		Status:    "allocating",
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	if err := manager.CompleteSessionAllocation(context.Background(), "test-session", SessionAllocationResult{
		Status:             "assigned",
		AllocatedSessionID: "test-session",
	}); err != nil {
		t.Fatalf("CompleteSessionAllocation() error = %v", err)
	}

	_, err = manager.client.CoreV1().Secrets("test-ns").Get(context.Background(), sessionAllocationSecretName("test-session"), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("allocation Secret should be deleted, got err=%v", err)
	}
}
