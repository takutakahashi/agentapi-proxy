package webhook

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// Filter defines filter criteria for listing webhooks
type Filter struct {
	// UserID filters by user ID
	UserID string
	// Status filters by webhook status
	Status WebhookStatus
	// Type filters by webhook type
	Type WebhookType
	// Scope filters by resource scope
	Scope entities.ResourceScope
	// TeamID filters by team ID (for team-scoped webhooks)
	TeamID string
	// TeamIDs filters by multiple team IDs (returns webhooks for any of these teams)
	TeamIDs []string
}

// GitHubMatcher defines criteria for finding webhooks that match a GitHub payload
type GitHubMatcher struct {
	// Repository is the full name of the repository (e.g., "owner/repo")
	Repository string
	// EnterpriseURL is the GitHub Enterprise Server URL (empty for github.com)
	EnterpriseURL string
	// Event is the GitHub event type (e.g., "push", "pull_request")
	Event string
}

// Manager defines the interface for webhook management
type Manager interface {
	// Create creates a new webhook configuration
	Create(ctx context.Context, webhook *WebhookConfig) error

	// Get retrieves a webhook by ID
	Get(ctx context.Context, id string) (*WebhookConfig, error)

	// List retrieves webhooks matching the filter
	List(ctx context.Context, filter Filter) ([]*WebhookConfig, error)

	// Update updates an existing webhook
	Update(ctx context.Context, webhook *WebhookConfig) error

	// Delete removes a webhook by ID
	Delete(ctx context.Context, id string) error

	// FindByGitHubRepository finds webhooks that may match a GitHub webhook
	// Returns webhooks where the repository matches allowed_repositories
	FindByGitHubRepository(ctx context.Context, matcher GitHubMatcher) ([]*WebhookConfig, error)

	// RegenerateSecret generates a new secret for a webhook
	RegenerateSecret(ctx context.Context, id string) (string, error)

	// RecordDelivery records a webhook delivery
	RecordDelivery(ctx context.Context, id string, record DeliveryRecord) error
}
