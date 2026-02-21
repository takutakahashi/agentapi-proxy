package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// LabelTaskType is the label key identifying task ConfigMaps
	LabelTaskType = "agentapi.proxy/type"
	// LabelTaskTypeValue is the label value for task ConfigMaps
	LabelTaskTypeValue = "task"
	// LabelTaskScope is the label key for the task scope (user or team)
	LabelTaskScope = "agentapi.proxy/scope"
	// LabelTaskOwnerHash is the label key for the hashed owner ID
	LabelTaskOwnerHash = "agentapi.proxy/owner-hash"
	// LabelTaskTeamHash is the label key for the hashed team ID (team scope only)
	LabelTaskTeamHash = "agentapi.proxy/team-hash"
	// LabelTaskTaskType is the label key for the task type (user or agent)
	LabelTaskTaskType = "agentapi.proxy/task-type"
	// LabelTaskGroupID is the label key for the group ID
	LabelTaskGroupID = "agentapi.proxy/group-id"
	// LabelTaskStatus is the label key for the task status
	LabelTaskStatus = "agentapi.proxy/task-status"
	// LabelTaskSessionID is the label key for the session ID
	LabelTaskSessionID = "agentapi.proxy/session-id"

	// AnnotationTaskOwnerID is the annotation key for the raw owner ID
	AnnotationTaskOwnerID = "agentapi.proxy/owner-id"
	// AnnotationTaskTeamID is the annotation key for the raw team ID (may contain "/")
	AnnotationTaskTeamID = "agentapi.proxy/team-id"

	// ConfigMapKeyTask is the data key within the ConfigMap for task JSON
	ConfigMapKeyTask = "task.json"
	// TaskConfigMapPrefix is the prefix for task ConfigMap names
	TaskConfigMapPrefix = "agentapi-task-"
)

// taskLinkJSON is the JSON representation of a task link
type taskLinkJSON struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// taskJSON is the JSON representation of a task stored in ConfigMap
type taskJSON struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	TaskType    string         `json:"task_type"`
	Scope       string         `json:"scope"`
	OwnerID     string         `json:"owner_id"`
	TeamID      string         `json:"team_id,omitempty"`
	GroupID     string         `json:"group_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Links       []taskLinkJSON `json:"links,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// KubernetesTaskRepository implements TaskRepository using Kubernetes ConfigMaps.
// Each task is stored as a separate ConfigMap with labels for filtering.
type KubernetesTaskRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesTaskRepository creates a new KubernetesTaskRepository
func NewKubernetesTaskRepository(client kubernetes.Interface, namespace string) *KubernetesTaskRepository {
	return &KubernetesTaskRepository{
		client:    client,
		namespace: namespace,
	}
}

// Create persists a new task as a Kubernetes ConfigMap.
func (r *KubernetesTaskRepository) Create(ctx context.Context, task *entities.Task) error {
	if err := task.Validate(); err != nil {
		return fmt.Errorf("invalid task: %w", err)
	}

	data, err := r.entityToJSONBytes(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.configMapName(task.ID()),
			Namespace:   r.namespace,
			Labels:      r.buildLabels(task),
			Annotations: r.buildAnnotations(task),
		},
		Data: map[string]string{
			ConfigMapKeyTask: string(data),
		},
	}

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("task already exists: %s", task.ID())
		}
		return fmt.Errorf("failed to create task ConfigMap: %w", err)
	}

	return nil
}

// GetByID retrieves a task by its UUID.
func (r *KubernetesTaskRepository) GetByID(ctx context.Context, id string) (*entities.Task, error) {
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(id), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, entities.ErrTaskNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get task ConfigMap: %w", err)
	}

	return r.loadFromConfigMap(cm)
}

// List retrieves tasks matching the filter.
func (r *KubernetesTaskRepository) List(ctx context.Context, filter repositories.TaskFilter) ([]*entities.Task, error) {
	labelSelector := r.buildLabelSelector(filter)

	cmList, err := r.client.CoreV1().ConfigMaps(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list task ConfigMaps: %w", err)
	}

	// Build a set of allowed team IDs for in-process filtering (TeamIDs OR logic)
	teamIDSet := make(map[string]struct{})
	for _, tid := range filter.TeamIDs {
		teamIDSet[tid] = struct{}{}
	}

	var result []*entities.Task
	for i := range cmList.Items {
		cm := &cmList.Items[i]
		task, err := r.loadFromConfigMap(cm)
		if err != nil {
			// Skip malformed entries
			continue
		}

		// In-process filter: TeamIDs (OR logic)
		if len(filter.TeamIDs) > 0 {
			if _, ok := teamIDSet[task.TeamID()]; !ok {
				continue
			}
		}

		result = append(result, task)
	}

	if result == nil {
		result = []*entities.Task{}
	}

	return result, nil
}

// Update replaces an existing task's content.
func (r *KubernetesTaskRepository) Update(ctx context.Context, task *entities.Task) error {
	if err := task.Validate(); err != nil {
		return fmt.Errorf("invalid task: %w", err)
	}

	existing, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(task.ID()), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return entities.ErrTaskNotFound{ID: task.ID()}
		}
		return fmt.Errorf("failed to get task ConfigMap for update: %w", err)
	}

	data, err := r.entityToJSONBytes(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	existing.Labels = r.buildLabels(task)
	existing.Annotations = r.buildAnnotations(task)
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	existing.Data[ConfigMapKeyTask] = string(data)

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update task ConfigMap: %w", err)
	}

	return nil
}

// Delete removes a task by ID.
func (r *KubernetesTaskRepository) Delete(ctx context.Context, id string) error {
	err := r.client.CoreV1().ConfigMaps(r.namespace).Delete(ctx, r.configMapName(id), metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return entities.ErrTaskNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete task ConfigMap: %w", err)
	}
	return nil
}

// configMapName returns the ConfigMap name for a given task ID.
func (r *KubernetesTaskRepository) configMapName(id string) string {
	return TaskConfigMapPrefix + id
}

// buildLabelSelector builds the label selector string for List queries.
func (r *KubernetesTaskRepository) buildLabelSelector(filter repositories.TaskFilter) string {
	parts := []string{
		fmt.Sprintf("%s=%s", LabelTaskType, LabelTaskTypeValue),
	}

	if len(filter.TeamIDs) == 0 {
		if filter.Scope != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskScope, string(filter.Scope)))
		}
		if filter.OwnerID != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskOwnerHash, hashID(filter.OwnerID)))
		}
		if filter.TeamID != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskTeamHash, hashID(filter.TeamID)))
		}
	} else if filter.Scope != "" {
		parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskScope, string(filter.Scope)))
	}

	if filter.TaskType != "" {
		parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskTaskType, string(filter.TaskType)))
	}
	if filter.GroupID != "" {
		parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskGroupID, filter.GroupID))
	}
	if filter.Status != "" {
		parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskStatus, string(filter.Status)))
	}
	if filter.SessionID != "" {
		parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskSessionID, filter.SessionID))
	}

	return strings.Join(parts, ",")
}

// buildLabels builds the label map for a ConfigMap
func (r *KubernetesTaskRepository) buildLabels(t *entities.Task) map[string]string {
	labels := map[string]string{
		LabelTaskType:      LabelTaskTypeValue,
		LabelTaskScope:     string(t.Scope()),
		LabelTaskOwnerHash: hashID(t.OwnerID()),
		LabelTaskTaskType:  string(t.TaskType()),
		LabelTaskStatus:    string(t.Status()),
	}
	if t.Scope() == entities.ScopeTeam && t.TeamID() != "" {
		labels[LabelTaskTeamHash] = hashID(t.TeamID())
	}
	if t.GroupID() != "" {
		// GroupID is a UUID so it's safe to use as a label value directly
		labels[LabelTaskGroupID] = t.GroupID()
	}
	if t.SessionID() != "" {
		// SessionID is a UUID so it's safe to use as a label value directly
		labels[LabelTaskSessionID] = t.SessionID()
	}
	return labels
}

// buildAnnotations builds the annotation map for a ConfigMap
func (r *KubernetesTaskRepository) buildAnnotations(t *entities.Task) map[string]string {
	annotations := map[string]string{
		AnnotationTaskOwnerID: t.OwnerID(),
	}
	if t.TeamID() != "" {
		annotations[AnnotationTaskTeamID] = t.TeamID()
	}
	return annotations
}

// entityToJSONBytes converts a Task entity to JSON bytes for storage
func (r *KubernetesTaskRepository) entityToJSONBytes(t *entities.Task) ([]byte, error) {
	links := make([]taskLinkJSON, 0, len(t.Links()))
	for _, l := range t.Links() {
		links = append(links, taskLinkJSON{
			ID:    l.ID(),
			URL:   l.URL(),
			Title: l.Title(),
		})
	}

	tj := &taskJSON{
		ID:          t.ID(),
		Title:       t.Title(),
		Description: t.Description(),
		Status:      string(t.Status()),
		TaskType:    string(t.TaskType()),
		Scope:       string(t.Scope()),
		OwnerID:     t.OwnerID(),
		TeamID:      t.TeamID(),
		GroupID:     t.GroupID(),
		SessionID:   t.SessionID(),
		Links:       links,
		CreatedAt:   t.CreatedAt(),
		UpdatedAt:   t.UpdatedAt(),
	}
	return json.Marshal(tj)
}

// loadFromConfigMap deserializes a ConfigMap into a Task entity.
func (r *KubernetesTaskRepository) loadFromConfigMap(cm *corev1.ConfigMap) (*entities.Task, error) {
	rawJSON, ok := cm.Data[ConfigMapKeyTask]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %s is missing task.json data", cm.Name)
	}

	var tj taskJSON
	if err := json.Unmarshal([]byte(rawJSON), &tj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task JSON from ConfigMap %s: %w", cm.Name, err)
	}

	// Prefer annotation values for IDs containing "/"
	if ownerID, ok := cm.Annotations[AnnotationTaskOwnerID]; ok && ownerID != "" {
		tj.OwnerID = ownerID
	}
	if teamID, ok := cm.Annotations[AnnotationTaskTeamID]; ok && teamID != "" {
		tj.TeamID = teamID
	}

	links := make([]*entities.TaskLink, 0, len(tj.Links))
	for _, l := range tj.Links {
		links = append(links, entities.NewTaskLink(l.ID, l.URL, l.Title))
	}

	task := entities.NewTask(
		tj.ID,
		tj.Title,
		tj.Description,
		entities.TaskStatus(tj.Status),
		entities.TaskType(tj.TaskType),
		entities.ResourceScope(tj.Scope),
		tj.OwnerID,
		tj.TeamID,
		tj.GroupID,
		tj.SessionID,
		links,
	)
	task.SetCreatedAt(tj.CreatedAt)
	task.SetUpdatedAt(tj.UpdatedAt)

	return task, nil
}
