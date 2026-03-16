package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// RateLimitStore tracks when notifications were last sent per user.
type RateLimitStore interface {
	// IsRateLimited returns true if a notification was sent to userID within the cooldown window.
	IsRateLimited(userID string) bool
	// RecordSent records that a notification was sent to userID right now.
	RecordSent(userID string) error
}

// -------------------------------------------------------------------------
// InMemoryRateLimitStore – simple per-pod fallback
// -------------------------------------------------------------------------

// InMemoryRateLimitStore is an in-memory RateLimitStore (single pod only).
type InMemoryRateLimitStore struct {
	mu             sync.RWMutex
	lastNotifiedAt map[string]time.Time
	cooldown       time.Duration
}

// NewInMemoryRateLimitStore creates a new InMemoryRateLimitStore.
func NewInMemoryRateLimitStore(cooldown time.Duration) *InMemoryRateLimitStore {
	return &InMemoryRateLimitStore{
		lastNotifiedAt: make(map[string]time.Time),
		cooldown:       cooldown,
	}
}

func (s *InMemoryRateLimitStore) IsRateLimited(userID string) bool {
	s.mu.RLock()
	last, ok := s.lastNotifiedAt[userID]
	s.mu.RUnlock()
	return ok && time.Since(last) < s.cooldown
}

func (s *InMemoryRateLimitStore) RecordSent(userID string) error {
	s.mu.Lock()
	s.lastNotifiedAt[userID] = time.Now()
	s.mu.Unlock()
	return nil
}

// -------------------------------------------------------------------------
// ConfigMapRateLimitStore – shared across pods via a Kubernetes ConfigMap
// -------------------------------------------------------------------------

const rateLimitConfigMapName = "agentapi-notification-ratelimit"

// ConfigMapRateLimitStore persists rate limit state in a Kubernetes ConfigMap
// so all pod replicas share the same cooldown window.
type ConfigMapRateLimitStore struct {
	client    kubernetes.Interface
	namespace string
	cooldown  time.Duration
}

// NewConfigMapRateLimitStore creates a new ConfigMapRateLimitStore.
func NewConfigMapRateLimitStore(client kubernetes.Interface, namespace string, cooldown time.Duration) *ConfigMapRateLimitStore {
	return &ConfigMapRateLimitStore{
		client:    client,
		namespace: namespace,
		cooldown:  cooldown,
	}
}

// IsRateLimited fetches the ConfigMap and checks whether the cooldown for userID has elapsed.
func (s *ConfigMapRateLimitStore) IsRateLimited(userID string) bool {
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(
		context.Background(), rateLimitConfigMapName, metav1.GetOptions{},
	)
	if err != nil {
		// ConfigMap not found → no cooldown recorded yet
		return false
	}

	raw, ok := cm.Data[userID]
	if !ok {
		return false
	}

	var last time.Time
	if err := json.Unmarshal([]byte(raw), &last); err != nil {
		return false
	}

	return time.Since(last) < s.cooldown
}

// RecordSent updates (or creates) the ConfigMap with the current timestamp for userID.
func (s *ConfigMapRateLimitStore) RecordSent(userID string) error {
	now, err := json.Marshal(time.Now())
	if err != nil {
		return fmt.Errorf("failed to marshal timestamp: %w", err)
	}

	ctx := context.Background()

	// Try to get the existing ConfigMap first.
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, rateLimitConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("failed to get ratelimit configmap: %w", err)
		}
		// Create a new one.
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rateLimitConfigMapName,
				Namespace: s.namespace,
				Labels: map[string]string{
					"agentapi.proxy/type": "notification-ratelimit",
				},
			},
			Data: map[string]string{
				userID: string(now),
			},
		}
		if _, createErr := s.client.CoreV1().ConfigMaps(s.namespace).Create(ctx, newCM, metav1.CreateOptions{}); createErr != nil {
			return fmt.Errorf("failed to create ratelimit configmap: %w", createErr)
		}
		return nil
	}

	// Update existing ConfigMap.
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[userID] = string(now)

	if _, updateErr := s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{}); updateErr != nil {
		// Log but don't fail – a race condition here is harmless for rate limiting.
		log.Printf("[RATELIMIT] Failed to update ratelimit configmap: %v", updateErr)
	}
	return nil
}
