package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	domainservices "github.com/takutakahashi/agentapi-proxy/internal/domain/services"
	infraservices "github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
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

// encryptedEnvVarJSON is the JSON representation of a single encrypted env var value.
// It embeds the encryption metadata needed to select the correct decryptor.
type encryptedEnvVarJSON struct {
	EncryptedValue string    `json:"v"`
	Algorithm      string    `json:"alg"`
	KeyID          string    `json:"kid"`
	EncryptedAt    time.Time `json:"at"`
	Version        string    `json:"ver,omitempty"`
}

// settingsJSON is the JSON representation of settings stored in Secret
type settingsJSON struct {
	Name                    string                                 `json:"name"`
	Bedrock                 *bedrockJSON                           `json:"bedrock,omitempty"`
	MCPServers              map[string]*mcpServerJSON              `json:"mcp_servers,omitempty"`
	Marketplaces            map[string]*marketplaceJSON            `json:"marketplaces,omitempty"`
	ClaudeCodeOAuthToken    string                                 `json:"claude_code_oauth_token,omitempty"`
	AuthMode                string                                 `json:"auth_mode,omitempty"`
	EnabledPlugins          []string                               `json:"enabled_plugins,omitempty"`           // plugin@marketplace format
	EnvVars                 map[string]string                      `json:"env_vars,omitempty"`                  // plain env vars (legacy / noop)
	EncryptedEnvVars        map[string]encryptedEnvVarJSON         `json:"encrypted_env_vars,omitempty"`        // encrypted env vars
	PreferredTeamID         string                                 `json:"preferred_team_id,omitempty"`         // "org/team-slug" format
	SlackUserID             string                                 `json:"slack_user_id,omitempty"`             // Slack DM notification user ID
	NotificationChannels    []string                               `json:"notification_channels,omitempty"`     // Active notification channels
	ExternalSessionManagers []entities.ExternalSessionManagerEntry `json:"external_session_managers,omitempty"` // Registered external session managers
	GitSync                 *gitSyncJSON                           `json:"git_sync,omitempty"`
	DefaultSessionProfileID string                                 `json:"default_session_profile_id,omitempty"`
	CreatedAt               time.Time                              `json:"created_at"`
	UpdatedAt               time.Time                              `json:"updated_at"`
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

// syncEncryptionConfigJSON is the JSON representation of sync encryption config
type syncEncryptionConfigJSON struct {
	KMSKeyARN    string `json:"kms_key_arn"`
	AWSRegion    string `json:"aws_region"`
	EncryptedDEK string `json:"encrypted_dek,omitempty"`
	DEKVersion   int    `json:"dek_version,omitempty"`
}

// gitSyncJSON is the JSON representation of GitHub sync configuration
type gitSyncJSON struct {
	Enabled      bool                     `json:"enabled"`
	RepoFullName string                   `json:"repo_full_name"`
	Branch       string                   `json:"branch"`
	RootPath     string                   `json:"root_path"`
	AutoPush     bool                     `json:"auto_push"`
	GitHubToken  string                   `json:"github_token,omitempty"`
	Encryption   syncEncryptionConfigJSON `json:"encryption"`
	LastPushedAt *time.Time               `json:"last_pushed_at,omitempty"`
}

// KubernetesSettingsRepository implements SettingsRepository using Kubernetes Secrets
type KubernetesSettingsRepository struct {
	client             kubernetes.Interface
	namespace          string
	encryptionRegistry *infraservices.EncryptionServiceRegistry // optional; nil = store env_vars as plain text
}

// NewKubernetesSettingsRepository creates a new KubernetesSettingsRepository.
// An optional EncryptionServiceRegistry may be passed to enable at-rest encryption of
// env_vars.  If nil (or if the primary algorithm is "noop"), env_vars are stored as
// plain text in the legacy env_vars field.
func NewKubernetesSettingsRepository(client kubernetes.Interface, namespace string, registry ...*infraservices.EncryptionServiceRegistry) *KubernetesSettingsRepository {
	var reg *infraservices.EncryptionServiceRegistry
	if len(registry) > 0 {
		reg = registry[0]
	}
	return &KubernetesSettingsRepository{
		client:             client,
		namespace:          namespace,
		encryptionRegistry: reg,
	}
}

// Save persists settings (creates or updates)
func (r *KubernetesSettingsRepository) Save(ctx context.Context, settings *entities.Settings) error {
	if err := settings.Validate(); err != nil {
		return fmt.Errorf("invalid settings: %w", err)
	}

	secretName := r.secretName(settings.Name())
	labelValue := sanitizeLabelValue(settings.Name())

	data, err := r.toJSON(ctx, settings)
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

	settings, err := r.fromSecret(ctx, secret)
	if err != nil {
		return nil, err
	}

	// Always ensure the name matches the requested name.
	// The stored JSON may have an empty name (legacy entries), so we set it here.
	if settings.Name() == "" {
		settings.SetName(name)
	}

	return settings, nil
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
		settings, err := r.fromSecret(ctx, &secret)
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

// encryptionSvc returns the primary encryption service, or nil if no registry is configured.
func (r *KubernetesSettingsRepository) encryptionSvc() domainservices.EncryptionService {
	if r.encryptionRegistry == nil {
		return nil
	}
	return r.encryptionRegistry.GetForEncryption()
}

// decryptionSvc returns the appropriate decryption service for the given metadata, or nil if no registry is configured.
func (r *KubernetesSettingsRepository) decryptionSvc(metadata domainservices.EncryptionMetadata) domainservices.EncryptionService {
	if r.encryptionRegistry == nil {
		return nil
	}
	return r.encryptionRegistry.GetForDecryption(metadata)
}

// toJSON converts Settings entity to JSON bytes, encrypting env_vars when a non-noop
// EncryptionServiceRegistry is configured.
func (r *KubernetesSettingsRepository) toJSON(ctx context.Context, settings *entities.Settings) ([]byte, error) {
	sj := &settingsJSON{
		Name:                 settings.Name(),
		ClaudeCodeOAuthToken: settings.ClaudeCodeOAuthToken(),
		AuthMode:             string(settings.AuthMode()),
		CreatedAt:            settings.CreatedAt(),
		UpdatedAt:            settings.UpdatedAt(),
	}

	if bedrock := settings.Bedrock(); bedrock != nil {
		sj.Bedrock = &bedrockJSON{
			Enabled:         bedrock.Enabled(),
			Model:           bedrock.Model(),
			AccessKeyID:     bedrock.AccessKeyID(),
			SecretAccessKey: bedrock.SecretAccessKey(),
			RoleARN:         bedrock.RoleARN(),
			Profile:         bedrock.Profile(),
		}
	}

	if mcpServers := settings.MCPServers(); mcpServers != nil && !mcpServers.IsEmpty() {
		sj.MCPServers = make(map[string]*mcpServerJSON)
		for name, server := range mcpServers.Servers() {
			sj.MCPServers[name] = &mcpServerJSON{
				Type:    server.Type(),
				URL:     server.URL(),
				Command: server.Command(),
				Args:    server.Args(),
				Env:     server.Env(),
				Headers: server.Headers(),
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

	if envVars := settings.EnvVars(); len(envVars) > 0 {
		if enc := r.encryptionSvc(); enc != nil && enc.Algorithm() != "noop" {
			sj.EncryptedEnvVars = make(map[string]encryptedEnvVarJSON, len(envVars))
			for k, v := range envVars {
				encrypted, err := enc.Encrypt(ctx, v)
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt env var %q: %w", k, err)
				}
				sj.EncryptedEnvVars[k] = encryptedEnvVarJSON{
					EncryptedValue: encrypted.EncryptedValue,
					Algorithm:      encrypted.Metadata.Algorithm,
					KeyID:          encrypted.Metadata.KeyID,
					EncryptedAt:    encrypted.Metadata.EncryptedAt,
					Version:        encrypted.Metadata.Version,
				}
			}
		} else {
			sj.EnvVars = envVars
		}
	}

	if preferredTeamID := settings.PreferredTeamID(); preferredTeamID != "" {
		sj.PreferredTeamID = preferredTeamID
	}

	if slackUserID := settings.SlackUserID(); slackUserID != "" {
		sj.SlackUserID = slackUserID
	}

	if channels := settings.NotificationChannels(); len(channels) > 0 {
		sj.NotificationChannels = channels
	}

	if managers := settings.ExternalSessionManagers(); len(managers) > 0 {
		sj.ExternalSessionManagers = managers
	}

	if gitSync := settings.GitSync(); gitSync != nil {
		j := &gitSyncJSON{
			Enabled:      gitSync.Enabled,
			RepoFullName: gitSync.RepoFullName,
			Branch:       gitSync.Branch,
			RootPath:     gitSync.RootPath,
			AutoPush:     gitSync.AutoPush,
			GitHubToken:  gitSync.GitHubToken,
			Encryption: syncEncryptionConfigJSON{
				KMSKeyARN:    gitSync.Encryption.KMSKeyARN,
				AWSRegion:    gitSync.Encryption.AWSRegion,
				EncryptedDEK: gitSync.Encryption.EncryptedDEK,
				DEKVersion:   gitSync.Encryption.DEKVersion,
			},
		}
		if !gitSync.LastPushedAt.IsZero() {
			j.LastPushedAt = &gitSync.LastPushedAt
		}
		sj.GitSync = j
	}

	if id := settings.DefaultSessionProfileID(); id != "" {
		sj.DefaultSessionProfileID = id
	}

	return json.Marshal(sj)
}

// fromSecret converts a Kubernetes Secret to Settings entity, decrypting env_vars as needed.
func (r *KubernetesSettingsRepository) fromSecret(ctx context.Context, secret *corev1.Secret) (*entities.Settings, error) {
	data, ok := secret.Data[SecretKeySettings]
	if !ok {
		return nil, fmt.Errorf("secret missing settings data")
	}

	var sj settingsJSON
	if err := json.Unmarshal(data, &sj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// Use the name from JSON; fall back to the Secret label if it is empty
	// (which can happen with settings stored before the name field was populated).
	settingsName := sj.Name
	if settingsName == "" {
		settingsName = secret.Labels[LabelSettingsName]
	}
	settings := entities.NewSettings(settingsName)
	settings.SetCreatedAt(sj.CreatedAt)
	settings.SetUpdatedAt(sj.UpdatedAt)

	if sj.Bedrock != nil {
		bedrock := entities.NewBedrockSettings(sj.Bedrock.Enabled)
		bedrock.SetModel(sj.Bedrock.Model)
		bedrock.SetAccessKeyID(sj.Bedrock.AccessKeyID)
		bedrock.SetSecretAccessKey(sj.Bedrock.SecretAccessKey)
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
			server.SetEnv(serverJSON.Env)
			server.SetHeaders(serverJSON.Headers)

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
		settings.SetClaudeCodeOAuthToken(sj.ClaudeCodeOAuthToken)
		// Reset updatedAt since SetClaudeCodeOAuthToken updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.AuthMode != "" {
		settings.SetAuthMode(entities.AuthMode(sj.AuthMode))
		// Reset updatedAt since SetAuthMode updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	// Load env vars: encrypted_env_vars takes precedence; plain env_vars serves as
	// fallback for legacy data and is merged for keys not present in the encrypted map.
	{
		merged := make(map[string]string)
		// 1. Decrypt encrypted_env_vars
		if len(sj.EncryptedEnvVars) > 0 {
			for k, ev := range sj.EncryptedEnvVars {
				decSvc := r.decryptionSvc(domainservices.EncryptionMetadata{
					Algorithm:   ev.Algorithm,
					KeyID:       ev.KeyID,
					EncryptedAt: ev.EncryptedAt,
					Version:     ev.Version,
				})
				if decSvc == nil {
					log.Printf("[SETTINGS] No decryption service for env var %q (alg=%s kid=%s), skipping", k, ev.Algorithm, ev.KeyID)
					continue
				}
				plaintext, err := decSvc.Decrypt(ctx, &domainservices.EncryptedData{
					EncryptedValue: ev.EncryptedValue,
					Metadata: domainservices.EncryptionMetadata{
						Algorithm:   ev.Algorithm,
						KeyID:       ev.KeyID,
						EncryptedAt: ev.EncryptedAt,
						Version:     ev.Version,
					},
				})
				if err != nil {
					log.Printf("[SETTINGS] Failed to decrypt env var %q: %v, skipping", k, err)
					continue
				}
				merged[k] = plaintext
			}
		}
		// 2. Merge plain env_vars (backward compat; don't overwrite already-decrypted keys)
		for k, v := range sj.EnvVars {
			if _, exists := merged[k]; !exists {
				merged[k] = v
			}
		}
		if len(merged) > 0 {
			settings.SetEnvVars(merged)
			settings.SetUpdatedAt(sj.UpdatedAt)
		}
	}

	if sj.PreferredTeamID != "" {
		settings.SetPreferredTeamID(sj.PreferredTeamID)
		// Reset updatedAt since SetPreferredTeamID updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.SlackUserID != "" {
		settings.SetSlackUserID(sj.SlackUserID)
		// Reset updatedAt since SetSlackUserID updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.NotificationChannels) > 0 {
		settings.SetNotificationChannels(sj.NotificationChannels)
		// Reset updatedAt since SetNotificationChannels updates it
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if len(sj.ExternalSessionManagers) > 0 {
		settings.SetExternalSessionManagers(sj.ExternalSessionManagers)
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.GitSync != nil {
		gs := &entities.GitSyncConfig{
			Enabled:      sj.GitSync.Enabled,
			RepoFullName: sj.GitSync.RepoFullName,
			Branch:       sj.GitSync.Branch,
			RootPath:     sj.GitSync.RootPath,
			AutoPush:     sj.GitSync.AutoPush,
			GitHubToken:  sj.GitSync.GitHubToken,
			Encryption: entities.SyncEncryptionConfig{
				KMSKeyARN:    sj.GitSync.Encryption.KMSKeyARN,
				AWSRegion:    sj.GitSync.Encryption.AWSRegion,
				EncryptedDEK: sj.GitSync.Encryption.EncryptedDEK,
				DEKVersion:   sj.GitSync.Encryption.DEKVersion,
			},
		}
		if sj.GitSync.LastPushedAt != nil {
			gs.LastPushedAt = *sj.GitSync.LastPushedAt
		}
		settings.SetGitSync(gs)
		settings.SetUpdatedAt(sj.UpdatedAt)
	}

	if sj.DefaultSessionProfileID != "" {
		settings.SetDefaultSessionProfileID(sj.DefaultSessionProfileID)
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
