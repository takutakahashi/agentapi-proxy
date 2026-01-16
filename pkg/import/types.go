package importexport

import (
	"time"
)

// TeamResources represents the root structure for team resource import/export
type TeamResources struct {
	APIVersion string           `yaml:"apiVersion" toml:"api_version" json:"apiVersion"`
	Kind       string           `yaml:"kind" toml:"kind" json:"kind"`
	Metadata   ResourceMetadata `yaml:"metadata" toml:"metadata" json:"metadata"`
	Schedules  []ScheduleImport `yaml:"schedules,omitempty" toml:"schedules,omitempty" json:"schedules,omitempty"`
	Webhooks   []WebhookImport  `yaml:"webhooks,omitempty" toml:"webhooks,omitempty" json:"webhooks,omitempty"`
}

// ResourceMetadata contains metadata about the team resources
type ResourceMetadata struct {
	TeamID      string `yaml:"team_id" toml:"team_id" json:"team_id"`
	Description string `yaml:"description,omitempty" toml:"description,omitempty" json:"description,omitempty"`
}

// ScheduleImport represents a schedule for import/export
type ScheduleImport struct {
	Name          string              `yaml:"name" toml:"name" json:"name"`
	Status        string              `yaml:"status,omitempty" toml:"status,omitempty" json:"status,omitempty"`
	ScheduledAt   *time.Time          `yaml:"scheduled_at,omitempty" toml:"scheduled_at,omitempty" json:"scheduled_at,omitempty"`
	CronExpr      string              `yaml:"cron_expr,omitempty" toml:"cron_expr,omitempty" json:"cron_expr,omitempty"`
	Timezone      string              `yaml:"timezone,omitempty" toml:"timezone,omitempty" json:"timezone,omitempty"`
	SessionConfig SessionConfigImport `yaml:"session_config" toml:"session_config" json:"session_config"`
}

// WebhookImport represents a webhook for import/export
type WebhookImport struct {
	Name            string                 `yaml:"name" toml:"name" json:"name"`
	Status          string                 `yaml:"status,omitempty" toml:"status,omitempty" json:"status,omitempty"`
	WebhookType     string                 `yaml:"webhook_type" toml:"webhook_type" json:"webhook_type"`
	Secret          string                 `yaml:"secret,omitempty" toml:"secret,omitempty" json:"secret,omitempty"`
	SignatureHeader string                 `yaml:"signature_header,omitempty" toml:"signature_header,omitempty" json:"signature_header,omitempty"`
	SignatureType   string                 `yaml:"signature_type,omitempty" toml:"signature_type,omitempty" json:"signature_type,omitempty"`
	MaxSessions     int                    `yaml:"max_sessions,omitempty" toml:"max_sessions,omitempty" json:"max_sessions,omitempty"`
	GitHub          *GitHubConfigImport    `yaml:"github,omitempty" toml:"github,omitempty" json:"github,omitempty"`
	Triggers        []WebhookTriggerImport `yaml:"triggers" toml:"triggers" json:"triggers"`
	SessionConfig   *SessionConfigImport   `yaml:"session_config,omitempty" toml:"session_config,omitempty" json:"session_config,omitempty"`
}

// SessionConfigImport represents session configuration for import/export
type SessionConfigImport struct {
	Environment map[string]string    `yaml:"environment,omitempty" toml:"environment,omitempty" json:"environment,omitempty"`
	Tags        map[string]string    `yaml:"tags,omitempty" toml:"tags,omitempty" json:"tags,omitempty"`
	Params      *SessionParamsImport `yaml:"params,omitempty" toml:"params,omitempty" json:"params,omitempty"`
}

// SessionParamsImport represents session parameters for import/export
type SessionParamsImport struct {
	InitialMessage         string `yaml:"initial_message,omitempty" toml:"initial_message,omitempty" json:"initial_message,omitempty"`
	InitialMessageTemplate string `yaml:"initial_message_template,omitempty" toml:"initial_message_template,omitempty" json:"initial_message_template,omitempty"`
	GitHubToken            string `yaml:"github_token,omitempty" toml:"github_token,omitempty" json:"github_token,omitempty"`
}

// GitHubConfigImport represents GitHub-specific webhook configuration for import/export
type GitHubConfigImport struct {
	EnterpriseURL       string   `yaml:"enterprise_url,omitempty" toml:"enterprise_url,omitempty" json:"enterprise_url,omitempty"`
	AllowedEvents       []string `yaml:"allowed_events,omitempty" toml:"allowed_events,omitempty" json:"allowed_events,omitempty"`
	AllowedRepositories []string `yaml:"allowed_repositories,omitempty" toml:"allowed_repositories,omitempty" json:"allowed_repositories,omitempty"`
}

// WebhookTriggerImport represents a webhook trigger for import/export
type WebhookTriggerImport struct {
	Name          string                         `yaml:"name" toml:"name" json:"name"`
	Priority      int                            `yaml:"priority,omitempty" toml:"priority,omitempty" json:"priority,omitempty"`
	Enabled       bool                           `yaml:"enabled" toml:"enabled" json:"enabled"`
	Conditions    WebhookTriggerConditionsImport `yaml:"conditions" toml:"conditions" json:"conditions"`
	SessionConfig *SessionConfigImport           `yaml:"session_config,omitempty" toml:"session_config,omitempty" json:"session_config,omitempty"`
	StopOnMatch   bool                           `yaml:"stop_on_match,omitempty" toml:"stop_on_match,omitempty" json:"stop_on_match,omitempty"`
}

// WebhookTriggerConditionsImport represents trigger conditions for import/export
type WebhookTriggerConditionsImport struct {
	GitHub     *GitHubConditionsImport   `yaml:"github,omitempty" toml:"github,omitempty" json:"github,omitempty"`
	JSONPath   []JSONPathConditionImport `yaml:"json_path,omitempty" toml:"json_path,omitempty" json:"json_path,omitempty"`
	GoTemplate string                    `yaml:"go_template,omitempty" toml:"go_template,omitempty" json:"go_template,omitempty"`
}

// GitHubConditionsImport represents GitHub-specific trigger conditions for import/export
type GitHubConditionsImport struct {
	Events       []string `yaml:"events,omitempty" toml:"events,omitempty" json:"events,omitempty"`
	Actions      []string `yaml:"actions,omitempty" toml:"actions,omitempty" json:"actions,omitempty"`
	Branches     []string `yaml:"branches,omitempty" toml:"branches,omitempty" json:"branches,omitempty"`
	Repositories []string `yaml:"repositories,omitempty" toml:"repositories,omitempty" json:"repositories,omitempty"`
	Labels       []string `yaml:"labels,omitempty" toml:"labels,omitempty" json:"labels,omitempty"`
	Paths        []string `yaml:"paths,omitempty" toml:"paths,omitempty" json:"paths,omitempty"`
	BaseBranches []string `yaml:"base_branches,omitempty" toml:"base_branches,omitempty" json:"base_branches,omitempty"`
	Draft        *bool    `yaml:"draft,omitempty" toml:"draft,omitempty" json:"draft,omitempty"`
	Sender       []string `yaml:"sender,omitempty" toml:"sender,omitempty" json:"sender,omitempty"`
}

// JSONPathConditionImport represents a JSONPath condition for import/export
type JSONPathConditionImport struct {
	Path     string      `yaml:"path" toml:"path" json:"path"`
	Operator string      `yaml:"operator" toml:"operator" json:"operator"`
	Value    interface{} `yaml:"value" toml:"value" json:"value"`
}

// ImportMode defines the import behavior
type ImportMode string

const (
	// ImportModeCreate creates new resources only, fails if already exists
	ImportModeCreate ImportMode = "create"
	// ImportModeUpdate updates existing resources only, fails if not exists
	ImportModeUpdate ImportMode = "update"
	// ImportModeUpsert creates or updates resources as needed
	ImportModeUpsert ImportMode = "upsert"
)

// ImportOptions contains options for import operations
type ImportOptions struct {
	DryRun        bool       // Only validate, don't create
	Mode          ImportMode // Import mode
	IDField       string     // Field to use for matching: "name" or "id"
	AllowPartial  bool       // Allow partial success
	RegenerateAll bool       // Regenerate all secrets
}

// ImportResult contains the results of an import operation
type ImportResult struct {
	Success bool           `json:"success"`
	Summary ImportSummary  `json:"summary"`
	Details []ImportDetail `json:"details"`
	Errors  []string       `json:"errors,omitempty"`
}

// ImportSummary contains summary statistics for each resource type
type ImportSummary struct {
	Schedules ImportResourceSummary `json:"schedules"`
	Webhooks  ImportResourceSummary `json:"webhooks"`
}

// ImportResourceSummary contains summary statistics for a resource type
type ImportResourceSummary struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

// ImportDetail contains detailed information about a single resource import
type ImportDetail struct {
	ResourceType string `json:"resource_type"` // "schedule" or "webhook"
	ResourceName string `json:"resource_name"`
	Action       string `json:"action"` // "created", "updated", "skipped", "failed"
	ID           string `json:"id,omitempty"`
	Status       string `json:"status"` // "success" or "error"
	Error        string `json:"error,omitempty"`
}

// ExportFormat defines the export format
type ExportFormat string

const (
	// ExportFormatYAML exports as YAML
	ExportFormatYAML ExportFormat = "yaml"
	// ExportFormatTOML exports as TOML
	ExportFormatTOML ExportFormat = "toml"
	// ExportFormatJSON exports as JSON
	ExportFormatJSON ExportFormat = "json"
)

// ExportOptions contains options for export operations
type ExportOptions struct {
	Format         ExportFormat // Output format
	IncludeSecrets bool         // Include webhook secrets in export
	StatusFilter   []string     // Filter by status (active, paused, etc.)
	IncludeTypes   []string     // Include only specified types (schedules, webhooks)
}
