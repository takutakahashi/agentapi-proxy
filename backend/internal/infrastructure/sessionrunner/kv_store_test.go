package sessionrunner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"k8s.io/client-go/kubernetes/fake"
)

func TestKVStorePoolBindings(t *testing.T) {
	ctx := context.Background()
	store := NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	require.NoError(t, store.CreateManager(ctx, &core.Manager{ID: "manager-a", Name: "manager-a", Enabled: true}))
	require.NoError(t, store.CreateLogicalPool(ctx, &core.LogicalPool{Name: "linux", Enabled: true}))
	require.NoError(t, store.CreatePoolSupplier(ctx, &core.PoolSupplier{Pool: "linux", ManagerID: "manager-a", Enabled: true}))
	require.NoError(t, store.CreateBinding(ctx, &core.Binding{Pool: "linux", SubjectType: core.SubjectTeam, SubjectID: "org/team", Enabled: true, Priority: 10, MaxConcurrent: 3}))

	pools, err := store.ListLogicalPools(ctx)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	bindings, err := store.ListBindings(ctx, "linux")
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.Equal(t, "org/team", bindings[0].SubjectID)
	require.Equal(t, core.BindingRoleUse, bindings[0].Role)
	require.Equal(t, 3, bindings[0].MaxConcurrent)
	bindings[0].Role = core.BindingRoleManage
	require.NoError(t, store.UpdateBinding(ctx, bindings[0]))
	updatedBindings, err := store.ListBindings(ctx, "linux")
	require.NoError(t, err)
	require.Equal(t, core.BindingRoleManage, updatedBindings[0].Role)

}

func TestKVStoreAllocationRetainsBindingID(t *testing.T) {
	ctx := context.Background()
	store := NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	require.NoError(t, store.Enqueue(ctx, &core.Allocation{SessionID: "session-a", Pool: "linux", BindingID: "binding-alice"}))

	allocation, err := store.GetAllocation(ctx, "session-a")
	require.NoError(t, err)
	require.Equal(t, "binding-alice", allocation.BindingID)
}

func TestKVStorePoolBindingIsUniqueByPoolAndSubject(t *testing.T) {
	ctx := context.Background()
	store := NewStore(kvstore.NewKubernetesStore(fake.NewSimpleClientset()), "test")
	first := &core.Binding{Pool: "linux", SubjectType: core.SubjectTeam, SubjectID: "org/team", Enabled: true}
	require.NoError(t, store.CreateBinding(ctx, first))
	require.NotEmpty(t, first.ID)

	duplicate := &core.Binding{Pool: "linux", SubjectType: core.SubjectTeam, SubjectID: "org/team", Enabled: true}
	require.ErrorIs(t, store.CreateBinding(ctx, duplicate), core.ErrConflict)
	require.NoError(t, store.CreateBinding(ctx, &core.Binding{Pool: "gpu", SubjectType: core.SubjectTeam, SubjectID: "org/team", Enabled: true}))
	require.NoError(t, store.CreateBinding(ctx, &core.Binding{Pool: "linux", SubjectType: core.SubjectUser, SubjectID: "org/team", Enabled: true}))
}

func TestKVStoreClaimIsAtomicAndFenced(t *testing.T) {
	ctx := context.Background()
	store := newVersionedTestStore(t)
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

func TestKVStoreExpiredLeaseCanBeReclaimed(t *testing.T) {
	ctx := context.Background()
	store := newVersionedTestStore(t)
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

func newVersionedTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(&versionedMemoryStore{records: make(map[string]kvstore.Record)}, "test")
}

type versionedMemoryStore struct {
	mu      sync.Mutex
	records map[string]kvstore.Record
}

func recordKey(kind kvstore.Kind, namespace, key string) string {
	return string(kind) + "\x00" + namespace + "\x00" + key
}

func cloneRecord(record kvstore.Record) kvstore.Record {
	record.Value = append([]byte(nil), record.Value...)
	return record
}

func (s *versionedMemoryStore) Create(_ context.Context, record kvstore.Record) (kvstore.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := recordKey(record.Kind, record.Namespace, record.Key)
	if _, exists := s.records[key]; exists {
		return kvstore.Record{}, kvstore.ErrConflict
	}
	record.Version = 1
	s.records[key] = cloneRecord(record)
	return cloneRecord(record), nil
}

func (s *versionedMemoryStore) Update(_ context.Context, record kvstore.Record) (kvstore.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := recordKey(record.Kind, record.Namespace, record.Key)
	current, exists := s.records[key]
	if !exists || current.Version != record.Version {
		return kvstore.Record{}, kvstore.ErrConflict
	}
	record.Version++
	s.records[key] = cloneRecord(record)
	return cloneRecord(record), nil
}

func (s *versionedMemoryStore) Get(_ context.Context, kind kvstore.Kind, namespace, key string) (kvstore.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.records[recordKey(kind, namespace, key)]
	if !exists {
		return kvstore.Record{}, kvstore.ErrNotFound
	}
	return cloneRecord(record), nil
}

func (s *versionedMemoryStore) Delete(_ context.Context, kind kvstore.Kind, namespace, key string, version int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mapKey := recordKey(kind, namespace, key)
	record, exists := s.records[mapKey]
	if !exists {
		return kvstore.ErrNotFound
	}
	if record.Version != version {
		return kvstore.ErrConflict
	}
	delete(s.records, mapKey)
	return nil
}

func (s *versionedMemoryStore) List(_ context.Context, query kvstore.Query) ([]kvstore.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]kvstore.Record, 0)
	for _, record := range s.records {
		if record.Kind == query.Kind && record.Namespace == query.Namespace {
			result = append(result, cloneRecord(record))
		}
	}
	return result, nil
}

func (s *versionedMemoryStore) Close() error { return nil }
