package sessionrunner

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Resolver struct {
	store        Store
	heartbeatTTL time.Duration
	now          func() time.Time
}

func NewResolver(store Store, heartbeatTTL time.Duration) *Resolver {
	return &Resolver{store: store, heartbeatTTL: heartbeatTTL, now: func() time.Time { return time.Now().UTC() }}
}

func (r *Resolver) AvailablePools(ctx context.Context, userID string, teams []string) ([]*Pool, error) {
	pools, err := r.store.ListPools(ctx)
	if err != nil {
		return nil, err
	}
	managers, err := r.store.ListManagers(ctx)
	if err != nil {
		return nil, err
	}
	bindings, err := r.store.ListBindings(ctx, "")
	if err != nil {
		return nil, err
	}
	managerByID := make(map[string]*Manager, len(managers))
	for _, manager := range managers {
		managerByID[manager.ID] = manager
	}
	allowed := allowedPoolNames(bindings, userID, teams)
	result := make([]*Pool, 0, len(pools))
	for _, pool := range pools {
		manager := managerByID[pool.ManagerID]
		if !allowed[pool.Name] || !pool.Enabled || pool.Draining || !r.managerAvailable(manager) {
			continue
		}
		result = append(result, pool)
	}
	return result, nil
}

func (r *Resolver) Resolve(ctx context.Context, userID string, teams []string, tags map[string]string) (*Pool, error) {
	available, err := r.AvailablePools(ctx, userID, teams)
	if err != nil {
		return nil, err
	}
	requested := strings.TrimSpace(tags["allocator.pool"])
	if requested == "" {
		if preference, getErr := r.store.GetPreference(ctx, SubjectUser, userID); getErr == nil && preference.Enabled {
			requested = preference.DefaultPool
		}
	}
	if requested == "" {
		for _, team := range teams {
			if preference, getErr := r.store.GetPreference(ctx, SubjectTeam, team); getErr == nil && preference.Enabled && preference.DefaultPool != "" {
				requested = preference.DefaultPool
				break
			}
		}
	}
	var candidates []*Pool
	for _, pool := range available {
		if requested != "" && pool.Name != requested {
			continue
		}
		if !poolMatchesTags(pool, tags) {
			continue
		}
		if requested == "" && !pool.IsDefault {
			continue
		}
		candidates = append(candidates, pool)
	}
	if len(candidates) == 0 {
		if requested != "" {
			return nil, fmt.Errorf("no authorized and healthy session pool matches %q", requested)
		}
		return nil, nil
	}
	// Stable ordering makes placement deterministic until load-aware scoring is
	// added. Multiple managers may provide the same logical pool.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ManagerID < candidates[j].ManagerID })
	return candidates[0], nil
}

func (r *Resolver) managerAvailable(manager *Manager) bool {
	if manager == nil || !manager.Enabled || manager.Draining {
		return false
	}
	if r.heartbeatTTL <= 0 || manager.LastHeartbeatAt.IsZero() {
		return true
	}
	return r.now().Sub(manager.LastHeartbeatAt) <= r.heartbeatTTL
}

func allowedPoolNames(bindings []*Binding, userID string, teams []string) map[string]bool {
	teamSet := make(map[string]bool, len(teams))
	for _, team := range teams {
		teamSet[team] = true
	}
	allowed := make(map[string]bool)
	for _, binding := range bindings {
		if !binding.Enabled {
			continue
		}
		if binding.SubjectType == SubjectUser && binding.SubjectID == userID {
			allowed[binding.Pool] = true
		}
		if binding.SubjectType == SubjectTeam && teamSet[binding.SubjectID] {
			allowed[binding.Pool] = true
		}
	}
	return allowed
}

func poolMatchesTags(pool *Pool, tags map[string]string) bool {
	for key, value := range tags {
		if !strings.HasPrefix(key, "allocator.") || key == "allocator.pool" {
			continue
		}
		label := strings.TrimPrefix(key, "allocator.")
		if pool.Labels[label] != value {
			return false
		}
	}
	return true
}
