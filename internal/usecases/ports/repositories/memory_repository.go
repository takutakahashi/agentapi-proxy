package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// MemoryFilter defines filter criteria for listing memory entries.
// All fields are AND-combined unless otherwise noted.
type MemoryFilter struct {
	// Scope restricts to "user" or "team" entries (empty = both)
	Scope entities.ResourceScope

	// OwnerID restricts to entries owned by a specific user (used for user-scope queries)
	OwnerID string

	// TeamID restricts to entries belonging to a specific team (used for team-scope queries)
	TeamID string

	// TeamIDs restricts to entries for any of these teams (OR logic).
	// Used when listing all team memories for a user who belongs to multiple teams.
	// When TeamIDs is set, TeamID is ignored.
	TeamIDs []string

	// Tags filters entries that contain ALL of the given key-value pairs
	Tags map[string]string

	// Query is a case-insensitive full-text search string applied to title and content
	Query string
}

// MemoryRepository defines the interface for memory entry persistence.
// Implementations must be swappable (e.g. Kubernetes ConfigMap, in-memory for testing).
type MemoryRepository interface {
	// Create persists a new memory entry.
	// Returns an error if an entry with the same ID already exists.
	Create(ctx context.Context, memory *entities.Memory) error

	// GetByID retrieves a memory entry by its UUID.
	// Returns ErrMemoryNotFound if the entry does not exist.
	GetByID(ctx context.Context, id string) (*entities.Memory, error)

	// List retrieves memory entries matching the filter.
	// All filter fields are AND-combined. Tag filtering and full-text search
	// may be applied in-process after fetching candidates via storage-level filters.
	List(ctx context.Context, filter MemoryFilter) ([]*entities.Memory, error)

	// Update replaces the content of an existing memory entry.
	// Returns ErrMemoryNotFound if the entry does not exist.
	Update(ctx context.Context, memory *entities.Memory) error

	// Delete removes a memory entry by ID.
	// Returns ErrMemoryNotFound if the entry does not exist.
	Delete(ctx context.Context, id string) error
}
