package repositories

import (
	"context"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// CachedSessionDTO is a serialisable snapshot of a Kubernetes session.
// It contains enough information to serve ListSessions responses from cache
// without making Kubernetes API calls.
type CachedSessionDTO struct {
	ID             string            `json:"id"`
	UserID         string            `json:"user_id"`
	Scope          string            `json:"scope"`
	TeamID         string            `json:"team_id"`
	Tags           map[string]string `json:"tags"`
	Status         string            `json:"status"`
	StartedAt      time.Time         `json:"started_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	LastMessageAt  time.Time         `json:"last_message_at"`
	Description    string            `json:"description"`
	Annotations    entities.SessionAnnotations `json:"annotations,omitempty"`
	ServicePort    int               `json:"service_port"`
	Namespace      string            `json:"namespace"`
	DeploymentName string            `json:"deployment_name"`
	ServiceName    string            `json:"service_name"`
	InitialMessage string            `json:"initial_message"`
	IsStock        bool              `json:"is_stock"`
}

// SessionListCacheRepository provides a short-lived cache layer for session
// list results.  Implementations are expected to be fast (Redis, in-process
// map, etc.) so that repeated calls to ListSessions do not generate Kubernetes
// API calls on every request.
type SessionListCacheRepository interface {
	// SetSessionListCache stores a slice of DTOs under cacheKey.
	// The entry expires after ttl.
	SetSessionListCache(ctx context.Context, cacheKey string, sessions []CachedSessionDTO, ttl time.Duration) error

	// GetSessionListCache retrieves the DTOs stored under cacheKey.
	// Returns (nil, nil) on a cache miss so callers can distinguish a miss from
	// an empty result.
	GetSessionListCache(ctx context.Context, cacheKey string) ([]CachedSessionDTO, error)

	// InvalidateSessionListCache deletes all session-list cache entries for the
	// given namespace.  Called by CreateSession and DeleteSession so stale lists
	// are not served after structural changes.
	InvalidateSessionListCache(ctx context.Context, namespace string) error

	// UpdateSessionInCache updates a single session in all cache entries for the namespace.
	// This is more efficient than invalidating the entire cache when only one session changes.
	// If the session doesn't exist in a cache entry, it's appended. If it exists, it's updated.
	UpdateSessionInCache(ctx context.Context, namespace string, session CachedSessionDTO) error

	// DeleteSessionFromCache removes a single session from all cache entries for the namespace.
	// This is more efficient than invalidating the entire cache when only one session is deleted.
	DeleteSessionFromCache(ctx context.Context, namespace string, sessionID string) error
}
