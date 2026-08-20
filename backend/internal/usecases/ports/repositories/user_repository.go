package repositories

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// UserRepository defines the interface for user data persistence
type UserRepository interface {
	// Save persists a user
	Save(ctx context.Context, user *entities.User) error

	// FindByID retrieves a user by their ID
	FindByID(ctx context.Context, id entities.UserID) (*entities.User, error)

	// FindByUsername retrieves a user by their username
	FindByUsername(ctx context.Context, username string) (*entities.User, error)

	// FindByEmail retrieves a user by their email
	FindByEmail(ctx context.Context, email string) (*entities.User, error)

	// FindByGitHubID retrieves a user by their GitHub ID
	FindByGitHubID(ctx context.Context, githubID int) (*entities.User, error)

	// FindByStatus retrieves users by status
	FindByStatus(ctx context.Context, status entities.UserStatus) ([]*entities.User, error)

	// FindByType retrieves users by type
	FindByType(ctx context.Context, userType entities.UserType) ([]*entities.User, error)

	// CountByStatus returns the number of users by status
	CountByStatus(ctx context.Context, status entities.UserStatus) (int, error)

	// FindAll retrieves all users
	FindAll(ctx context.Context) ([]*entities.User, error)

	// Update updates an existing user
	Update(ctx context.Context, user *entities.User) error

	// Delete removes a user
	Delete(ctx context.Context, id entities.UserID) error

	// Exists checks if a user exists
	Exists(ctx context.Context, id entities.UserID) (bool, error)

	// Count returns the total number of users
	Count(ctx context.Context) (int, error)

	// FindWithFilters retrieves users with filtering options
	FindWithFilters(ctx context.Context, filters UserFilters) ([]*entities.User, error)
}

// UserFilters defines filtering options for user queries
type UserFilters struct {
	Type      *entities.UserType
	Status    *entities.UserStatus
	Username  *string
	Email     *string
	Limit     int
	Offset    int
	SortBy    string
	SortOrder string // "asc" or "desc"
}
