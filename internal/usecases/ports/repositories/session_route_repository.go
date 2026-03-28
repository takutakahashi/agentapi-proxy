package repositories

import "context"

// SessionRoute holds routing information for proxying session requests to an external session manager (Proxy B)
type SessionRoute struct {
	SessionID       string // Proxy A's session ID (user-facing)
	RemoteSessionID string // Proxy B's session ID
	ProxyURL        string // Base URL of Proxy B (e.g. "http://proxy-b:8080")
	HMACSecret      string // HMAC secret for authenticating requests to Proxy B
}

// SessionRouteRepository persists and retrieves session routing information
type SessionRouteRepository interface {
	// Save creates or updates a session route entry
	Save(ctx context.Context, route *SessionRoute) error
	// Get retrieves routing information for the given session ID; returns nil, nil if not found
	Get(ctx context.Context, sessionID string) (*SessionRoute, error)
	// Delete removes routing information for the given session ID
	Delete(ctx context.Context, sessionID string) error
}
