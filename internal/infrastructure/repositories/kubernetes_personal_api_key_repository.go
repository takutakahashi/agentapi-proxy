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
	// LabelPersonalAPIKey is the label key for personal API key resources
	LabelPersonalAPIKey = "agentapi.proxy/personal-api-key"
	// LabelUserID is the label key for user ID
	LabelUserID = "agentapi.proxy/user-id"
	// SecretKeyPersonalAPIKey is the key in the Secret data for API key
	SecretKeyPersonalAPIKey = "api_key"
	// PersonalAPIKeySecretPrefix is the prefix for personal API key Secret names
	PersonalAPIKeySecretPrefix = "agentapi-personal-api-key-"
)

// personalAPIKeyJSON is the JSON representation of personal API key metadata
type personalAPIKeyJSON struct {
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KubernetesPersonalAPIKeyRepository implements PersonalAPIKeyRepository using Kubernetes Secrets
type KubernetesPersonalAPIKeyRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesPersonalAPIKeyRepository creates a new KubernetesPersonalAPIKeyRepository
func NewKubernetesPersonalAPIKeyRepository(client kubernetes.Interface, namespace string) *KubernetesPersonalAPIKeyRepository {
	return &KubernetesPersonalAPIKeyRepository{
		client:    client,
		namespace: namespace,
	}
}

// Save persists a personal API key
func (r *KubernetesPersonalAPIKeyRepository) Save(ctx context.Context, apiKey *entities.PersonalAPIKey) error {
	if err := apiKey.Validate(); err != nil {
		return fmt.Errorf("invalid personal API key: %w", err)
	}

	secretName := r.secretName(string(apiKey.UserID()))
	labelValue := sanitizeLabelValue(string(apiKey.UserID()))

	// Prepare metadata JSON
	metadata := personalAPIKeyJSON{
		UserID:    string(apiKey.UserID()),
		CreatedAt: apiKey.CreatedAt(),
		UpdatedAt: apiKey.UpdatedAt(),
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelPersonalAPIKey: "true",
				LabelUserID:         labelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			SecretKeyPersonalAPIKey: apiKey.APIKey(),
			"metadata.json":         string(metadataBytes),
		},
	}

	// Try to create first
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing
			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update personal API key secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create personal API key secret: %w", err)
	}

	return nil
}

// FindByUserID retrieves a personal API key by user ID
func (r *KubernetesPersonalAPIKeyRepository) FindByUserID(ctx context.Context, userID entities.UserID) (*entities.PersonalAPIKey, error) {
	secretName := r.secretName(string(userID))

	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("personal API key not found for user: %s", userID)
		}
		return nil, fmt.Errorf("failed to get personal API key secret: %w", err)
	}

	return r.fromSecret(secret)
}

// Delete removes a personal API key
func (r *KubernetesPersonalAPIKeyRepository) Delete(ctx context.Context, userID entities.UserID) error {
	secretName := r.secretName(string(userID))

	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete personal API key secret: %w", err)
	}

	return nil
}

// List retrieves all personal API keys
func (r *KubernetesPersonalAPIKeyRepository) List(ctx context.Context) ([]*entities.PersonalAPIKey, error) {
	// List all secrets with the personal API key label
	listOptions := metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", LabelPersonalAPIKey),
	}

	secretList, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list personal API key secrets: %w", err)
	}

	apiKeys := make([]*entities.PersonalAPIKey, 0, len(secretList.Items))

	for _, secret := range secretList.Items {
		apiKey, err := r.fromSecret(&secret)
		if err != nil {
			// Log error but continue with other secrets
			fmt.Printf("Warning: failed to parse personal API key from secret %s: %v\n", secret.Name, err)
			continue
		}
		apiKeys = append(apiKeys, apiKey)
	}

	return apiKeys, nil
}

// secretName generates Secret name from user ID
func (r *KubernetesPersonalAPIKeyRepository) secretName(userID string) string {
	return PersonalAPIKeySecretPrefix + sanitizeLabelValue(userID)
}

// fromSecret converts a Kubernetes Secret to PersonalAPIKey entity
func (r *KubernetesPersonalAPIKeyRepository) fromSecret(secret *corev1.Secret) (*entities.PersonalAPIKey, error) {
	// Extract API key from Secret
	apiKeyBytes, ok := secret.Data[SecretKeyPersonalAPIKey]
	if !ok {
		return nil, fmt.Errorf("api_key not found in secret")
	}

	// Extract metadata
	metadataBytes, ok := secret.Data["metadata.json"]
	if !ok {
		return nil, fmt.Errorf("metadata.json not found in secret")
	}

	var metadata personalAPIKeyJSON
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	apiKey := entities.NewPersonalAPIKey(entities.UserID(metadata.UserID), string(apiKeyBytes))
	apiKey.SetCreatedAt(metadata.CreatedAt)
	apiKey.SetUpdatedAt(metadata.UpdatedAt)

	return apiKey, nil
}
