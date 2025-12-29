package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

const (
	// MarketplaceSecretPrefix is the prefix for marketplace Secret names
	MarketplaceSecretPrefix = "marketplaces-"
	// MarketplaceSecretDataKey is the key in the Secret data for marketplace configuration
	MarketplaceSecretDataKey = "marketplaces.json"
	// LabelMarketplaces is the label key for marketplace resources
	LabelMarketplaces = "agentapi.proxy/marketplaces"
	// LabelMarketplacesName is the label key for marketplace name
	LabelMarketplacesName = "agentapi.proxy/marketplaces-name"
)

// MarketplaceConfig represents the structure of marketplace configuration
type MarketplaceConfig struct {
	Marketplaces map[string]MarketplaceServerConfig `json:"marketplaces"`
}

// MarketplaceServerConfig represents a single marketplace configuration
type MarketplaceServerConfig struct {
	URL string `json:"url"`
}

// KubernetesMarketplaceSecretSyncer implements MarketplaceSecretSyncer using Kubernetes Secrets
type KubernetesMarketplaceSecretSyncer struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesMarketplaceSecretSyncer creates a new KubernetesMarketplaceSecretSyncer
func NewKubernetesMarketplaceSecretSyncer(client kubernetes.Interface, namespace string) *KubernetesMarketplaceSecretSyncer {
	return &KubernetesMarketplaceSecretSyncer{
		client:    client,
		namespace: namespace,
	}
}

// Sync creates or updates the marketplace secret based on settings
func (s *KubernetesMarketplaceSecretSyncer) Sync(ctx context.Context, settings *entities.Settings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	// If no marketplaces, delete the secret
	if settings.Marketplaces() == nil || settings.Marketplaces().IsEmpty() {
		return s.Delete(ctx, settings.Name())
	}

	secretName := s.secretName(settings.Name())
	labelValue := sanitizeMarketplaceLabelValue(settings.Name())

	// Build marketplace config from settings
	mpConfig := s.buildMarketplaceConfig(settings)
	data, err := json.MarshalIndent(mpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal marketplace config: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: s.namespace,
			Labels: map[string]string{
				LabelMarketplaces:     "true",
				LabelMarketplacesName: labelValue,
				LabelManagedBy:        "settings",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			MarketplaceSecretDataKey: data,
		},
	}

	// Try to get existing secret
	existing, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			_, err = s.client.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create marketplace secret: %w", err)
			}
			log.Printf("[MARKETPLACE_SYNCER] Created marketplace secret %s", secretName)
			return nil
		}
		return fmt.Errorf("failed to get marketplace secret: %w", err)
	}

	// Check if secret is managed by settings
	if existing.Labels[LabelManagedBy] != "settings" {
		// Secret exists but is not managed by settings, skip update
		log.Printf("[MARKETPLACE_SYNCER] Skipping update for secret %s: not managed by settings", secretName)
		return nil
	}

	// Update existing secret
	secret.ResourceVersion = existing.ResourceVersion
	_, err = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update marketplace secret: %w", err)
	}
	log.Printf("[MARKETPLACE_SYNCER] Updated marketplace secret %s", secretName)

	return nil
}

// Delete removes the marketplace secret for the given name
func (s *KubernetesMarketplaceSecretSyncer) Delete(ctx context.Context, name string) error {
	secretName := s.secretName(name)

	// Check if secret exists and is managed by settings
	existing, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Secret doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to get marketplace secret: %w", err)
	}

	// Only delete if managed by settings
	if existing.Labels[LabelManagedBy] != "settings" {
		log.Printf("[MARKETPLACE_SYNCER] Skipping delete for secret %s: not managed by settings", secretName)
		return nil
	}

	err = s.client.CoreV1().Secrets(s.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete marketplace secret: %w", err)
	}
	log.Printf("[MARKETPLACE_SYNCER] Deleted marketplace secret %s", secretName)

	return nil
}

// secretName returns the Secret name for the given settings name
func (s *KubernetesMarketplaceSecretSyncer) secretName(name string) string {
	return MarketplaceSecretPrefix + sanitizeMarketplaceSecretName(name)
}

// buildMarketplaceConfig builds the marketplace config from settings
func (s *KubernetesMarketplaceSecretSyncer) buildMarketplaceConfig(settings *entities.Settings) *MarketplaceConfig {
	config := &MarketplaceConfig{
		Marketplaces: make(map[string]MarketplaceServerConfig),
	}

	marketplaces := settings.Marketplaces()
	if marketplaces == nil {
		return config
	}

	for name, marketplace := range marketplaces.Marketplaces() {
		mpConfig := MarketplaceServerConfig{
			URL: marketplace.URL(),
		}
		config.Marketplaces[name] = mpConfig
	}

	return config
}

// sanitizeMarketplaceSecretName sanitizes a string to be used as a Kubernetes Secret name
func sanitizeMarketplaceSecretName(s string) string {
	// Convert to lowercase
	sanitized := strings.ToLower(s)
	// Replace non-alphanumeric characters (except dash) with dash
	re := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	// Remove leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")
	// Collapse multiple dashes
	re = regexp.MustCompile(`-+`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	// Truncate to 253 characters (max Secret name length is 253)
	maxLen := 253 - len(MarketplaceSecretPrefix)
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
	}
	return sanitized
}

// sanitizeMarketplaceLabelValue sanitizes a string to be used as a Kubernetes label value
func sanitizeMarketplaceLabelValue(s string) string {
	// Label values must be 63 characters or less
	// Must start and end with alphanumeric character (or be empty)
	// Can contain dashes, underscores, dots, and alphanumerics
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	sanitized := re.ReplaceAllString(s, "-")
	// Remove leading/trailing invalid chars
	sanitized = strings.Trim(sanitized, "-_.")
	// Truncate to 63 characters
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}
	// Ensure it ends with alphanumeric
	sanitized = strings.TrimRight(sanitized, "-_.")
	return sanitized
}
