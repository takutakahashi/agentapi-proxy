package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

const (
	// UserTeamMappingConfigMapName is the name of the ConfigMap that stores user-team mappings
	UserTeamMappingConfigMapName = "agentapi-user-team-mapping"
	// LabelUserTeamMappingType is the label key that identifies user-team mapping ConfigMaps
	LabelUserTeamMappingType = "agentapi.proxy/type"
	// LabelUserTeamMappingTypeValue is the label value for user-team mapping ConfigMaps
	LabelUserTeamMappingTypeValue = "user-team-mapping"
)

// userTeamMappingEntry is the JSON representation of a single user's team memberships
type userTeamMappingEntry struct {
	Teams     []auth.GitHubTeamMembership `json:"teams"`
	UpdatedAt time.Time                   `json:"updated_at"`
}

// KubernetesUserTeamMappingRepository implements auth.TeamMappingRepository using a single
// Kubernetes ConfigMap. Each user is stored as a separate key in the ConfigMap's data field,
// with the value being a JSON-encoded userTeamMappingEntry.
//
// ConfigMap schema:
//
//	Name:      agentapi-user-team-mapping
//	Namespace: <same as other resources>
//	Labels:    agentapi.proxy/type: user-team-mapping
//	Data:
//	  {username}: {"teams":[...], "updated_at":"RFC3339"}
type KubernetesUserTeamMappingRepository struct {
	client    kubernetes.Interface
	namespace string
	ttl       time.Duration
}

// NewKubernetesUserTeamMappingRepository creates a new KubernetesUserTeamMappingRepository
func NewKubernetesUserTeamMappingRepository(client kubernetes.Interface, namespace string) *KubernetesUserTeamMappingRepository {
	return &KubernetesUserTeamMappingRepository{
		client:    client,
		namespace: namespace,
		ttl:       5 * time.Minute,
	}
}

// Get retrieves the team memberships for a given username from the ConfigMap.
// Returns (teams, true, nil) if found, (nil, false, nil) if not found, or (nil, false, err) on error.
func (r *KubernetesUserTeamMappingRepository) Get(ctx context.Context, username string) ([]auth.GitHubTeamMembership, bool, error) {
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, UserTeamMappingConfigMapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to get user-team mapping ConfigMap: %w", err)
	}

	rawJSON, ok := cm.Data[username]
	if !ok {
		return nil, false, nil
	}

	var entry userTeamMappingEntry
	if err := json.Unmarshal([]byte(rawJSON), &entry); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal team mapping for user %s: %w", username, err)
	}

	if r.ttl > 0 && time.Since(entry.UpdatedAt) > r.ttl {
		return nil, false, nil
	}

	return entry.Teams, true, nil
}

// Set stores the team memberships for a given username in the ConfigMap.
// Uses merge-patch to avoid resourceVersion conflicts under concurrent writes.
// Falls back to Create when the ConfigMap doesn't yet exist.
func (r *KubernetesUserTeamMappingRepository) Set(ctx context.Context, username string, teams []auth.GitHubTeamMembership) error {
	entry := userTeamMappingEntry{
		Teams:     teams,
		UpdatedAt: time.Now().UTC(),
	}

	rawJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal team mapping for user %s: %w", username, err)
	}

	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := r.patchOrCreate(ctx, username, string(rawJSON)); err != nil {
			// AlreadyExists means two pods raced to create; retry the patch path.
			if k8serrors.IsAlreadyExists(err) && attempt < maxRetries-1 {
				continue
			}
			return fmt.Errorf("failed to set team mapping for user %s: %w", username, err)
		}
		return nil
	}

	return fmt.Errorf("failed to set team mapping for user %s after %d retries", username, maxRetries)
}

// patchOrCreate applies the user entry via merge-patch when the ConfigMap exists,
// or creates the ConfigMap when it does not. Merge-patch does not require
// resourceVersion, so concurrent writes to different keys never conflict.
func (r *KubernetesUserTeamMappingRepository) patchOrCreate(ctx context.Context, username, rawJSON string) error {
	patch := map[string]interface{}{
		"data": map[string]string{username: rawJSON},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal patch: %w", err)
	}

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Patch(
		ctx,
		UserTeamMappingConfigMapName,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("failed to patch ConfigMap: %w", err)
	}

	// ConfigMap does not exist — create it.
	cm := r.buildConfigMap(map[string]string{username: rawJSON})
	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
	// Propagate AlreadyExists so the caller can retry the patch path.
	return err
}

// buildConfigMap constructs the ConfigMap object with the given data.
func (r *KubernetesUserTeamMappingRepository) buildConfigMap(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      UserTeamMappingConfigMapName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelUserTeamMappingType: LabelUserTeamMappingTypeValue,
			},
		},
		Data: data,
	}
}
