package sessionrunner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKubernetesStorePoolBindings(t *testing.T) {
	ctx := context.Background()
	store := NewKubernetesStore(fake.NewSimpleClientset(), "test")
	require.NoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "manager-a", Enabled: true}))
	require.NoError(t, store.CreatePool(ctx, &core.Pool{Name: "linux", ManagerID: "manager-a", Enabled: true}))
	require.NoError(t, store.CreateBinding(ctx, &core.Binding{Pool: "linux", SubjectType: core.SubjectTeam, SubjectID: "org/team", Enabled: true, Priority: 10}))

	pools, err := store.ListPools(ctx)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	bindings, err := store.ListBindings(ctx, "linux")
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, "org/team", bindings[0].SubjectID)

	require.NoError(t, store.PutPreference(ctx, &core.Preference{SubjectType: core.SubjectTeam, SubjectID: "org/team", Enabled: true, DefaultPool: "linux"}))
	preference, err := store.GetPreference(ctx, core.SubjectTeam, "org/team")
	require.NoError(t, err)
	require.Equal(t, "linux", preference.DefaultPool)
}

func TestKubernetesStoreClaimIsAtomicAndFenced(t *testing.T) {
	ctx := context.Background()
	store := NewKubernetesStore(fake.NewSimpleClientset(), "test")
	require.NoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux"}))
	require.NoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-b", ManagerID: "manager-b", Pool: "linux"}))
	require.NoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux"}))

	var wg sync.WaitGroup
	results := make(chan *core.Allocation, 2)
	for _, runnerID := range []string{"runner-a", "runner-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			allocation, ok, err := store.ClaimNext(ctx, "linux", id, time.Minute)
			require.NoError(t, err)
			if ok {
				results <- allocation
			}
		}(runnerID)
	}
	wg.Wait()
	close(results)
	claimed := make([]*core.Allocation, 0, 1)
	for allocation := range results {
		claimed = append(claimed, allocation)
	}
	require.Len(t, claimed, 1)

	allocation := claimed[0]
	_, err := store.Acknowledge(ctx, allocation.SessionID, "stale-runner", allocation.LeaseID)
	require.ErrorIs(t, err, core.ErrConflict)
	acked, err := store.Acknowledge(ctx, allocation.SessionID, allocation.RunnerID, allocation.LeaseID)
	require.NoError(t, err)
	require.Equal(t, core.AllocationClaimed, acked.Status)
}

func TestKubernetesStoreExpiredLeaseCanBeReclaimed(t *testing.T) {
	ctx := context.Background()
	store := NewKubernetesStore(fake.NewSimpleClientset(), "test")
	require.NoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-a", ManagerID: "manager-a", Pool: "linux"}))
	require.NoError(t, store.CreateRunner(ctx, &core.Runner{ID: "runner-b", ManagerID: "manager-b", Pool: "linux"}))
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	require.NoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux"}))
	first, ok, err := store.ClaimNext(ctx, "linux", "runner-a", time.Second)
	require.NoError(t, err)
	require.True(t, ok)

	now = now.Add(2 * time.Second)
	second, ok, err := store.ClaimNext(ctx, "linux", "runner-b", time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEqual(t, first.LeaseID, second.LeaseID)
	require.Equal(t, "runner-b", second.RunnerID)
	_, err = store.Acknowledge(ctx, first.SessionID, first.RunnerID, first.LeaseID)
	require.True(t, errors.Is(err, core.ErrConflict))
}
