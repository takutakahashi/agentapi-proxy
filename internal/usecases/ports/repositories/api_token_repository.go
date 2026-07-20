package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// APITokenRepository defines persistence for named API tokens.
//
// All lookup-by-ID methods return entities.ErrAPITokenNotFound when a token
// does not exist (or, for cross-scope safety, when the caller is not allowed
// to observe it). Create returns entities.ErrAPITokenAlreadyExists when a
// token with the given ID already exists, enabling idempotent migration.
type APITokenRepository interface {
	// Create persists a new token. Returns ErrAPITokenAlreadyExists if a
	// token with the same ID already exists (idempotent-safe create).
	Create(ctx context.Context, token *entities.APIToken) error

	// GetByID retrieves a token by its public ID.
	GetByID(ctx context.Context, tokenID string) (*entities.APIToken, error)

	// GetBySecret retrieves a token by its plaintext secret. Used by the
	// auth service for authentication lookups.
	GetBySecret(ctx context.Context, secret string) (*entities.APIToken, error)

	// ListByOwner lists tokens for a personal owner (scope == user).
	ListByOwner(ctx context.Context, userID entities.UserID) ([]*entities.APIToken, error)

	// ListByTeam lists tokens for a team (scope == team).
	ListByTeam(ctx context.Context, teamID string) ([]*entities.APIToken, error)

	// ListAll lists every token. Used by bootstrap/migration.
	ListAll(ctx context.Context) ([]*entities.APIToken, error)

	// Delete removes a token by ID. It is idempotent: returning nil when the
	// token does not exist is safe.
	Delete(ctx context.Context, tokenID string) error
}
