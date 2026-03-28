package repositories

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// SessionRoute holds routing information for proxying session requests to an external session manager (Proxy B)
type SessionRoute struct {
	SessionID       string // Proxy A's session ID (user-facing)
	RemoteSessionID string // Proxy B's session ID
	ProxyURL        string // Base URL of Proxy B (e.g. "http://proxy-b:8080")
	HMACSecret      string // HMAC secret for authenticating requests to Proxy B
}

// RemoteProvisionSettingsBuilder is an optional interface that SessionManager implementations
// may provide to build fully-resolved provision settings for forwarding to an external session
// manager (Proxy B). Proxy A calls this to embed all resolved settings (env vars, Bedrock config,
// MCP servers, OAuth token, etc.) so that Proxy B can create the session without needing to
// re-resolve secrets from its own cluster.
type RemoteProvisionSettingsBuilder interface {
	// BuildRemoteProvisionSettings resolves the full session settings (base → team → user layers)
	// and returns a SessionSettings struct with all values embedded, ready for forwarding.
	BuildRemoteProvisionSettings(ctx context.Context, sessionID string, req *entities.RunServerRequest) (*sessionsettings.SessionSettings, error)
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
