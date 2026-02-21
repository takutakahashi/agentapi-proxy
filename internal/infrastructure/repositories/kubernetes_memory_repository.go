package repositories

import (
	"context"
	"crypto/sha256"
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
	// LabelMemoryType is the label key identifying memory ConfigMaps
	LabelMemoryType = "agentapi.proxy/type"
	// LabelMemoryTypeValue is the label value for memory ConfigMaps
	LabelMemoryTypeValue = "memory"
	// LabelMemoryScope is the label key for the memory scope (user or team)
	LabelMemoryScope = "agentapi.proxy/scope"
	// LabelMemoryOwnerHash is the label key for the hashed owner ID
	LabelMemoryOwnerHash = "agentapi.proxy/owner-hash"
	// LabelMemoryTeamHash is the label key for the hashed team ID (team scope only)
	LabelMemoryTeamHash = "agentapi.proxy/team-hash"

	// AnnotationMemoryOwnerID is the annotation key for the raw owner ID
	AnnotationMemoryOwnerID = "agentapi.proxy/owner-id"
	// AnnotationMemoryTeamID is the annotation key for the raw team ID (may contain "/")
	AnnotationMemoryTeamID = "agentapi.proxy/team-id"

	// ConfigMapKeyMemory is the data key within the ConfigMap for memory JSON
	ConfigMapKeyMemory = "memory.json"
	// MemoryConfigMapPrefix is the prefix for memory ConfigMap names
	MemoryConfigMapPrefix = "agentapi-memory-"
)

// memoryJSON is the JSON representation of a memory entry stored in ConfigMap
type memoryJSON struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Tags      map[string]string `json:"tags,omitempty"`
	Scope     string            `json:"scope"`
	OwnerID   string            `json:"owner_id"`
	TeamID    string            `json:"team_id,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// KubernetesMemoryRepository implements MemoryRepository using Kubernetes ConfigMaps.
// Each memory entry is stored as a separate ConfigMap with labels for filtering.
// Labels contain hashed IDs to handle values with characters (e.g. "/") that are
// not allowed in Kubernetes label values. Raw values are stored in annotations.
type KubernetesMemoryRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesMemoryRepository creates a new KubernetesMemoryRepository
func NewKubernetesMemoryRepository(client kubernetes.Interface, namespace string) *KubernetesMemoryRepository {
	return &KubernetesMemoryRepository{
		client:    client,
		namespace: namespace,
	}
}

// Create persists a new memory entry as a Kubernetes ConfigMap.
// Returns an error if an entry with the same ID already exists.
func (r *KubernetesMemoryRepository) Create(ctx context.Context, memory *entities.Memory) error {
	if err := memory.Validate(); err != nil {
		return fmt.Errorf("invalid memory entry: %w", err)
	}

	data, err := r.entityToJSONBytes(memory)
	if err != nil {
		return fmt.Errorf("failed to marshal memory: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.configMapName(memory.ID()),
			Namespace:   r.namespace,
			Labels:      r.buildLabels(memory),
			Annotations: r.buildAnnotations(memory),
		},
		Data: map[string]string{
			ConfigMapKeyMemory: string(data),
		},
	}

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("memory entry already exists: %s", memory.ID())
		}
		return fmt.Errorf("failed to create memory ConfigMap: %w", err)
	}

	return nil
}

// GetByID retrieves a memory entry by its UUID.
func (r *KubernetesMemoryRepository) GetByID(ctx context.Context, id string) (*entities.Memory, error) {
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(id), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, entities.ErrMemoryNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get memory ConfigMap: %w", err)
	}

	return r.loadFromConfigMap(cm)
}

// List retrieves memory entries matching the filter.
// K8s label selectors are used to narrow candidates; tag/text filtering is in-process.
func (r *KubernetesMemoryRepository) List(ctx context.Context, filter repositories.MemoryFilter) ([]*entities.Memory, error) {
	labelSelector := r.buildLabelSelector(filter)

	cmList, err := r.client.CoreV1().ConfigMaps(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list memory ConfigMaps: %w", err)
	}

	// Build a set of allowed team IDs for in-process filtering (TeamIDs OR logic)
	teamIDSet := make(map[string]struct{})
	for _, tid := range filter.TeamIDs {
		teamIDSet[tid] = struct{}{}
	}

	var result []*entities.Memory
	for i := range cmList.Items {
		cm := &cmList.Items[i]
		memory, err := r.loadFromConfigMap(cm)
		if err != nil {
			// Skip malformed entries
			continue
		}

		// In-process filter: TeamIDs (OR logic, applied when label selector couldn't narrow by team)
		if len(filter.TeamIDs) > 0 {
			if _, ok := teamIDSet[memory.TeamID()]; !ok {
				continue
			}
		}

		// In-process filter: tags (must contain ALL filter tags)
		if !memory.MatchesTags(filter.Tags) {
			continue
		}

		// In-process filter: full-text search (title + content)
		if !memory.MatchesText(filter.Query) {
			continue
		}

		result = append(result, memory)
	}

	if result == nil {
		result = []*entities.Memory{}
	}

	return result, nil
}

// Update replaces an existing memory entry's content.
func (r *KubernetesMemoryRepository) Update(ctx context.Context, memory *entities.Memory) error {
	if err := memory.Validate(); err != nil {
		return fmt.Errorf("invalid memory entry: %w", err)
	}

	// Verify the entry exists
	existing, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(memory.ID()), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return entities.ErrMemoryNotFound{ID: memory.ID()}
		}
		return fmt.Errorf("failed to get memory ConfigMap for update: %w", err)
	}

	data, err := r.entityToJSONBytes(memory)
	if err != nil {
		return fmt.Errorf("failed to marshal memory: %w", err)
	}

	existing.Labels = r.buildLabels(memory)
	existing.Annotations = r.buildAnnotations(memory)
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	existing.Data[ConfigMapKeyMemory] = string(data)

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update memory ConfigMap: %w", err)
	}

	return nil
}

// Delete removes a memory entry by ID.
func (r *KubernetesMemoryRepository) Delete(ctx context.Context, id string) error {
	err := r.client.CoreV1().ConfigMaps(r.namespace).Delete(ctx, r.configMapName(id), metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return entities.ErrMemoryNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete memory ConfigMap: %w", err)
	}
	return nil
}

// configMapName returns the ConfigMap name for a given memory ID.
func (r *KubernetesMemoryRepository) configMapName(id string) string {
	return MemoryConfigMapPrefix + id
}

// buildLabelSelector builds the label selector string for List queries.
// Always includes the type label. Adds scope/owner/team labels when set.
// Note: TeamIDs (multiple values) cannot be expressed as a single label selector,
// so when TeamIDs is set the selector only includes the type label and
// in-process filtering handles the team constraint.
func (r *KubernetesMemoryRepository) buildLabelSelector(filter repositories.MemoryFilter) string {
	parts := []string{
		fmt.Sprintf("%s=%s", LabelMemoryType, LabelMemoryTypeValue),
	}

	// TeamIDs takes priority; in that case we cannot narrow by team in K8s
	if len(filter.TeamIDs) == 0 {
		if filter.Scope != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelMemoryScope, string(filter.Scope)))
		}
		if filter.OwnerID != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelMemoryOwnerHash, hashID(filter.OwnerID)))
		}
		if filter.TeamID != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelMemoryTeamHash, hashID(filter.TeamID)))
		}
	} else if filter.Scope != "" {
		parts = append(parts, fmt.Sprintf("%s=%s", LabelMemoryScope, string(filter.Scope)))
	}

	return strings.Join(parts, ",")
}

// buildLabels builds the label map for a ConfigMap
func (r *KubernetesMemoryRepository) buildLabels(m *entities.Memory) map[string]string {
	labels := map[string]string{
		LabelMemoryType:      LabelMemoryTypeValue,
		LabelMemoryScope:     string(m.Scope()),
		LabelMemoryOwnerHash: hashID(m.OwnerID()),
	}
	if m.Scope() == entities.ScopeTeam && m.TeamID() != "" {
		labels[LabelMemoryTeamHash] = hashID(m.TeamID())
	}
	return labels
}

// buildAnnotations builds the annotation map for a ConfigMap
func (r *KubernetesMemoryRepository) buildAnnotations(m *entities.Memory) map[string]string {
	annotations := map[string]string{
		AnnotationMemoryOwnerID: m.OwnerID(),
	}
	if m.TeamID() != "" {
		annotations[AnnotationMemoryTeamID] = m.TeamID()
	}
	return annotations
}

// entityToJSONBytes converts a Memory entity to JSON bytes for storage
func (r *KubernetesMemoryRepository) entityToJSONBytes(m *entities.Memory) ([]byte, error) {
	mj := &memoryJSON{
		ID:        m.ID(),
		Title:     m.Title(),
		Content:   m.Content(),
		Tags:      m.Tags(),
		Scope:     string(m.Scope()),
		OwnerID:   m.OwnerID(),
		TeamID:    m.TeamID(),
		CreatedAt: m.CreatedAt(),
		UpdatedAt: m.UpdatedAt(),
	}
	return json.Marshal(mj)
}

// loadFromConfigMap deserializes a ConfigMap into a Memory entity.
// Annotation values take priority over JSON for ownerID and teamID
// to correctly handle IDs containing "/" which cannot be stored in label values.
func (r *KubernetesMemoryRepository) loadFromConfigMap(cm *corev1.ConfigMap) (*entities.Memory, error) {
	rawJSON, ok := cm.Data[ConfigMapKeyMemory]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %s is missing memory.json data", cm.Name)
	}

	var mj memoryJSON
	if err := json.Unmarshal([]byte(rawJSON), &mj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal memory JSON from ConfigMap %s: %w", cm.Name, err)
	}

	// Prefer annotation values (annotations are not subject to label character restrictions)
	if ownerID, ok := cm.Annotations[AnnotationMemoryOwnerID]; ok && ownerID != "" {
		mj.OwnerID = ownerID
	}
	if teamID, ok := cm.Annotations[AnnotationMemoryTeamID]; ok && teamID != "" {
		mj.TeamID = teamID
	}

	m := entities.NewMemoryWithTags(
		mj.ID,
		mj.Title,
		mj.Content,
		entities.ResourceScope(mj.Scope),
		mj.OwnerID,
		mj.TeamID,
		mj.Tags,
	)
	m.SetCreatedAt(mj.CreatedAt)
	m.SetUpdatedAt(mj.UpdatedAt)

	return m, nil
}

// hashID returns a short hex hash of the given string suitable for use as a Kubernetes label value.
// Uses the first 16 hex characters of SHA-256 (8 bytes = 64 bits of entropy).
func hashID(id string) string {
	h := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%x", h[:8])
}
