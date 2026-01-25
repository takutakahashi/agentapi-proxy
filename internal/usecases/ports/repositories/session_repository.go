package repositories

import (
	"context"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SessionManager manages the lifecycle of sessions
type SessionManager interface {
	// CreateSession creates a new session and starts it
	CreateSession(ctx context.Context, id string, req *entities.RunServerRequest) (entities.Session, error)

	// GetSession returns a session by ID, nil if not found
	GetSession(id string) entities.Session

	// ListSessions returns all sessions matching the filter
	ListSessions(filter entities.SessionFilter) []entities.Session

	// DeleteSession stops and removes a session
	DeleteSession(id string) error

	// SendMessage sends a message to an existing session
	SendMessage(ctx context.Context, id string, message string) error

	// Shutdown gracefully stops all sessions
	Shutdown(timeout time.Duration) error
}
