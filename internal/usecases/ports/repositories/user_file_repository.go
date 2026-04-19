package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// UserFileRepository defines the interface for user-managed file persistence.
// Files are stored per-user and embedded into sessions at creation time.
type UserFileRepository interface {
	// Save creates or updates a file for the given user.
	Save(ctx context.Context, userID string, file *entities.UserFile) error

	// FindByID retrieves a single file by its ID for the given user.
	FindByID(ctx context.Context, userID string, fileID string) (*entities.UserFile, error)

	// List returns all files for the given user.
	List(ctx context.Context, userID string) ([]*entities.UserFile, error)

	// Delete removes a file by its ID for the given user.
	Delete(ctx context.Context, userID string, fileID string) error
}
