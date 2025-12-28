package services

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// MarketplaceSecretSyncer syncs marketplace settings to Kubernetes Secrets
type MarketplaceSecretSyncer interface {
	// Sync creates or updates the marketplace secret based on settings
	// If settings has no marketplaces, the secret will be deleted
	Sync(ctx context.Context, settings *entities.Settings) error

	// Delete removes the marketplace secret for the given name
	Delete(ctx context.Context, name string) error
}
