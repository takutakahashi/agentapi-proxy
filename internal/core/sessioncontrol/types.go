package sessioncontrol

import (
	"context"
	"encoding/json"
	"time"
)

type Command struct {
	ID        string          `json:"id"`
	StreamID  string          `json:"stream_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type Event struct {
	ID              string          `json:"id"`
	StreamID        string          `json:"stream_id,omitempty"`
	Type            string          `json:"type"`
	CommandID       string          `json:"command_id,omitempty"`
	CommandStreamID string          `json:"command_stream_id,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

type Store interface {
	TouchConnection(ctx context.Context, sessionID string) error
	IsConnected(ctx context.Context, sessionID string) (bool, error)
	EnqueueCommand(ctx context.Context, sessionID string, command Command) (string, error)
	ReadCommands(ctx context.Context, sessionID, after string, wait time.Duration, count int64) ([]Command, error)
	AckCommand(ctx context.Context, sessionID, streamID string) error
	AppendEvents(ctx context.Context, sessionID string, events []Event) (string, error)
	ReadEvents(ctx context.Context, sessionID, after string, wait time.Duration, count int64) ([]Event, error)
}
