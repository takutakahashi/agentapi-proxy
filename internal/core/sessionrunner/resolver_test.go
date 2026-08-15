package sessionrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type resolverStore struct {
	Store
	managers    []*Manager
	pools       []*Pool
	bindings    []*Binding
	preferences map[string]*Preference
}

func (s *resolverStore) ListManagers(context.Context) ([]*Manager, error) { return s.managers, nil }
func (s *resolverStore) ListPools(context.Context) ([]*Pool, error)       { return s.pools, nil }
func (s *resolverStore) ListBindings(context.Context, string) ([]*Binding, error) {
	return s.bindings, nil
}
func (s *resolverStore) GetPreference(_ context.Context, kind SubjectType, id string) (*Preference, error) {
	if value := s.preferences[string(kind)+":"+id]; value != nil {
		return value, nil
	}
	return nil, ErrNotFound
}

func TestResolverRequiresBindingAndSelectsPool(t *testing.T) {
	now := time.Now().UTC()
	store := &resolverStore{
		managers:    []*Manager{{ID: "manager-a", Enabled: true, LastHeartbeatAt: now}},
		pools:       []*Pool{{Name: "linux", ManagerID: "manager-a", Enabled: true, Labels: map[string]string{"arch": "amd64"}}},
		bindings:    []*Binding{{Pool: "linux", SubjectType: SubjectTeam, SubjectID: "org/team", Enabled: true}},
		preferences: map[string]*Preference{},
	}
	resolver := NewResolver(store, time.Minute)
	resolver.now = func() time.Time { return now }
	pool, err := resolver.Resolve(context.Background(), "alice", []string{"org/team"}, map[string]string{"allocator.pool": "linux", "allocator.arch": "amd64"})
	require.NoError(t, err)
	require.Equal(t, "manager-a", pool.ManagerID)

	_, err = resolver.Resolve(context.Background(), "bob", nil, map[string]string{"allocator.pool": "linux"})
	require.Error(t, err)
}

func TestResolverUsesUserPreference(t *testing.T) {
	store := &resolverStore{
		managers:    []*Manager{{ID: "manager-a", Enabled: true}},
		pools:       []*Pool{{Name: "linux", ManagerID: "manager-a", Enabled: true}},
		bindings:    []*Binding{{Pool: "linux", SubjectType: SubjectUser, SubjectID: "alice", Enabled: true}},
		preferences: map[string]*Preference{"user:alice": {SubjectType: SubjectUser, SubjectID: "alice", Enabled: true, DefaultPool: "linux"}},
	}
	pool, err := NewResolver(store, 0).Resolve(context.Background(), "alice", nil, nil)
	require.NoError(t, err)
	require.Equal(t, "linux", pool.Name)
}
