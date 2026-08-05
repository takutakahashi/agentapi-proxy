package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// WebhookFilter defines filter criteria for listing webhooks
type WebhookFilter struct {
	// UserID filters by user ID
	UserID string
	// Status filters by webhook status
	Status entities.WebhookStatus
	// Scope filters by resource scope
	Scope entities.ResourceScope
	// TeamID filters by team ID (for team-scoped webhooks)
	TeamID string
	// TeamIDs filters by multiple team IDs (returns webhooks for any of these teams)
	TeamIDs []string
	// Type filters by webhook type
	Type entities.WebhookType
}

// GitHubMatcher defines criteria for finding webhooks that match a GitHub event
type GitHubMatcher struct {
	// Repository is the full repository name (e.g., "owner/repo")
	Repository string
	// EnterpriseURL is the GitHub Enterprise host (empty for github.com)
	EnterpriseURL string
	// Event is the GitHub event type (e.g., "push", "pull_request")
	Event string
}

// WebhookRepository defines the interface for webhook data persistence
type WebhookRepository interface {
	// Create creates a new webhook
	Create(ctx context.Context, webhook *entities.Webhook) error

	// Get retrieves a webhook by ID
	Get(ctx context.Context, id string) (*entities.Webhook, error)

	// List retrieves webhooks matching the filter
	List(ctx context.Context, filter WebhookFilter) ([]*entities.Webhook, error)

	// Update updates an existing webhook
	Update(ctx context.Context, webhook *entities.Webhook) error

	// Delete removes a webhook by ID
	Delete(ctx context.Context, id string) error

	// FindByGitHubRepository finds webhooks that match a GitHub repository
	// This is used during webhook reception to find candidate webhooks
	FindByGitHubRepository(ctx context.Context, matcher GitHubMatcher) ([]*entities.Webhook, error)

	// RecordDelivery records a delivery attempt
	RecordDelivery(ctx context.Context, id string, record *entities.WebhookDeliveryRecord) error
}
