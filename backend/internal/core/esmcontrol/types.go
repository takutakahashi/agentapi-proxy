package esmcontrol

import (
	"context"
	"time"
)

type Command struct {
	ID              string              `json:"id"`
	CancelRequestID string              `json:"cancel_request_id,omitempty"`
	StreamID        string              `json:"stream_id,omitempty"`
	ManagerID       string              `json:"manager_id"`
	SessionID       string              `json:"session_id"`
	RemoteSessionID string              `json:"remote_session_id"`
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	RawQuery        string              `json:"raw_query,omitempty"`
	Headers         map[string][]string `json:"headers,omitempty"`
	Body            []byte              `json:"body,omitempty"`
	Deadline        time.Time           `json:"deadline"`
	CreatedAt       time.Time           `json:"created_at"`
}

type ResponseFrame struct {
	ID              string              `json:"id"`
	StreamID        string              `json:"stream_id,omitempty"`
	RequestID       string              `json:"request_id"`
	CommandStreamID string              `json:"command_stream_id,omitempty"`
	Sequence        int64               `json:"sequence"`
	Status          int                 `json:"status,omitempty"`
	Headers         map[string][]string `json:"headers,omitempty"`
	Body            []byte              `json:"body,omitempty"`
	Done            bool                `json:"done,omitempty"`
	Error           string              `json:"error,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
}

type Store interface {
	TouchManager(ctx context.Context, managerID, instanceID string) error
	IsManagerConnected(ctx context.Context, managerID string) (bool, error)
	EnqueueCommand(ctx context.Context, managerID string, command Command) (string, error)
	ReadCommands(ctx context.Context, managerID, after string, wait time.Duration, count int64) ([]Command, error)
	AckCommand(ctx context.Context, managerID, streamID string) error
	AppendFrames(ctx context.Context, requestID string, frames []ResponseFrame) (string, error)
	ReadFrames(ctx context.Context, requestID, after string, wait time.Duration, count int64) ([]ResponseFrame, error)
	RequestBelongsToManager(ctx context.Context, requestID, managerID string) (bool, error)
}
