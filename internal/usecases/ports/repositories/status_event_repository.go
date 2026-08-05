package repositories

import (
	"context"
	"time"
)

// StatusChangeEvent represents a cross-pod session status change notification.
type StatusChangeEvent struct {
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
	PodID     string    `json:"pod_id"`
}

// StatusEventRepository provides cross-pod session status synchronisation via a
// shared pub/sub backend (e.g. Redis).
//
// Implementations must be safe for concurrent use.
type StatusEventRepository interface {
	// SetStatus persists the runtime status for sessionID so other pods can read it.
	// podID is the identifier of the calling pod (used for deduplication on receive).
	SetStatus(ctx context.Context, sessionID, status, podID string) error

	// GetStatus retrieves the latest known runtime status for sessionID from the
	// shared store.  Returns ("", time.Time{}, nil) when no entry exists.
	GetStatus(ctx context.Context, sessionID string) (status string, updatedAt time.Time, err error)

	// PublishStatusChange broadcasts a StatusChangeEvent to all pods subscribing
	// to the global status-change channel.
	PublishStatusChange(ctx context.Context, event StatusChangeEvent) error

	// SubscribeGlobal returns a channel that receives StatusChangeEvent values
	// published by any pod.  The channel is closed when ctx is cancelled.
	// Callers must drain or discard the channel promptly; slow consumers will
	// drop events rather than block the publisher.
	SubscribeGlobal(ctx context.Context) (<-chan StatusChangeEvent, error)

	// DeleteStatus removes the status entry for sessionID from the shared store.
	// Called during session deletion to free resources.
	DeleteStatus(ctx context.Context, sessionID string) error
}
