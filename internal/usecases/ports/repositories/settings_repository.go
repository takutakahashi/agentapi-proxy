package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SettingsRepository defines the interface for settings data persistence
type SettingsRepository interface {
	// Save persists settings (creates or updates)
	Save(ctx context.Context, settings *entities.Settings) error

	// FindByName retrieves settings by name
	FindByName(ctx context.Context, name string) (*entities.Settings, error)

	// Delete removes settings by name
	Delete(ctx context.Context, name string) error

	// Exists checks if settings exist for the given name
	Exists(ctx context.Context, name string) (bool, error)

	// List retrieves all settings
	List(ctx context.Context) ([]*entities.Settings, error)
}
