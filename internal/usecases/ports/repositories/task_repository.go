package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// TaskFilter defines filter criteria for listing tasks.
// All fields are AND-combined unless otherwise noted.
type TaskFilter struct {
	// Scope restricts to "user" or "team" entries (empty = both)
	Scope entities.ResourceScope

	// OwnerID restricts to tasks owned by a specific user (used for user-scope queries)
	OwnerID string

	// TeamID restricts to tasks belonging to a specific team (used for team-scope queries)
	TeamID string

	// TeamIDs restricts to tasks for any of these teams (OR logic).
	// Used when listing all team tasks for a user who belongs to multiple teams.
	// When TeamIDs is set, TeamID is ignored.
	TeamIDs []string

	// GroupID restricts to tasks belonging to a specific group (empty = all groups)
	GroupID string

	// Status restricts to tasks with the given status (empty = all statuses)
	Status entities.TaskStatus

	// TaskType restricts to tasks with the given type (empty = all types)
	TaskType entities.TaskType
}

// TaskRepository defines the interface for task persistence.
type TaskRepository interface {
	// Create persists a new task.
	// Returns an error if a task with the same ID already exists.
	Create(ctx context.Context, task *entities.Task) error

	// GetByID retrieves a task by its UUID.
	// Returns ErrTaskNotFound if the task does not exist.
	GetByID(ctx context.Context, id string) (*entities.Task, error)

	// List retrieves tasks matching the filter.
	List(ctx context.Context, filter TaskFilter) ([]*entities.Task, error)

	// Update replaces the content of an existing task.
	// Returns ErrTaskNotFound if the task does not exist.
	Update(ctx context.Context, task *entities.Task) error

	// Delete removes a task by ID.
	// Returns ErrTaskNotFound if the task does not exist.
	Delete(ctx context.Context, id string) error
}

// TaskGroupFilter defines filter criteria for listing task groups.
type TaskGroupFilter struct {
	// Scope restricts to "user" or "team" entries (empty = both)
	Scope entities.ResourceScope

	// OwnerID restricts to groups owned by a specific user (used for user-scope queries)
	OwnerID string

	// TeamID restricts to groups belonging to a specific team (used for team-scope queries)
	TeamID string

	// TeamIDs restricts to groups for any of these teams (OR logic).
	// When TeamIDs is set, TeamID is ignored.
	TeamIDs []string
}

// TaskGroupRepository defines the interface for task group persistence.
type TaskGroupRepository interface {
	// Create persists a new task group.
	// Returns an error if a group with the same ID already exists.
	Create(ctx context.Context, group *entities.TaskGroup) error

	// GetByID retrieves a task group by its UUID.
	// Returns ErrTaskGroupNotFound if the group does not exist.
	GetByID(ctx context.Context, id string) (*entities.TaskGroup, error)

	// List retrieves task groups matching the filter.
	List(ctx context.Context, filter TaskGroupFilter) ([]*entities.TaskGroup, error)

	// Update replaces the content of an existing task group.
	// Returns ErrTaskGroupNotFound if the group does not exist.
	Update(ctx context.Context, group *entities.TaskGroup) error

	// Delete removes a task group by ID.
	// Returns ErrTaskGroupNotFound if the group does not exist.
	Delete(ctx context.Context, id string) error
}
