package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
)

const (
	// LabelSettings is the label key for settings resources
	LabelSettings = "agentapi.proxy/settings"
	// LabelSettingsName is the label key for settings name
	LabelSettingsName = "agentapi.proxy/settings-name"
	// SecretKeySettings is the key in the Secret data for settings JSON
	SecretKeySettings = "settings.json"
	// SettingsSecretPrefix is the prefix for settings Secret names
	SettingsSecretPrefix = "agentapi-settings-"
)

// settingsJSON is the JSON representation of settings stored in Secret
type settingsJSON struct {
	Name                 string                      `json:"name"`
	Bedrock              *bedrockJSON                `json:"bedrock,omitempty"`
	MCPServers           map[string]*mcpServerJSON   `json:"mcp_servers,omitempty"`
	Marketplaces         map[string]*marketplaceJSON `json:"marketplaces,omitempty"`
	ClaudeCodeOAuthToken string                      `json:"claude_code_oauth_token,omitempty"`
	AuthMode             string                      `json:"auth_mode,omitempty"`
	EnabledPlugins       []string                    `json:"enabled_plugins,omitempty"` // plugin@marketplace format
	CreatedAt            time.Time                   `json:"created_at"`
	UpdatedAt            time.Time                   `json:"updated_at"`
}

// bedrockJSON is the JSON representation of Bedrock settings
type bedrockJSON struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// mcpServerJSON is the JSON representation of a single MCP server
type mcpServerJSON struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// marketplaceJSON is the JSON representation of a single marketplace
type marketplaceJSON struct {
	URL string `json:"url"`
}

// KubernetesSettingsRepository implements SettingsRepository using Kubernetes Secrets
type KubernetesSettingsRepository struct {
	client            kubernetes.Interface
	namespace         string
	encryptionService services.EncryptionService
}

// NewKubernetesSettingsRepository creates a new KubernetesSettingsRepository
func NewKubernetesSettingsRepository(client kubernetes.Interface, namespace string, encryptionService services.EncryptionService) *KubernetesSettingsRepository {
	return &KubernetesSettingsRepository{
		client:            client,
		namespace:         namespace,
		encryptionService: encryptionService,
	}
}

// Save persists settings (creates or updates)
func (r *KubernetesSettingsRepository) Save(ctx context.Context, settings *entities.Settings) error {
	if err := settings.Validate(); err != nil {
		return fmt.Errorf("invalid settings: %w", err)
	}

	secretName := r.secretName(settings.Name())
	labelValue := sanitizeLabelValue(settings.Name())

	// Convert to JSON
	data, err := r.toJSON(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelSettings:     "true",
				LabelSettingsName: labelValue,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeySettings: data,
		},
	}

	// Try to create first
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Update existing
			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update settings secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create settings secret: %w", err)
	}

	return nil
}

// FindByName retrieves settings by name
func (r *KubernetesSettingsRepository) FindByName(ctx context.Context, name string) (*entities.Settings, error) {
	secretName := r.secretName(name)

	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("settings not found: %s", name)
		}
		return nil, fmt.Errorf("failed to get settings secret: %w", err)
	}

	return r.fromSecret(secret)
}

// Delete removes settings by name
func (r *KubernetesSettingsRepository) Delete(ctx context.Context, name string) error {
	secretName := r.secretName(name)

	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("settings not found: %s", name)
		}
		return fmt.Errorf("failed to delete settings secret: %w", err)
	}

	return nil
}

// Exists checks if settings exist for the given name
func (r *KubernetesSettingsRepository) Exists(ctx context.Context, name string) (bool, error) {
	secretName := r.secretName(name)

	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check settings existence: %w", err)
	}

	return true, nil
}

// List retrieves all settings
func (r *KubernetesSettingsRepository) List(ctx context.Context) ([]*entities.Settings, error) {
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=true", LabelSettings),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list settings secrets: %w", err)
	}

	var settingsList []*entities.Settings
	for _, secret := range secrets.Items {
		settings, err := r.fromSecret(&secret)
		if err != nil {
			// Skip invalid settings
			continue
		}
		settingsList = append(settingsList, settings)
	}

	return settingsList, nil
}

// secretName returns the Secret name for the given settings name
func (r *KubernetesSettingsRepository) secretName(name string) string {
	return SettingsSecretPrefix + sanitizeSecretName(name)
}

// encryptValue encrypts a plaintext value and returns a JSON-encoded EncryptedData
func (r *KubernetesSettingsRepository) encryptValue(ctx context.Context, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	encrypted, err := r.encryptionService.Encrypt(ctx, plaintext)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt value: %w", err)
	}

	// JSON encode the EncryptedData
	data, err := json.Marshal(encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to marshal encrypted data: %w", err)
	}

	return string(data), nil
}

// decryptValue attempts to decrypt a value if it's encrypted, otherwise returns it as-is
func (r *KubernetesSettingsRepository) decryptValue(ctx context.Context, value string) (string, error) {
	if value == "" {
		return "", nil
	}

	// Try to unmarshal as EncryptedData
	var encrypted services.EncryptedData
	if err := json.Unmarshal([]byte(value), &encrypted); err != nil {
		// Not encrypted, return as-is (plaintext for backward compatibility)
		return value, nil
	}

	// Decrypt
	plaintext, err := r.encryptionService.Decrypt(ctx, &encrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt value: %w", err)
	}

	return plaintext, nil
}

// toJSON converts Settings entity to JSON bytes
func (r *KubernetesSettingsRepository) toJSON(settings *entities.Settings) ([]byte, error) {
	ctx := context.Background()

	// Encrypt OAuth token if present
	oauthToken, err := r.encryptValue(ctx, settings.ClaudeCodeOAuthToken())
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt oauth token: %w", err)
	}

	sj := &settingsJSON{
		Name:                 settings.Name(),
		ClaudeCodeOAuthToken: oauthToken,
		AuthMode:             string(settings.AuthMode()),
		CreatedAt:            settings.CreatedAt(),
		UpdatedAt:            settings.UpdatedAt(),
	}

	if bedrock := settings.Bedrock(); bedrock != nil {
		// Encrypt sensitive Bedrock fields
		accessKeyID, err := r.encryptValue(ctx, bedrock.AccessKeyID())
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt access key id: %w", err)
		}
		secretAccessKey, err := r.encryptValue(ctx, bedrock.SecretAccessKey())
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt secret access key: %w", err)
		}

		sj.Bedrock = &bedrockJSON{
			Enabled:         bedrock.Enabled(),
			Model:           bedrock.Model(),
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
			RoleARN:         bedrock.RoleARN(),
			Profile:         bedrock.Profile(),
		}
	}

	if mcpServers := settings.MCPServers(); mcpServers != nil && !mcpServers.IsEmpty() {
		sj.MCPServers = make(map[string]*mcpServerJSON)
		for name, server := range mcpServers.Servers() {
			// Encrypt env values
			encryptedEnv := make(map[string]string)
			for k, v := range server.Env() {
				encrypted, err := r.encryptValue(ctx, v)
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt env %s: %w", k, err)
				}
				encryptedEnv[k] = encrypted
			}

			// Encrypt header values
			encryptedHeaders := make(map[string]string)
			for k, v := range server.Headers() {
				encrypted, err := r.encryptValue(ctx, v)
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt header %s: %w", k, err)
				}
				encryptedHeaders[k] = encrypted
			}

			sj.MCPServers[name] = &mcpServerJSON{
				Type:    server.Type(),
				URL:     server.URL(),
				Command: server.Command(),
				Args:    server.Args(),
				Env:     encryptedEnv,
				Headers: encryptedHeaders,
			}
		}
	}

	if marketplaces := settings.Marketplaces(); marketplaces != nil && !marketplaces.IsEmpty() {
		sj.Marketplaces = make(map[string]*marketplaceJSON)
		for name, marketplace := range marketplaces.Marketplaces() {
			sj.Marketplaces[name] = &marketplaceJSON{
				URL: marketplace.URL(),
			}
		}
	}

	if plugins := settings.EnabledPlugins(); len(plugins) > 0 {
		sj.EnabledPlugins = plugins
	}

	return json.Marshal(sj)
}

// fromSecret converts a Kubernetes Secret to Settings entity
func (r *KubernetesSettingsRepository) fromSecret(secret *corev1.Secret) (*entities.Settings, error) {
	ctx := context.Background()

	data, ok := secret.Data[SecretKeySettings]
	if !ok {
		return nil, fmt.Errorf("secret missing settings data")
	}

	var sj settingsJSON
	if err := json.Unmarshal(data, &sj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	settings := entities.NewSettings(sj.Name)
	settings.SetCreatedAt(sj.CreatedAt)
	settings.SetUpdatedAt(sj.UpdatedAt)

	if sj.Bedrock != nil {
		bedrock := entities.NewBedrockSettings(sj.Bedrock.Enabled)
		bedrock.SetModel(sj.Bedrock.Model)

		// Decrypt sensitive Bedrock fields
		accessKeyID, err := r.decryptValue(ctx, sj.Bedrock.AccessKeyID)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt access key id: %w", err)
		}
		secretAccessKey, err := r.decryptValue(ctx, sj.Bedrock.SecretAccessKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt secret access key: %w", err)
		}

		bedrock.SetAccessKeyID(accessKeyID)
		bedrock.SetSecretAccessKey(secretAccessKey)
		bedrock.SetRoleARN(sj.Bedrock.RoleARN)
		bedrock.SetProfile(sj.Bedrock.Profile)
		settings.SetBedrock(bedrock)
		// Reset updatedAt since SetBedrock updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.MCPServers) > 0 {
		mcpServers := entities.NewMCPServersSettings()
		for name, serverJSON := range sj.MCPServers {
			server := entities.NewMCPServer(name, serverJSON.Type)
			server.SetURL(serverJSON.URL)
			server.SetCommand(serverJSON.Command)
			server.SetArgs(serverJSON.Args)

			// Decrypt env values
			decryptedEnv := make(map[string]string)
			for k, v := range serverJSON.Env {
				decrypted, err := r.decryptValue(ctx, v)
				if err != nil {
					return nil, fmt.Errorf("failed to decrypt env %s: %w", k, err)
				}
				decryptedEnv[k] = decrypted
			}
			server.SetEnv(decryptedEnv)

			// Decrypt header values
			decryptedHeaders := make(map[string]string)
			for k, v := range serverJSON.Headers {
				decrypted, err := r.decryptValue(ctx, v)
				if err != nil {
					return nil, fmt.Errorf("failed to decrypt header %s: %w", k, err)
				}
				decryptedHeaders[k] = decrypted
			}
			server.SetHeaders(decryptedHeaders)

			mcpServers.SetServer(name, server)
		}
		settings.SetMCPServers(mcpServers)
		// Reset updatedAt since SetMCPServers updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.Marketplaces) > 0 {
		marketplaces := entities.NewMarketplacesSettings()
		for name, mpJSON := range sj.Marketplaces {
			marketplace := entities.NewMarketplace(name)
			marketplace.SetURL(mpJSON.URL)
			marketplaces.SetMarketplace(name, marketplace)
		}
		settings.SetMarketplaces(marketplaces)
		// Reset updatedAt since SetMarketplaces updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.EnabledPlugins) > 0 {
		settings.SetEnabledPlugins(sj.EnabledPlugins)
		// Reset updatedAt since SetEnabledPlugins updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.ClaudeCodeOAuthToken != "" {
		// Decrypt OAuth token
		oauthToken, err := r.decryptValue(ctx, sj.ClaudeCodeOAuthToken)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt oauth token: %w", err)
		}
		settings.SetClaudeCodeOAuthToken(oauthToken)
		// Reset updatedAt since SetClaudeCodeOAuthToken updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.AuthMode != "" {
		settings.SetAuthMode(entities.AuthMode(sj.AuthMode))
		// Reset updatedAt since SetAuthMode updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	return settings, nil
}

// sanitizeSecretName sanitizes a string to be used as a Kubernetes Secret name
// Secret names must be lowercase, alphanumeric, and may contain dashes
// Example: "myorg/backend-team" -> "myorg-backend-team"
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
	if len(sanitized) > 253-len(SettingsSecretPrefix) {
		sanitized = sanitized[:253-len(SettingsSecretPrefix)]
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
