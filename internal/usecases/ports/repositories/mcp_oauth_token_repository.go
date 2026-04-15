package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// MCPOAuthTokenRepository persists OAuth tokens for user × MCP server pairs.
type MCPOAuthTokenRepository interface {
	// Save creates or replaces the token for the given user × server.
	Save(ctx context.Context, token *entities.MCPOAuthToken) error

	// FindByUserAndServer returns the stored token, or nil when not found.
	FindByUserAndServer(ctx context.Context, userID, serverName string) (*entities.MCPOAuthToken, error)

	// Delete removes the token (called when the user disconnects a server).
	Delete(ctx context.Context, userID, serverName string) error

	// ListByUser returns all tokens stored for a user.
	ListByUser(ctx context.Context, userID string) ([]*entities.MCPOAuthToken, error)
}
