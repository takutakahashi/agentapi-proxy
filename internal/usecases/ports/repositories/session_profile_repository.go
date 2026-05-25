package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SessionProfileFilter defines filter criteria for listing session profiles
type SessionProfileFilter struct {
	// UserID filters by user ID
	UserID string
	// Scope filters by resource scope
	Scope entities.ResourceScope
	// TeamID filters by team ID
	TeamID string
	// TeamIDs filters by multiple team IDs
	TeamIDs []string
	// ManagedOnly when true returns only managed (is_managed=true) profiles
	ManagedOnly bool
}

// SessionProfileRepository defines the interface for session profile data persistence
type SessionProfileRepository interface {
	// Create creates a new session profile
	Create(ctx context.Context, profile *entities.SessionProfile) error

	// Get retrieves a session profile by ID
	Get(ctx context.Context, id string) (*entities.SessionProfile, error)

	// List retrieves session profiles matching the filter
	List(ctx context.Context, filter SessionProfileFilter) ([]*entities.SessionProfile, error)

	// Update updates an existing session profile
	Update(ctx context.Context, profile *entities.SessionProfile) error

	// Delete removes a session profile by ID
	Delete(ctx context.Context, id string) error
}
