package services

import (
	"context"
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
	// CredentialsSecretPrefix is the prefix for credentials Secret names
	CredentialsSecretPrefix = "agent-credentials-"
	// LabelCredentials is the label key for credentials resources
	LabelCredentials = "agentapi.proxy/credentials"
	// LabelCredentialsName is the label key for credentials name
	LabelCredentialsName = "agentapi.proxy/credentials-name"
	// LabelManagedBy is the label key for managed-by
	LabelManagedBy = "agentapi.proxy/managed-by"
)

// KubernetesCredentialsSecretSyncer implements CredentialsSecretSyncer using Kubernetes Secrets
type KubernetesCredentialsSecretSyncer struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesCredentialsSecretSyncer creates a new KubernetesCredentialsSecretSyncer
func NewKubernetesCredentialsSecretSyncer(client kubernetes.Interface, namespace string) *KubernetesCredentialsSecretSyncer {
	return &KubernetesCredentialsSecretSyncer{
		client:    client,
		namespace: namespace,
	}
}

// Sync creates or updates the credentials secret based on settings
func (s *KubernetesCredentialsSecretSyncer) Sync(ctx context.Context, settings *entities.Settings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	secretName := s.secretName(settings.Name())
	labelValue := sanitizeLabelValue(settings.Name())

	// Build secret data from settings
	data := s.buildSecretData(settings)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: s.namespace,
			Labels: map[string]string{
				LabelCredentials:     "true",
				LabelCredentialsName: labelValue,
				LabelManagedBy:       "settings",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}

	// Try to get existing secret
	existing, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			_, err = s.client.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create credentials secret: %w", err)
			}
			log.Printf("[CREDENTIALS_SYNCER] Created credentials secret %s", secretName)
			return nil
		}
		return fmt.Errorf("failed to get credentials secret: %w", err)
	}

	// Check if secret is managed by settings
	if existing.Labels[LabelManagedBy] != "settings" {
		// Secret exists but is not managed by settings, skip update
		log.Printf("[CREDENTIALS_SYNCER] Skipping update for secret %s: not managed by settings", secretName)
		return nil
	}

	// Update existing secret
	secret.ResourceVersion = existing.ResourceVersion
	_, err = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update credentials secret: %w", err)
	}
	log.Printf("[CREDENTIALS_SYNCER] Updated credentials secret %s", secretName)

	return nil
}

// Delete removes the credentials secret for the given name
func (s *KubernetesCredentialsSecretSyncer) Delete(ctx context.Context, name string) error {
	secretName := s.secretName(name)

	// Check if secret exists and is managed by settings
	existing, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Secret doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to get credentials secret: %w", err)
	}

	// Only delete if managed by settings
	if existing.Labels[LabelManagedBy] != "settings" {
		log.Printf("[CREDENTIALS_SYNCER] Skipping delete for secret %s: not managed by settings", secretName)
		return nil
	}

	err = s.client.CoreV1().Secrets(s.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete credentials secret: %w", err)
	}
	log.Printf("[CREDENTIALS_SYNCER] Deleted credentials secret %s", secretName)

	return nil
}

// secretName returns the Secret name for the given settings name
func (s *KubernetesCredentialsSecretSyncer) secretName(name string) string {
	return CredentialsSecretPrefix + sanitizeSecretName(name)
}

// buildSecretData builds the secret data from settings based on auth_mode
func (s *KubernetesCredentialsSecretSyncer) buildSecretData(settings *entities.Settings) map[string][]byte {
	data := make(map[string][]byte)

	// Build secret data based on the configured auth_mode
	switch settings.AuthMode() {
	case entities.AuthModeOAuth:
		data["CLAUDE_CODE_OAUTH_TOKEN"] = []byte(settings.ClaudeCodeOAuthToken())
		data["CLAUDE_CODE_USE_BEDROCK"] = []byte("0")

	case entities.AuthModeBedrock:
		bedrock := settings.Bedrock()
		if bedrock != nil {
			data["CLAUDE_CODE_USE_BEDROCK"] = []byte("1")

			if bedrock.Model() != "" {
				data["ANTHROPIC_MODEL"] = []byte(bedrock.Model())
			}
			if bedrock.AccessKeyID() != "" {
				data["AWS_ACCESS_KEY_ID"] = []byte(bedrock.AccessKeyID())
			}
			if bedrock.SecretAccessKey() != "" {
				data["AWS_SECRET_ACCESS_KEY"] = []byte(bedrock.SecretAccessKey())
			}
			if bedrock.RoleARN() != "" {
				data["AWS_ROLE_ARN"] = []byte(bedrock.RoleARN())
			}
			if bedrock.Profile() != "" {
				data["AWS_PROFILE"] = []byte(bedrock.Profile())
			}
		}

	default:
		// No auth_mode set, don't add any authentication credentials
	}

	return data
}

// sanitizeSecretName sanitizes a string to be used as a Kubernetes Secret name
// Secret names must be lowercase, alphanumeric, and may contain dashes
func sanitizeSecretName(s string) string {
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
	maxLen := 253 - len(CredentialsSecretPrefix)
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
	}
	return sanitized
}

// sanitizeLabelValue sanitizes a string to be used as a Kubernetes label value
func sanitizeLabelValue(s string) string {
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
