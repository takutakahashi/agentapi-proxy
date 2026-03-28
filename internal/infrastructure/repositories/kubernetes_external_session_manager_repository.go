package repositories

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// LabelESM is the label key for external session manager resources
	LabelESM = "agentapi.proxy/external-session-manager"
	// LabelESMID is the label key for the ESM ID
	LabelESMID = "agentapi.proxy/esm-id"
	// LabelESMScope is the label key for ESM scope
	LabelESMScope = "agentapi.proxy/esm-scope"
	// LabelESMUserID is the label key for ESM owner user ID
	LabelESMUserID = "agentapi.proxy/esm-user-id"
	// LabelESMTeamIDHash is the label key for hashed ESM team ID
	LabelESMTeamIDHash = "agentapi.proxy/esm-team-id-hash"
	// AnnotationESMTeamID is the annotation key for the original ESM team ID
	AnnotationESMTeamID = "agentapi.proxy/esm-team-id"
	// SecretKeyESM is the key in the Secret data for ESM JSON
	SecretKeyESM = "esm.json"
	// ESMSecretPrefix is the prefix for ESM Secret names
	ESMSecretPrefix = "agentapi-esm-"
)

// esmJSON is the JSON representation for storage
type esmJSON struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	URL        string                 `json:"url"`
	UserID     string                 `json:"user_id"`
	Scope      entities.ResourceScope `json:"scope,omitempty"`
	TeamID     string                 `json:"team_id,omitempty"`
	HMACSecret string                 `json:"hmac_secret"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// KubernetesExternalSessionManagerRepository implements ExternalSessionManagerRepository using Kubernetes Secrets
type KubernetesExternalSessionManagerRepository struct {
	client    kubernetes.Interface
	namespace string
	mu        sync.RWMutex
}

// NewKubernetesExternalSessionManagerRepository creates a new repository instance
func NewKubernetesExternalSessionManagerRepository(client kubernetes.Interface, namespace string) *KubernetesExternalSessionManagerRepository {
	return &KubernetesExternalSessionManagerRepository{
		client:    client,
		namespace: namespace,
	}
}

// Create persists a new external session manager.
// If the ESM has no HMAC secret set, one is generated automatically.
func (r *KubernetesExternalSessionManagerRepository) Create(ctx context.Context, esm *entities.ExternalSessionManager) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Auto-generate HMAC secret if not set
	if esm.HMACSecret() == "" {
		secret, err := generateESMSecret(32)
		if err != nil {
			return fmt.Errorf("failed to generate HMAC secret: %w", err)
		}
		esm.SetHMACSecret(secret)
	}

	if err := esm.Validate(); err != nil {
		return fmt.Errorf("invalid external session manager: %w", err)
	}

	return r.save(ctx, esm, false)
}

// Get retrieves an external session manager by ID
func (r *KubernetesExternalSessionManagerRepository) Get(ctx context.Context, id string) (*entities.ExternalSessionManager, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	secretName := esmSecretName(id)
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("external session manager not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get ESM secret: %w", err)
	}

	return r.fromSecret(secret)
}

// List retrieves external session managers matching the filter
func (r *KubernetesExternalSessionManagerRepository) List(ctx context.Context, filter repositories.ExternalSessionManagerFilter) ([]*entities.ExternalSessionManager, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	secretList, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", LabelESM),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list ESM secrets: %w", err)
	}

	// Build a set of caller's team IDs for fast lookup
	callerTeams := make(map[string]bool, len(filter.TeamIDs))
	for _, t := range filter.TeamIDs {
		callerTeams[t] = true
	}

	result := make([]*entities.ExternalSessionManager, 0, len(secretList.Items))
	for _, s := range secretList.Items {
		esm, err := r.fromSecret(&s)
		if err != nil {
			fmt.Printf("Warning: failed to parse ESM from secret %s: %v\n", s.Name, err)
			continue
		}

		// Scope filter
		if filter.Scope != "" && esm.Scope() != filter.Scope {
			continue
		}
		// TeamID filter
		if filter.TeamID != "" && esm.TeamID() != filter.TeamID {
			continue
		}

		// Access filter: user sees own user-scoped ESMs + team-scoped ESMs for their teams
		switch esm.Scope() {
		case entities.ScopeUser:
			if filter.UserID != "" && esm.UserID() != filter.UserID {
				continue
			}
		case entities.ScopeTeam:
			if !callerTeams[esm.TeamID()] {
				// Not a member of this team — skip unless they created it
				if filter.UserID != "" && esm.UserID() != filter.UserID {
					continue
				}
			}
		}

		result = append(result, esm)
	}

	return result, nil
}

// Update updates an existing external session manager
func (r *KubernetesExternalSessionManagerRepository) Update(ctx context.Context, esm *entities.ExternalSessionManager) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := esm.Validate(); err != nil {
		return fmt.Errorf("invalid external session manager: %w", err)
	}

	return r.save(ctx, esm, true)
}

// Delete removes an external session manager by ID
func (r *KubernetesExternalSessionManagerRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	secretName := esmSecretName(id)
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete ESM secret: %w", err)
	}
	return nil
}

// save writes the ESM to a Kubernetes Secret (create or update)
func (r *KubernetesExternalSessionManagerRepository) save(ctx context.Context, esm *entities.ExternalSessionManager, update bool) error {
	data, err := json.Marshal(esmJSON{
		ID:         esm.ID(),
		Name:       esm.Name(),
		URL:        esm.URL(),
		UserID:     esm.UserID(),
		Scope:      esm.Scope(),
		TeamID:     esm.TeamID(),
		HMACSecret: esm.HMACSecret(),
		CreatedAt:  esm.CreatedAt(),
		UpdatedAt:  esm.UpdatedAt(),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal ESM: %w", err)
	}

	secretName := esmSecretName(esm.ID())
	labels := map[string]string{
		LabelESM:       "true",
		LabelESMID:     esm.ID(),
		LabelESMScope:  string(esm.Scope()),
		LabelESMUserID: sanitizeLabelValue(esm.UserID()),
	}
	annotations := make(map[string]string)
	if esm.TeamID() != "" {
		labels[LabelESMTeamIDHash] = services.HashTeamID(esm.TeamID())
		annotations[AnnotationESMTeamID] = esm.TeamID()
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   r.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeyESM: data,
		},
	}

	if update {
		// Fetch existing to preserve ResourceVersion
		existing, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return fmt.Errorf("external session manager not found: %s", esm.ID())
			}
			return fmt.Errorf("failed to get existing ESM secret: %w", err)
		}
		existing.Data[SecretKeyESM] = data
		existing.Labels = labels
		existing.Annotations = annotations
		_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
		return err
	}

	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return fmt.Errorf("external session manager already exists: %s", esm.ID())
		}
		return fmt.Errorf("failed to create ESM secret: %w", err)
	}
	return nil
}

// fromSecret converts a Kubernetes Secret to an ExternalSessionManager entity
func (r *KubernetesExternalSessionManagerRepository) fromSecret(secret *corev1.Secret) (*entities.ExternalSessionManager, error) {
	data, ok := secret.Data[SecretKeyESM]
	if !ok {
		return nil, fmt.Errorf("esm.json not found in secret %s", secret.Name)
	}

	var j esmJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ESM JSON: %w", err)
	}

	// Prefer team_id from annotation (handles slash in team IDs correctly)
	if annotationTeamID, ok := secret.Annotations[AnnotationESMTeamID]; ok && annotationTeamID != "" {
		j.TeamID = annotationTeamID
	}

	esm := entities.NewExternalSessionManager(j.ID, j.Name, j.URL, j.UserID)
	esm.SetScope(j.Scope)
	esm.SetTeamID(j.TeamID)
	// Set secret directly without touching updatedAt (deserialization)
	esm.SetHMACSecret(j.HMACSecret)
	esm.SetCreatedAt(j.CreatedAt)
	esm.SetUpdatedAt(j.UpdatedAt)

	return esm, nil
}

// esmSecretName returns the Kubernetes Secret name for an ESM
func esmSecretName(id string) string {
	return ESMSecretPrefix + id
}

// generateESMSecret generates a random hex secret of the given byte length
func generateESMSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
