package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// CredentialsRepository defines the interface for credentials data persistence
type CredentialsRepository interface {
	// Save persists credentials (creates or updates)
	Save(ctx context.Context, credentials *entities.Credentials) error

	// FindByName retrieves credentials by name
	FindByName(ctx context.Context, name string) (*entities.Credentials, error)

	// Delete removes credentials by name
	Delete(ctx context.Context, name string) error

	// Exists checks if credentials exist for the given name
	Exists(ctx context.Context, name string) (bool, error)

	// List retrieves all credentials names
	List(ctx context.Context) ([]*entities.Credentials, error)
}
