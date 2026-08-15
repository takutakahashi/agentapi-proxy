package repositories

import (
	"context"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// Message represents a message in a conversation
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

type SessionStatusEvent struct {
	SessionID string    `json:"session_id"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

type SessionMessageEvent struct {
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
}

type SessionStatusWatcher interface {
	SubscribeStatusEvents() (<-chan SessionStatusEvent, func())
}

type SessionMessageWatcher interface {
	SubscribeMessageEvents(sessionID string) (<-chan SessionMessageEvent, func())
}

// SessionManager manages the lifecycle of sessions
type SessionManager interface {
	// CreateSession creates a new session and starts it
	CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error)

	// GetSession returns a session by ID, nil if not found
	GetSession(id string) entities.Session

	// ListSessions returns all sessions matching the filter
	ListSessions(filter entities.SessionFilter) []entities.Session

	// DeleteSession stops and removes a session
	DeleteSession(id string) error

	// SendMessage sends a message to an existing session
	SendMessage(ctx context.Context, id string, message string) error

	// StopAgent sends a stop signal (Ctrl+C) to the running agent in the session
	StopAgent(ctx context.Context, id string) error

	// GetMessages retrieves conversation history from a session
	GetMessages(ctx context.Context, id string) ([]Message, error)

	// Shutdown gracefully stops all sessions
	Shutdown(timeout time.Duration) error
}

// SessionWorkloadEnsurer is implemented by managers that can lazily recreate
// a missing runtime while retaining the same logical session ID.
type SessionWorkloadEnsurer interface {
	// EnsureSessionWorkload returns restoring=true while the workload is being
	// created or is not ready yet. A missing canonical session is an error.
	EnsureSessionWorkload(ctx context.Context, id string) (session entities.Session, restoring bool, err error)
}

// SandboxDomains is execution-plane network-filter state for a session.
type SandboxDomains struct {
	Allowed []string `json:"allowed"`
	Denied  []string `json:"denied"`
}

// SessionSandboxDomainReader is implemented by session managers that can
// query their runtime's network filter.
type SessionSandboxDomainReader interface {
	GetSessionSandboxDomains(ctx context.Context, id string) (*SandboxDomains, error)
}

// SessionToucher updates execution-plane activity timestamps after a proxied
// message. The API calls this capability instead of mutating Kubernetes state.
type SessionToucher interface {
	TouchSession(ctx context.Context, id string, at time.Time) error
}
