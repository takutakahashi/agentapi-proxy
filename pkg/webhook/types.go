package webhook

import (
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// WebhookType defines the type of webhook source
type WebhookType string

const (
	// WebhookTypeGitHub indicates a GitHub/GHES webhook
	WebhookTypeGitHub WebhookType = "github"
	// WebhookTypeCustom indicates a custom webhook (reserved for future)
	WebhookTypeCustom WebhookType = "custom"
)

// WebhookStatus defines the current status of a webhook
type WebhookStatus string

const (
	// WebhookStatusActive indicates the webhook is active and will process events
	WebhookStatusActive WebhookStatus = "active"
	// WebhookStatusPaused indicates the webhook is paused
	WebhookStatusPaused WebhookStatus = "paused"
)

// DeliveryStatus defines the status of a webhook delivery
type DeliveryStatus string

const (
	// DeliveryStatusProcessed indicates the webhook was processed and a session was created
	DeliveryStatusProcessed DeliveryStatus = "processed"
	// DeliveryStatusSkipped indicates the webhook was skipped (no matching trigger)
	DeliveryStatusSkipped DeliveryStatus = "skipped"
	// DeliveryStatusFailed indicates the webhook processing failed
	DeliveryStatusFailed DeliveryStatus = "failed"
)

// WebhookConfig represents a webhook configuration
type WebhookConfig struct {
	// ID is the unique identifier for the webhook
	ID string `json:"id"`
	// Name is a human-readable name for the webhook
	Name string `json:"name"`
	// UserID is the ID of the user who created the webhook
	UserID string `json:"user_id"`
	// Scope defines the ownership scope ("user" or "team"). Defaults to "user".
	Scope entities.ResourceScope `json:"scope,omitempty"`
	// TeamID is the team identifier (e.g., "org/team-slug") when Scope is "team"
	TeamID string `json:"team_id,omitempty"`
	// Status is the current status of the webhook
	Status WebhookStatus `json:"status"`

	// Type is the webhook source type
	Type WebhookType `json:"type"`
	// Secret is the HMAC secret for signature verification
	Secret string `json:"secret"`

	// GitHub contains GitHub-specific configuration
	GitHub *GitHubConfig `json:"github,omitempty"`

	// Triggers define which events start which sessions
	Triggers []Trigger `json:"triggers"`

	// SessionConfig contains default session configuration (can be overridden by triggers)
	SessionConfig *SessionConfig `json:"session_config,omitempty"`

	// Tracking
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	LastDelivery  *DeliveryRecord `json:"last_delivery,omitempty"`
	DeliveryCount int64           `json:"delivery_count"`
}

// GitHubConfig contains GitHub-specific webhook configuration
type GitHubConfig struct {
	// EnterpriseURL is the GitHub Enterprise Server URL (empty for github.com)
	EnterpriseURL string `json:"enterprise_url,omitempty"`
	// AllowedEvents filters which GitHub events are accepted (empty = all)
	AllowedEvents []string `json:"allowed_events,omitempty"`
	// AllowedRepositories filters which repositories are accepted (e.g., "owner/repo" or "owner/*")
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

// Trigger defines a rule for starting a session based on webhook payload
type Trigger struct {
	// ID is the unique identifier for the trigger
	ID string `json:"id"`
	// Name is a human-readable name for the trigger
	Name string `json:"name"`
	// Priority determines evaluation order (lower = higher priority)
	Priority int `json:"priority"`
	// Enabled indicates if this trigger is active
	Enabled bool `json:"enabled"`

	// Conditions define when this trigger matches
	Conditions TriggerConditions `json:"conditions"`

	// SessionConfig contains session configuration for this trigger (overrides webhook default)
	SessionConfig *SessionConfig `json:"session_config,omitempty"`

	// StopOnMatch indicates if subsequent triggers should be skipped when this matches
	StopOnMatch bool `json:"stop_on_match"`
}

// TriggerConditions defines the conditions for a trigger to match
type TriggerConditions struct {
	// GitHub contains GitHub-specific conditions
	GitHub *GitHubConditions `json:"github,omitempty"`
	// JSONPath contains JSONPath-based conditions (for custom webhooks)
	JSONPath []JSONPathCondition `json:"jsonpath,omitempty"`
}

// GitHubConditions defines GitHub-specific trigger conditions
type GitHubConditions struct {
	// Events filters by GitHub event type (e.g., "push", "pull_request")
	Events []string `json:"events,omitempty"`
	// Actions filters by event action (e.g., "opened", "synchronize")
	Actions []string `json:"actions,omitempty"`
	// Branches filters by branch name (supports glob patterns like "feature/*")
	Branches []string `json:"branches,omitempty"`
	// Repositories filters by repository full name (e.g., "owner/repo")
	Repositories []string `json:"repositories,omitempty"`
	// Labels filters by issue/PR labels
	Labels []string `json:"labels,omitempty"`
	// Paths filters by changed file paths (supports glob patterns)
	Paths []string `json:"paths,omitempty"`
	// BaseBranches filters by PR base branch
	BaseBranches []string `json:"base_branches,omitempty"`
	// Draft filters by PR draft status
	Draft *bool `json:"draft,omitempty"`
	// Sender filters by GitHub username
	Sender []string `json:"sender,omitempty"`
}

// JSONPathCondition defines a JSONPath-based condition
type JSONPathCondition struct {
	// Path is the JSONPath expression (e.g., "$.event.type")
	Path string `json:"path"`
	// Operator is the comparison operator
	Operator ConditionOperator `json:"operator"`
	// Value is the value to compare against
	Value interface{} `json:"value"`
}

// ConditionOperator defines the comparison operator for conditions
type ConditionOperator string

const (
	// OperatorEquals checks for equality
	OperatorEquals ConditionOperator = "eq"
	// OperatorNotEquals checks for inequality
	OperatorNotEquals ConditionOperator = "ne"
	// OperatorContains checks if string contains substring
	OperatorContains ConditionOperator = "contains"
	// OperatorMatches checks against a regular expression
	OperatorMatches ConditionOperator = "matches"
	// OperatorIn checks if value is in a list
	OperatorIn ConditionOperator = "in"
	// OperatorExists checks if path exists
	OperatorExists ConditionOperator = "exists"
)

// SessionConfig contains session creation parameters
type SessionConfig struct {
	// Environment variables to set for the session
	Environment map[string]string `json:"environment,omitempty"`
	// Tags for the session
	Tags map[string]string `json:"tags,omitempty"`
	// InitialMessageTemplate is a Go template for the initial message
	// Available variables: .payload (raw payload), .event (GitHub event), .repository, etc.
	InitialMessageTemplate string `json:"initial_message_template,omitempty"`
	// Params contains session parameters
	Params *SessionParams `json:"params,omitempty"`
}

// SessionParams contains additional session parameters
type SessionParams struct {
	// GithubToken is a GitHub token to use for the session
	// "inherit" means use the webhook's authentication
	GithubToken string `json:"github_token,omitempty"`
}

// DeliveryRecord represents a single webhook delivery
type DeliveryRecord struct {
	// ID is the unique identifier for the delivery
	ID string `json:"id"`
	// ReceivedAt is when the webhook was received
	ReceivedAt time.Time `json:"received_at"`
	// Status is the result of the delivery
	Status DeliveryStatus `json:"status"`
	// MatchedTrigger is the ID of the trigger that matched (if any)
	MatchedTrigger string `json:"matched_trigger,omitempty"`
	// SessionID is the ID of the created session (if any)
	SessionID string `json:"session_id,omitempty"`
	// Error contains the error message if delivery failed
	Error string `json:"error,omitempty"`
}

// GetScope returns the resource scope, defaulting to "user" if not set
func (w *WebhookConfig) GetScope() entities.ResourceScope {
	if w.Scope == "" {
		return entities.ScopeUser
	}
	return w.Scope
}

// Validate checks if the webhook configuration is valid
func (w *WebhookConfig) Validate() error {
	if w.ID == "" {
		return ErrInvalidWebhook{Field: "id", Message: "id is required"}
	}
	if w.Name == "" {
		return ErrInvalidWebhook{Field: "name", Message: "name is required"}
	}
	if w.UserID == "" {
		return ErrInvalidWebhook{Field: "user_id", Message: "user_id is required"}
	}
	if w.Type == "" {
		return ErrInvalidWebhook{Field: "type", Message: "type is required"}
	}
	if w.Type != WebhookTypeGitHub && w.Type != WebhookTypeCustom {
		return ErrInvalidWebhook{Field: "type", Message: "type must be 'github' or 'custom'"}
	}
	if len(w.Triggers) == 0 {
		return ErrInvalidWebhook{Field: "triggers", Message: "at least one trigger is required"}
	}
	return nil
}

// MaskSecret returns the secret with only the last 4 characters visible
func (w *WebhookConfig) MaskSecret() string {
	if len(w.Secret) <= 4 {
		return "****"
	}
	return "****" + w.Secret[len(w.Secret)-4:]
}

// ErrInvalidWebhook represents a validation error
type ErrInvalidWebhook struct {
	Field   string
	Message string
}

func (e ErrInvalidWebhook) Error() string {
	return "invalid webhook: " + e.Field + ": " + e.Message
}

// ErrWebhookNotFound is returned when a webhook is not found
type ErrWebhookNotFound struct {
	ID string
}

func (e ErrWebhookNotFound) Error() string {
	return "webhook not found: " + e.ID
}
