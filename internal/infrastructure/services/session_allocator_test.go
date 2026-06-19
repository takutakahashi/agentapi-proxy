package services

import (
	"context"
	"testing"
	"time"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
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

func TestNextSessionAllocationClaimsRequestCreatedWhileSubscribing(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.SetSessionAllocationNotifier(&subscribeHookNotifier{
		onSubscribe: func() {
			if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
				SessionID: "test-session",
				Request:   &entities.RunServerRequest{UserID: "test-user", Scope: entities.ScopeUser},
				Status:    sessionallocation.StatusPending,
			}); err != nil {
				t.Fatalf("saveSessionAllocation() error = %v", err)
			}
		},
	})

	allocation, ok, err := manager.NextSessionAllocation(context.Background(), 30*time.Second)
	if err != nil {
		t.Fatalf("NextSessionAllocation() error = %v", err)
	}
	if !ok {
		t.Fatalf("NextSessionAllocation() ok=false, want true")
	}
	if allocation.SessionID != "test-session" {
		t.Fatalf("allocation.SessionID = %q, want test-session", allocation.SessionID)
	}
	if allocation.Status != sessionallocation.StatusAllocating {
		t.Fatalf("allocation.Status = %q, want allocating", allocation.Status)
	}
}

func TestNextExternalSessionAllocationClaimsRequestCreatedWhileSubscribing(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.SetSessionAllocationNotifier(&subscribeHookNotifier{
		onSubscribe: func() {
			if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
				SessionID: "test-session",
				ManagerID: "manager-a",
				Request:   &entities.RunServerRequest{UserID: "test-user", Scope: entities.ScopeUser},
				Status:    sessionallocation.StatusPending,
			}); err != nil {
				t.Fatalf("saveSessionAllocation() error = %v", err)
			}
		},
	})

	allocation, ok, err := manager.NextExternalSessionAllocation(context.Background(), "manager-a", 30*time.Second)
	if err != nil {
		t.Fatalf("NextExternalSessionAllocation() error = %v", err)
	}
	if !ok {
		t.Fatalf("NextExternalSessionAllocation() ok=false, want true")
	}
	if allocation.SessionID != "test-session" {
		t.Fatalf("allocation.SessionID = %q, want test-session", allocation.SessionID)
	}
	if allocation.Status != sessionallocation.StatusAllocating {
		t.Fatalf("allocation.Status = %q, want allocating", allocation.Status)
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

	if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "test-session",
		Request:   &entities.RunServerRequest{UserID: "test-user"},
		Status:    sessionallocation.StatusAllocating,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	if err := manager.CompleteSessionAllocation(context.Background(), "test-session", sessionallocation.AllocationResult{
		Status:             sessionallocation.StatusAssigned,
		AllocatedSessionID: "test-session",
	}); err != nil {
		t.Fatalf("CompleteSessionAllocation() error = %v", err)
	}

	_, err = manager.client.CoreV1().Secrets("test-ns").Get(context.Background(), sessionAllocationSecretName("test-session"), metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("allocation Secret should be deleted, got err=%v", err)
	}
}

func TestListSessionsIncludesAllocatingSessionAllocation(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}

	if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "test-session",
		Request: &entities.RunServerRequest{
			UserID: "test-user",
			Scope:  entities.ScopeUser,
			Tags:   map[string]string{"purpose": "test"},
		},
		Status: sessionallocation.StatusAllocating,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	sessions := manager.ListSessions(entities.SessionFilter{
		UserID: "test-user",
		Status: "allocating",
		Tags:   map[string]string{"purpose": "test"},
		Scope:  entities.ScopeUser,
	})

	if len(sessions) != 1 {
		t.Fatalf("ListSessions() returned %d sessions, want 1", len(sessions))
	}
	if sessions[0].ID() != "test-session" {
		t.Fatalf("session.ID() = %q, want test-session", sessions[0].ID())
	}
	if sessions[0].Status() != "allocating" {
		t.Fatalf("session.Status() = %q, want allocating", sessions[0].Status())
	}
}

func TestSessionAllocationInvalidatesSessionListCache(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	cache := &recordingSessionListCacheRepo{}
	manager.SetSessionListCacheRepository(cache)

	if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "test-session",
		Request:   &entities.RunServerRequest{UserID: "test-user", Scope: entities.ScopeUser},
		Status:    sessionallocation.StatusPending,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}
	if cache.invalidations != 1 {
		t.Fatalf("invalidations after save = %d, want 1", cache.invalidations)
	}

	if err := manager.deleteSessionAllocation(context.Background(), "test-session"); err != nil {
		t.Fatalf("deleteSessionAllocation() error = %v", err)
	}
	if cache.invalidations != 2 {
		t.Fatalf("invalidations after delete = %d, want 2", cache.invalidations)
	}
}

func TestListSessionsDoesNotPopulateCacheWhileAllocationExists(t *testing.T) {
	t.Setenv("LOG_DIR", t.TempDir())

	cfg := config.DefaultConfig()
	cfg.KubernetesSession.Namespace = "test-ns"

	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), fake.NewSimpleClientset())
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	cache := &recordingSessionListCacheRepo{}
	manager.SetSessionListCacheRepository(cache)

	if err := manager.saveSessionAllocation(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "test-session",
		Request: &entities.RunServerRequest{
			UserID: "test-user",
			Scope:  entities.ScopeUser,
			Tags:   map[string]string{"purpose": "test"},
		},
		Status: sessionallocation.StatusPending,
	}); err != nil {
		t.Fatalf("saveSessionAllocation() error = %v", err)
	}

	sessions := manager.ListSessions(entities.SessionFilter{
		UserID: "test-user",
		Tags:   map[string]string{"purpose": "test"},
		Scope:  entities.ScopeUser,
	})
	if len(sessions) != 1 {
		t.Fatalf("ListSessions() returned %d sessions, want 1", len(sessions))
	}
	if cache.setCalls != 0 {
		t.Fatalf("SetSessionListCache calls = %d, want 0 while allocation exists", cache.setCalls)
	}
}

type recordingSessionListCacheRepo struct {
	setCalls      int
	invalidations int
}

func (r *recordingSessionListCacheRepo) SetSessionListCache(context.Context, string, []portrepos.CachedSessionDTO, time.Duration) error {
	r.setCalls++
	return nil
}

func (r *recordingSessionListCacheRepo) GetSessionListCache(context.Context, string) ([]portrepos.CachedSessionDTO, error) {
	return nil, nil
}

func (r *recordingSessionListCacheRepo) InvalidateSessionListCache(context.Context, string) error {
	r.invalidations++
	return nil
}

type subscribeHookNotifier struct {
	onSubscribe func()
}

func (n *subscribeHookNotifier) Notify(context.Context) error {
	return nil
}

func (n *subscribeHookNotifier) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	if n.onSubscribe != nil {
		n.onSubscribe()
		n.onSubscribe = nil
	}
	ch := make(chan struct{})
	return ch, func() { close(ch) }, nil
}
