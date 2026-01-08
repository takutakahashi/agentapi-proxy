package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	// LabelWebhook is the label key for webhook resources
	LabelWebhook = "agentapi.proxy/webhook"
	// SecretKeyWebhooks is the key in the Secret data for webhooks JSON
	SecretKeyWebhooks = "webhooks.json"
	// WebhookSecretName is the name of the Secret containing all webhooks
	WebhookSecretName = "agentapi-webhooks"
)

// webhooksData is the JSON structure stored in the Secret
type webhooksData struct {
	Webhooks []*WebhookConfig `json:"webhooks"`
}

// KubernetesManager implements Manager using Kubernetes Secrets
type KubernetesManager struct {
	client    kubernetes.Interface
	namespace string
	mu        sync.RWMutex
}

// NewKubernetesManager creates a new KubernetesManager
func NewKubernetesManager(client kubernetes.Interface, namespace string) *KubernetesManager {
	return &KubernetesManager{
		client:    client,
		namespace: namespace,
	}
}

// Create creates a new webhook
func (m *KubernetesManager) Create(ctx context.Context, webhook *WebhookConfig) error {
	if err := webhook.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	webhooks, err := m.loadWebhooks(ctx)
	if err != nil {
		return fmt.Errorf("failed to load webhooks: %w", err)
	}

	// Check for duplicate ID
	for _, w := range webhooks {
		if w.ID == webhook.ID {
			return fmt.Errorf("webhook already exists: %s", webhook.ID)
		}
	}

	// Generate secret if not provided
	if webhook.Secret == "" {
		secret, err := generateSecret(32)
		if err != nil {
			return fmt.Errorf("failed to generate secret: %w", err)
		}
		webhook.Secret = secret
	}

	now := time.Now()
	webhook.CreatedAt = now
	webhook.UpdatedAt = now

	webhooks = append(webhooks, webhook)

	if err := m.saveWebhooks(ctx, webhooks); err != nil {
		return fmt.Errorf("failed to save webhooks: %w", err)
	}

	return nil
}

// Get retrieves a webhook by ID
func (m *KubernetesManager) Get(ctx context.Context, id string) (*WebhookConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	webhooks, err := m.loadWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load webhooks: %w", err)
	}

	for _, w := range webhooks {
		if w.ID == id {
			return w, nil
		}
	}

	return nil, ErrWebhookNotFound{ID: id}
}

// List retrieves webhooks matching the filter
func (m *KubernetesManager) List(ctx context.Context, filter Filter) ([]*WebhookConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	webhooks, err := m.loadWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load webhooks: %w", err)
	}

	var result []*WebhookConfig
	for _, w := range webhooks {
		if filter.UserID != "" && w.UserID != filter.UserID {
			continue
		}
		if filter.Status != "" && w.Status != filter.Status {
			continue
		}
		if filter.Type != "" && w.Type != filter.Type {
			continue
		}
		// Scope filter (use GetScope() to handle default value)
		if filter.Scope != "" && w.GetScope() != filter.Scope {
			continue
		}
		// TeamID filter
		if filter.TeamID != "" && w.TeamID != filter.TeamID {
			continue
		}
		// TeamIDs filter (for team-scoped webhooks, check if webhook's team is in user's teams)
		if len(filter.TeamIDs) > 0 && w.GetScope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if w.TeamID == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}
		result = append(result, w)
	}

	return result, nil
}

// Update updates an existing webhook
func (m *KubernetesManager) Update(ctx context.Context, webhook *WebhookConfig) error {
	if err := webhook.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	webhooks, err := m.loadWebhooks(ctx)
	if err != nil {
		return fmt.Errorf("failed to load webhooks: %w", err)
	}

	found := false
	for i, w := range webhooks {
		if w.ID == webhook.ID {
			webhook.UpdatedAt = time.Now()
			webhooks[i] = webhook
			found = true
			break
		}
	}

	if !found {
		return ErrWebhookNotFound{ID: webhook.ID}
	}

	if err := m.saveWebhooks(ctx, webhooks); err != nil {
		return fmt.Errorf("failed to save webhooks: %w", err)
	}

	return nil
}

// Delete removes a webhook by ID
func (m *KubernetesManager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	webhooks, err := m.loadWebhooks(ctx)
	if err != nil {
		return fmt.Errorf("failed to load webhooks: %w", err)
	}

	found := false
	var newWebhooks []*WebhookConfig
	for _, w := range webhooks {
		if w.ID == id {
			found = true
			continue
		}
		newWebhooks = append(newWebhooks, w)
	}

	if !found {
		return ErrWebhookNotFound{ID: id}
	}

	if err := m.saveWebhooks(ctx, newWebhooks); err != nil {
		return fmt.Errorf("failed to save webhooks: %w", err)
	}

	return nil
}

// FindByGitHubRepository finds webhooks that may match a GitHub webhook
func (m *KubernetesManager) FindByGitHubRepository(ctx context.Context, matcher GitHubMatcher) ([]*WebhookConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	webhooks, err := m.loadWebhooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load webhooks: %w", err)
	}

	var result []*WebhookConfig
	for _, w := range webhooks {
		// Only GitHub webhooks
		if w.Type != WebhookTypeGitHub {
			continue
		}

		// Only active webhooks
		if w.Status != WebhookStatusActive {
			continue
		}

		// Check enterprise URL match
		if w.GitHub != nil {
			webhookEnterpriseURL := normalizeEnterpriseURL(w.GitHub.EnterpriseURL)
			matcherEnterpriseURL := normalizeEnterpriseURL(matcher.EnterpriseURL)
			if webhookEnterpriseURL != matcherEnterpriseURL {
				continue
			}

			// Check allowed events
			if len(w.GitHub.AllowedEvents) > 0 {
				eventAllowed := false
				for _, allowedEvent := range w.GitHub.AllowedEvents {
					if allowedEvent == matcher.Event {
						eventAllowed = true
						break
					}
				}
				if !eventAllowed {
					continue
				}
			}

			// Check allowed repositories
			if len(w.GitHub.AllowedRepositories) > 0 {
				repoAllowed := false
				for _, allowedRepo := range w.GitHub.AllowedRepositories {
					if matchRepository(allowedRepo, matcher.Repository) {
						repoAllowed = true
						break
					}
				}
				if !repoAllowed {
					continue
				}
			}
		}

		result = append(result, w)
	}

	return result, nil
}

// RegenerateSecret generates a new secret for a webhook
func (m *KubernetesManager) RegenerateSecret(ctx context.Context, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	webhooks, err := m.loadWebhooks(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load webhooks: %w", err)
	}

	var newSecret string
	found := false
	for _, w := range webhooks {
		if w.ID == id {
			newSecret, err = generateSecret(32)
			if err != nil {
				return "", fmt.Errorf("failed to generate secret: %w", err)
			}
			w.Secret = newSecret
			w.UpdatedAt = time.Now()
			found = true
			break
		}
	}

	if !found {
		return "", ErrWebhookNotFound{ID: id}
	}

	if err := m.saveWebhooks(ctx, webhooks); err != nil {
		return "", fmt.Errorf("failed to save webhooks: %w", err)
	}

	return newSecret, nil
}

// RecordDelivery records a webhook delivery
func (m *KubernetesManager) RecordDelivery(ctx context.Context, id string, record DeliveryRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	webhooks, err := m.loadWebhooks(ctx)
	if err != nil {
		return fmt.Errorf("failed to load webhooks: %w", err)
	}

	found := false
	for _, w := range webhooks {
		if w.ID == id {
			w.LastDelivery = &record
			w.DeliveryCount++
			w.UpdatedAt = time.Now()
			found = true
			break
		}
	}

	if !found {
		return ErrWebhookNotFound{ID: id}
	}

	if err := m.saveWebhooks(ctx, webhooks); err != nil {
		return fmt.Errorf("failed to save webhooks: %w", err)
	}

	return nil
}

// loadWebhooks loads webhooks from the Kubernetes Secret
func (m *KubernetesManager) loadWebhooks(ctx context.Context) ([]*WebhookConfig, error) {
	secret, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, WebhookSecretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return []*WebhookConfig{}, nil
		}
		return nil, fmt.Errorf("failed to get webhooks secret: %w", err)
	}

	data, ok := secret.Data[SecretKeyWebhooks]
	if !ok {
		return []*WebhookConfig{}, nil
	}

	var wd webhooksData
	if err := json.Unmarshal(data, &wd); err != nil {
		return nil, fmt.Errorf("failed to unmarshal webhooks: %w", err)
	}

	return wd.Webhooks, nil
}

// saveWebhooks saves webhooks to the Kubernetes Secret
func (m *KubernetesManager) saveWebhooks(ctx context.Context, webhooks []*WebhookConfig) error {
	wd := webhooksData{Webhooks: webhooks}
	data, err := json.Marshal(wd)
	if err != nil {
		return fmt.Errorf("failed to marshal webhooks: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      WebhookSecretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				LabelWebhook: "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeyWebhooks: data,
		},
	}

	// Try to create first
	_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Get existing secret to preserve any other data
			existing, getErr := m.client.CoreV1().Secrets(m.namespace).Get(ctx, WebhookSecretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}

			existing.Data[SecretKeyWebhooks] = data
			_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update webhooks secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create webhooks secret: %w", err)
	}

	return nil
}

// generateSecret generates a random hex-encoded secret
func generateSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// normalizeEnterpriseURL normalizes enterprise URL for comparison
func normalizeEnterpriseURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	return strings.ToLower(url)
}

// matchRepository checks if a repository matches a pattern
// Pattern can be "owner/repo" (exact match) or "owner/*" (wildcard)
func matchRepository(pattern, repository string) bool {
	if pattern == repository {
		return true
	}

	// Check wildcard pattern (e.g., "owner/*")
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		parts := strings.SplitN(repository, "/", 2)
		if len(parts) == 2 && parts[0] == prefix {
			return true
		}
	}

	return false
}
