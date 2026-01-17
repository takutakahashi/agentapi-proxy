package importexport

import (
	"context"
	"fmt"
	"slices"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
)

// Exporter handles exporting of team resources
type Exporter struct {
	scheduleManager   schedule.Manager
	webhookRepository repositories.WebhookRepository
}

// NewExporter creates a new Exporter instance
func NewExporter(
	scheduleManager schedule.Manager,
	webhookRepository repositories.WebhookRepository,
) *Exporter {
	return &Exporter{
		scheduleManager:   scheduleManager,
		webhookRepository: webhookRepository,
	}
}

// Export exports team resources
func (e *Exporter) Export(ctx context.Context, teamID, userID string, options ExportOptions) (*TeamResources, error) {
	resources := &TeamResources{
		APIVersion: "agentapi.proxy/v1",
		Kind:       "TeamResources",
		Metadata: ResourceMetadata{
			TeamID: teamID,
		},
		Schedules: []ScheduleImport{},
		Webhooks:  []WebhookImport{},
	}

	// Determine which resource types to include
	includeSchedules := len(options.IncludeTypes) == 0 || slices.Contains(options.IncludeTypes, "schedules")
	includeWebhooks := len(options.IncludeTypes) == 0 || slices.Contains(options.IncludeTypes, "webhooks")

	// Export schedules
	if includeSchedules {
		schedules, err := e.scheduleManager.List(ctx, schedule.ScheduleFilter{
			UserID: userID,
			Scope:  entities.ScopeTeam,
			TeamID: teamID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list schedules: %w", err)
		}

		for _, s := range schedules {
			// Apply status filter if specified
			if len(options.StatusFilter) > 0 && !slices.Contains(options.StatusFilter, string(s.Status)) {
				continue
			}

			scheduleImport := e.convertScheduleToImport(s)
			resources.Schedules = append(resources.Schedules, scheduleImport)
		}
	}

	// Export webhooks
	if includeWebhooks {
		webhooks, err := e.webhookRepository.List(ctx, repositories.WebhookFilter{
			UserID: userID,
			Scope:  entities.ScopeTeam,
			TeamID: teamID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list webhooks: %w", err)
		}

		for _, w := range webhooks {
			// Apply status filter if specified
			if len(options.StatusFilter) > 0 && !slices.Contains(options.StatusFilter, string(w.Status())) {
				continue
			}

			webhookImport := e.convertWebhookToImport(w, options.IncludeSecrets)
			resources.Webhooks = append(resources.Webhooks, webhookImport)
		}
	}

	return resources, nil
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

func (e *Exporter) convertWebhookToImport(w *entities.Webhook, includeSecrets bool) WebhookImport {
	webhookImport := WebhookImport{
		Name:            w.Name(),
		Status:          string(w.Status()),
		WebhookType:     string(w.WebhookType()),
		SignatureHeader: w.SignatureHeader(),
		SignatureType:   string(w.SignatureType()),
		MaxSessions:     w.MaxSessions(),
	}

	// Include secret if requested (masked by default for security)
	if includeSecrets {
		webhookImport.Secret = w.Secret()
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
		triggerImport := e.convertTriggerToImport(trigger)
		webhookImport.Triggers = append(webhookImport.Triggers, triggerImport)
	}

	// Convert session config
	if w.SessionConfig() != nil {
		sessionConfig := e.convertWebhookSessionConfigToImport(w.SessionConfig())
		webhookImport.SessionConfig = &sessionConfig
	}

	return webhookImport
}

func (e *Exporter) convertTriggerToImport(trigger entities.WebhookTrigger) WebhookTriggerImport {
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
		sessionConfig := e.convertWebhookSessionConfigToImport(trigger.SessionConfig())
		triggerImport.SessionConfig = &sessionConfig
	}

	return triggerImport
}

func (e *Exporter) convertWebhookSessionConfigToImport(config *entities.WebhookSessionConfig) SessionConfigImport {
	sessionConfig := SessionConfigImport{
		Environment: config.Environment(),
		Tags:        config.Tags(),
	}

	if config.Params() != nil || config.InitialMessageTemplate() != "" {
		sessionConfig.Params = &SessionParamsImport{}
		if config.InitialMessageTemplate() != "" {
			sessionConfig.Params.InitialMessageTemplate = config.InitialMessageTemplate()
		}
		if config.Params() != nil && config.Params().GithubToken() != "" {
			sessionConfig.Params.GitHubToken = config.Params().GithubToken()
		}
	}

	return sessionConfig
}
