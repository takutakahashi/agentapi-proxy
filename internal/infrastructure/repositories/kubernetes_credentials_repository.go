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
	// LabelCredentials is the label key for credentials resources
	LabelCredentials = "agentapi.proxy/credentials"
	// LabelCredentialsName is the label key for credentials name
	LabelCredentialsName = "agentapi.proxy/credentials-name"
	// SecretKeyCredentials is the key in the Secret data for credentials JSON
	SecretKeyCredentials = "credentials.json"
	// CredentialsSecretPrefix is the prefix for credentials Secret names
	CredentialsSecretPrefix = "agentapi-credentials-"
)

// credentialsJSON is the JSON representation of credentials stored in a Secret
type credentialsJSON struct {
	Name      string          `json:"name"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// KubernetesCredentialsRepository implements CredentialsRepository using Kubernetes Secrets
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

	data, err := r.toJSON(creds)
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelCredentials:     "true",
				LabelCredentialsName: labelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeyCredentials: data,
		},
	}

	// Try to create first
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing
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

	// Always ensure the name matches the requested name
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
			// Skip invalid entries
			continue
		}
		credsList = append(credsList, creds)
	}

	return credsList, nil
}

// secretName returns the Secret name for the given credentials name
func (r *KubernetesCredentialsRepository) secretName(name string) string {
	sanitized := sanitizeSecretName(name)
	// Truncate to fit within 253 char limit accounting for prefix
	maxLen := 253 - len(CredentialsSecretPrefix)
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
	}
	return CredentialsSecretPrefix + sanitized
}

// toJSON converts Credentials entity to JSON bytes
func (r *KubernetesCredentialsRepository) toJSON(creds *entities.Credentials) ([]byte, error) {
	cj := &credentialsJSON{
		Name:      creds.Name(),
		Data:      creds.Data(),
		CreatedAt: creds.CreatedAt(),
		UpdatedAt: creds.UpdatedAt(),
	}
	return json.Marshal(cj)
}

// fromSecret converts a Kubernetes Secret to Credentials entity
func (r *KubernetesCredentialsRepository) fromSecret(secret *corev1.Secret) (*entities.Credentials, error) {
	data, ok := secret.Data[SecretKeyCredentials]
	if !ok {
		return nil, fmt.Errorf("secret missing credentials data")
	}

	var cj credentialsJSON
	if err := json.Unmarshal(data, &cj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credentials: %w", err)
	}

	// Use the name from JSON; fall back to the Secret label if it is empty
	credName := cj.Name
	if credName == "" {
		credName = secret.Labels[LabelCredentialsName]
	}

	creds := entities.NewCredentials(credName, cj.Data)
	creds.SetCreatedAt(cj.CreatedAt)
	creds.SetUpdatedAt(cj.UpdatedAt)

	return creds, nil
}
