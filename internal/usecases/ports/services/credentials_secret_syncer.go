package services

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// CredentialsSecretSyncer defines the interface for syncing settings to credentials secrets
type CredentialsSecretSyncer interface {
	// Sync creates or updates the credentials secret based on settings
	// The credentials secret contains environment variables derived from settings (e.g., Bedrock configuration)
	Sync(ctx context.Context, settings *entities.Settings) error

	// Delete removes the credentials secret for the given name
	Delete(ctx context.Context, name string) error
}
