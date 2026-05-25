package repositories

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

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	LabelSessionProfile       = "agentapi.proxy/session-profile"
	LabelSessionProfileID     = "agentapi.proxy/session-profile-id"
	LabelSessionProfileScope  = "agentapi.proxy/session-profile-scope"
	LabelSessionProfileUserID = "agentapi.proxy/session-profile-user-id"
	// LabelSessionProfileTeamIDHash is the label key for hashed team ID
	LabelSessionProfileTeamIDHash = "agentapi.proxy/session-profile-team-id-hash"
	// LabelSessionProfileManaged marks a profile as system-managed (admin-only)
	LabelSessionProfileManaged = "agentapi.proxy/session-profile-managed"
	// AnnotationSessionProfileTeamID stores the original (unescaped) team ID
	AnnotationSessionProfileTeamID = "agentapi.proxy/session-profile-team-id"
	// SecretKeySessionProfile is the key in the Secret data for session profile JSON
	SecretKeySessionProfile = "session-profile.json"
	// SessionProfileSecretPrefix is the prefix for session profile Secret names
	SessionProfileSecretPrefix = "agentapi-session-profile-"
)

// sessionProfileJSON is the JSON representation for storage
type sessionProfileJSON struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	UserID      string                   `json:"user_id"`
	Scope       entities.ResourceScope   `json:"scope,omitempty"`
	TeamID      string                   `json:"team_id,omitempty"`
	IsDefault   bool                     `json:"is_default,omitempty"`
	IsManaged   bool                     `json:"is_managed,omitempty"`
	Config      sessionProfileConfigJSON `json:"config"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

type sessionProfileConfigJSON struct {
	Environment            map[string]string       `json:"environment,omitempty"`
	Tags                   map[string]string       `json:"tags,omitempty"`
	InitialMessageTemplate string                  `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                  `json:"reuse_message_template,omitempty"`
	Params                 *entities.SessionParams `json:"params,omitempty"`
	ReuseSession           bool                    `json:"reuse_session,omitempty"`
	MemoryKey              map[string]string       `json:"memory_key,omitempty"`
}

// KubernetesSessionProfileRepository implements SessionProfileRepository using Kubernetes Secrets
type KubernetesSessionProfileRepository struct {
	client    kubernetes.Interface
	namespace string
	mu        sync.RWMutex
}

// NewKubernetesSessionProfileRepository creates a new KubernetesSessionProfileRepository
func NewKubernetesSessionProfileRepository(client kubernetes.Interface, namespace string) *KubernetesSessionProfileRepository {
	return &KubernetesSessionProfileRepository{
		client:    client,
		namespace: namespace,
	}
}

func sessionProfileSecretName(id string) string {
	return SessionProfileSecretPrefix + id
}

// Create creates a new session profile
func (r *KubernetesSessionProfileRepository) Create(ctx context.Context, profile *entities.SessionProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	secretName := sessionProfileSecretName(profile.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("session profile already exists: %s", profile.ID())
	}
	if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check session profile existence: %w", err)
	}

	return r.saveProfile(ctx, profile)
}

// Get retrieves a session profile by ID
func (r *KubernetesSessionProfileRepository) Get(ctx context.Context, id string) (*entities.SessionProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.loadProfile(ctx, id)
}

// List retrieves session profiles matching the filter
func (r *KubernetesSessionProfileRepository) List(ctx context.Context, filter portrepos.SessionProfileFilter) ([]*entities.SessionProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profiles, err := r.loadAllProfiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load session profiles: %w", err)
	}

	var result []*entities.SessionProfile
	for _, p := range profiles {
		if filter.ManagedOnly && !p.IsManaged() {
			continue
		}
		if filter.UserID != "" && p.UserID() != filter.UserID {
			continue
		}
		if filter.Scope != "" && p.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && p.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 && p.Scope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if p.TeamID() == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}
		result = append(result, p)
	}

	return result, nil
}

// Update updates an existing session profile
func (r *KubernetesSessionProfileRepository) Update(ctx context.Context, profile *entities.SessionProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	secretName := sessionProfileSecretName(profile.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return entities.ErrSessionProfileNotFound{ID: profile.ID()}
		}
		return fmt.Errorf("failed to get session profile: %w", err)
	}

	profile.SetUpdatedAt(time.Now())
	return r.saveProfile(ctx, profile)
}

// Delete removes a session profile by ID
func (r *KubernetesSessionProfileRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	secretName := sessionProfileSecretName(id)
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return entities.ErrSessionProfileNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete session profile secret: %w", err)
	}

	return nil
}

func (r *KubernetesSessionProfileRepository) loadProfile(ctx context.Context, id string) (*entities.SessionProfile, error) {
	secretName := sessionProfileSecretName(id)
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, entities.ErrSessionProfileNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get session profile secret: %w", err)
	}

	data, ok := secret.Data[SecretKeySessionProfile]
	if !ok {
		return nil, fmt.Errorf("session profile secret missing data key: %s", SecretKeySessionProfile)
	}

	var pj sessionProfileJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session profile: %w", err)
	}

	// Restore team ID from annotation if present
	if annotationTeamID, ok := secret.Annotations[AnnotationSessionProfileTeamID]; ok && annotationTeamID != "" {
		pj.TeamID = annotationTeamID
	}

	return r.jsonToEntity(&pj), nil
}

func (r *KubernetesSessionProfileRepository) loadAllProfiles(ctx context.Context) ([]*entities.SessionProfile, error) {
	labelSelector := fmt.Sprintf("%s=true", LabelSessionProfile)
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list session profile secrets: %w", err)
	}

	result := make([]*entities.SessionProfile, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		data, ok := secret.Data[SecretKeySessionProfile]
		if !ok {
			continue
		}

		var pj sessionProfileJSON
		if err := json.Unmarshal(data, &pj); err != nil {
			continue
		}

		if annotationTeamID, ok := secret.Annotations[AnnotationSessionProfileTeamID]; ok && annotationTeamID != "" {
			pj.TeamID = annotationTeamID
		}

		result = append(result, r.jsonToEntity(&pj))
	}

	return result, nil
}

func (r *KubernetesSessionProfileRepository) saveProfile(ctx context.Context, profile *entities.SessionProfile) error {
	pj := r.entityToJSON(profile)
	data, err := json.Marshal(pj)
	if err != nil {
		return fmt.Errorf("failed to marshal session profile: %w", err)
	}

	secretName := sessionProfileSecretName(profile.ID())
	labels := map[string]string{
		LabelSessionProfile:       "true",
		LabelSessionProfileID:     profile.ID(),
		LabelSessionProfileScope:  string(profile.Scope()),
		LabelSessionProfileUserID: profile.UserID(),
	}
	if profile.IsManaged() {
		labels[LabelSessionProfileManaged] = "true"
	}
	annotations := make(map[string]string)
	if profile.TeamID() != "" {
		labels[LabelSessionProfileTeamIDHash] = services.HashTeamID(profile.TeamID())
		annotations[AnnotationSessionProfileTeamID] = profile.TeamID()
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
			SecretKeySessionProfile: data,
		},
	}

	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existing, getErr := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}

			existing.Data[SecretKeySessionProfile] = data
			existing.Labels = labels
			existing.Annotations = annotations

			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update session profile secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create session profile secret: %w", err)
	}

	return nil
}

func (r *KubernetesSessionProfileRepository) jsonToEntity(pj *sessionProfileJSON) *entities.SessionProfile {
	profile := entities.NewSessionProfile(pj.ID, pj.Name, pj.UserID)
	profile.SetDescription(pj.Description)
	profile.SetScope(pj.Scope)
	profile.SetTeamID(pj.TeamID)
	profile.SetIsDefault(pj.IsDefault)
	profile.SetIsManaged(pj.IsManaged)
	profile.SetCreatedAt(pj.CreatedAt)
	profile.SetUpdatedAt(pj.UpdatedAt)

	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(pj.Config.Environment)
	cfg.SetTags(pj.Config.Tags)
	cfg.SetInitialMessageTemplate(pj.Config.InitialMessageTemplate)
	cfg.SetReuseMessageTemplate(pj.Config.ReuseMessageTemplate)
	cfg.SetParams(pj.Config.Params)
	cfg.SetReuseSession(pj.Config.ReuseSession)
	cfg.SetMemoryKey(pj.Config.MemoryKey)
	profile.SetConfig(cfg)

	return profile
}

func (r *KubernetesSessionProfileRepository) entityToJSON(profile *entities.SessionProfile) *sessionProfileJSON {
	cfg := profile.Config()
	return &sessionProfileJSON{
		ID:          profile.ID(),
		Name:        profile.Name(),
		Description: profile.Description(),
		UserID:      profile.UserID(),
		Scope:       profile.Scope(),
		TeamID:      profile.TeamID(),
		IsDefault:   profile.IsDefault(),
		IsManaged:   profile.IsManaged(),
		Config: sessionProfileConfigJSON{
			Environment:            cfg.Environment(),
			Tags:                   cfg.Tags(),
			InitialMessageTemplate: cfg.InitialMessageTemplate(),
			ReuseMessageTemplate:   cfg.ReuseMessageTemplate(),
			Params:                 cfg.Params(),
			ReuseSession:           cfg.ReuseSession(),
			MemoryKey:              cfg.MemoryKey(),
		},
		CreatedAt: profile.CreatedAt(),
		UpdatedAt: profile.UpdatedAt(),
	}
}
