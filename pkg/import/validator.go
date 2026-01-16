package importexport

import (
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
)

// Validator validates imported resources
type Validator struct {
	cronParser cron.Parser
}

// NewValidator creates a new Validator instance
func NewValidator() *Validator {
	return &Validator{
		cronParser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Validate validates the entire TeamResources structure
func (v *Validator) Validate(resources *TeamResources) error {
	// Validate metadata
	if err := v.ValidateMetadata(resources.Metadata); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	// Validate schedules
	scheduleNames := make(map[string]bool)
	for i, schedule := range resources.Schedules {
		if err := v.ValidateSchedule(schedule); err != nil {
			return fmt.Errorf("schedule[%d] validation failed: %w", i, err)
		}
		// Check for duplicate names
		if scheduleNames[schedule.Name] {
			return fmt.Errorf("duplicate schedule name: %s", schedule.Name)
		}
		scheduleNames[schedule.Name] = true
	}

	// Validate webhooks
	webhookNames := make(map[string]bool)
	for i, webhook := range resources.Webhooks {
		if err := v.ValidateWebhook(webhook); err != nil {
			return fmt.Errorf("webhook[%d] validation failed: %w", i, err)
		}
		// Check for duplicate names
		if webhookNames[webhook.Name] {
			return fmt.Errorf("duplicate webhook name: %s", webhook.Name)
		}
		webhookNames[webhook.Name] = true
	}

	return nil
}

// ValidateMetadata validates the metadata section
func (v *Validator) ValidateMetadata(metadata ResourceMetadata) error {
	if metadata.TeamID == "" {
		return fmt.Errorf("team_id is required")
	}

	// Validate team_id format (org/team-slug)
	if !strings.Contains(metadata.TeamID, "/") {
		return fmt.Errorf("team_id must be in format 'org/team-slug'")
	}

	parts := strings.Split(metadata.TeamID, "/")
	if len(parts) != 2 {
		return fmt.Errorf("team_id must be in format 'org/team-slug'")
	}

	if parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("team_id must have non-empty org and team-slug")
	}

	return nil
}

// ValidateSchedule validates a single schedule
func (v *Validator) ValidateSchedule(schedule ScheduleImport) error {
	if schedule.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Validate status
	if schedule.Status != "" {
		validStatuses := map[string]bool{"active": true, "paused": true, "completed": true}
		if !validStatuses[schedule.Status] {
			return fmt.Errorf("invalid status: %s (must be active, paused, or completed)", schedule.Status)
		}
	}

	// Either scheduled_at or cron_expr must be set
	if schedule.ScheduledAt == nil && schedule.CronExpr == "" {
		return fmt.Errorf("either scheduled_at or cron_expr must be set")
	}

	// Validate cron expression if provided
	if schedule.CronExpr != "" {
		if _, err := v.cronParser.Parse(schedule.CronExpr); err != nil {
			return fmt.Errorf("invalid cron expression %q: %w", schedule.CronExpr, err)
		}
	}

	// Validate timezone if provided
	if schedule.Timezone != "" {
		// Basic validation - could be enhanced with actual timezone lookup
		if len(schedule.Timezone) < 3 {
			return fmt.Errorf("invalid timezone: %s", schedule.Timezone)
		}
	}

	// Validate session config
	if err := v.ValidateSessionConfig(schedule.SessionConfig); err != nil {
		return fmt.Errorf("session_config validation failed: %w", err)
	}

	return nil
}

// ValidateWebhook validates a single webhook
func (v *Validator) ValidateWebhook(webhook WebhookImport) error {
	if webhook.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Validate status
	if webhook.Status != "" {
		validStatuses := map[string]bool{"active": true, "paused": true}
		if !validStatuses[webhook.Status] {
			return fmt.Errorf("invalid status: %s (must be active or paused)", webhook.Status)
		}
	}

	// Validate webhook type
	if webhook.WebhookType == "" {
		return fmt.Errorf("webhook_type is required")
	}
	validTypes := map[string]bool{"github": true, "custom": true}
	if !validTypes[webhook.WebhookType] {
		return fmt.Errorf("invalid webhook_type: %s (must be github or custom)", webhook.WebhookType)
	}

	// Validate signature type if provided
	if webhook.SignatureType != "" {
		validSignatureTypes := map[string]bool{"hmac": true, "static": true}
		if !validSignatureTypes[webhook.SignatureType] {
			return fmt.Errorf("invalid signature_type: %s (must be hmac or static)", webhook.SignatureType)
		}
	}

	// Validate max_sessions
	if webhook.MaxSessions < 0 {
		return fmt.Errorf("max_sessions must be non-negative")
	}
	if webhook.MaxSessions > 100 {
		return fmt.Errorf("max_sessions must not exceed 100")
	}

	// Validate GitHub config if type is github
	if webhook.WebhookType == "github" && webhook.GitHub != nil {
		if err := v.ValidateGitHubConfig(*webhook.GitHub); err != nil {
			return fmt.Errorf("github config validation failed: %w", err)
		}
	}

	// Validate triggers
	if len(webhook.Triggers) == 0 {
		return fmt.Errorf("at least one trigger is required")
	}

	triggerNames := make(map[string]bool)
	for i, trigger := range webhook.Triggers {
		if err := v.ValidateTrigger(trigger); err != nil {
			return fmt.Errorf("trigger[%d] validation failed: %w", i, err)
		}
		// Check for duplicate trigger names
		if triggerNames[trigger.Name] {
			return fmt.Errorf("duplicate trigger name: %s", trigger.Name)
		}
		triggerNames[trigger.Name] = true
	}

	// Validate session config if provided
	if webhook.SessionConfig != nil {
		if err := v.ValidateSessionConfig(*webhook.SessionConfig); err != nil {
			return fmt.Errorf("session_config validation failed: %w", err)
		}
	}

	return nil
}

// ValidateGitHubConfig validates GitHub configuration
func (v *Validator) ValidateGitHubConfig(config GitHubConfigImport) error {
	// Basic validation - could be enhanced
	// Note: Empty AllowedEvents and AllowedRepositories are allowed for now
	// as they may be optional depending on the webhook configuration

	return nil
}

// ValidateTrigger validates a webhook trigger
func (v *Validator) ValidateTrigger(trigger WebhookTriggerImport) error {
	if trigger.Name == "" {
		return fmt.Errorf("name is required")
	}

	if trigger.Priority < 0 {
		return fmt.Errorf("priority must be non-negative")
	}

	// Validate conditions
	if err := v.ValidateTriggerConditions(trigger.Conditions); err != nil {
		return fmt.Errorf("conditions validation failed: %w", err)
	}

	// Validate session config if provided
	if trigger.SessionConfig != nil {
		if err := v.ValidateSessionConfig(*trigger.SessionConfig); err != nil {
			return fmt.Errorf("session_config validation failed: %w", err)
		}
	}

	return nil
}

// ValidateTriggerConditions validates trigger conditions
func (v *Validator) ValidateTriggerConditions(conditions WebhookTriggerConditionsImport) error {
	// At least one condition type should be specified
	hasCondition := conditions.GitHub != nil || len(conditions.JSONPath) > 0 || conditions.GoTemplate != ""
	if !hasCondition {
		return fmt.Errorf("at least one condition type (github, json_path, or go_template) must be specified")
	}

	// Validate GitHub conditions
	if conditions.GitHub != nil {
		if err := v.ValidateGitHubConditions(*conditions.GitHub); err != nil {
			return fmt.Errorf("github conditions validation failed: %w", err)
		}
	}

	// Validate JSONPath conditions
	for i, jsonPath := range conditions.JSONPath {
		if err := v.ValidateJSONPathCondition(jsonPath); err != nil {
			return fmt.Errorf("json_path[%d] validation failed: %w", i, err)
		}
	}

	return nil
}

// ValidateGitHubConditions validates GitHub conditions
func (v *Validator) ValidateGitHubConditions(conditions GitHubConditionsImport) error {
	// Basic validation - could be enhanced with actual event/action validation
	return nil
}

// ValidateJSONPathCondition validates a JSONPath condition
func (v *Validator) ValidateJSONPathCondition(condition JSONPathConditionImport) error {
	if condition.Path == "" {
		return fmt.Errorf("path is required")
	}

	if condition.Operator == "" {
		return fmt.Errorf("operator is required")
	}

	validOperators := map[string]bool{
		"eq": true, "ne": true, "contains": true,
		"matches": true, "in": true, "exists": true,
	}
	if !validOperators[condition.Operator] {
		return fmt.Errorf("invalid operator: %s", condition.Operator)
	}

	// For exists operator, value is not required
	if condition.Operator != "exists" && condition.Value == nil {
		return fmt.Errorf("value is required for operator %s", condition.Operator)
	}

	return nil
}

// ValidateSessionConfig validates session configuration
func (v *Validator) ValidateSessionConfig(config SessionConfigImport) error {
	// Validate params if provided
	if config.Params != nil {
		// Both initial_message and initial_message_template should not be set
		if config.Params.InitialMessage != "" && config.Params.InitialMessageTemplate != "" {
			return fmt.Errorf("cannot specify both initial_message and initial_message_template")
		}
	}

	return nil
}
