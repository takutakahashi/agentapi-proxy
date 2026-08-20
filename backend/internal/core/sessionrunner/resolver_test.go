package sessionrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type resolverStore struct {
	Store
	managers  []*Manager
	pools     []*LogicalPool
	suppliers []*PoolSupplier
	bindings  []*Binding
}

func (s *resolverStore) ListManagers(context.Context) ([]*Manager, error) { return s.managers, nil }
func (s *resolverStore) ListLogicalPools(context.Context) ([]*LogicalPool, error) {
	return s.pools, nil
}
func (s *resolverStore) ListPoolSuppliers(context.Context) ([]*PoolSupplier, error) {
	return s.suppliers, nil
}
func (s *resolverStore) ListBindings(context.Context, string) ([]*Binding, error) {
	return s.bindings, nil
}

func TestResolverRequiresBindingAndSelectsPool(t *testing.T) {
	now := time.Now().UTC()
	store := &resolverStore{
		managers:  []*Manager{{ID: "manager-a", Enabled: true, LastHeartbeatAt: now}},
		pools:     []*LogicalPool{{Name: "linux", Enabled: true, Labels: map[string]string{"arch": "amd64"}}},
		suppliers: []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings:  []*Binding{{Pool: "linux", SubjectType: SubjectTeam, SubjectID: "org/team", Enabled: true}},
	}
	resolver := NewResolver(store, time.Minute)
	resolver.now = func() time.Time { return now }
	pool, err := resolver.Resolve(context.Background(), Subject{Type: SubjectTeam, ID: "org/team"}, map[string]string{"allocator.pool": "linux", "allocator.arch": "amd64"})
	require.NoError(t, err)
	require.Equal(t, "linux", pool.Pool.Name)
	require.Equal(t, SubjectTeam, pool.Binding.SubjectType)

	_, err = resolver.Resolve(context.Background(), Subject{Type: SubjectUser, ID: "bob"}, map[string]string{"allocator.pool": "linux"})
	require.Error(t, err)
}

func TestResolverSelectsHighestPriorityEffectiveBinding(t *testing.T) {
	store := &resolverStore{
		managers: []*Manager{{ID: "manager-a", Enabled: true}},
		pools: []*LogicalPool{
			{Name: "lower", Enabled: true},
			{Name: "higher", Enabled: true},
		},
		suppliers: []*PoolSupplier{
			{Pool: "lower", ManagerID: "manager-a", Enabled: true},
			{Pool: "higher", ManagerID: "manager-a", Enabled: true},
		},
		bindings: []*Binding{
			{Pool: "lower", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true, Priority: 10},
			{Pool: "higher", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true, Priority: 20},
		},
	}

	resolved, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, nil)
	require.NoError(t, err)
	require.Equal(t, "higher", resolved.Pool.Name)

	resolved, err = NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, map[string]string{"allocator.pool": "lower"})
	require.NoError(t, err)
	require.Equal(t, "lower", resolved.Pool.Name)
}

func TestResolverExplicitOnlyPoolRequiresPoolSelector(t *testing.T) {
	store := &resolverStore{
		managers:  []*Manager{{ID: "manager-a", Enabled: true}},
		pools:     []*LogicalPool{{Name: "native-mac", Enabled: true}},
		suppliers: []*PoolSupplier{{Pool: "native-mac", ManagerID: "manager-a", Enabled: true}},
		bindings:  []*Binding{{Pool: "native-mac", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true, ExplicitOnly: true}},
	}
	resolver := NewResolver(store, 0)

	available, err := resolver.AvailablePools(context.Background(), Subject{Type: SubjectUser, ID: "alice"})
	require.NoError(t, err)
	require.Len(t, available, 1)

	resolved, err := resolver.Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, nil)
	require.NoError(t, err)
	require.Nil(t, resolved)

	resolved, err = resolver.Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, map[string]string{"allocator.pool": "native-mac"})
	require.NoError(t, err)
	require.Equal(t, "native-mac", resolved.Pool.Name)
}

func TestResolverBreaksEqualPriorityByPoolName(t *testing.T) {
	store := &resolverStore{
		managers: []*Manager{{ID: "manager-a", Enabled: true}},
		pools: []*LogicalPool{
			{Name: "z-pool", Enabled: true},
			{Name: "a-pool", Enabled: true},
		},
		suppliers: []*PoolSupplier{
			{Pool: "z-pool", ManagerID: "manager-a", Enabled: true},
			{Pool: "a-pool", ManagerID: "manager-a", Enabled: true},
		},
		bindings: []*Binding{
			{Pool: "z-pool", SubjectType: SubjectAll, Enabled: true, Priority: 10},
			{Pool: "a-pool", SubjectType: SubjectAll, Enabled: true, Priority: 10},
		},
	}

	resolved, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, nil)
	require.NoError(t, err)
	require.Equal(t, "a-pool", resolved.Pool.Name)
}

func TestResolverAllowsClusterWideBinding(t *testing.T) {
	store := &resolverStore{
		managers:  []*Manager{{ID: "manager-a", Enabled: true}},
		pools:     []*LogicalPool{{Name: "linux", Enabled: true}},
		suppliers: []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings:  []*Binding{{Pool: "linux", SubjectType: SubjectAll, Enabled: true}},
	}

	pool, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "any-user"}, map[string]string{"allocator.pool": "linux"})
	require.NoError(t, err)
	require.Equal(t, "linux", pool.Pool.Name)

	available, err := NewResolver(store, 0).AvailablePools(context.Background(), Subject{Type: SubjectTeam, ID: "any/team"})
	require.NoError(t, err)
	require.Len(t, available, 1)
}

func TestResolverReturnsLogicalPoolOnceForMultipleSuppliers(t *testing.T) {
	store := &resolverStore{
		managers: []*Manager{{ID: "manager-a", Enabled: true}, {ID: "manager-b", Enabled: true}},
		pools:    []*LogicalPool{{Name: "linux", Enabled: true}},
		suppliers: []*PoolSupplier{
			{Pool: "linux", ManagerID: "manager-a", Enabled: true},
			{Pool: "linux", ManagerID: "manager-b", Enabled: true},
		},
		bindings: []*Binding{{Pool: "linux", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true}},
	}
	pools, err := NewResolver(store, 0).AvailablePools(context.Background(), Subject{Type: SubjectUser, ID: "alice"})
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Equal(t, "linux", pools[0].Name)
}

func TestResolverPrefersExactBindingOverAll(t *testing.T) {
	store := &resolverStore{
		managers:  []*Manager{{ID: "manager-a", Enabled: true}},
		pools:     []*LogicalPool{{Name: "linux", Enabled: true}},
		suppliers: []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings: []*Binding{
			{ID: "binding-all", Pool: "linux", SubjectType: SubjectAll, Enabled: true, MaxConcurrent: 100},
			{ID: "binding-alice", Pool: "linux", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true, MaxConcurrent: 2},
		},
	}

	resolved, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, nil)
	require.NoError(t, err)
	require.Equal(t, "binding-alice", resolved.Binding.ID)
	require.Equal(t, 2, resolved.Binding.MaxConcurrent)

	resolved, err = NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "bob"}, nil)
	require.NoError(t, err)
	require.Equal(t, "binding-all", resolved.Binding.ID)
}

func TestResolverDoesNotUseBindingsFromAnotherScope(t *testing.T) {
	store := &resolverStore{
		managers:  []*Manager{{ID: "manager-a", Enabled: true}},
		pools:     []*LogicalPool{{Name: "linux", Enabled: true}},
		suppliers: []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings:  []*Binding{{ID: "binding-team", Pool: "linux", SubjectType: SubjectTeam, SubjectID: "org/team", Enabled: true}},
	}

	resolved, err := NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectUser, ID: "alice"}, nil)
	require.NoError(t, err)
	require.Nil(t, resolved)

	store.bindings = []*Binding{{ID: "binding-user", Pool: "linux", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true}}
	resolved, err = NewResolver(store, 0).Resolve(context.Background(), Subject{Type: SubjectTeam, ID: "org/team"}, nil)
	require.NoError(t, err)
	require.Nil(t, resolved)
}

func TestResolverWithoutEffectiveBindingLeavesPoolSelectionUnchanged(t *testing.T) {
	store := &resolverStore{
		managers:  []*Manager{{ID: "manager-a", Enabled: true}},
		pools:     []*LogicalPool{{Name: "linux", Enabled: true}},
		suppliers: []*PoolSupplier{{Pool: "linux", ManagerID: "manager-a", Enabled: true}},
	}

	resolver := NewResolver(store, 0)
	for _, subject := range []Subject{
		{Type: SubjectUser, ID: "alice"},
		{Type: SubjectTeam, ID: "org/team"},
	} {
		resolved, err := resolver.Resolve(context.Background(), subject, nil)
		require.NoError(t, err)
		require.Nil(t, resolved)

		available, err := resolver.AvailablePools(context.Background(), subject)
		require.NoError(t, err)
		require.Empty(t, available)
	}
}
