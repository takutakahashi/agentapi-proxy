package services

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// MCPSecretSyncer syncs MCP server settings to Kubernetes Secrets
type MCPSecretSyncer interface {
	// Sync creates or updates the MCP servers secret based on settings
	// If settings has no MCP servers, the secret will be deleted
	Sync(ctx context.Context, settings *entities.Settings) error

	// Delete removes the MCP servers secret for the given name
	Delete(ctx context.Context, name string) error
}
