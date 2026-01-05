package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

// KubernetesSubscriptionSecretSyncer syncs subscription data to Kubernetes Secrets
type KubernetesSubscriptionSecretSyncer struct {
	clientset    kubernetes.Interface
	namespace    string
	storage      notification.Storage
	secretPrefix string
}

// NewKubernetesSubscriptionSecretSyncer creates a new KubernetesSubscriptionSecretSyncer
func NewKubernetesSubscriptionSecretSyncer(
	clientset kubernetes.Interface,
	namespace string,
	storage notification.Storage,
	secretPrefix string,
) *KubernetesSubscriptionSecretSyncer {
	if secretPrefix == "" {
		secretPrefix = "notification-subscriptions"
	}
	return &KubernetesSubscriptionSecretSyncer{
		clientset:    clientset,
		namespace:    namespace,
		storage:      storage,
		secretPrefix: secretPrefix,
	}
}

// Sync creates or updates the subscription Secret for a user
func (s *KubernetesSubscriptionSecretSyncer) Sync(userID string) error {
	ctx := context.Background()

	// Get current subscriptions from storage
	subscriptions, err := s.storage.GetSubscriptions(userID)
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	// Serialize subscriptions to JSON
	data, err := json.Marshal(subscriptions)
	if err != nil {
		return fmt.Errorf("failed to marshal subscriptions: %w", err)
	}

	secretName := fmt.Sprintf("%s-%s", s.secretPrefix, sanitizeLabelValue(userID))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: s.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "agentapi-proxy",
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"app.kubernetes.io/component":  "notification-subscription",
				"agentapi.proxy/user-id":       sanitizeLabelValue(userID),
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"subscriptions.json": data,
		},
	}

	// Try to get existing secret
	existingSecret, err := s.clientset.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			_, createErr := s.clientset.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
			if createErr != nil {
				return fmt.Errorf("failed to create subscription secret: %w", createErr)
			}
			log.Printf("[SUBSCRIPTION_SECRET_SYNCER] Created subscription secret %s for user %s", secretName, userID)
			return nil
		}
		return fmt.Errorf("failed to get subscription secret: %w", err)
	}

	// Update existing secret
	existingSecret.Data = secret.Data
	existingSecret.Labels = secret.Labels
	_, err = s.clientset.CoreV1().Secrets(s.namespace).Update(ctx, existingSecret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update subscription secret: %w", err)
	}
	log.Printf("[SUBSCRIPTION_SECRET_SYNCER] Updated subscription secret %s for user %s", secretName, userID)

	return nil
}

// GetSecretName returns the secret name for a given user ID
func (s *KubernetesSubscriptionSecretSyncer) GetSecretName(userID string) string {
	return fmt.Sprintf("%s-%s", s.secretPrefix, sanitizeLabelValue(userID))
}
