package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// LabelSchedule is the label key for schedule resources
	LabelSchedule = "agentapi.proxy/schedule"
	// LabelScheduleUserID is the label key for schedule user ID
	LabelScheduleUserID = "agentapi.proxy/schedule-user-id"
	// SecretKeySchedules is the key in the Secret data for schedules JSON
	SecretKeySchedules = "schedules.json"
	// ScheduleSecretName is the name of the Secret containing all schedules
	ScheduleSecretName = "agentapi-schedules"
)

// schedulesData is the JSON structure stored in the Secret
type schedulesData struct {
	Schedules []*Schedule `json:"schedules"`
}

// KubernetesManager implements Manager using Kubernetes Secrets
type KubernetesManager struct {
	client    kubernetes.Interface
	namespace string
	mu        sync.RWMutex
}

// NewKubernetesManager creates a new KubernetesManager
func NewKubernetesManager(client kubernetes.Interface, namespace string) *KubernetesManager {
	return &KubernetesManager{
		client:    client,
		namespace: namespace,
	}
}

// Create creates a new schedule
func (m *KubernetesManager) Create(ctx context.Context, schedule *Schedule) error {
	if err := schedule.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	schedules, err := m.loadSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load schedules: %w", err)
	}

	// Check for duplicate ID
	for _, s := range schedules {
		if s.ID == schedule.ID {
			return fmt.Errorf("schedule already exists: %s", schedule.ID)
		}
	}

	now := time.Now()
	schedule.CreatedAt = now
	schedule.UpdatedAt = now

	schedules = append(schedules, schedule)

	if err := m.saveSchedules(ctx, schedules); err != nil {
		return fmt.Errorf("failed to save schedules: %w", err)
	}

	return nil
}

// Get retrieves a schedule by ID
func (m *KubernetesManager) Get(ctx context.Context, id string) (*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedules, err := m.loadSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedules: %w", err)
	}

	for _, s := range schedules {
		if s.ID == id {
			return s, nil
		}
	}

	return nil, ErrScheduleNotFound{ID: id}
}

// List retrieves schedules matching the filter
func (m *KubernetesManager) List(ctx context.Context, filter ScheduleFilter) ([]*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedules, err := m.loadSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedules: %w", err)
	}

	var result []*Schedule
	for _, s := range schedules {
		if filter.UserID != "" && s.UserID != filter.UserID {
			continue
		}
		if filter.Status != "" && s.Status != filter.Status {
			continue
		}
		result = append(result, s)
	}

	return result, nil
}

// Update updates an existing schedule
func (m *KubernetesManager) Update(ctx context.Context, schedule *Schedule) error {
	if err := schedule.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	schedules, err := m.loadSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load schedules: %w", err)
	}

	found := false
	for i, s := range schedules {
		if s.ID == schedule.ID {
			schedule.UpdatedAt = time.Now()
			schedules[i] = schedule
			found = true
			break
		}
	}

	if !found {
		return ErrScheduleNotFound{ID: schedule.ID}
	}

	if err := m.saveSchedules(ctx, schedules); err != nil {
		return fmt.Errorf("failed to save schedules: %w", err)
	}

	return nil
}

// Delete removes a schedule by ID
func (m *KubernetesManager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	schedules, err := m.loadSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load schedules: %w", err)
	}

	found := false
	var newSchedules []*Schedule
	for _, s := range schedules {
		if s.ID == id {
			found = true
			continue
		}
		newSchedules = append(newSchedules, s)
	}

	if !found {
		return ErrScheduleNotFound{ID: id}
	}

	if err := m.saveSchedules(ctx, newSchedules); err != nil {
		return fmt.Errorf("failed to save schedules: %w", err)
	}

	return nil
}

// GetDueSchedules returns schedules that are due for execution
func (m *KubernetesManager) GetDueSchedules(ctx context.Context, now time.Time) ([]*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedules, err := m.loadSchedules(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedules: %w", err)
	}

	var due []*Schedule
	for _, s := range schedules {
		if s.IsDue(now) {
			due = append(due, s)
		}
	}

	return due, nil
}

// RecordExecution records an execution attempt
func (m *KubernetesManager) RecordExecution(ctx context.Context, id string, record ExecutionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	schedules, err := m.loadSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load schedules: %w", err)
	}

	found := false
	for _, s := range schedules {
		if s.ID == id {
			s.LastExecution = &record
			s.ExecutionCount++
			s.UpdatedAt = time.Now()
			found = true
			break
		}
	}

	if !found {
		return ErrScheduleNotFound{ID: id}
	}

	if err := m.saveSchedules(ctx, schedules); err != nil {
		return fmt.Errorf("failed to save schedules: %w", err)
	}

	return nil
}

// UpdateNextExecution updates the next execution time for a schedule
func (m *KubernetesManager) UpdateNextExecution(ctx context.Context, id string, nextAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	schedules, err := m.loadSchedules(ctx)
	if err != nil {
		return fmt.Errorf("failed to load schedules: %w", err)
	}

	found := false
	for _, s := range schedules {
		if s.ID == id {
			s.NextExecutionAt = &nextAt
			s.UpdatedAt = time.Now()
			found = true
			break
		}
	}

	if !found {
		return ErrScheduleNotFound{ID: id}
	}

	if err := m.saveSchedules(ctx, schedules); err != nil {
		return fmt.Errorf("failed to save schedules: %w", err)
	}

	return nil
}

// loadSchedules loads schedules from the Kubernetes Secret
func (m *KubernetesManager) loadSchedules(ctx context.Context) ([]*Schedule, error) {
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, ScheduleSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return []*Schedule{}, nil
		}
		return nil, fmt.Errorf("failed to get schedules secret: %w", err)
	}

	data, ok := secret.Data[SecretKeySchedules]
	if !ok {
		return []*Schedule{}, nil
	}

	var sd schedulesData
	if err := json.Unmarshal(data, &sd); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schedules: %w", err)
	}

	return sd.Schedules, nil
}

// saveSchedules saves schedules to the Kubernetes Secret
func (m *KubernetesManager) saveSchedules(ctx context.Context, schedules []*Schedule) error {
	sd := schedulesData{Schedules: schedules}
	data, err := json.Marshal(sd)
	if err != nil {
		return fmt.Errorf("failed to marshal schedules: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ScheduleSecretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				LabelSchedule: "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeySchedules: data,
		},
	}

	// Try to create first
	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Get existing secret to preserve any other data
			existing, getErr := m.client.CoreV1().Secrets(m.namespace).Get(ctx, ScheduleSecretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}

			existing.Data[SecretKeySchedules] = data
			_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update schedules secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create schedules secret: %w", err)
	}

	return nil
}
