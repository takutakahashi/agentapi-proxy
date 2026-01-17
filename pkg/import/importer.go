package importexport

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
)

// Importer handles importing of team resources
type Importer struct {
	scheduleManager   schedule.Manager
	webhookRepository repositories.WebhookRepository
	validator         *Validator
}

// NewImporter creates a new Importer instance
func NewImporter(
	scheduleManager schedule.Manager,
	webhookRepository repositories.WebhookRepository,
) *Importer {
	return &Importer{
		scheduleManager:   scheduleManager,
		webhookRepository: webhookRepository,
		validator:         NewValidator(),
	}
}

// Import imports team resources
func (i *Importer) Import(ctx context.Context, resources *TeamResources, userID string, options ImportOptions) (*ImportResult, error) {
	// Validate the resources first
	if err := i.validator.Validate(resources); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	result := &ImportResult{
		Success: true,
		Summary: ImportSummary{},
		Details: []ImportDetail{},
		Errors:  []string{},
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

	// Check if any failures occurred
	if result.Summary.Schedules.Failed > 0 || result.Summary.Webhooks.Failed > 0 {
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

	// Find existing schedule by name if mode is update or upsert
	var existingSchedule *schedule.Schedule
	if options.Mode == ImportModeUpdate || options.Mode == ImportModeUpsert {
		schedules, err := i.scheduleManager.List(ctx, schedule.ScheduleFilter{
			UserID: userID,
			Scope:  entities.ScopeTeam,
			TeamID: teamID,
		})
		if err == nil {
			for _, s := range schedules {
				if s.Name == scheduleImport.Name {
					existingSchedule = s
					break
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

	// Dry run - don't actually create/update
	if options.DryRun {
		detail.Action = action + "d (dry-run)"
		if existingSchedule != nil {
			detail.ID = existingSchedule.ID
		}
		return detail
	}

	// Convert import to schedule entity
	scheduleEntity, err := i.convertScheduleImport(scheduleImport, teamID, userID, existingSchedule)
	if err != nil {
		detail.Action = "failed"
		detail.Status = "error"
		detail.Error = err.Error()
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

	// Find existing webhook by name if mode is update or upsert
	var existingWebhook *entities.Webhook
	if options.Mode == ImportModeUpdate || options.Mode == ImportModeUpsert {
		webhooks, err := i.webhookRepository.List(ctx, repositories.WebhookFilter{
			UserID: userID,
			Scope:  entities.ScopeTeam,
			TeamID: teamID,
		})
		if err == nil {
			for _, w := range webhooks {
				if w.Name() == webhookImport.Name {
					existingWebhook = w
					break
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

	// Dry run - don't actually create/update
	if options.DryRun {
		detail.Action = action + "d (dry-run)"
		if existingWebhook != nil {
			detail.ID = existingWebhook.ID()
		}
		return detail
	}

	// Convert import to webhook entity
	webhookEntity, err := i.convertWebhookImport(webhookImport, teamID, userID, existingWebhook, options)
	if err != nil {
		detail.Action = "failed"
		detail.Status = "error"
		detail.Error = err.Error()
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

func (i *Importer) convertWebhookImport(webhookImport WebhookImport, teamID, userID string, existing *entities.Webhook, options ImportOptions) (*entities.Webhook, error) {
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

	// Set secret
	if options.RegenerateAll || (webhookImport.Secret == "" && existing == nil) {
		// Generate new secret
		secret, err := generateSecret(32)
		if err != nil {
			return nil, fmt.Errorf("failed to generate secret: %w", err)
		}
		webhookEntity.SetSecret(secret)
	} else if webhookImport.Secret != "" {
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
