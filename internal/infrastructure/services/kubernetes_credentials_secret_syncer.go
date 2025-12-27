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
	"k8s.io/apimachinery/pkg/types"
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

	// Try to create the secret first
	_, err := s.client.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err == nil {
		log.Printf("[CREDENTIALS_SYNCER] Created credentials secret %s", secretName)
		return nil
	}

	// If secret already exists, update it using patch
	if errors.IsAlreadyExists(err) {
		patchData, patchErr := buildSecretPatch(secret)
		if patchErr != nil {
			return fmt.Errorf("failed to build patch data: %w", patchErr)
		}

		_, patchErr = s.client.CoreV1().Secrets(s.namespace).Patch(
			ctx,
			secretName,
			types.MergePatchType,
			patchData,
			metav1.PatchOptions{},
		)
		if patchErr != nil {
			return fmt.Errorf("failed to patch credentials secret: %w", patchErr)
		}
		log.Printf("[CREDENTIALS_SYNCER] Patched credentials secret %s", secretName)
		return nil
	}

	return fmt.Errorf("failed to create credentials secret: %w", err)
}

// Delete removes the credentials secret for the given name
func (s *KubernetesCredentialsSecretSyncer) Delete(ctx context.Context, name string) error {
	secretName := s.secretName(name)

	// Directly delete the secret without checking if it exists
	err := s.client.CoreV1().Secrets(s.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Secret doesn't exist, nothing to delete
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

// buildSecretData builds the secret data from settings
func (s *KubernetesCredentialsSecretSyncer) buildSecretData(settings *entities.Settings) map[string][]byte {
	data := make(map[string][]byte)

	bedrock := settings.Bedrock()
	if bedrock != nil && bedrock.Enabled() {
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

// secretPatch represents the patch data structure for updating a Secret
type secretPatch struct {
	Metadata metadataPatch     `json:"metadata"`
	Data     map[string][]byte `json:"data"`
}

type metadataPatch struct {
	Labels map[string]string `json:"labels"`
}

// buildSecretPatch creates a JSON merge patch for the given secret
func buildSecretPatch(secret *corev1.Secret) ([]byte, error) {
	patch := secretPatch{
		Metadata: metadataPatch{
			Labels: secret.Labels,
		},
		Data: secret.Data,
	}
	return json.Marshal(patch)
}
