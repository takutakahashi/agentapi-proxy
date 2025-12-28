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
	// MCPSecretPrefix is the prefix for MCP servers Secret names
	// This matches the existing mcp_servers_user_secret_prefix default value
	MCPSecretPrefix = "mcp-servers-"
	// MCPSecretDataKey is the key in the Secret data for MCP servers configuration
	MCPSecretDataKey = "mcp-servers.json"
	// LabelMCPServers is the label key for MCP servers resources
	LabelMCPServers = "agentapi.proxy/mcp-servers"
	// LabelMCPServersName is the label key for MCP servers name
	LabelMCPServersName = "agentapi.proxy/mcp-servers-name"
)

// MCPConfig represents the structure of MCP configuration (matches pkg/mcp/merge.go)
type MCPConfig struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers"`
}

// MCPServerConfig represents a single MCP server configuration
type MCPServerConfig struct {
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// KubernetesMCPSecretSyncer implements MCPSecretSyncer using Kubernetes Secrets
type KubernetesMCPSecretSyncer struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesMCPSecretSyncer creates a new KubernetesMCPSecretSyncer
func NewKubernetesMCPSecretSyncer(client kubernetes.Interface, namespace string) *KubernetesMCPSecretSyncer {
	return &KubernetesMCPSecretSyncer{
		client:    client,
		namespace: namespace,
	}
}

// Sync creates or updates the MCP servers secret based on settings
func (s *KubernetesMCPSecretSyncer) Sync(ctx context.Context, settings *entities.Settings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	// If no MCP servers, delete the secret
	if settings.MCPServers() == nil || settings.MCPServers().IsEmpty() {
		return s.Delete(ctx, settings.Name())
	}

	secretName := s.secretName(settings.Name())
	labelValue := sanitizeMCPLabelValue(settings.Name())

	// Build MCP config from settings
	mcpConfig := s.buildMCPConfig(settings)
	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: s.namespace,
			Labels: map[string]string{
				LabelMCPServers:     "true",
				LabelMCPServersName: labelValue,
				LabelManagedBy:      "settings",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			MCPSecretDataKey: data,
		},
	}

	// Try to get existing secret
	existing, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Create new secret
			_, err = s.client.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("failed to create MCP servers secret: %w", err)
			}
			log.Printf("[MCP_SYNCER] Created MCP servers secret %s", secretName)
			return nil
		}
		return fmt.Errorf("failed to get MCP servers secret: %w", err)
	}

	// Check if secret is managed by settings
	if existing.Labels[LabelManagedBy] != "settings" {
		// Secret exists but is not managed by settings, skip update
		log.Printf("[MCP_SYNCER] Skipping update for secret %s: not managed by settings", secretName)
		return nil
	}

	// Update existing secret
	secret.ResourceVersion = existing.ResourceVersion
	_, err = s.client.CoreV1().Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update MCP servers secret: %w", err)
	}
	log.Printf("[MCP_SYNCER] Updated MCP servers secret %s", secretName)

	return nil
}

// Delete removes the MCP servers secret for the given name
func (s *KubernetesMCPSecretSyncer) Delete(ctx context.Context, name string) error {
	secretName := s.secretName(name)

	// Check if secret exists and is managed by settings
	existing, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// Secret doesn't exist, nothing to delete
			return nil
		}
		return fmt.Errorf("failed to get MCP servers secret: %w", err)
	}

	// Only delete if managed by settings
	if existing.Labels[LabelManagedBy] != "settings" {
		log.Printf("[MCP_SYNCER] Skipping delete for secret %s: not managed by settings", secretName)
		return nil
	}

	err = s.client.CoreV1().Secrets(s.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete MCP servers secret: %w", err)
	}
	log.Printf("[MCP_SYNCER] Deleted MCP servers secret %s", secretName)

	return nil
}

// secretName returns the Secret name for the given settings name
func (s *KubernetesMCPSecretSyncer) secretName(name string) string {
	return MCPSecretPrefix + sanitizeMCPSecretName(name)
}

// buildMCPConfig builds the MCP config from settings
func (s *KubernetesMCPSecretSyncer) buildMCPConfig(settings *entities.Settings) *MCPConfig {
	config := &MCPConfig{
		MCPServers: make(map[string]MCPServerConfig),
	}

	mcpServers := settings.MCPServers()
	if mcpServers == nil {
		return config
	}

	for name, server := range mcpServers.Servers() {
		serverConfig := MCPServerConfig{
			Type:    server.Type(),
			URL:     server.URL(),
			Command: server.Command(),
		}

		if len(server.Args()) > 0 {
			serverConfig.Args = server.Args()
		}

		if len(server.Env()) > 0 {
			serverConfig.Env = server.Env()
		}

		if len(server.Headers()) > 0 {
			serverConfig.Headers = server.Headers()
		}

		config.MCPServers[name] = serverConfig
	}

	return config
}

// sanitizeMCPSecretName sanitizes a string to be used as a Kubernetes Secret name
func sanitizeMCPSecretName(s string) string {
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
	maxLen := 253 - len(MCPSecretPrefix)
	if len(sanitized) > maxLen {
		sanitized = sanitized[:maxLen]
	}
	return sanitized
}

// sanitizeMCPLabelValue sanitizes a string to be used as a Kubernetes label value
func sanitizeMCPLabelValue(s string) string {
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
