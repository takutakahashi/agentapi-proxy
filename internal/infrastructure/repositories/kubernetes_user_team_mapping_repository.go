package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
}

// NewKubernetesUserTeamMappingRepository creates a new KubernetesUserTeamMappingRepository
func NewKubernetesUserTeamMappingRepository(client kubernetes.Interface, namespace string) *KubernetesUserTeamMappingRepository {
	return &KubernetesUserTeamMappingRepository{
		client:    client,
		namespace: namespace,
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

	return entry.Teams, true, nil
}

// Set stores the team memberships for a given username in the ConfigMap.
// Creates the ConfigMap if it does not exist.
// Retries up to 3 times on conflict (409) to handle concurrent updates from multiple pods.
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
		if err := r.upsert(ctx, username, string(rawJSON)); err != nil {
			if k8serrors.IsConflict(err) && attempt < maxRetries-1 {
				// Retry on conflict (concurrent update from another pod)
				continue
			}
			return fmt.Errorf("failed to set team mapping for user %s: %w", username, err)
		}
		return nil
	}

	return fmt.Errorf("failed to set team mapping for user %s after %d retries", username, maxRetries)
}

// upsert creates or updates the ConfigMap with the given user entry.
func (r *KubernetesUserTeamMappingRepository) upsert(ctx context.Context, username, rawJSON string) error {
	existing, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, UserTeamMappingConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get ConfigMap: %w", err)
		}

		// ConfigMap does not exist — create it
		cm := r.buildConfigMap(map[string]string{username: rawJSON})
		_, err = r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
		if err != nil {
			// Another pod may have created it concurrently; treat AlreadyExists as conflict
			if k8serrors.IsAlreadyExists(err) {
				return &k8serrors.StatusError{ErrStatus: metav1.Status{Reason: metav1.StatusReasonConflict}}
			}
			return fmt.Errorf("failed to create ConfigMap: %w", err)
		}
		return nil
	}

	// ConfigMap exists — update the user's entry
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	existing.Data[username] = rawJSON

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}
	return nil
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
