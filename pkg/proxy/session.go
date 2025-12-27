package proxy

import (
	"context"
	"time"
)

// Session represents a running agentapi session
type Session interface {
	// ID returns the unique session identifier
	ID() string

	// Addr returns the address (host:port) the session is running on
	// For local sessions, this returns "localhost:{port}"
	// For Kubernetes sessions, this returns "{service-dns}:{port}"
	Addr() string

	// UserID returns the user ID that owns this session
	UserID() string

	// Tags returns the session tags
	Tags() map[string]string

	// Status returns the current status of the session
	Status() string

	// StartedAt returns when the session was started
	StartedAt() time.Time

	// Description returns the session description
	// Returns tags["description"] if exists, otherwise returns InitialMessage
	Description() string

	// Cancel cancels the session context to trigger shutdown
	Cancel()
}

// SessionFilter defines filter criteria for listing sessions
type SessionFilter struct {
	UserID string
	Status string
	Tags   map[string]string
}

// SessionManager manages the lifecycle of sessions
type SessionManager interface {
	// CreateSession creates a new session and starts it
	CreateSession(ctx context.Context, id string, req *RunServerRequest) (Session, error)

	// GetSession returns a session by ID, nil if not found
	GetSession(id string) Session

	// ListSessions returns all sessions matching the filter
	ListSessions(filter SessionFilter) []Session

	// DeleteSession stops and removes a session
	DeleteSession(id string) error

	// Shutdown gracefully stops all sessions
	Shutdown(timeout time.Duration) error
}
