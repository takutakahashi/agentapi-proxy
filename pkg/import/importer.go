package importexport

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
)

// Importer handles importing of team resources
type Importer struct {
	scheduleManager    schedule.Manager
	webhookRepository  repositories.WebhookRepository
	settingsRepository repositories.SettingsRepository
	encryptionService  services.EncryptionService
	validator          *Validator
}

// NewImporter creates a new Importer instance
func NewImporter(
	scheduleManager schedule.Manager,
	webhookRepository repositories.WebhookRepository,
	settingsRepository repositories.SettingsRepository,
	encryptionService services.EncryptionService,
) *Importer {
	return &Importer{
		scheduleManager:    scheduleManager,
		webhookRepository:  webhookRepository,
		settingsRepository: settingsRepository,
		encryptionService:  encryptionService,
		validator:          NewValidator(),
	}
}

// Import imports team resources
func (i *Importer) Import(ctx context.Context, resources *TeamResources, userID string, options ImportOptions) (*ImportResult, error) {
	result := &ImportResult{
		Success: true,
		Summary: ImportSummary{},
		Details: []ImportDetail{},
		Errors:  []string{},
	}

	// Validate the resources first
	if err := i.validator.Validate(resources); err != nil {
		// In dry-run mode, return validation errors in the result instead of failing immediately
		if options.DryRun {
			result.Success = false
			result.Errors = append(result.Errors, fmt.Sprintf("Validation failed: %v", err))
			return result, nil
		}
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Import schedules
	for _, scheduleImport := range resources.Schedules {
		detail := i.importSchedule(ctx, scheduleImport, resources.Metadata.TeamID, userID, options)
		result.Details = append(result.Details, detail)

		switch detail.Action {
		case "created":
			result.Summary.Schedules.Created++
		case "updated":
			result.Summary.Schedules.Updated++
		case "skipped":
			result.Summary.Schedules.Skipped++
		case "failed":
			result.Summary.Schedules.Failed++
			result.Errors = append(result.Errors, detail.Error)
			if !options.AllowPartial {
				result.Success = false
				return result, nil
			}
		}
	}

	// Import webhooks
	for _, webhookImport := range resources.Webhooks {
		detail := i.importWebhook(ctx, webhookImport, resources.Metadata.TeamID, userID, options)
		result.Details = append(result.Details, detail)

		switch detail.Action {
		case "created":
			result.Summary.Webhooks.Created++
		case "updated":
			result.Summary.Webhooks.Updated++
		case "skipped":
			result.Summary.Webhooks.Skipped++
		case "failed":
			result.Summary.Webhooks.Failed++
			result.Errors = append(result.Errors, detail.Error)
			if !options.AllowPartial {
				result.Success = false
				return result, nil
			}
		}
	}

	// Import settings
	if resources.Settings != nil {
		detail := i.importSettings(ctx, *resources.Settings, resources.Metadata.TeamID, userID, options)
		result.Details = append(result.Details, detail)

		switch detail.Action {
		case "created":
			result.Summary.Settings.Created++
		case "updated":
			result.Summary.Settings.Updated++
		case "skipped":
			result.Summary.Settings.Skipped++
		case "failed":
			result.Summary.Settings.Failed++
			result.Errors = append(result.Errors, detail.Error)
			if !options.AllowPartial {
				result.Success = false
				return result, nil
			}
		}
	}

	// Check if any failures occurred
	if result.Summary.Schedules.Failed > 0 || result.Summary.Webhooks.Failed > 0 || result.Summary.Settings.Failed > 0 {
		result.Success = false
	}

	return result, nil
}

func (i *Importer) importSchedule(ctx context.Context, scheduleImport ScheduleImport, teamID, userID string, options ImportOptions) ImportDetail {
	detail := ImportDetail{
		ResourceType: "schedule",
		ResourceName: scheduleImport.Name,
		Status:       "success",
	}

	// Find existing schedule by ID (preferred) or name (fallback) if mode is update or upsert
	var existingSchedule *schedule.Schedule
	if options.Mode == ImportModeUpdate || options.Mode == ImportModeUpsert {
		schedules, err := i.scheduleManager.List(ctx, schedule.ScheduleFilter{
			UserID: userID,
			Scope:  entities.ScopeTeam,
			TeamID: teamID,
		})
		if err == nil {
			// First, try to match by ID if provided
			if scheduleImport.ID != "" {
				for _, s := range schedules {
					if s.ID == scheduleImport.ID {
						existingSchedule = s
						break
					}
				}
			}
			// Fallback to name matching if ID not found or not provided
			if existingSchedule == nil {
				for _, s := range schedules {
					if s.Name == scheduleImport.Name {
						existingSchedule = s
						break
					}
				}
			}
		}
	}

	// Determine action based on mode and existence
	var action string
	switch options.Mode {
	case ImportModeCreate:
		if existingSchedule != nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = fmt.Sprintf("schedule with name %q already exists", scheduleImport.Name)
			return detail
		}
		action = "create"
	case ImportModeUpdate:
		if existingSchedule == nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = fmt.Sprintf("schedule with name %q does not exist", scheduleImport.Name)
			return detail
		}
		action = "update"
	case ImportModeUpsert:
		if existingSchedule != nil {
			action = "update"
		} else {
			action = "create"
		}
	}

	// Convert import to schedule entity
	scheduleEntity, err := i.convertScheduleImport(scheduleImport, teamID, userID, existingSchedule)
	if err != nil {
		detail.Action = "failed"
		detail.Status = "error"
		detail.Error = err.Error()
		return detail
	}

	// Dry run - don't actually create/update, but generate diff
	if options.DryRun {
		detail.Action = action + "d (dry-run)"
		if existingSchedule != nil {
			detail.ID = existingSchedule.ID
			// Generate diff for update
			diff, err := generateDiff(existingSchedule, scheduleEntity, scheduleImport.Name)
			if err == nil && diff != nil {
				detail.Diff = diff
			}
		} else {
			// For create, show the new resource as diff
			diff, err := generateDiff(nil, scheduleEntity, scheduleImport.Name)
			if err == nil && diff != nil {
				detail.Diff = diff
			}
		}
		return detail
	}

	// Create or update
	if action == "create" {
		if err := i.scheduleManager.Create(ctx, scheduleEntity); err != nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = err.Error()
			return detail
		}
		detail.Action = "created"
	} else {
		if err := i.scheduleManager.Update(ctx, scheduleEntity); err != nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = err.Error()
			return detail
		}
		detail.Action = "updated"
	}

	detail.ID = scheduleEntity.ID
	return detail
}

func (i *Importer) importWebhook(ctx context.Context, webhookImport WebhookImport, teamID, userID string, options ImportOptions) ImportDetail {
	detail := ImportDetail{
		ResourceType: "webhook",
		ResourceName: webhookImport.Name,
		Status:       "success",
	}

	// Find existing webhook by ID (preferred) or name (fallback) if mode is update or upsert
	var existingWebhook *entities.Webhook
	if options.Mode == ImportModeUpdate || options.Mode == ImportModeUpsert {
		webhooks, err := i.webhookRepository.List(ctx, repositories.WebhookFilter{
			UserID: userID,
			Scope:  entities.ScopeTeam,
			TeamID: teamID,
		})
		if err == nil {
			// First, try to match by ID if provided
			if webhookImport.ID != "" {
				for _, w := range webhooks {
					if w.ID() == webhookImport.ID {
						existingWebhook = w
						break
					}
				}
			}
			// Fallback to name matching if ID not found or not provided
			if existingWebhook == nil {
				for _, w := range webhooks {
					if w.Name() == webhookImport.Name {
						existingWebhook = w
						break
					}
				}
			}
		}
	}

	// Determine action based on mode and existence
	var action string
	switch options.Mode {
	case ImportModeCreate:
		if existingWebhook != nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = fmt.Sprintf("webhook with name %q already exists", webhookImport.Name)
			return detail
		}
		action = "create"
	case ImportModeUpdate:
		if existingWebhook == nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = fmt.Sprintf("webhook with name %q does not exist", webhookImport.Name)
			return detail
		}
		action = "update"
	case ImportModeUpsert:
		if existingWebhook != nil {
			action = "update"
		} else {
			action = "create"
		}
	}

	// Convert import to webhook entity
	webhookEntity, err := i.convertWebhookImport(ctx, webhookImport, teamID, userID, existingWebhook, options)
	if err != nil {
		detail.Action = "failed"
		detail.Status = "error"
		detail.Error = err.Error()
		return detail
	}

	// Dry run - don't actually create/update, but generate diff
	if options.DryRun {
		detail.Action = action + "d (dry-run)"
		if existingWebhook != nil {
			detail.ID = existingWebhook.ID()
			// Generate diff for update
			diff, err := generateDiff(existingWebhook, webhookEntity, webhookImport.Name)
			if err == nil && diff != nil {
				detail.Diff = diff
			}
		} else {
			// For create, show the new resource as diff
			diff, err := generateDiff(nil, webhookEntity, webhookImport.Name)
			if err == nil && diff != nil {
				detail.Diff = diff
			}
		}
		return detail
	}

	// Create or update
	if action == "create" {
		if err := i.webhookRepository.Create(ctx, webhookEntity); err != nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = err.Error()
			return detail
		}
		detail.Action = "created"
	} else {
		if err := i.webhookRepository.Update(ctx, webhookEntity); err != nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = err.Error()
			return detail
		}
		detail.Action = "updated"
	}

	detail.ID = webhookEntity.ID()
	return detail
}

func (i *Importer) convertScheduleImport(scheduleImport ScheduleImport, teamID, userID string, existing *schedule.Schedule) (*schedule.Schedule, error) {
	var scheduleEntity *schedule.Schedule
	if existing != nil {
		scheduleEntity = existing
	} else {
		scheduleEntity = &schedule.Schedule{
			ID:        uuid.New().String(),
			CreatedAt: time.Now(),
		}
	}

	scheduleEntity.Name = scheduleImport.Name
	scheduleEntity.UserID = userID
	scheduleEntity.Scope = entities.ScopeTeam
	scheduleEntity.TeamID = teamID
	scheduleEntity.UpdatedAt = time.Now()

	// Set status (default to active if not specified)
	if scheduleImport.Status != "" {
		scheduleEntity.Status = schedule.ScheduleStatus(scheduleImport.Status)
	} else {
		scheduleEntity.Status = schedule.ScheduleStatusActive
	}

	// Set schedule timing
	scheduleEntity.ScheduledAt = scheduleImport.ScheduledAt
	scheduleEntity.CronExpr = scheduleImport.CronExpr

	// Set timezone (default to UTC if not specified)
	if scheduleImport.Timezone != "" {
		scheduleEntity.Timezone = scheduleImport.Timezone
	} else {
		scheduleEntity.Timezone = "UTC"
	}

	// Convert session config
	scheduleEntity.SessionConfig = schedule.SessionConfig{
		Environment: scheduleImport.SessionConfig.Environment,
		Tags:        scheduleImport.SessionConfig.Tags,
	}

	if scheduleImport.SessionConfig.Params != nil {
		scheduleEntity.SessionConfig.Params = &entities.SessionParams{}
		if scheduleImport.SessionConfig.Params.InitialMessage != "" {
			scheduleEntity.SessionConfig.Params.Message = scheduleImport.SessionConfig.Params.InitialMessage
		}
	}

	return scheduleEntity, nil
}

func (i *Importer) convertWebhookImport(ctx context.Context, webhookImport WebhookImport, teamID, userID string, existing *entities.Webhook, options ImportOptions) (*entities.Webhook, error) {
	var webhookEntity *entities.Webhook
	if existing != nil {
		webhookEntity = existing
	} else {
		webhookEntity = entities.NewWebhook(
			uuid.New().String(),
			webhookImport.Name,
			userID,
			entities.WebhookType(webhookImport.WebhookType),
		)
	}

	webhookEntity.SetName(webhookImport.Name)
	webhookEntity.SetScope(entities.ScopeTeam)
	webhookEntity.SetTeamID(teamID)

	// Set status
	if webhookImport.Status != "" {
		webhookEntity.SetStatus(entities.WebhookStatus(webhookImport.Status))
	}

	// Set secret (with decryption if encrypted)
	if webhookImport.SecretEncrypted != nil {
		// Encrypted secret - decrypt it
		if i.encryptionService == nil {
			return nil, fmt.Errorf("encrypted secret found but encryption service not configured")
		}

		encrypted := &services.EncryptedData{
			EncryptedValue: webhookImport.Secret,
			Metadata: services.EncryptionMetadata{
				Algorithm:   webhookImport.SecretEncrypted.Algorithm,
				KeyID:       webhookImport.SecretEncrypted.KeyID,
				EncryptedAt: webhookImport.SecretEncrypted.EncryptedAt,
				Version:     webhookImport.SecretEncrypted.Version,
			},
		}

		plaintext, err := i.encryptionService.Decrypt(ctx, encrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt secret: %w", err)
		}
		webhookEntity.SetSecret(plaintext)
	} else if options.RegenerateAll || (webhookImport.Secret == "" && existing == nil) {
		// Generate new secret
		secret, err := generateSecret(32)
		if err != nil {
			return nil, fmt.Errorf("failed to generate secret: %w", err)
		}
		webhookEntity.SetSecret(secret)
	} else if webhookImport.Secret != "" {
		// Plain text secret
		webhookEntity.SetSecret(webhookImport.Secret)
	}

	// Set signature configuration
	if webhookImport.SignatureHeader != "" {
		webhookEntity.SetSignatureHeader(webhookImport.SignatureHeader)
	}
	if webhookImport.SignatureType != "" {
		webhookEntity.SetSignatureType(entities.WebhookSignatureType(webhookImport.SignatureType))
	}

	// Set max sessions
	if webhookImport.MaxSessions > 0 {
		webhookEntity.SetMaxSessions(webhookImport.MaxSessions)
	}

	// Set GitHub config
	if webhookImport.GitHub != nil {
		githubConfig := entities.NewWebhookGitHubConfig()
		if webhookImport.GitHub.EnterpriseURL != "" {
			githubConfig.SetEnterpriseURL(webhookImport.GitHub.EnterpriseURL)
		}
		if len(webhookImport.GitHub.AllowedEvents) > 0 {
			githubConfig.SetAllowedEvents(webhookImport.GitHub.AllowedEvents)
		}
		if len(webhookImport.GitHub.AllowedRepositories) > 0 {
			githubConfig.SetAllowedRepositories(webhookImport.GitHub.AllowedRepositories)
		}
		webhookEntity.SetGitHub(githubConfig)
	}

	// Convert triggers
	triggers := make([]entities.WebhookTrigger, 0, len(webhookImport.Triggers))
	for _, triggerImport := range webhookImport.Triggers {
		trigger, err := i.convertTriggerImport(triggerImport)
		if err != nil {
			return nil, fmt.Errorf("failed to convert trigger %q: %w", triggerImport.Name, err)
		}
		triggers = append(triggers, trigger)
	}
	webhookEntity.SetTriggers(triggers)

	// Set session config
	if webhookImport.SessionConfig != nil {
		sessionConfig := i.convertWebhookSessionConfig(*webhookImport.SessionConfig)
		webhookEntity.SetSessionConfig(sessionConfig)
	}

	return webhookEntity, nil
}

func (i *Importer) convertTriggerImport(triggerImport WebhookTriggerImport) (entities.WebhookTrigger, error) {
	trigger := entities.NewWebhookTrigger(uuid.New().String(), triggerImport.Name)
	trigger.SetPriority(triggerImport.Priority)
	trigger.SetEnabled(triggerImport.Enabled)
	trigger.SetStopOnMatch(triggerImport.StopOnMatch)

	// Convert conditions
	var conditions entities.WebhookTriggerConditions

	if triggerImport.Conditions.GitHub != nil {
		githubConditions := entities.NewWebhookGitHubConditions()
		githubConditions.SetEvents(triggerImport.Conditions.GitHub.Events)
		githubConditions.SetActions(triggerImport.Conditions.GitHub.Actions)
		githubConditions.SetBranches(triggerImport.Conditions.GitHub.Branches)
		githubConditions.SetRepositories(triggerImport.Conditions.GitHub.Repositories)
		githubConditions.SetLabels(triggerImport.Conditions.GitHub.Labels)
		githubConditions.SetPaths(triggerImport.Conditions.GitHub.Paths)
		githubConditions.SetBaseBranches(triggerImport.Conditions.GitHub.BaseBranches)
		githubConditions.SetDraft(triggerImport.Conditions.GitHub.Draft)
		githubConditions.SetSender(triggerImport.Conditions.GitHub.Sender)
		conditions.SetGitHub(githubConditions)
	}

	if len(triggerImport.Conditions.JSONPath) > 0 {
		jsonPathConditions := make([]entities.WebhookJSONPathCondition, 0, len(triggerImport.Conditions.JSONPath))
		for _, jp := range triggerImport.Conditions.JSONPath {
			jsonPathConditions = append(jsonPathConditions, entities.NewWebhookJSONPathCondition(
				jp.Path,
				jp.Operator,
				jp.Value,
			))
		}
		conditions.SetJSONPath(jsonPathConditions)
	}

	if triggerImport.Conditions.GoTemplate != "" {
		conditions.SetGoTemplate(triggerImport.Conditions.GoTemplate)
	}

	trigger.SetConditions(conditions)

	// Set session config
	if triggerImport.SessionConfig != nil {
		sessionConfig := i.convertWebhookSessionConfig(*triggerImport.SessionConfig)
		trigger.SetSessionConfig(sessionConfig)
	}

	return trigger, nil
}

func (i *Importer) convertWebhookSessionConfig(configImport SessionConfigImport) *entities.WebhookSessionConfig {
	config := entities.NewWebhookSessionConfig()

	if configImport.Environment != nil {
		config.SetEnvironment(configImport.Environment)
	}
	if configImport.Tags != nil {
		config.SetTags(configImport.Tags)
	}

	if configImport.Params != nil {
		params := entities.NewWebhookSessionParams()
		if configImport.Params.GitHubToken != "" {
			params.SetGithubToken(configImport.Params.GitHubToken)
		}
		config.SetParams(params)

		if configImport.Params.InitialMessageTemplate != "" {
			config.SetInitialMessageTemplate(configImport.Params.InitialMessageTemplate)
		}
	}

	return config
}

// generateSecret generates a random secret of the specified length
func generateSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func (i *Importer) importSettings(ctx context.Context, settingsImport SettingsImport, teamID, userID string, options ImportOptions) ImportDetail {
	detail := ImportDetail{
		ResourceType: "settings",
		ResourceName: settingsImport.Name,
		Status:       "success",
	}

	// Settings repository must be available
	if i.settingsRepository == nil {
		detail.Action = "failed"
		detail.Status = "error"
		detail.Error = "settings repository not available"
		return detail
	}

	// Find existing settings
	var existingSettings *entities.Settings
	existing, err := i.settingsRepository.FindByName(ctx, teamID)
	if err == nil {
		existingSettings = existing
	}

	// Determine action based on mode and existence
	var action string
	switch options.Mode {
	case ImportModeCreate:
		if existingSettings != nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = fmt.Sprintf("settings with name %q already exists", settingsImport.Name)
			return detail
		}
		action = "create"
	case ImportModeUpdate:
		if existingSettings == nil {
			detail.Action = "failed"
			detail.Status = "error"
			detail.Error = fmt.Sprintf("settings with name %q does not exist", settingsImport.Name)
			return detail
		}
		action = "update"
	case ImportModeUpsert:
		if existingSettings != nil {
			action = "update"
		} else {
			action = "create"
		}
	}

	// Convert import to settings entity
	settingsEntity, err := i.convertSettingsImport(ctx, settingsImport, existingSettings)
	if err != nil {
		detail.Action = "failed"
		detail.Status = "error"
		detail.Error = err.Error()
		return detail
	}

	// Dry run - don't actually create/update, but generate diff
	if options.DryRun {
		detail.Action = action + "d (dry-run)"
		if existingSettings != nil {
			// Generate diff for update
			diff, err := generateDiff(existingSettings, settingsEntity, settingsImport.Name)
			if err == nil && diff != nil {
				detail.Diff = diff
			}
		} else {
			// For create, show the new resource as diff
			diff, err := generateDiff(nil, settingsEntity, settingsImport.Name)
			if err == nil && diff != nil {
				detail.Diff = diff
			}
		}
		return detail
	}

	// Save settings
	if err := i.settingsRepository.Save(ctx, settingsEntity); err != nil {
		detail.Action = "failed"
		detail.Status = "error"
		detail.Error = err.Error()
		return detail
	}

	detail.Action = action + "d"
	return detail
}

func (i *Importer) convertSettingsImport(ctx context.Context, settingsImport SettingsImport, existing *entities.Settings) (*entities.Settings, error) {
	var settingsEntity *entities.Settings
	if existing != nil {
		settingsEntity = existing
	} else {
		settingsEntity = entities.NewSettings(settingsImport.Name)
	}

	// Bedrock settings
	if settingsImport.Bedrock != nil {
		bedrock := entities.NewBedrockSettings(settingsImport.Bedrock.Enabled)
		bedrock.SetModel(settingsImport.Bedrock.Model)
		bedrock.SetRoleARN(settingsImport.Bedrock.RoleARN)
		bedrock.SetProfile(settingsImport.Bedrock.Profile)

		// Decrypt AccessKeyID
		if settingsImport.Bedrock.AccessKeyIDEncrypted != nil {
			if i.encryptionService == nil {
				return nil, fmt.Errorf("encrypted access_key_id found but encryption service not configured")
			}
			encrypted := &services.EncryptedData{
				EncryptedValue: settingsImport.Bedrock.AccessKeyID,
				Metadata: services.EncryptionMetadata{
					Algorithm:   settingsImport.Bedrock.AccessKeyIDEncrypted.Algorithm,
					KeyID:       settingsImport.Bedrock.AccessKeyIDEncrypted.KeyID,
					EncryptedAt: settingsImport.Bedrock.AccessKeyIDEncrypted.EncryptedAt,
					Version:     settingsImport.Bedrock.AccessKeyIDEncrypted.Version,
				},
			}
			plaintext, err := i.encryptionService.Decrypt(ctx, encrypted)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt access_key_id: %w", err)
			}
			bedrock.SetAccessKeyID(plaintext)
		} else if settingsImport.Bedrock.AccessKeyID != "" {
			bedrock.SetAccessKeyID(settingsImport.Bedrock.AccessKeyID)
		}

		// Decrypt SecretAccessKey
		if settingsImport.Bedrock.SecretAccessKeyEncrypted != nil {
			if i.encryptionService == nil {
				return nil, fmt.Errorf("encrypted secret_access_key found but encryption service not configured")
			}
			encrypted := &services.EncryptedData{
				EncryptedValue: settingsImport.Bedrock.SecretAccessKey,
				Metadata: services.EncryptionMetadata{
					Algorithm:   settingsImport.Bedrock.SecretAccessKeyEncrypted.Algorithm,
					KeyID:       settingsImport.Bedrock.SecretAccessKeyEncrypted.KeyID,
					EncryptedAt: settingsImport.Bedrock.SecretAccessKeyEncrypted.EncryptedAt,
					Version:     settingsImport.Bedrock.SecretAccessKeyEncrypted.Version,
				},
			}
			plaintext, err := i.encryptionService.Decrypt(ctx, encrypted)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt secret_access_key: %w", err)
			}
			bedrock.SetSecretAccessKey(plaintext)
		} else if settingsImport.Bedrock.SecretAccessKey != "" {
			bedrock.SetSecretAccessKey(settingsImport.Bedrock.SecretAccessKey)
		}

		settingsEntity.SetBedrock(bedrock)
	}

	// MCP Servers
	if settingsImport.MCPServers != nil {
		mcpServers := entities.NewMCPServersSettings()
		for name, serverImport := range settingsImport.MCPServers {
			server, err := i.convertMCPServerImport(ctx, name, serverImport)
			if err != nil {
				return nil, fmt.Errorf("failed to convert MCP server %s: %w", name, err)
			}
			mcpServers.SetServer(name, server)
		}
		settingsEntity.SetMCPServers(mcpServers)
	}

	// Marketplaces
	if settingsImport.Marketplaces != nil {
		marketplaces := entities.NewMarketplacesSettings()
		for name, marketplaceImport := range settingsImport.Marketplaces {
			marketplace := entities.NewMarketplace(name)
			marketplace.SetURL(marketplaceImport.URL)
			marketplaces.SetMarketplace(name, marketplace)
		}
		settingsEntity.SetMarketplaces(marketplaces)
	}

	// Decrypt ClaudeCodeOAuthToken
	if settingsImport.ClaudeCodeOAuthTokenEncrypted != nil {
		if i.encryptionService == nil {
			return nil, fmt.Errorf("encrypted oauth token found but encryption service not configured")
		}
		encrypted := &services.EncryptedData{
			EncryptedValue: settingsImport.ClaudeCodeOAuthToken,
			Metadata: services.EncryptionMetadata{
				Algorithm:   settingsImport.ClaudeCodeOAuthTokenEncrypted.Algorithm,
				KeyID:       settingsImport.ClaudeCodeOAuthTokenEncrypted.KeyID,
				EncryptedAt: settingsImport.ClaudeCodeOAuthTokenEncrypted.EncryptedAt,
				Version:     settingsImport.ClaudeCodeOAuthTokenEncrypted.Version,
			},
		}
		plaintext, err := i.encryptionService.Decrypt(ctx, encrypted)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt oauth token: %w", err)
		}
		settingsEntity.SetClaudeCodeOAuthToken(plaintext)
	} else if settingsImport.ClaudeCodeOAuthToken != "" {
		settingsEntity.SetClaudeCodeOAuthToken(settingsImport.ClaudeCodeOAuthToken)
	}

	// Auth mode
	if settingsImport.AuthMode != "" {
		settingsEntity.SetAuthMode(entities.AuthMode(settingsImport.AuthMode))
	}

	// Enabled plugins
	if settingsImport.EnabledPlugins != nil {
		settingsEntity.SetEnabledPlugins(settingsImport.EnabledPlugins)
	}

	return settingsEntity, nil
}

func (i *Importer) convertMCPServerImport(ctx context.Context, name string, serverImport *MCPServerImport) (*entities.MCPServer, error) {
	server := entities.NewMCPServer(name, serverImport.Type)
	server.SetURL(serverImport.URL)
	server.SetCommand(serverImport.Command)
	server.SetArgs(serverImport.Args)

	// Decrypt Env (each value individually)
	if len(serverImport.Env) > 0 {
		env := make(map[string]string)
		for k, v := range serverImport.Env {
			if serverImport.EnvEncrypted != nil && serverImport.EnvEncrypted[k] != nil {
				// Encrypted value - decrypt it
				if i.encryptionService == nil {
					return nil, fmt.Errorf("encrypted env %s found but encryption service not configured", k)
				}
				encrypted := &services.EncryptedData{
					EncryptedValue: v,
					Metadata: services.EncryptionMetadata{
						Algorithm:   serverImport.EnvEncrypted[k].Algorithm,
						KeyID:       serverImport.EnvEncrypted[k].KeyID,
						EncryptedAt: serverImport.EnvEncrypted[k].EncryptedAt,
						Version:     serverImport.EnvEncrypted[k].Version,
					},
				}
				plaintext, err := i.encryptionService.Decrypt(ctx, encrypted)
				if err != nil {
					return nil, fmt.Errorf("failed to decrypt env %s: %w", k, err)
				}
				env[k] = plaintext
			} else {
				// Plain text value
				env[k] = v
			}
		}
		server.SetEnv(env)
	}

	// Decrypt Headers (each value individually)
	if len(serverImport.Headers) > 0 {
		headers := make(map[string]string)
		for k, v := range serverImport.Headers {
			if serverImport.HeadersEncrypted != nil && serverImport.HeadersEncrypted[k] != nil {
				// Encrypted value - decrypt it
				if i.encryptionService == nil {
					return nil, fmt.Errorf("encrypted header %s found but encryption service not configured", k)
				}
				encrypted := &services.EncryptedData{
					EncryptedValue: v,
					Metadata: services.EncryptionMetadata{
						Algorithm:   serverImport.HeadersEncrypted[k].Algorithm,
						KeyID:       serverImport.HeadersEncrypted[k].KeyID,
						EncryptedAt: serverImport.HeadersEncrypted[k].EncryptedAt,
						Version:     serverImport.HeadersEncrypted[k].Version,
					},
				}
				plaintext, err := i.encryptionService.Decrypt(ctx, encrypted)
				if err != nil {
					return nil, fmt.Errorf("failed to decrypt header %s: %w", k, err)
				}
				headers[k] = plaintext
			} else {
				// Plain text value
				headers[k] = v
			}
		}
		server.SetHeaders(headers)
	}

	return server, nil
}
