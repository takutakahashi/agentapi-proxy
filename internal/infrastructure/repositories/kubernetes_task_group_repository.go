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
	// LabelTaskGroupType is the label key identifying task group ConfigMaps
	LabelTaskGroupType = "agentapi.proxy/type"
	// LabelTaskGroupTypeValue is the label value for task group ConfigMaps
	LabelTaskGroupTypeValue = "task-group"
	// LabelTaskGroupScope is the label key for the task group scope (user or team)
	LabelTaskGroupScope = "agentapi.proxy/scope"
	// LabelTaskGroupOwnerHash is the label key for the hashed owner ID
	LabelTaskGroupOwnerHash = "agentapi.proxy/owner-hash"
	// LabelTaskGroupTeamHash is the label key for the hashed team ID (team scope only)
	LabelTaskGroupTeamHash = "agentapi.proxy/team-hash"

	// AnnotationTaskGroupOwnerID is the annotation key for the raw owner ID
	AnnotationTaskGroupOwnerID = "agentapi.proxy/owner-id"
	// AnnotationTaskGroupTeamID is the annotation key for the raw team ID (may contain "/")
	AnnotationTaskGroupTeamID = "agentapi.proxy/team-id"

	// ConfigMapKeyTaskGroup is the data key within the ConfigMap for task group JSON
	ConfigMapKeyTaskGroup = "task_group.json"
	// TaskGroupConfigMapPrefix is the prefix for task group ConfigMap names
	TaskGroupConfigMapPrefix = "agentapi-task-group-"
)

// taskGroupJSON is the JSON representation of a task group stored in ConfigMap
type taskGroupJSON struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Scope       string    `json:"scope"`
	OwnerID     string    `json:"owner_id"`
	TeamID      string    `json:"team_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// KubernetesTaskGroupRepository implements TaskGroupRepository using Kubernetes ConfigMaps.
type KubernetesTaskGroupRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesTaskGroupRepository creates a new KubernetesTaskGroupRepository
func NewKubernetesTaskGroupRepository(client kubernetes.Interface, namespace string) *KubernetesTaskGroupRepository {
	return &KubernetesTaskGroupRepository{
		client:    client,
		namespace: namespace,
	}
}

// Create persists a new task group as a Kubernetes ConfigMap.
func (r *KubernetesTaskGroupRepository) Create(ctx context.Context, group *entities.TaskGroup) error {
	if err := group.Validate(); err != nil {
		return fmt.Errorf("invalid task group: %w", err)
	}

	data, err := r.entityToJSONBytes(group)
	if err != nil {
		return fmt.Errorf("failed to marshal task group: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.configMapName(group.ID()),
			Namespace:   r.namespace,
			Labels:      r.buildLabels(group),
			Annotations: r.buildAnnotations(group),
		},
		Data: map[string]string{
			ConfigMapKeyTaskGroup: string(data),
		},
	}

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("task group already exists: %s", group.ID())
		}
		return fmt.Errorf("failed to create task group ConfigMap: %w", err)
	}

	return nil
}

// GetByID retrieves a task group by its UUID.
func (r *KubernetesTaskGroupRepository) GetByID(ctx context.Context, id string) (*entities.TaskGroup, error) {
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(id), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, entities.ErrTaskGroupNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get task group ConfigMap: %w", err)
	}

	return r.loadFromConfigMap(cm)
}

// List retrieves task groups matching the filter.
func (r *KubernetesTaskGroupRepository) List(ctx context.Context, filter repositories.TaskGroupFilter) ([]*entities.TaskGroup, error) {
	labelSelector := r.buildLabelSelector(filter)

	cmList, err := r.client.CoreV1().ConfigMaps(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list task group ConfigMaps: %w", err)
	}

	// Build a set of allowed team IDs for in-process filtering (TeamIDs OR logic)
	teamIDSet := make(map[string]struct{})
	for _, tid := range filter.TeamIDs {
		teamIDSet[tid] = struct{}{}
	}

	var result []*entities.TaskGroup
	for i := range cmList.Items {
		cm := &cmList.Items[i]
		group, err := r.loadFromConfigMap(cm)
		if err != nil {
			// Skip malformed entries
			continue
		}

		// In-process filter: TeamIDs (OR logic)
		if len(filter.TeamIDs) > 0 {
			if _, ok := teamIDSet[group.TeamID()]; !ok {
				continue
			}
		}

		result = append(result, group)
	}

	if result == nil {
		result = []*entities.TaskGroup{}
	}

	return result, nil
}

// Update replaces an existing task group's content.
func (r *KubernetesTaskGroupRepository) Update(ctx context.Context, group *entities.TaskGroup) error {
	if err := group.Validate(); err != nil {
		return fmt.Errorf("invalid task group: %w", err)
	}

	existing, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(group.ID()), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return entities.ErrTaskGroupNotFound{ID: group.ID()}
		}
		return fmt.Errorf("failed to get task group ConfigMap for update: %w", err)
	}

	data, err := r.entityToJSONBytes(group)
	if err != nil {
		return fmt.Errorf("failed to marshal task group: %w", err)
	}

	existing.Labels = r.buildLabels(group)
	existing.Annotations = r.buildAnnotations(group)
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	existing.Data[ConfigMapKeyTaskGroup] = string(data)

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update task group ConfigMap: %w", err)
	}

	return nil
}

// Delete removes a task group by ID.
func (r *KubernetesTaskGroupRepository) Delete(ctx context.Context, id string) error {
	err := r.client.CoreV1().ConfigMaps(r.namespace).Delete(ctx, r.configMapName(id), metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return entities.ErrTaskGroupNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete task group ConfigMap: %w", err)
	}
	return nil
}

// configMapName returns the ConfigMap name for a given task group ID.
func (r *KubernetesTaskGroupRepository) configMapName(id string) string {
	return TaskGroupConfigMapPrefix + id
}

// buildLabelSelector builds the label selector string for List queries.
func (r *KubernetesTaskGroupRepository) buildLabelSelector(filter repositories.TaskGroupFilter) string {
	parts := []string{
		fmt.Sprintf("%s=%s", LabelTaskGroupType, LabelTaskGroupTypeValue),
	}

	if len(filter.TeamIDs) == 0 {
		if filter.Scope != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskGroupScope, string(filter.Scope)))
		}
		if filter.OwnerID != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskGroupOwnerHash, hashID(filter.OwnerID)))
		}
		if filter.TeamID != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskGroupTeamHash, hashID(filter.TeamID)))
		}
	} else if filter.Scope != "" {
		parts = append(parts, fmt.Sprintf("%s=%s", LabelTaskGroupScope, string(filter.Scope)))
	}

	return strings.Join(parts, ",")
}

// buildLabels builds the label map for a ConfigMap
func (r *KubernetesTaskGroupRepository) buildLabels(g *entities.TaskGroup) map[string]string {
	labels := map[string]string{
		LabelTaskGroupType:      LabelTaskGroupTypeValue,
		LabelTaskGroupScope:     string(g.Scope()),
		LabelTaskGroupOwnerHash: hashID(g.OwnerID()),
	}
	if g.Scope() == entities.ScopeTeam && g.TeamID() != "" {
		labels[LabelTaskGroupTeamHash] = hashID(g.TeamID())
	}
	return labels
}

// buildAnnotations builds the annotation map for a ConfigMap
func (r *KubernetesTaskGroupRepository) buildAnnotations(g *entities.TaskGroup) map[string]string {
	annotations := map[string]string{
		AnnotationTaskGroupOwnerID: g.OwnerID(),
	}
	if g.TeamID() != "" {
		annotations[AnnotationTaskGroupTeamID] = g.TeamID()
	}
	return annotations
}

// entityToJSONBytes converts a TaskGroup entity to JSON bytes for storage
func (r *KubernetesTaskGroupRepository) entityToJSONBytes(g *entities.TaskGroup) ([]byte, error) {
	gj := &taskGroupJSON{
		ID:          g.ID(),
		Name:        g.Name(),
		Description: g.Description(),
		Scope:       string(g.Scope()),
		OwnerID:     g.OwnerID(),
		TeamID:      g.TeamID(),
		CreatedAt:   g.CreatedAt(),
		UpdatedAt:   g.UpdatedAt(),
	}
	return json.Marshal(gj)
}

// loadFromConfigMap deserializes a ConfigMap into a TaskGroup entity.
func (r *KubernetesTaskGroupRepository) loadFromConfigMap(cm *corev1.ConfigMap) (*entities.TaskGroup, error) {
	rawJSON, ok := cm.Data[ConfigMapKeyTaskGroup]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %s is missing task_group.json data", cm.Name)
	}

	var gj taskGroupJSON
	if err := json.Unmarshal([]byte(rawJSON), &gj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task group JSON from ConfigMap %s: %w", cm.Name, err)
	}

	// Prefer annotation values for IDs containing "/"
	if ownerID, ok := cm.Annotations[AnnotationTaskGroupOwnerID]; ok && ownerID != "" {
		gj.OwnerID = ownerID
	}
	if teamID, ok := cm.Annotations[AnnotationTaskGroupTeamID]; ok && teamID != "" {
		gj.TeamID = teamID
	}

	group := entities.NewTaskGroup(
		gj.ID,
		gj.Name,
		gj.Description,
		entities.ResourceScope(gj.Scope),
		gj.OwnerID,
		gj.TeamID,
	)
	group.SetCreatedAt(gj.CreatedAt)
	group.SetUpdatedAt(gj.UpdatedAt)

	return group, nil
}
