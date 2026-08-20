package sessionrunner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
)

const (
	labelResource = "agentapi.proxy/session-runner-resource"
	labelPoolHash = "agentapi.proxy/session-runner-pool-hash"
	dataKey       = "resource.json"
)

type Store struct {
	kv        kvstore.Store
	namespace string
	now       func() time.Time
}

func NewStore(kv kvstore.Store, namespace string) *Store {
	return &Store{kv: kv, namespace: namespace, now: func() time.Time { return time.Now().UTC() }}
}

type secretDocument struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Metadata   documentMetadata  `json:"metadata"`
	Type       string            `json:"type"`
	Data       map[string][]byte `json:"data"`
}

type documentMetadata struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

func hashName(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

func managerName(id string) string       { return "agentapi-session-manager-" + hashName(id) }
func logicalPoolName(pool string) string { return "agentapi-session-logical-pool-" + hashName(pool) }
func poolSupplierName(managerID, pool string) string {
	return "agentapi-session-pool-supplier-" + hashName(managerID+"\x00"+pool)
}
func bindingName(id string) string    { return "agentapi-session-pool-binding-" + hashName(id) }
func runnerName(id string) string     { return "agentapi-session-runner-" + hashName(id) }
func allocationName(id string) string { return "agentapi-session-allocation-pool-" + hashName(id) }

func (s *Store) create(ctx context.Context, name, resource, pool string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	labels := map[string]string{labelResource: resource}
	if pool != "" {
		labels[labelPoolHash] = hashName(pool)
	}
	document, err := json.Marshal(secretDocument{APIVersion: "v1", Kind: "Secret",
		Metadata: documentMetadata{Name: name, Namespace: s.namespace, Labels: labels},
		Type:     "Opaque", Data: map[string][]byte{dataKey: raw}})
	if err != nil {
		return err
	}
	_, err = s.kv.Create(ctx, kvstore.Record{Kind: kvstore.KindSecret, Namespace: s.namespace, Key: name, Value: document})
	if errors.Is(err, kvstore.ErrConflict) {
		return core.ErrConflict
	}
	return err
}

func (s *Store) get(ctx context.Context, name string, out any) (kvstore.Record, error) {
	record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, name)
	if errors.Is(err, kvstore.ErrNotFound) {
		return kvstore.Record{}, core.ErrNotFound
	}
	if err != nil {
		return kvstore.Record{}, err
	}
	var document secretDocument
	if err := json.Unmarshal(record.Value, &document); err != nil {
		return kvstore.Record{}, err
	}
	if err := json.Unmarshal(document.Data[dataKey], out); err != nil {
		return kvstore.Record{}, err
	}
	return record, nil
}

func (s *Store) update(ctx context.Context, name string, value any) error {
	for attempts := 0; attempts < 5; attempts++ {
		record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, name)
		if errors.Is(err, kvstore.ErrNotFound) {
			return core.ErrNotFound
		}
		if err != nil {
			return err
		}
		var document secretDocument
		if err := json.Unmarshal(record.Value, &document); err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		document.Data[dataKey] = raw
		record.Value, err = json.Marshal(document)
		if err != nil {
			return err
		}
		if _, err = s.kv.Update(ctx, record); errors.Is(err, kvstore.ErrConflict) {
			continue
		}
		return err
	}
	return core.ErrConflict
}

func (s *Store) delete(ctx context.Context, name string) error {
	record, err := s.kv.Get(ctx, kvstore.KindSecret, s.namespace, name)
	if errors.Is(err, kvstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	err = s.kv.Delete(ctx, kvstore.KindSecret, s.namespace, name, record.Version)
	if errors.Is(err, kvstore.ErrNotFound) {
		return nil
	}
	if errors.Is(err, kvstore.ErrConflict) {
		return core.ErrConflict
	}
	return err
}

func (s *Store) list(ctx context.Context, resource string, decode func([]byte) error) error {
	items, err := s.kv.List(ctx, kvstore.Query{Kind: kvstore.KindSecret, Namespace: s.namespace})
	if err != nil {
		return err
	}
	for i := range items {
		var document secretDocument
		if err := json.Unmarshal(items[i].Value, &document); err != nil {
			continue
		}
		if document.Metadata.Labels[labelResource] != resource {
			continue
		}
		if err := decode(document.Data[dataKey]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) CreateManager(ctx context.Context, manager *core.Manager) error {
	now := s.now()
	if manager.ID == "" {
		manager.ID = uuid.NewString()
	}
	manager.CreatedAt, manager.UpdatedAt = now, now
	return s.create(ctx, managerName(manager.ID), "manager", "", manager)
}

func (s *Store) GetManager(ctx context.Context, id string) (*core.Manager, error) {
	var value core.Manager
	_, err := s.get(ctx, managerName(id), &value)
	return &value, err
}

func (s *Store) ListManagers(ctx context.Context) ([]*core.Manager, error) {
	var result []*core.Manager
	err := s.list(ctx, "manager", func(raw []byte) error {
		var value core.Manager
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		result = append(result, &value)
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, err
}

func (s *Store) UpdateManager(ctx context.Context, manager *core.Manager) error {
	current, err := s.GetManager(ctx, manager.ID)
	if err != nil {
		return err
	}
	manager.CreatedAt = current.CreatedAt
	if manager.ConnectionTokenHash == "" {
		manager.ConnectionTokenHash = current.ConnectionTokenHash
	}
	manager.UpdatedAt = s.now()
	return s.update(ctx, managerName(manager.ID), manager)
}

func (s *Store) DeleteManager(ctx context.Context, id string) error {
	return s.delete(ctx, managerName(id))
}

func (s *Store) CreateLogicalPool(ctx context.Context, pool *core.LogicalPool) error {
	now := s.now()
	pool.CreatedAt, pool.UpdatedAt = now, now
	return s.create(ctx, logicalPoolName(pool.Name), "logical-pool", pool.Name, pool)
}

func (s *Store) GetLogicalPool(ctx context.Context, name string) (*core.LogicalPool, error) {
	var value core.LogicalPool
	_, err := s.get(ctx, logicalPoolName(name), &value)
	return &value, err
}

func (s *Store) ListLogicalPools(ctx context.Context) ([]*core.LogicalPool, error) {
	var result []*core.LogicalPool
	err := s.list(ctx, "logical-pool", func(raw []byte) error {
		var value core.LogicalPool
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		result = append(result, &value)
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, err
}

func (s *Store) UpdateLogicalPool(ctx context.Context, pool *core.LogicalPool) error {
	current, err := s.GetLogicalPool(ctx, pool.Name)
	if err != nil {
		return err
	}
	pool.CreatedAt, pool.UpdatedAt = current.CreatedAt, s.now()
	return s.update(ctx, logicalPoolName(pool.Name), pool)
}

func (s *Store) DeleteLogicalPool(ctx context.Context, name string) error {
	return s.delete(ctx, logicalPoolName(name))
}

func (s *Store) CreatePoolSupplier(ctx context.Context, supplier *core.PoolSupplier) error {
	now := s.now()
	supplier.CreatedAt, supplier.UpdatedAt = now, now
	return s.create(ctx, poolSupplierName(supplier.ManagerID, supplier.Pool), "pool-supplier", supplier.Pool, supplier)
}

func (s *Store) GetPoolSupplier(ctx context.Context, managerID, pool string) (*core.PoolSupplier, error) {
	var value core.PoolSupplier
	_, err := s.get(ctx, poolSupplierName(managerID, pool), &value)
	return &value, err
}

func (s *Store) ListPoolSuppliers(ctx context.Context) ([]*core.PoolSupplier, error) {
	var result []*core.PoolSupplier
	err := s.list(ctx, "pool-supplier", func(raw []byte) error {
		var value core.PoolSupplier
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		result = append(result, &value)
		return nil
	})
	sort.Slice(result, func(i, j int) bool {
		if result[i].Pool == result[j].Pool {
			return result[i].ManagerID < result[j].ManagerID
		}
		return result[i].Pool < result[j].Pool
	})
	return result, err
}

func (s *Store) UpdatePoolSupplier(ctx context.Context, supplier *core.PoolSupplier) error {
	current, err := s.GetPoolSupplier(ctx, supplier.ManagerID, supplier.Pool)
	if err != nil {
		return err
	}
	supplier.CreatedAt, supplier.UpdatedAt = current.CreatedAt, s.now()
	return s.update(ctx, poolSupplierName(supplier.ManagerID, supplier.Pool), supplier)
}

func (s *Store) DeletePoolSupplier(ctx context.Context, managerID, pool string) error {
	return s.delete(ctx, poolSupplierName(managerID, pool))
}

func (s *Store) CreateBinding(ctx context.Context, binding *core.Binding) error {
	existing, err := s.ListBindings(ctx, binding.Pool)
	if err != nil {
		return err
	}
	for _, candidate := range existing {
		if candidate.SubjectType == binding.SubjectType && candidate.SubjectID == binding.SubjectID {
			return core.ErrConflict
		}
	}
	binding.ID = "binding-" + hashName(binding.Pool+"\x00"+string(binding.SubjectType)+"\x00"+binding.SubjectID)
	now := s.now()
	binding.CreatedAt, binding.UpdatedAt = now, now
	return s.create(ctx, bindingName(binding.ID), "binding", binding.Pool, binding)
}

func (s *Store) ListBindings(ctx context.Context, pool string) ([]*core.Binding, error) {
	var result []*core.Binding
	err := s.list(ctx, "binding", func(raw []byte) error {
		var value core.Binding
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if pool == "" || value.Pool == pool {
			if value.Role == "" {
				value.Role = core.BindingRoleUse
			}
			result = append(result, &value)
		}
		return nil
	})
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority == result[j].Priority {
			return result[i].ID < result[j].ID
		}
		return result[i].Priority > result[j].Priority
	})
	return result, err
}

func (s *Store) UpdateBinding(ctx context.Context, binding *core.Binding) error {
	bindings, err := s.ListBindings(ctx, binding.Pool)
	if err != nil {
		return err
	}
	for _, current := range bindings {
		if current.ID == binding.ID {
			binding.CreatedAt, binding.UpdatedAt = current.CreatedAt, s.now()
			return s.update(ctx, bindingName(binding.ID), binding)
		}
	}
	return core.ErrNotFound
}

func (s *Store) DeleteBinding(ctx context.Context, id string) error {
	return s.delete(ctx, bindingName(id))
}

func (s *Store) CreateRunner(ctx context.Context, runner *core.Runner) error {
	if runner.ID == "" {
		runner.ID = uuid.NewString()
	}
	now := s.now()
	runner.CreatedAt, runner.UpdatedAt, runner.LastSeen = now, now, now
	if runner.Status == "" {
		runner.Status = core.RunnerIdle
	}
	return s.create(ctx, runnerName(runner.ID), "runner", runner.Pool, runner)
}

func (s *Store) GetRunner(ctx context.Context, id string) (*core.Runner, error) {
	var value core.Runner
	_, err := s.get(ctx, runnerName(id), &value)
	return &value, err
}

func (s *Store) UpdateRunner(ctx context.Context, runner *core.Runner) error {
	current, err := s.GetRunner(ctx, runner.ID)
	if err != nil {
		return err
	}
	runner.CreatedAt, runner.UpdatedAt = current.CreatedAt, s.now()
	return s.update(ctx, runnerName(runner.ID), runner)
}

func (s *Store) ListRunners(ctx context.Context, pool string) ([]*core.Runner, error) {
	var result []*core.Runner
	err := s.list(ctx, "runner", func(raw []byte) error {
		var value core.Runner
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if pool == "" || value.Pool == pool {
			result = append(result, &value)
		}
		return nil
	})
	return result, err
}

func (s *Store) DeleteRunner(ctx context.Context, id string) error {
	return s.delete(ctx, runnerName(id))
}

func (s *Store) Enqueue(ctx context.Context, allocation *core.Allocation) error {
	now := s.now()
	allocation.Status = core.AllocationPending
	if allocation.Generation <= 0 {
		allocation.Generation = 1
	}
	allocation.CreatedAt, allocation.UpdatedAt = now, now
	return s.create(ctx, allocationName(allocation.SessionID), "allocation", allocation.Pool, allocation)
}

func (s *Store) GetAllocation(ctx context.Context, id string) (*core.Allocation, error) {
	var value core.Allocation
	_, err := s.get(ctx, allocationName(id), &value)
	return &value, err
}

func (s *Store) ListAllocations(ctx context.Context, pool string) ([]*core.Allocation, error) {
	var result []*core.Allocation
	err := s.list(ctx, "allocation", func(raw []byte) error {
		var value core.Allocation
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if pool == "" || value.Pool == pool {
			result = append(result, &value)
		}
		return nil
	})
	return result, err
}

func (s *Store) DeleteAllocation(ctx context.Context, sessionID string) error {
	return s.delete(ctx, allocationName(sessionID))
}

func (s *Store) ClaimNext(ctx context.Context, pool, runnerID string, lease time.Duration) (*core.Allocation, bool, error) {
	runner, err := s.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, false, err
	}
	if runner.Pool != pool {
		return nil, false, core.ErrUnauthorized
	}
	records, err := s.kv.List(ctx, kvstore.Query{Kind: kvstore.KindSecret, Namespace: s.namespace})
	if err != nil {
		return nil, false, err
	}
	type candidate struct {
		record     kvstore.Record
		document   secretDocument
		allocation core.Allocation
	}
	var candidates []candidate
	for _, record := range records {
		var document secretDocument
		if json.Unmarshal(record.Value, &document) != nil ||
			document.Metadata.Labels[labelResource] != "allocation" ||
			document.Metadata.Labels[labelPoolHash] != hashName(pool) {
			continue
		}
		var allocation core.Allocation
		if json.Unmarshal(document.Data[dataKey], &allocation) == nil {
			candidates = append(candidates, candidate{record: record, document: document, allocation: allocation})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].allocation.CreatedAt.Before(candidates[j].allocation.CreatedAt)
	})
	now := s.now()
	for i := range candidates {
		item := &candidates[i]
		allocation := item.allocation
		claimable := allocation.Status == core.AllocationPending || (allocation.Status == core.AllocationLeased && now.After(allocation.LeaseExpiresAt))
		if !claimable {
			continue
		}
		leaseID, err := randomToken(24)
		if err != nil {
			return nil, false, err
		}
		allocation.Status = core.AllocationLeased
		allocation.ManagerID = runner.ManagerID
		allocation.RunnerID = runnerID
		allocation.LeaseID = leaseID
		allocation.LeaseExpiresAt = now.Add(lease)
		allocation.Attempts++
		allocation.UpdatedAt = now
		raw, _ := json.Marshal(&allocation)
		item.document.Data[dataKey] = raw
		item.record.Value, _ = json.Marshal(item.document)
		if _, err := s.kv.Update(ctx, item.record); errors.Is(err, kvstore.ErrConflict) {
			continue
		} else if err != nil {
			return nil, false, err
		}
		return &allocation, true, nil
	}
	return nil, false, nil
}

func (s *Store) Acknowledge(ctx context.Context, sessionID, runnerID, leaseID string) (*core.Allocation, error) {
	return s.transitionLease(ctx, sessionID, runnerID, leaseID, core.AllocationClaimed, "")
}

func (s *Store) Fail(ctx context.Context, sessionID, runnerID, leaseID string) (*core.Allocation, error) {
	return s.transitionLease(ctx, sessionID, runnerID, leaseID, core.AllocationPending, "failed")
}

func (s *Store) transitionLease(ctx context.Context, sessionID, runnerID, leaseID string, status core.AllocationStatus, _ string) (*core.Allocation, error) {
	for attempts := 0; attempts < 5; attempts++ {
		var allocation core.Allocation
		record, err := s.get(ctx, allocationName(sessionID), &allocation)
		if err != nil {
			return nil, err
		}
		if allocation.RunnerID != runnerID || allocation.LeaseID != leaseID || allocation.Status != core.AllocationLeased {
			return nil, core.ErrConflict
		}
		if s.now().After(allocation.LeaseExpiresAt) {
			return nil, core.ErrConflict
		}
		allocation.Status = status
		allocation.UpdatedAt = s.now()
		if status == core.AllocationPending {
			allocation.RunnerID, allocation.LeaseID = "", ""
			allocation.LeaseExpiresAt = time.Time{}
			allocation.Generation++
		}
		raw, _ := json.Marshal(&allocation)
		var document secretDocument
		if err := json.Unmarshal(record.Value, &document); err != nil {
			return nil, err
		}
		document.Data[dataKey] = raw
		record.Value, _ = json.Marshal(document)
		if _, err := s.kv.Update(ctx, record); errors.Is(err, kvstore.ErrConflict) {
			continue
		} else if err != nil {
			return nil, err
		}
		return &allocation, nil
	}
	return nil, core.ErrConflict
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

var _ core.Store = (*Store)(nil)

func ValidateSubject(kind core.SubjectType, id string) error {
	if (kind != core.SubjectUser && kind != core.SubjectTeam) || id == "" {
		return fmt.Errorf("subject_type must be user or team and subject_id is required")
	}
	return nil
}
