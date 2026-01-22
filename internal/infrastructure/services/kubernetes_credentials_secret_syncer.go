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
	// EnvSecretPrefix is the prefix for environment variable Secret names
	EnvSecretPrefix = "agent-env-"
	// LabelEnv is the label key for environment variable resources
	LabelEnv = "agentapi.proxy/env"
	// LabelEnvName is the label key for environment variable name
	LabelEnvName = "agentapi.proxy/env-name"
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

// Sync creates or updates the environment secret based on settings
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
				LabelEnv:       "true",
				LabelEnvName:   labelValue,
				LabelManagedBy: "settings",
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
				return fmt.Errorf("failed to create environment secret: %w", err)
			}
			log.Printf("[ENV_SYNCER] Created environment secret %s", secretName)
			return nil
		}
		return fmt.Errorf("failed to get environment secret: %w", err)
	}

	// Check if secret is managed by settings
	if existing.Labels[LabelManagedBy] != "settings" {
		// Secret exists but is not managed by settings, skip update
		log.Printf("[ENV_SYNCER] Skipping update for secret %s: not managed by settings", secretName)
		return nil
	}

	// Update existing secret
	secret.ResourceVersion = existing.ResourceVersion
	_, err = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update environment secret: %w", err)
	}
	log.Printf("[ENV_SYNCER] Updated environment secret %s", secretName)

	return nil
}

// Delete removes the environment secret for the given name
func (s *KubernetesCredentialsSecretSyncer) Delete(ctx context.Context, name string) error {
	secretName := s.secretName(name)

	// Check if secret exists and is managed by settings
	existing, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Secret doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to get environment secret: %w", err)
	}

	// Only delete if managed by settings
	if existing.Labels[LabelManagedBy] != "settings" {
		log.Printf("[ENV_SYNCER] Skipping delete for secret %s: not managed by settings", secretName)
		return nil
	}

	err = s.client.CoreV1().Secrets(s.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete environment secret: %w", err)
	}
	log.Printf("[ENV_SYNCER] Deleted environment secret %s", secretName)

	return nil
}

// MigrateSecrets migrates old agent-credentials-* secrets to agent-env-* secrets
// This function is idempotent and safe to run multiple times
func (s *KubernetesCredentialsSecretSyncer) MigrateSecrets(ctx context.Context) error {
	// List all secrets with the old prefix in the namespace
	oldPrefix := "agent-credentials-"

	listOptions := metav1.ListOptions{
		LabelSelector: LabelManagedBy + "=settings",
	}

	secrets, err := s.client.CoreV1().Secrets(s.namespace).List(ctx, listOptions)
	if err != nil {
		return fmt.Errorf("failed to list secrets for migration: %w", err)
	}

	migratedCount := 0
	skippedCount := 0

	for _, secret := range secrets.Items {
		// Only migrate secrets with the old prefix
		if !strings.HasPrefix(secret.Name, oldPrefix) {
			continue
		}

		// Extract the name part after the prefix
		namePart := strings.TrimPrefix(secret.Name, oldPrefix)
		newSecretName := EnvSecretPrefix + namePart

		// Check if the new secret already exists
		_, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, newSecretName, metav1.GetOptions{})
		if err == nil {
			// New secret already exists, skip migration but log it
			log.Printf("[ENV_SYNCER] Migration: Secret %s already exists as %s, skipping", secret.Name, newSecretName)
			log.Printf("[ENV_SYNCER] Migration: Old secret %s is preserved for backward compatibility", secret.Name)
			skippedCount++
			continue
		}
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to check if new secret exists: %w", err)
		}

		// Preserve original label value if it exists, otherwise use sanitized name
		labelValue := secret.Labels["agentapi.proxy/credentials-name"]
		if labelValue == "" {
			labelValue = sanitizeLabelValue(namePart)
		}

		// Create new secret with updated labels
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      newSecretName,
				Namespace: s.namespace,
				Labels: map[string]string{
					LabelEnv:       "true",
					LabelEnvName:   labelValue,
					LabelManagedBy: "settings",
				},
				Annotations: secret.Annotations, // Preserve annotations
			},
			Type: secret.Type,
			Data: secret.Data, // Copy data as-is
		}

		// Create the new secret
		_, err = s.client.CoreV1().Secrets(s.namespace).Create(ctx, newSecret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create new secret %s: %w", newSecretName, err)
		}
		log.Printf("[ENV_SYNCER] Migration: Created new secret %s from %s", newSecretName, secret.Name)

		// NOTE: We intentionally DO NOT delete the old secret here to ensure backward compatibility
		// during rolling updates. Old secrets can be manually cleaned up after confirming
		// all pods are using the new secret names.
		log.Printf("[ENV_SYNCER] Migration: Old secret %s is preserved for backward compatibility", secret.Name)

		migratedCount++
	}

	if migratedCount > 0 || skippedCount > 0 {
		log.Printf("[ENV_SYNCER] Migration complete: migrated %d secrets, skipped %d secrets", migratedCount, skippedCount)
	}

	return nil
}

// secretName returns the Secret name for the given settings name
func (s *KubernetesCredentialsSecretSyncer) secretName(name string) string {
	return EnvSecretPrefix + sanitizeSecretName(name)
}

// buildSecretData builds the secret data from settings
// Only credentials for the configured auth_mode are stored
func (s *KubernetesCredentialsSecretSyncer) buildSecretData(settings *entities.Settings) map[string][]byte {
	data := make(map[string][]byte)

	// Determine which authentication mode to use
	authMode := settings.AuthMode()
	if authMode == "" {
		// If auth_mode is not set, determine based on what credentials exist
		// OAuth takes priority if both exist
		if settings.HasClaudeCodeOAuthToken() {
			authMode = entities.AuthModeOAuth
		} else if bedrock := settings.Bedrock(); bedrock != nil && bedrock.Enabled() {
			authMode = entities.AuthModeBedrock
		}
	}

	// Add credentials based on auth mode
	switch authMode {
	case entities.AuthModeOAuth:
		// Only add OAuth token for OAuth mode
		if settings.HasClaudeCodeOAuthToken() {
			data["CLAUDE_CODE_OAUTH_TOKEN"] = []byte(settings.ClaudeCodeOAuthToken())
		}
		data["CLAUDE_CODE_USE_BEDROCK"] = []byte("0")
	case entities.AuthModeBedrock:
		// Only add Bedrock credentials for Bedrock mode
		bedrock := settings.Bedrock()
		if bedrock != nil && (bedrock.Enabled() || bedrock.AccessKeyID() != "" || bedrock.SecretAccessKey() != "") {
			s.addBedrockCredentials(data, bedrock)
		}
		data["CLAUDE_CODE_USE_BEDROCK"] = []byte("1")
	}

	return data
}

// addBedrockCredentials adds Bedrock-related credentials to the secret data
// Note: CLAUDE_CODE_USE_BEDROCK flag is set by buildSecretData(), not here
func (s *KubernetesCredentialsSecretSyncer) addBedrockCredentials(data map[string][]byte, bedrock *entities.BedrockSettings) {
	if bedrock == nil {
		return
	}

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
	maxLen := 253 - len(EnvSecretPrefix)
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
