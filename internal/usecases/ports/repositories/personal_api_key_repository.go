package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// PersonalAPIKeyRepository defines the interface for personal API key persistence
type PersonalAPIKeyRepository interface {
	// FindByUserID retrieves a personal API key by user ID
	FindByUserID(ctx context.Context, userID entities.UserID) (*entities.PersonalAPIKey, error)

	// Save persists a personal API key
	Save(ctx context.Context, apiKey *entities.PersonalAPIKey) error

	// Delete removes a personal API key
	Delete(ctx context.Context, userID entities.UserID) error
}
