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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	labelResource = "agentapi.proxy/session-runner-resource"
	labelPoolHash = "agentapi.proxy/session-runner-pool-hash"
	dataKey       = "resource.json"
)

type KubernetesStore struct {
	client    kubernetes.Interface
	namespace string
	now       func() time.Time
}

func NewKubernetesStore(client kubernetes.Interface, namespace string) *KubernetesStore {
	return &KubernetesStore{client: client, namespace: namespace, now: func() time.Time { return time.Now().UTC() }}
}

func hashName(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}

func managerName(id string) string { return "agentapi-session-manager-" + hashName(id) }
func poolName(managerID, pool string) string {
	return "agentapi-session-pool-" + hashName(managerID+"\x00"+pool)
}
func bindingName(id string) string { return "agentapi-session-pool-binding-" + hashName(id) }
func preferenceName(kind core.SubjectType, id string) string {
	return "agentapi-session-pool-preference-" + hashName(string(kind)+"\x00"+id)
}
func runnerName(id string) string     { return "agentapi-session-runner-" + hashName(id) }
func allocationName(id string) string { return "agentapi-session-allocation-pool-" + hashName(id) }

func (s *KubernetesStore) create(ctx context.Context, name, resource, pool string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	labels := map[string]string{labelResource: resource}
	if pool != "" {
		labels[labelPoolHash] = hashName(pool)
	}
	_, err = s.client.CoreV1().Secrets(s.namespace).Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace, Labels: labels},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{dataKey: raw},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return core.ErrConflict
	}
	return err
}

func (s *KubernetesStore) get(ctx context.Context, name string, out any) (*corev1.Secret, error) {
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, core.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(secret.Data[dataKey], out); err != nil {
		return nil, err
	}
	return secret, nil
}

func (s *KubernetesStore) update(ctx context.Context, name string, value any) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return core.ErrNotFound
		}
		if err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		secret.Data[dataKey] = raw
		_, err = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		return err
	})
}

func (s *KubernetesStore) delete(ctx context.Context, name string) error {
	err := s.client.CoreV1().Secrets(s.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (s *KubernetesStore) list(ctx context.Context, resource string, decode func([]byte) error) error {
	items, err := s.client.CoreV1().Secrets(s.namespace).List(ctx, metav1.ListOptions{LabelSelector: labelResource + "=" + resource})
	if err != nil {
		return err
	}
	for i := range items.Items {
		if err := decode(items.Items[i].Data[dataKey]); err != nil {
			return err
		}
	}
	return nil
}

func (s *KubernetesStore) CreateManager(ctx context.Context, manager *core.Manager) error {
	now := s.now()
	if manager.ID == "" {
		manager.ID = uuid.NewString()
	}
	manager.CreatedAt, manager.UpdatedAt = now, now
	return s.create(ctx, managerName(manager.ID), "manager", "", manager)
}

func (s *KubernetesStore) GetManager(ctx context.Context, id string) (*core.Manager, error) {
	var value core.Manager
	_, err := s.get(ctx, managerName(id), &value)
	return &value, err
}

func (s *KubernetesStore) ListManagers(ctx context.Context) ([]*core.Manager, error) {
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

func (s *KubernetesStore) UpdateManager(ctx context.Context, manager *core.Manager) error {
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

func (s *KubernetesStore) DeleteManager(ctx context.Context, id string) error {
	return s.delete(ctx, managerName(id))
}

func (s *KubernetesStore) CreatePool(ctx context.Context, pool *core.Pool) error {
	now := s.now()
	pool.CreatedAt, pool.UpdatedAt = now, now
	return s.create(ctx, poolName(pool.ManagerID, pool.Name), "pool", pool.Name, pool)
}

func (s *KubernetesStore) GetPool(ctx context.Context, managerID, name string) (*core.Pool, error) {
	var value core.Pool
	_, err := s.get(ctx, poolName(managerID, name), &value)
	return &value, err
}

func (s *KubernetesStore) ListPools(ctx context.Context) ([]*core.Pool, error) {
	var result []*core.Pool
	err := s.list(ctx, "pool", func(raw []byte) error {
		var value core.Pool
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		result = append(result, &value)
		return nil
	})
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].ManagerID < result[j].ManagerID
		}
		return result[i].Name < result[j].Name
	})
	return result, err
}

func (s *KubernetesStore) UpdatePool(ctx context.Context, pool *core.Pool) error {
	current, err := s.GetPool(ctx, pool.ManagerID, pool.Name)
	if err != nil {
		return err
	}
	pool.CreatedAt, pool.UpdatedAt = current.CreatedAt, s.now()
	return s.update(ctx, poolName(pool.ManagerID, pool.Name), pool)
}

func (s *KubernetesStore) DeletePool(ctx context.Context, managerID, name string) error {
	return s.delete(ctx, poolName(managerID, name))
}

func (s *KubernetesStore) CreateBinding(ctx context.Context, binding *core.Binding) error {
	if binding.ID == "" {
		binding.ID = uuid.NewString()
	}
	now := s.now()
	binding.CreatedAt, binding.UpdatedAt = now, now
	return s.create(ctx, bindingName(binding.ID), "binding", binding.Pool, binding)
}

func (s *KubernetesStore) ListBindings(ctx context.Context, pool string) ([]*core.Binding, error) {
	var result []*core.Binding
	err := s.list(ctx, "binding", func(raw []byte) error {
		var value core.Binding
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if pool == "" || value.Pool == pool {
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

func (s *KubernetesStore) DeleteBinding(ctx context.Context, id string) error {
	return s.delete(ctx, bindingName(id))
}

func (s *KubernetesStore) PutPreference(ctx context.Context, preference *core.Preference) error {
	preference.UpdatedAt = s.now()
	name := preferenceName(preference.SubjectType, preference.SubjectID)
	if err := s.create(ctx, name, "preference", preference.DefaultPool, preference); err != nil {
		if !errors.Is(err, core.ErrConflict) {
			return err
		}
		return s.update(ctx, name, preference)
	}
	return nil
}

func (s *KubernetesStore) GetPreference(ctx context.Context, kind core.SubjectType, id string) (*core.Preference, error) {
	var value core.Preference
	_, err := s.get(ctx, preferenceName(kind, id), &value)
	return &value, err
}

func (s *KubernetesStore) CreateRunner(ctx context.Context, runner *core.Runner) error {
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

func (s *KubernetesStore) GetRunner(ctx context.Context, id string) (*core.Runner, error) {
	var value core.Runner
	_, err := s.get(ctx, runnerName(id), &value)
	return &value, err
}

func (s *KubernetesStore) UpdateRunner(ctx context.Context, runner *core.Runner) error {
	current, err := s.GetRunner(ctx, runner.ID)
	if err != nil {
		return err
	}
	runner.CreatedAt, runner.UpdatedAt = current.CreatedAt, s.now()
	return s.update(ctx, runnerName(runner.ID), runner)
}

func (s *KubernetesStore) ListRunners(ctx context.Context, pool string) ([]*core.Runner, error) {
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

func (s *KubernetesStore) Enqueue(ctx context.Context, allocation *core.Allocation) error {
	now := s.now()
	allocation.Status = core.AllocationPending
	if allocation.Generation <= 0 {
		allocation.Generation = 1
	}
	allocation.CreatedAt, allocation.UpdatedAt = now, now
	return s.create(ctx, allocationName(allocation.SessionID), "allocation", allocation.Pool, allocation)
}

func (s *KubernetesStore) GetAllocation(ctx context.Context, id string) (*core.Allocation, error) {
	var value core.Allocation
	_, err := s.get(ctx, allocationName(id), &value)
	return &value, err
}

func (s *KubernetesStore) ClaimNext(ctx context.Context, pool, runnerID string, lease time.Duration) (*core.Allocation, bool, error) {
	runner, err := s.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, false, err
	}
	if runner.Pool != pool {
		return nil, false, core.ErrUnauthorized
	}
	secrets, err := s.client.CoreV1().Secrets(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelResource + "=allocation," + labelPoolHash + "=" + hashName(pool),
	})
	if err != nil {
		return nil, false, err
	}
	sort.Slice(secrets.Items, func(i, j int) bool {
		return secrets.Items[i].CreationTimestamp.Before(&secrets.Items[j].CreationTimestamp)
	})
	now := s.now()
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		var allocation core.Allocation
		if err := json.Unmarshal(secret.Data[dataKey], &allocation); err != nil {
			continue
		}
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
		secret.Data[dataKey] = raw
		if _, err := s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{}); apierrors.IsConflict(err) {
			continue
		} else if err != nil {
			return nil, false, err
		}
		return &allocation, true, nil
	}
	return nil, false, nil
}

func (s *KubernetesStore) Acknowledge(ctx context.Context, sessionID, runnerID, leaseID string) (*core.Allocation, error) {
	return s.transitionLease(ctx, sessionID, runnerID, leaseID, core.AllocationClaimed, "")
}

func (s *KubernetesStore) Fail(ctx context.Context, sessionID, runnerID, leaseID string) (*core.Allocation, error) {
	return s.transitionLease(ctx, sessionID, runnerID, leaseID, core.AllocationPending, "failed")
}

func (s *KubernetesStore) transitionLease(ctx context.Context, sessionID, runnerID, leaseID string, status core.AllocationStatus, _ string) (*core.Allocation, error) {
	var result *core.Allocation
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var allocation core.Allocation
		secret, err := s.get(ctx, allocationName(sessionID), &allocation)
		if err != nil {
			return err
		}
		if allocation.RunnerID != runnerID || allocation.LeaseID != leaseID || allocation.Status != core.AllocationLeased {
			return core.ErrConflict
		}
		if s.now().After(allocation.LeaseExpiresAt) {
			return core.ErrConflict
		}
		allocation.Status = status
		allocation.UpdatedAt = s.now()
		if status == core.AllocationPending {
			allocation.RunnerID, allocation.LeaseID = "", ""
			allocation.LeaseExpiresAt = time.Time{}
			allocation.Generation++
		}
		raw, _ := json.Marshal(&allocation)
		secret.Data[dataKey] = raw
		if _, err := s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
			return err
		}
		result = &allocation
		return nil
	})
	return result, err
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

var _ core.Store = (*KubernetesStore)(nil)

func ValidateSubject(kind core.SubjectType, id string) error {
	if (kind != core.SubjectUser && kind != core.SubjectTeam) || id == "" {
		return fmt.Errorf("subject_type must be user or team and subject_id is required")
	}
	return nil
}
