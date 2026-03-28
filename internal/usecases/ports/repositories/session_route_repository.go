package repositories

import (
	"context"
	"time"
)

// SessionRoute holds routing information for proxying session requests to an external session manager (Proxy B).
// It also stores metadata used when listing ESM-created sessions.
type SessionRoute struct {
	SessionID       string // Proxy A's session ID (user-facing)
	RemoteSessionID string // Proxy B's session ID
	ProxyURL        string // Base URL of Proxy B (e.g. "http://proxy-b:8080")
	HMACSecret      string // HMAC secret for authenticating requests to Proxy B
	// Metadata for session listing
	UserID    string
	Scope     string
	TeamID    string
	Tags      map[string]string
	StartedAt time.Time
}

// SessionRouteRepository persists and retrieves session routing information
type SessionRouteRepository interface {
	// Save creates or updates a session route entry
	Save(ctx context.Context, route *SessionRoute) error
	// Get retrieves routing information for the given session ID; returns nil, nil if not found
	Get(ctx context.Context, sessionID string) (*SessionRoute, error)
	// List retrieves all session routes; if userID is non-empty, only routes for that user are returned
	List(ctx context.Context, userID string) ([]*SessionRoute, error)
	// Delete removes routing information for the given session ID
	Delete(ctx context.Context, sessionID string) error
}
