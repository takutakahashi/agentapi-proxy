package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	// LabelSchedule is the label key for schedule resources
	LabelSchedule = "agentapi.proxy/schedule"
	// LabelScheduleID is the label key for schedule ID
	LabelScheduleID = "agentapi.proxy/schedule-id"
	// LabelScheduleScope is the label key for schedule scope (user or team)
	LabelScheduleScope = "agentapi.proxy/schedule-scope"
	// LabelScheduleUserID is the label key for schedule user ID
	LabelScheduleUserID = "agentapi.proxy/schedule-user-id"
	// LabelScheduleTeamID is the label key for schedule team ID
	LabelScheduleTeamID = "agentapi.proxy/schedule-team-id"
	// SecretKeySchedule is the key in the Secret data for single schedule JSON
	SecretKeySchedule = "schedule.json"
	// ScheduleSecretPrefix is the prefix for schedule Secret names
	ScheduleSecretPrefix = "agentapi-schedule-"
	// LegacyScheduleSecretName is the name of the legacy Secret containing all schedules
	LegacyScheduleSecretName = "agentapi-schedules"
	// LegacySecretKeySchedules is the key in the legacy Secret data for schedules JSON
	LegacySecretKeySchedules = "schedules.json"
)

// schedulesData is the JSON structure stored in the legacy Secret
type schedulesData struct {
	Schedules []*Schedule `json:"schedules"`
}

// scheduleSecretName returns the Secret name for a given schedule ID
func scheduleSecretName(id string) string {
	return ScheduleSecretPrefix + id
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

	// Check if schedule already exists
	secretName := scheduleSecretName(schedule.ID)
	_, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("schedule already exists: %s", schedule.ID)
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check schedule existence: %w", err)
	}

	now := time.Now()
	schedule.CreatedAt = now
	schedule.UpdatedAt = now

	if err := m.saveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	return nil
}

// Get retrieves a schedule by ID
func (m *KubernetesManager) Get(ctx context.Context, id string) (*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.loadSchedule(ctx, id)
}

// List retrieves schedules matching the filter
func (m *KubernetesManager) List(ctx context.Context, filter ScheduleFilter) ([]*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedules, err := m.loadAllSchedules(ctx)
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
		// Scope filter (use GetScope() to handle default value)
		if filter.Scope != "" && s.GetScope() != filter.Scope {
			continue
		}
		// TeamID filter
		if filter.TeamID != "" && s.TeamID != filter.TeamID {
			continue
		}
		// TeamIDs filter (for team-scoped schedules, check if schedule's team is in user's teams)
		if len(filter.TeamIDs) > 0 && s.GetScope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if s.TeamID == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
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

	// Check if schedule exists
	secretName := scheduleSecretName(schedule.ID)
	_, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return ErrScheduleNotFound{ID: schedule.ID}
		}
		return fmt.Errorf("failed to get schedule: %w", err)
	}

	schedule.UpdatedAt = time.Now()

	if err := m.saveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	return nil
}

// Delete removes a schedule by ID
func (m *KubernetesManager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.deleteScheduleSecret(ctx, id)
}

// GetDueSchedules returns schedules that are due for execution
func (m *KubernetesManager) GetDueSchedules(ctx context.Context, now time.Time) ([]*Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	schedules, err := m.loadAllSchedules(ctx)
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

	schedule, err := m.loadSchedule(ctx, id)
	if err != nil {
		return err
	}

	schedule.LastExecution = &record
	schedule.ExecutionCount++
	schedule.UpdatedAt = time.Now()

	if err := m.saveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	return nil
}

// UpdateNextExecution updates the next execution time for a schedule
func (m *KubernetesManager) UpdateNextExecution(ctx context.Context, id string, nextAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	schedule, err := m.loadSchedule(ctx, id)
	if err != nil {
		return err
	}

	schedule.NextExecutionAt = &nextAt
	schedule.UpdatedAt = time.Now()

	if err := m.saveSchedule(ctx, schedule); err != nil {
		return fmt.Errorf("failed to save schedule: %w", err)
	}

	return nil
}

// loadSchedule loads a single schedule from its Kubernetes Secret
func (m *KubernetesManager) loadSchedule(ctx context.Context, id string) (*Schedule, error) {
	secretName := scheduleSecretName(id)
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, ErrScheduleNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get schedule secret: %w", err)
	}

	data, ok := secret.Data[SecretKeySchedule]
	if !ok {
		return nil, fmt.Errorf("schedule secret missing data key: %s", SecretKeySchedule)
	}

	var schedule Schedule
	if err := json.Unmarshal(data, &schedule); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schedule: %w", err)
	}

	return &schedule, nil
}

// loadAllSchedules loads all schedules from Kubernetes Secrets using label selector
func (m *KubernetesManager) loadAllSchedules(ctx context.Context) ([]*Schedule, error) {
	labelSelector := fmt.Sprintf("%s=true", LabelSchedule)
	secrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list schedule secrets: %w", err)
	}

	result := make([]*Schedule, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		// Skip legacy secret
		if secret.Name == LegacyScheduleSecretName {
			continue
		}

		data, ok := secret.Data[SecretKeySchedule]
		if !ok {
			continue
		}

		var schedule Schedule
		if err := json.Unmarshal(data, &schedule); err != nil {
			continue
		}

		result = append(result, &schedule)
	}

	return result, nil
}

// saveSchedule saves a schedule to its own Kubernetes Secret
func (m *KubernetesManager) saveSchedule(ctx context.Context, schedule *Schedule) error {
	data, err := json.Marshal(schedule)
	if err != nil {
		return fmt.Errorf("failed to marshal schedule: %w", err)
	}

	secretName := scheduleSecretName(schedule.ID)
	labels := map[string]string{
		LabelSchedule:       "true",
		LabelScheduleID:     schedule.ID,
		LabelScheduleScope:  string(schedule.GetScope()),
		LabelScheduleUserID: schedule.UserID,
	}
	if schedule.TeamID != "" {
		labels[LabelScheduleTeamID] = schedule.TeamID
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: m.namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeySchedule: data,
		},
	}

	// Try to create first
	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existing, getErr := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}

			existing.Data[SecretKeySchedule] = data
			existing.Labels = labels

			_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update schedule secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create schedule secret: %w", err)
	}

	return nil
}

// deleteScheduleSecret deletes a schedule's Kubernetes Secret
func (m *KubernetesManager) deleteScheduleSecret(ctx context.Context, id string) error {
	secretName := scheduleSecretName(id)
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return ErrScheduleNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete schedule secret: %w", err)
	}
	return nil
}

// MigrateFromLegacy migrates schedules from the legacy single-Secret format
// to individual Secrets per schedule. This is idempotent - schedules that
// already exist as individual Secrets are skipped. The legacy Secret is
// preserved after migration for backup purposes.
func (m *KubernetesManager) MigrateFromLegacy(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if legacy Secret exists
	legacySecret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, LegacyScheduleSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Printf("[SCHEDULE_MIGRATION] No legacy secret found, skipping migration")
			return nil
		}
		return fmt.Errorf("failed to get legacy secret: %w", err)
	}

	// Parse legacy schedules
	data, ok := legacySecret.Data[LegacySecretKeySchedules]
	if !ok {
		log.Printf("[SCHEDULE_MIGRATION] Legacy secret has no schedules data, skipping migration")
		return nil
	}

	var sd schedulesData
	if err := json.Unmarshal(data, &sd); err != nil {
		return fmt.Errorf("failed to unmarshal legacy schedules: %w", err)
	}

	if len(sd.Schedules) == 0 {
		log.Printf("[SCHEDULE_MIGRATION] No schedules in legacy secret, skipping migration")
		return nil
	}

	log.Printf("[SCHEDULE_MIGRATION] Found %d schedules in legacy secret, starting migration", len(sd.Schedules))

	migratedCount := 0
	skippedCount := 0
	errorCount := 0

	for _, schedule := range sd.Schedules {
		// Check if individual Secret already exists
		secretName := scheduleSecretName(schedule.ID)
		_, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err == nil {
			// Schedule already migrated
			log.Printf("[SCHEDULE_MIGRATION] Schedule %s already exists, skipping", schedule.ID)
			skippedCount++
			continue
		}
		if !errors.IsNotFound(err) {
			log.Printf("[SCHEDULE_MIGRATION] Error checking schedule %s: %v, continuing", schedule.ID, err)
			errorCount++
			continue
		}

		// Migrate schedule to individual Secret
		if err := m.saveSchedule(ctx, schedule); err != nil {
			log.Printf("[SCHEDULE_MIGRATION] Failed to migrate schedule %s: %v, continuing", schedule.ID, err)
			errorCount++
			continue
		}

		log.Printf("[SCHEDULE_MIGRATION] Migrated schedule %s (%s)", schedule.ID, schedule.Name)
		migratedCount++
	}

	log.Printf("[SCHEDULE_MIGRATION] Migration complete: %d migrated, %d skipped, %d errors",
		migratedCount, skippedCount, errorCount)

	return nil
}
