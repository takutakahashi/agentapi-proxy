package repositories

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	// LabelCredentials is the label key for credentials resources
	LabelCredentials = "agentapi.proxy/credentials"
	// LabelCredentialsName is the label key for credentials name
	LabelCredentialsName = "agentapi.proxy/credentials-name"
	// SecretKeyCredentials is the key in the Secret data for the raw auth.json content.
	// Using "auth.json" so it is consistent with the legacy agentapi-agent-env-* mechanism
	// and can be read directly by the session manager without any unwrapping.
	SecretKeyCredentials = "auth.json"
	// CredentialsSecretPrefix is the prefix for credentials Secret names
	CredentialsSecretPrefix = "agentapi-credentials-"
	// AnnotationCredentialsName stores the original (unsanitised) credentials name
	AnnotationCredentialsName = "agentapi.proxy/credentials-name"
	// AnnotationCredentialsCreatedAt stores the creation timestamp in RFC3339 format
	AnnotationCredentialsCreatedAt = "agentapi.proxy/credentials-created-at"
	// AnnotationCredentialsUpdatedAt stores the last update timestamp in RFC3339 format
	AnnotationCredentialsUpdatedAt = "agentapi.proxy/credentials-updated-at"
)

// KubernetesCredentialsRepository implements CredentialsRepository using Kubernetes Secrets.
// The raw auth.json content is stored directly under the "auth.json" key so that the
// session manager can embed it into the provision payload without any parsing.
type KubernetesCredentialsRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesCredentialsRepository creates a new KubernetesCredentialsRepository
func NewKubernetesCredentialsRepository(client kubernetes.Interface, namespace string) *KubernetesCredentialsRepository {
	return &KubernetesCredentialsRepository{
		client:    client,
		namespace: namespace,
	}
}

// Save persists credentials (creates or updates)
func (r *KubernetesCredentialsRepository) Save(ctx context.Context, creds *entities.Credentials) error {
	if err := creds.Validate(); err != nil {
		return fmt.Errorf("invalid credentials: %w", err)
	}

	secretName := r.secretName(creds.Name())
	labelValue := sanitizeLabelValue(creds.Name())
	now := time.Now().UTC().Format(time.RFC3339)

	// Fetch existing to preserve created_at annotation
	createdAt := now
	existing, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		if v, ok := existing.Annotations[AnnotationCredentialsCreatedAt]; ok && v != "" {
			createdAt = v
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelCredentials:     "true",
				LabelCredentialsName: labelValue,
			},
			Annotations: map[string]string{
				AnnotationCredentialsName:      creds.Name(),
				AnnotationCredentialsCreatedAt: createdAt,
				AnnotationCredentialsUpdatedAt: now,
			},
		},
		Type: corev1.SecretTypeOpaque,
		// Store raw auth.json bytes directly under "auth.json" key.
		Data: map[string][]byte{
			SecretKeyCredentials: creds.Data(),
		},
	}

	// Try to create first
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update credentials secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create credentials secret: %w", err)
	}

	return nil
}

// FindByName retrieves credentials by name
func (r *KubernetesCredentialsRepository) FindByName(ctx context.Context, name string) (*entities.Credentials, error) {
	secretName := r.secretName(name)

	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("credentials not found: %s", name)
		}
		return nil, fmt.Errorf("failed to get credentials secret: %w", err)
	}

	creds, err := r.fromSecret(secret)
	if err != nil {
		return nil, err
	}

	if creds.Name() == "" {
		creds.SetName(name)
	}

	return creds, nil
}

// Delete removes credentials by name
func (r *KubernetesCredentialsRepository) Delete(ctx context.Context, name string) error {
	secretName := r.secretName(name)

	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("credentials not found: %s", name)
		}
		return fmt.Errorf("failed to delete credentials secret: %w", err)
	}

	return nil
}

// Exists checks if credentials exist for the given name
func (r *KubernetesCredentialsRepository) Exists(ctx context.Context, name string) (bool, error) {
	secretName := r.secretName(name)

	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check credentials existence: %w", err)
	}

	return true, nil
}

// List retrieves all credentials
func (r *KubernetesCredentialsRepository) List(ctx context.Context) ([]*entities.Credentials, error) {
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", LabelCredentials),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials secrets: %w", err)
	}

	var credsList []*entities.Credentials
	for i := range secrets.Items {
		creds, err := r.fromSecret(&secrets.Items[i])
		if err != nil {
			continue
		}
		credsList = append(credsList, creds)
	}

	return credsList, nil
}

// secretName returns the Secret name for the given credentials name
func (r *KubernetesCredentialsRepository) secretName(name string) string {
	sanitized := sanitizeSecretName(name)
	maxLen := 253 - len(CredentialsSecretPrefix)
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
	}
	return CredentialsSecretPrefix + sanitized
}

// fromSecret converts a Kubernetes Secret to a Credentials entity.
// The raw auth.json bytes are stored directly under the "auth.json" key.
// Timestamps are stored in annotations.
func (r *KubernetesCredentialsRepository) fromSecret(secret *corev1.Secret) (*entities.Credentials, error) {
	rawData, ok := secret.Data[SecretKeyCredentials]
	if !ok {
		return nil, fmt.Errorf("secret missing auth.json data")
	}

	// Determine name: annotation → label fallback
	credName := ""
	if secret.Annotations != nil {
		credName = secret.Annotations[AnnotationCredentialsName]
	}
	if credName == "" {
		credName = secret.Labels[LabelCredentialsName]
	}

	creds := entities.NewCredentials(credName, rawData)

	// Restore timestamps from annotations
	if secret.Annotations != nil {
		if v := secret.Annotations[AnnotationCredentialsCreatedAt]; v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				creds.SetCreatedAt(t)
			}
		}
		if v := secret.Annotations[AnnotationCredentialsUpdatedAt]; v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				creds.SetUpdatedAt(t)
			}
		}
	}

	return creds, nil
}
