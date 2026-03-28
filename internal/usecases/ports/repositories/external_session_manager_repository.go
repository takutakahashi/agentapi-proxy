package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// ExternalSessionManagerFilter defines filter criteria for listing external session managers
type ExternalSessionManagerFilter struct {
	UserID  string
	Scope   entities.ResourceScope
	TeamID  string
	TeamIDs []string // caller's team memberships for team-scope visibility
}

// ExternalSessionManagerRepository defines the interface for external session manager persistence
type ExternalSessionManagerRepository interface {
	// Create persists a new external session manager
	Create(ctx context.Context, esm *entities.ExternalSessionManager) error

	// Get retrieves an external session manager by ID
	Get(ctx context.Context, id string) (*entities.ExternalSessionManager, error)

	// List retrieves external session managers matching the filter
	List(ctx context.Context, filter ExternalSessionManagerFilter) ([]*entities.ExternalSessionManager, error)

	// Update updates an existing external session manager
	Update(ctx context.Context, esm *entities.ExternalSessionManager) error

	// Delete removes an external session manager by ID
	Delete(ctx context.Context, id string) error
}
