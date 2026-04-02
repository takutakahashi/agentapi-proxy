package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SlackBotFilter defines filter criteria for listing SlackBots
type SlackBotFilter struct {
	// UserID filters by user ID
	UserID string
	// Status filters by SlackBot status
	Status entities.SlackBotStatus
	// Scope filters by resource scope
	Scope entities.ResourceScope
	// TeamID filters by team ID (for team-scoped SlackBots)
	TeamID string
	// TeamIDs filters by multiple team IDs
	TeamIDs []string
}

// SlackBotRepository defines the interface for SlackBot data persistence
type SlackBotRepository interface {
	// Create creates a new SlackBot
	Create(ctx context.Context, slackBot *entities.SlackBot) error

	// Get retrieves a SlackBot by ID
	Get(ctx context.Context, id string) (*entities.SlackBot, error)

	// List retrieves SlackBots matching the filter
	List(ctx context.Context, filter SlackBotFilter) ([]*entities.SlackBot, error)

	// Update updates an existing SlackBot
	Update(ctx context.Context, slackBot *entities.SlackBot) error

	// Delete removes a SlackBot by ID
	Delete(ctx context.Context, id string) error
}
