package services

import (
	"context"
	"errors"
	"time"

	coresessioncontrol "github.com/takutakahashi/agentapi-proxy/internal/core/sessioncontrol"
	"testing"
)

type compatibilityControlStore struct {
	connected bool
	err       error
}

func (s *compatibilityControlStore) TouchConnection(context.Context, string) error { return s.err }
func (s *compatibilityControlStore) IsConnected(context.Context, string) (bool, error) {
	return s.connected, s.err
}
func (s *compatibilityControlStore) EnqueueCommand(context.Context, string, coresessioncontrol.Command) (string, error) {
	return "", nil
}
func (s *compatibilityControlStore) ReadCommands(context.Context, string, string, time.Duration, int64) ([]coresessioncontrol.Command, error) {
	return nil, nil
}
func (s *compatibilityControlStore) AckCommand(context.Context, string, string) error { return nil }
func (s *compatibilityControlStore) AppendEvents(context.Context, string, []coresessioncontrol.Event) (string, error) {
	return "", nil
}
func (s *compatibilityControlStore) ReadEvents(context.Context, string, string, time.Duration, int64) ([]coresessioncontrol.Event, error) {
	return nil, nil
}

func TestConnectedSessionControlStoreRequiresActiveLease(t *testing.T) {
	store := &compatibilityControlStore{}
	manager := &KubernetesSessionManager{sessionControlStore: store}
	if got := manager.connectedSessionControlStore(context.Background(), "old-session"); got != nil {
		t.Fatal("old session without a control lease must use direct transport")
	}

	store.connected = true
	if got := manager.connectedSessionControlStore(context.Background(), "new-session"); got != store {
		t.Fatal("session with an active control lease must use long polling")
	}

	store.err = errors.New("redis unavailable")
	if got := manager.connectedSessionControlStore(context.Background(), "new-session"); got != nil {
		t.Fatal("connection lookup failure must fall back to direct transport")
	}
}
