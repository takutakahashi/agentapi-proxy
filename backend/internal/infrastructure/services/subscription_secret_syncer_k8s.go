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

// UpdateSubscriptions writes subs directly to the Kubernetes Secret, bypassing local file storage.
// This implements notification.SubscriptionWriter.
func (s *KubernetesSubscriptionSecretSyncer) UpdateSubscriptions(userID string, subs []notification.Subscription) error {
	ctx := context.Background()

	data, err := json.Marshal(subs)
	if err != nil {
		return fmt.Errorf("failed to marshal subscriptions: %w", err)
	}

	secretName := s.GetSecretName(userID)

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

	existing, err := s.clientset.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, createErr := s.clientset.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
			if createErr != nil {
				return fmt.Errorf("failed to create subscription secret: %w", createErr)
			}
			log.Printf("[SUBSCRIPTION_SECRET_SYNCER] Created subscription secret %s for user %s", secretName, userID)
			return nil
		}
		return fmt.Errorf("failed to get subscription secret: %w", err)
	}

	existing.Data = secret.Data
	existing.Labels = secret.Labels
	if _, updateErr := s.clientset.CoreV1().Secrets(s.namespace).Update(ctx, existing, metav1.UpdateOptions{}); updateErr != nil {
		return fmt.Errorf("failed to update subscription secret: %w", updateErr)
	}
	log.Printf("[SUBSCRIPTION_SECRET_SYNCER] Updated subscription secret %s for user %s (%d subs)", secretName, userID, len(subs))
	return nil
}

// GetSecretName returns the secret name for a given user ID
func (s *KubernetesSubscriptionSecretSyncer) GetSecretName(userID string) string {
	return fmt.Sprintf("%s-%s", s.secretPrefix, sanitizeLabelValue(userID))
}

// GetSubscriptions reads all active subscriptions for a user from the Kubernetes Secret.
// This implements notification.SubscriptionReader.
func (s *KubernetesSubscriptionSecretSyncer) GetSubscriptions(userID string) ([]notification.Subscription, error) {
	ctx := context.Background()
	secretName := s.GetSecretName(userID)

	secret, err := s.clientset.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return []notification.Subscription{}, nil
		}
		return nil, fmt.Errorf("failed to get subscription secret %s: %w", secretName, err)
	}

	data, ok := secret.Data["subscriptions.json"]
	if !ok {
		return []notification.Subscription{}, nil
	}

	var subs []notification.Subscription
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal subscriptions from secret %s: %w", secretName, err)
	}

	log.Printf("[SUBSCRIPTION_SECRET_SYNCER] Read %d subscriptions for user %s from secret %s", len(subs), userID, secretName)
	return subs, nil
}

// GetAllSubscriptions reads all active subscriptions from all Kubernetes Secrets
// that have the notification-subscription label.
// This implements notification.SubscriptionReader.
func (s *KubernetesSubscriptionSecretSyncer) GetAllSubscriptions() ([]notification.Subscription, error) {
	ctx := context.Background()

	secretList, err := s.clientset.CoreV1().Secrets(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/component=notification-subscription",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list subscription secrets: %w", err)
	}

	var allSubs []notification.Subscription
	for _, secret := range secretList.Items {
		data, ok := secret.Data["subscriptions.json"]
		if !ok {
			continue
		}

		var subs []notification.Subscription
		if err := json.Unmarshal(data, &subs); err != nil {
			log.Printf("[SUBSCRIPTION_SECRET_SYNCER] Warning: failed to unmarshal subscriptions from secret %s: %v", secret.Name, err)
			continue
		}

		allSubs = append(allSubs, subs...)
	}

	log.Printf("[SUBSCRIPTION_SECRET_SYNCER] Read %d total subscriptions from %d secrets", len(allSubs), len(secretList.Items))
	return allSubs, nil
}
