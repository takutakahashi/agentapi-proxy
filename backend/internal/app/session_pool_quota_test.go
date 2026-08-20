package app

import (
	"context"
	"errors"
	"testing"

	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	infrasessionrunner "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCheckSessionPoolQuota(t *testing.T) {
	ctx := context.Background()
	store := infrasessionrunner.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	binding := &sessionrunnercore.Binding{
		Pool: "linux", SubjectType: sessionrunnercore.SubjectUser, SubjectID: "alice",
		Enabled: true, MaxConcurrent: 2,
	}
	if err := store.CreateBinding(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(ctx, &sessionrunnercore.Allocation{SessionID: "session-1", Pool: "linux", BindingID: binding.ID}); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(ctx, &sessionrunnercore.Allocation{SessionID: "legacy", Pool: "linux"}); err != nil {
		t.Fatal(err)
	}

	server := &Server{sessionRunnerStore: store}
	if err := server.checkSessionPoolQuota(ctx, binding); err != nil {
		t.Fatalf("quota below limit: %v", err)
	}
	if err := store.Enqueue(ctx, &sessionrunnercore.Allocation{SessionID: "session-2", Pool: "linux", BindingID: binding.ID}); err != nil {
		t.Fatal(err)
	}

	err := server.checkSessionPoolQuota(ctx, binding)
	var quotaErr *sessionrunnercore.QuotaExceededError
	if !errors.As(err, &quotaErr) {
		t.Fatalf("quota error = %v, want QuotaExceededError", err)
	}
	if quotaErr.Active != 2 || quotaErr.MaxConcurrent != 2 || quotaErr.BindingID != binding.ID {
		t.Fatalf("unexpected quota error: %+v", quotaErr)
	}
}

func TestCheckSessionPoolQuotaUnlimited(t *testing.T) {
	server := &Server{}
	if err := server.checkSessionPoolQuota(context.Background(), &sessionrunnercore.Binding{MaxConcurrent: 0}); err != nil {
		t.Fatalf("unlimited quota returned error: %v", err)
	}
}

func TestDeleteSessionPoolAllocation(t *testing.T) {
	ctx := context.Background()
	store := infrasessionrunner.NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	if err := store.Enqueue(ctx, &sessionrunnercore.Allocation{SessionID: "session-1", Pool: "linux", BindingID: "binding-alice"}); err != nil {
		t.Fatal(err)
	}
	server := &Server{sessionRunnerStore: store}
	if err := server.DeleteSessionPoolAllocation(ctx, "session-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAllocation(ctx, "session-1"); !errors.Is(err, sessionrunnercore.ErrNotFound) {
		t.Fatalf("allocation remains after deletion: %v", err)
	}
}

func TestAllocationCountsTowardQuota(t *testing.T) {
	active := []sessionrunnercore.AllocationStatus{
		sessionrunnercore.AllocationPending,
		sessionrunnercore.AllocationLeased,
		sessionrunnercore.AllocationClaimed,
		sessionrunnercore.AllocationRunning,
	}
	for _, status := range active {
		if !allocationCountsTowardQuota(status) {
			t.Errorf("status %q should count toward quota", status)
		}
	}
	for _, status := range []sessionrunnercore.AllocationStatus{sessionrunnercore.AllocationCompleted, sessionrunnercore.AllocationFailed} {
		if allocationCountsTowardQuota(status) {
			t.Errorf("status %q should not count toward quota", status)
		}
	}
}
