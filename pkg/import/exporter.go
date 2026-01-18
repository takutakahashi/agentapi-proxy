package importexport

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
)

// Exporter handles exporting of team resources
type Exporter struct {
	scheduleManager    schedule.Manager
	webhookRepository  repositories.WebhookRepository
	settingsRepository repositories.SettingsRepository
	encryptionService  services.EncryptionService
}

// NewExporter creates a new Exporter instance
func NewExporter(
	scheduleManager schedule.Manager,
	webhookRepository repositories.WebhookRepository,
	settingsRepository repositories.SettingsRepository,
	encryptionService services.EncryptionService,
) *Exporter {
	return &Exporter{
		scheduleManager:    scheduleManager,
		webhookRepository:  webhookRepository,
		settingsRepository: settingsRepository,
		encryptionService:  encryptionService,
	}
}

// Export exports team resources
func (e *Exporter) Export(ctx context.Context, teamID, userID string, options ExportOptions) (*TeamResources, error) {
	// Log encryption mode
	if e.encryptionService != nil {
		if e.shouldEncrypt() {
			log.Printf("[EXPORT] team=%s: Encrypting secrets with algorithm=%s, keyID=%s",
				teamID, e.encryptionService.Algorithm(), e.encryptionService.KeyID())
		} else {
			log.Printf("[EXPORT] team=%s: WARNING - Using noop encryption, secrets will be exported in plaintext", teamID)
		}
	} else {
		log.Printf("[EXPORT] team=%s: WARNING - No encryption service configured, secrets will not be exported", teamID)
	}

	resources := &TeamResources{
		APIVersion: "agentapi.proxy/v1",
		Kind:       "TeamResources",
		Metadata: ResourceMetadata{
			TeamID: teamID,
		},
		Schedules: []ScheduleImport{},
		Webhooks:  []WebhookImport{},
	}

	// Export schedules (always all)
	schedules, err := e.scheduleManager.List(ctx, schedule.ScheduleFilter{
		UserID: userID,
		Scope:  entities.ScopeTeam,
		TeamID: teamID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list schedules: %w", err)
	}
	for _, s := range schedules {
		scheduleImport := e.convertScheduleToImport(s)
		resources.Schedules = append(resources.Schedules, scheduleImport)
	}

	// Export webhooks (always all, with secrets)
	webhooks, err := e.webhookRepository.List(ctx, repositories.WebhookFilter{
		UserID: userID,
		Scope:  entities.ScopeTeam,
		TeamID: teamID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list webhooks: %w", err)
	}
	for _, w := range webhooks {
		webhookImport, err := e.convertWebhookToImport(ctx, w)
		if err != nil {
			return nil, fmt.Errorf("failed to convert webhook %s: %w", w.Name(), err)
		}
		resources.Webhooks = append(resources.Webhooks, webhookImport)
	}

	// Export settings (always include if exists)
	if e.settingsRepository != nil {
		settings, err := e.settingsRepository.FindByName(ctx, teamID)
		if err != nil {
			if !isNotFoundError(err) {
				return nil, fmt.Errorf("failed to get settings: %w", err)
			}
			// Settings not found, skip
		} else {
			settingsImport, err := e.convertSettingsToImport(ctx, settings)
			if err != nil {
				return nil, fmt.Errorf("failed to convert settings: %w", err)
			}
			resources.Settings = settingsImport
		}
	}

	return resources, nil
}

// shouldEncrypt returns true if secrets should be encrypted
func (e *Exporter) shouldEncrypt() bool {
	if e.encryptionService == nil {
		return false
	}
	return e.encryptionService.Algorithm() != "noop"
}

// isNoopEncryption returns true if using noop encryption
func (e *Exporter) isNoopEncryption() bool {
	return e.encryptionService != nil && e.encryptionService.Algorithm() == "noop"
}

// toEncryptedSecretData converts EncryptedData to EncryptedSecretData
func (e *Exporter) toEncryptedSecretData(encrypted *services.EncryptedData) *EncryptedSecretData {
	return &EncryptedSecretData{
		Algorithm:   encrypted.Metadata.Algorithm,
		KeyID:       encrypted.Metadata.KeyID,
		EncryptedAt: encrypted.Metadata.EncryptedAt,
		Version:     encrypted.Metadata.Version,
	}
}

// isNotFoundError checks if error is a not found error
func isNotFoundError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found")
}

func (e *Exporter) convertScheduleToImport(s *schedule.Schedule) ScheduleImport {
	scheduleImport := ScheduleImport{
		Name:        s.Name,
		Status:      string(s.Status),
		ScheduledAt: s.ScheduledAt,
		CronExpr:    s.CronExpr,
		Timezone:    s.Timezone,
		SessionConfig: SessionConfigImport{
			Environment: s.SessionConfig.Environment,
			Tags:        s.SessionConfig.Tags,
		},
	}

	if s.SessionConfig.Params != nil {
		scheduleImport.SessionConfig.Params = &SessionParamsImport{
			InitialMessage: s.SessionConfig.Params.Message,
		}
	}

	return scheduleImport
}

func (e *Exporter) convertWebhookToImport(ctx context.Context, w *entities.Webhook) (WebhookImport, error) {
	webhookImport := WebhookImport{
		Name:            w.Name(),
		Status:          string(w.Status()),
		WebhookType:     string(w.WebhookType()),
		SignatureHeader: w.SignatureHeader(),
		SignatureType:   string(w.SignatureType()),
		MaxSessions:     w.MaxSessions(),
	}

	// Always include secret (encrypt if encryption service is available)
	if w.Secret() != "" {
		if e.shouldEncrypt() {
			encrypted, err := e.encryptionService.Encrypt(ctx, w.Secret())
			if err != nil {
				return webhookImport, fmt.Errorf("failed to encrypt secret: %w", err)
			}
			webhookImport.Secret = encrypted.EncryptedValue
			webhookImport.SecretEncrypted = e.toEncryptedSecretData(encrypted)
		} else if e.isNoopEncryption() {
			webhookImport.Secret = w.Secret()
		}
	}

	// Convert GitHub config
	if w.GitHub() != nil {
		webhookImport.GitHub = &GitHubConfigImport{
			EnterpriseURL:       w.GitHub().EnterpriseURL(),
			AllowedEvents:       w.GitHub().AllowedEvents(),
			AllowedRepositories: w.GitHub().AllowedRepositories(),
		}
	}

	// Convert triggers
	webhookImport.Triggers = make([]WebhookTriggerImport, 0, len(w.Triggers()))
	for _, trigger := range w.Triggers() {
		triggerImport, err := e.convertTriggerToImport(ctx, trigger)
		if err != nil {
			return webhookImport, fmt.Errorf("failed to convert trigger %s: %w", trigger.Name(), err)
		}
		webhookImport.Triggers = append(webhookImport.Triggers, triggerImport)
	}

	// Convert session config
	if w.SessionConfig() != nil {
		sessionConfig, err := e.convertWebhookSessionConfigToImport(ctx, w.SessionConfig())
		if err != nil {
			return webhookImport, fmt.Errorf("failed to convert session config: %w", err)
		}
		webhookImport.SessionConfig = &sessionConfig
	}

	return webhookImport, nil
}

func (e *Exporter) convertTriggerToImport(ctx context.Context, trigger entities.WebhookTrigger) (WebhookTriggerImport, error) {
	triggerImport := WebhookTriggerImport{
		Name:        trigger.Name(),
		Priority:    trigger.Priority(),
		Enabled:     trigger.Enabled(),
		StopOnMatch: trigger.StopOnMatch(),
	}

	// Convert conditions
	conditions := trigger.Conditions()

	if conditions.GitHub() != nil {
		triggerImport.Conditions.GitHub = &GitHubConditionsImport{
			Events:       conditions.GitHub().Events(),
			Actions:      conditions.GitHub().Actions(),
			Branches:     conditions.GitHub().Branches(),
			Repositories: conditions.GitHub().Repositories(),
			Labels:       conditions.GitHub().Labels(),
			Paths:        conditions.GitHub().Paths(),
			BaseBranches: conditions.GitHub().BaseBranches(),
			Draft:        conditions.GitHub().Draft(),
			Sender:       conditions.GitHub().Sender(),
		}
	}

	if len(conditions.JSONPath()) > 0 {
		triggerImport.Conditions.JSONPath = make([]JSONPathConditionImport, 0, len(conditions.JSONPath()))
		for _, jp := range conditions.JSONPath() {
			triggerImport.Conditions.JSONPath = append(triggerImport.Conditions.JSONPath, JSONPathConditionImport{
				Path:     jp.Path(),
				Operator: string(jp.Operator()),
				Value:    jp.Value(),
			})
		}
	}

	if conditions.GoTemplate() != "" {
		triggerImport.Conditions.GoTemplate = conditions.GoTemplate()
	}

	// Convert session config
	if trigger.SessionConfig() != nil {
		sessionConfig, err := e.convertWebhookSessionConfigToImport(ctx, trigger.SessionConfig())
		if err != nil {
			return triggerImport, fmt.Errorf("failed to convert trigger session config: %w", err)
		}
		triggerImport.SessionConfig = &sessionConfig
	}

	return triggerImport, nil
}

func (e *Exporter) convertWebhookSessionConfigToImport(ctx context.Context, config *entities.WebhookSessionConfig) (SessionConfigImport, error) {
	sessionConfig := SessionConfigImport{
		Environment: config.Environment(),
		Tags:        config.Tags(),
	}

	if config.Params() != nil || config.InitialMessageTemplate() != "" {
		sessionConfig.Params = &SessionParamsImport{}
		if config.InitialMessageTemplate() != "" {
			sessionConfig.Params.InitialMessageTemplate = config.InitialMessageTemplate()
		}

		// Encrypt GitHub token if present
		if config.Params() != nil && config.Params().GithubToken() != "" {
			token := config.Params().GithubToken()
			if e.shouldEncrypt() {
				encrypted, err := e.encryptionService.Encrypt(ctx, token)
				if err != nil {
					return sessionConfig, fmt.Errorf("failed to encrypt github token: %w", err)
				}
				sessionConfig.Params.GitHubToken = encrypted.EncryptedValue
				sessionConfig.Params.GitHubTokenEncrypted = e.toEncryptedSecretData(encrypted)
			} else if e.isNoopEncryption() {
				sessionConfig.Params.GitHubToken = token
			}
		}
	}

	return sessionConfig, nil
}

// convertSettingsToImport converts Settings entity to SettingsImport
func (e *Exporter) convertSettingsToImport(ctx context.Context, s *entities.Settings) (*SettingsImport, error) {
	settingsImport := &SettingsImport{
		Name:           s.Name(),
		AuthMode:       string(s.AuthMode()),
		EnabledPlugins: s.EnabledPlugins(),
	}

	// Bedrock settings
	if bedrock := s.Bedrock(); bedrock != nil {
		bedrockImport := &BedrockSettingsImport{
			Enabled: bedrock.Enabled(),
			Model:   bedrock.Model(),
			RoleARN: bedrock.RoleARN(),
			Profile: bedrock.Profile(),
		}

		// Encrypt AccessKeyID
		if bedrock.AccessKeyID() != "" {
			if e.shouldEncrypt() {
				encrypted, err := e.encryptionService.Encrypt(ctx, bedrock.AccessKeyID())
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt access_key_id: %w", err)
				}
				bedrockImport.AccessKeyID = encrypted.EncryptedValue
				bedrockImport.AccessKeyIDEncrypted = e.toEncryptedSecretData(encrypted)
			} else if e.isNoopEncryption() {
				bedrockImport.AccessKeyID = bedrock.AccessKeyID()
			}
		}

		// Encrypt SecretAccessKey
		if bedrock.SecretAccessKey() != "" {
			if e.shouldEncrypt() {
				encrypted, err := e.encryptionService.Encrypt(ctx, bedrock.SecretAccessKey())
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt secret_access_key: %w", err)
				}
				bedrockImport.SecretAccessKey = encrypted.EncryptedValue
				bedrockImport.SecretAccessKeyEncrypted = e.toEncryptedSecretData(encrypted)
			} else if e.isNoopEncryption() {
				bedrockImport.SecretAccessKey = bedrock.SecretAccessKey()
			}
		}

		settingsImport.Bedrock = bedrockImport
	}

	// MCP Servers
	if mcpServers := s.MCPServers(); mcpServers != nil && !mcpServers.IsEmpty() {
		settingsImport.MCPServers = make(map[string]*MCPServerImport)
		for name, server := range mcpServers.Servers() {
			serverImport, err := e.convertMCPServerToImport(ctx, server)
			if err != nil {
				return nil, fmt.Errorf("failed to convert MCP server %s: %w", name, err)
			}
			settingsImport.MCPServers[name] = serverImport
		}
	}

	// Marketplaces
	if marketplaces := s.Marketplaces(); marketplaces != nil && !marketplaces.IsEmpty() {
		settingsImport.Marketplaces = make(map[string]*MarketplaceImport)
		for name, marketplace := range marketplaces.Marketplaces() {
			settingsImport.Marketplaces[name] = &MarketplaceImport{
				URL: marketplace.URL(),
			}
		}
	}

	// Encrypt ClaudeCodeOAuthToken
	if s.ClaudeCodeOAuthToken() != "" {
		if e.shouldEncrypt() {
			encrypted, err := e.encryptionService.Encrypt(ctx, s.ClaudeCodeOAuthToken())
			if err != nil {
				return nil, fmt.Errorf("failed to encrypt oauth token: %w", err)
			}
			settingsImport.ClaudeCodeOAuthToken = encrypted.EncryptedValue
			settingsImport.ClaudeCodeOAuthTokenEncrypted = e.toEncryptedSecretData(encrypted)
		} else if e.isNoopEncryption() {
			settingsImport.ClaudeCodeOAuthToken = s.ClaudeCodeOAuthToken()
		}
	}

	return settingsImport, nil
}

// convertMCPServerToImport converts MCPServer entity to MCPServerImport
func (e *Exporter) convertMCPServerToImport(ctx context.Context, server *entities.MCPServer) (*MCPServerImport, error) {
	serverImport := &MCPServerImport{
		Type:    server.Type(),
		URL:     server.URL(),
		Command: server.Command(),
		Args:    server.Args(),
	}

	// Encrypt Env (each value individually)
	if len(server.Env()) > 0 {
		serverImport.Env = make(map[string]string)
		if e.shouldEncrypt() {
			serverImport.EnvEncrypted = make(map[string]*EncryptedSecretData)
			for k, v := range server.Env() {
				encrypted, err := e.encryptionService.Encrypt(ctx, v)
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt env %s: %w", k, err)
				}
				serverImport.Env[k] = encrypted.EncryptedValue
				serverImport.EnvEncrypted[k] = e.toEncryptedSecretData(encrypted)
			}
		} else if e.isNoopEncryption() {
			serverImport.Env = server.Env()
		}
	}

	// Encrypt Headers (each value individually)
	if len(server.Headers()) > 0 {
		serverImport.Headers = make(map[string]string)
		if e.shouldEncrypt() {
			serverImport.HeadersEncrypted = make(map[string]*EncryptedSecretData)
			for k, v := range server.Headers() {
				encrypted, err := e.encryptionService.Encrypt(ctx, v)
				if err != nil {
					return nil, fmt.Errorf("failed to encrypt header %s: %w", k, err)
				}
				serverImport.Headers[k] = encrypted.EncryptedValue
				serverImport.HeadersEncrypted[k] = e.toEncryptedSecretData(encrypted)
			}
		} else if e.isNoopEncryption() {
			serverImport.Headers = server.Headers()
		}
	}

	return serverImport, nil
}
