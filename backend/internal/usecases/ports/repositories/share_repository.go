package repositories

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// ShareRepository defines the interface for share storage
type ShareRepository interface {
	// Save persists a session share
	Save(share *entities.SessionShare) error

	// FindByToken retrieves a share by its token
	FindByToken(token string) (*entities.SessionShare, error)

	// FindBySessionID retrieves a share by session ID
	FindBySessionID(sessionID string) (*entities.SessionShare, error)

	// Delete removes a share by session ID
	Delete(sessionID string) error

	// DeleteByToken removes a share by token
	DeleteByToken(token string) error

	// CleanupExpired removes all expired shares and returns the count of deleted shares
	CleanupExpired() (int, error)
}
