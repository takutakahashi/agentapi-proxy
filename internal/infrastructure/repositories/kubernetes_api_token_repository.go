package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	// LabelAPIToken marks a Secret as an API token.
	LabelAPIToken = "agentapi.proxy/api-token"
	// LabelAPITokenScope holds the token scope ("user" or "team").
	LabelAPITokenScope = "agentapi.proxy/api-token-scope"
	// LabelAPITokenOwner holds the owner user id (personal scope) or the
	// team id (team scope), sanitized for use as a label value.
	LabelAPITokenOwner = "agentapi.proxy/api-token-owner"
	// AnnotationAPITokenOwnerID stores the unsanitized owner id.
	AnnotationAPITokenOwnerID = "agentapi.proxy/owner-id"
	// AnnotationAPITokenTeamID stores the team id (team scope).
	AnnotationAPITokenTeamID = "agentapi.proxy/team-id"
	// AnnotationAPITokenCreatedBy stores the creator user id.
	AnnotationAPITokenCreatedBy = "agentapi.proxy/created-by"
	// AnnotationAPITokenMigratedFrom marks the migration source for legacy
	// tokens ("personal-api-key" or "team-config").
	AnnotationAPITokenMigratedFrom = "agentapi.proxy/migrated-from"
	// AnnotationAPITokenMigrationSourceID stores the legacy identifier used
	// to derive the deterministic migration token id.
	AnnotationAPITokenMigrationSourceID = "agentapi.proxy/migration-source-id"
	// SecretKeyAPITokenSecret stores the plaintext token secret.
	SecretKeyAPITokenSecret = "secret"
	// SecretKeyAPITokenMetadata stores the JSON metadata (no secret).
	SecretKeyAPITokenMetadata = "token.json"
	// APITokenSecretPrefix is the prefix for API token Secret names.
	APITokenSecretPrefix = "agentapi-api-token-"
)

// apiTokenMetadataJSON is the JSON representation of token metadata stored in
// the Secret. It intentionally excludes the plaintext secret.
type apiTokenMetadataJSON struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Scope       string    `json:"scope"`
	UserID      string    `json:"user_id"`
	TeamID      string    `json:"team_id,omitempty"`
	Permissions []string  `json:"permissions"`
	TokenPrefix string    `json:"token_prefix"`
	ExpiresAt   *string   `json:"expires_at,omitempty"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// KubernetesAPITokenRepository implements APITokenRepository using one
// Kubernetes Secret per token.
type KubernetesAPITokenRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesAPITokenRepository creates a new KubernetesAPITokenRepository.
func NewKubernetesAPITokenRepository(client kubernetes.Interface, namespace string) *KubernetesAPITokenRepository {
	return &KubernetesAPITokenRepository{
		client:    client,
		namespace: namespace,
	}
}

// Create persists a new token. Returns ErrAPITokenAlreadyExists when a token
// with the same ID already exists (no overwrite).
func (r *KubernetesAPITokenRepository) Create(ctx context.Context, token *entities.APIToken) error {
	if err := token.Validate(); err != nil {
		return fmt.Errorf("invalid api token: %w", err)
	}

	secret, err := r.toSecret(token)
	if err != nil {
		return err
	}

	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return entities.ErrAPITokenAlreadyExists
		}
		return fmt.Errorf("failed to create api token secret: %w", err)
	}
	return nil
}

// GetByID retrieves a token by its public ID.
func (r *KubernetesAPITokenRepository) GetByID(ctx context.Context, tokenID string) (*entities.APIToken, error) {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, r.secretName(tokenID), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, entities.ErrAPITokenNotFound
		}
		return nil, fmt.Errorf("failed to get api token secret: %w", err)
	}
	return r.fromSecret(secret)
}

// GetBySecret retrieves a token by its plaintext secret. This is an O(n) list
// scan since secrets are not indexable by content; the in-memory auth service
// is the primary fast path. Returns ErrAPITokenNotFound when no match.
func (r *KubernetesAPITokenRepository) GetBySecret(ctx context.Context, secret string) (*entities.APIToken, error) {
	list, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", LabelAPIToken),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list api token secrets: %w", err)
	}
	for i := range list.Items {
		s := &list.Items[i]
		if string(s.Data[SecretKeyAPITokenSecret]) == secret {
			return r.fromSecret(s)
		}
	}
	return nil, entities.ErrAPITokenNotFound
}

// ListByOwner lists personal tokens owned by userID.
func (r *KubernetesAPITokenRepository) ListByOwner(ctx context.Context, userID entities.UserID) ([]*entities.APIToken, error) {
	selector := fmt.Sprintf("%s=true,%s=user,%s=%s",
		LabelAPIToken, LabelAPITokenScope, LabelAPITokenOwner, sanitizeLabelValue(string(userID)))
	return r.list(ctx, selector)
}

// ListByTeam lists team tokens for the given team id.
func (r *KubernetesAPITokenRepository) ListByTeam(ctx context.Context, teamID string) ([]*entities.APIToken, error) {
	selector := fmt.Sprintf("%s=true,%s=team,%s=%s",
		LabelAPIToken, LabelAPITokenScope, LabelAPITokenOwner, sanitizeLabelValue(teamID))
	return r.list(ctx, selector)
}

// ListAll lists every API token.
func (r *KubernetesAPITokenRepository) ListAll(ctx context.Context) ([]*entities.APIToken, error) {
	return r.list(ctx, fmt.Sprintf("%s=true", LabelAPIToken))
}

// Delete removes a token by ID. Idempotent: returns nil when not found.
func (r *KubernetesAPITokenRepository) Delete(ctx context.Context, tokenID string) error {
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, r.secretName(tokenID), metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete api token secret: %w", err)
	}
	return nil
}

// ApplyMigrationAnnotations adds provenance annotations to an existing
// migrated token's Secret. It is idempotent and best-effort: it merges the
// given annotations into whatever the Secret already carries. It satisfies
// services.APITokenAnnotator.
func (r *KubernetesAPITokenRepository) ApplyMigrationAnnotations(ctx context.Context, tokenID, source, sourceID string) error {
	name := r.secretName(tokenID)
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // nothing to annotate
		}
		return fmt.Errorf("failed to get api token secret for annotation: %w", err)
	}
	if secret.Annotations == nil {
		secret.Annotations = map[string]string{}
	}
	secret.Annotations[AnnotationAPITokenMigratedFrom] = source
	secret.Annotations[AnnotationAPITokenMigrationSourceID] = sourceID
	if _, err := r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to apply migration annotations: %w", err)
	}
	return nil
}

// list lists tokens matching a label selector, skipping unparseable secrets.
func (r *KubernetesAPITokenRepository) list(ctx context.Context, selector string) ([]*entities.APIToken, error) {
	list, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("failed to list api token secrets: %w", err)
	}
	out := make([]*entities.APIToken, 0, len(list.Items))
	for i := range list.Items {
		tok, err := r.fromSecret(&list.Items[i])
		if err != nil {
			fmt.Printf("Warning: failed to parse api token from secret %s: %v\n", list.Items[i].Name, err)
			continue
		}
		out = append(out, tok)
	}
	return out, nil
}

// secretName builds the Secret name for a token ID. The token ID is expected
// to be opaque and already safe for K8s names, but we sanitize defensively.
func (r *KubernetesAPITokenRepository) secretName(tokenID string) string {
	return APITokenSecretPrefix + sanitizeLabelValue(tokenID)
}

// toSecret converts an APIToken entity into a Kubernetes Secret.
func (r *KubernetesAPITokenRepository) toSecret(token *entities.APIToken) (*corev1.Secret, error) {
	metadata, err := r.toJSON(token)
	if err != nil {
		return nil, err
	}

	ownerLabel := string(token.UserID())
	if token.Scope() == entities.APITokenScopeTeam {
		ownerLabel = token.TeamID()
	}

	annotations := map[string]string{
		AnnotationAPITokenOwnerID:   string(token.UserID()),
		AnnotationAPITokenCreatedBy: string(token.CreatedBy()),
	}
	if token.Scope() == entities.APITokenScopeTeam {
		annotations[AnnotationAPITokenTeamID] = token.TeamID()
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.secretName(token.ID()),
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelAPIToken:      "true",
				LabelAPITokenScope: string(token.Scope()),
				LabelAPITokenOwner: sanitizeLabelValue(ownerLabel),
			},
			Annotations: annotations,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeyAPITokenSecret:   []byte(token.Secret()),
			SecretKeyAPITokenMetadata: metadata,
		},
	}, nil
}

// toJSON serializes token metadata to JSON bytes (no secret).
func (r *KubernetesAPITokenRepository) toJSON(token *entities.APIToken) ([]byte, error) {
	perms := make([]string, len(token.Permissions()))
	for i, p := range token.Permissions() {
		perms[i] = string(p)
	}
	var expiresAt *string
	if exp := token.ExpiresAt(); exp != nil {
		s := exp.Format(time.RFC3339Nano)
		expiresAt = &s
	}
	return json.Marshal(apiTokenMetadataJSON{
		ID:          token.ID(),
		Name:        token.Name(),
		Scope:       string(token.Scope()),
		UserID:      string(token.UserID()),
		TeamID:      token.TeamID(),
		Permissions: perms,
		TokenPrefix: token.DisplayPrefix(),
		ExpiresAt:   expiresAt,
		CreatedBy:   string(token.CreatedBy()),
		CreatedAt:   token.CreatedAt(),
		UpdatedAt:   token.UpdatedAt(),
	})
}

// fromSecret parses a Kubernetes Secret into an APIToken entity.
func (r *KubernetesAPITokenRepository) fromSecret(secret *corev1.Secret) (*entities.APIToken, error) {
	secretBytes, ok := secret.Data[SecretKeyAPITokenSecret]
	if !ok {
		return nil, fmt.Errorf("api token secret %s missing %q key", secret.Name, SecretKeyAPITokenSecret)
	}
	metaBytes, ok := secret.Data[SecretKeyAPITokenMetadata]
	if !ok {
		return nil, fmt.Errorf("api token secret %s missing %q key", secret.Name, SecretKeyAPITokenMetadata)
	}
	var meta apiTokenMetadataJSON
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal api token metadata: %w", err)
	}

	perms := make([]entities.Permission, len(meta.Permissions))
	for i, p := range meta.Permissions {
		perms[i] = entities.Permission(p)
	}

	var expiresAt *time.Time
	if meta.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339Nano, *meta.ExpiresAt)
		if err != nil {
			t, err = time.Parse(time.RFC3339, *meta.ExpiresAt)
			if err != nil {
				return nil, fmt.Errorf("failed to parse expires_at: %w", err)
			}
		}
		expiresAt = &t
	}

	return entities.RestoreAPIToken(
		meta.ID,
		string(secretBytes),
		meta.TokenPrefix,
		meta.Name,
		entities.APITokenScope(meta.Scope),
		entities.UserID(meta.UserID),
		meta.TeamID,
		perms,
		expiresAt,
		entities.UserID(meta.CreatedBy),
		meta.CreatedAt,
		meta.UpdatedAt,
	), nil
}
