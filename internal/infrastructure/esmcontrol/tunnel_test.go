package esmcontrol

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
)

type tunnelStore struct {
	connected bool
	command   core.Command
	commands  []core.Command
	frames    []core.ResponseFrame
}

func (s *tunnelStore) TouchManager(context.Context, string, string) error { return nil }
func (s *tunnelStore) IsManagerConnected(context.Context, string) (bool, error) {
	return s.connected, nil
}
func (s *tunnelStore) EnqueueCommand(_ context.Context, _ string, command core.Command) (string, error) {
	s.command = command
	s.commands = append(s.commands, command)
	for i := range s.frames {
		s.frames[i].RequestID = command.ID
	}
	return "1-0", nil
}
func (s *tunnelStore) ReadCommands(context.Context, string, string, time.Duration, int64) ([]core.Command, error) {
	return nil, nil
}
func (s *tunnelStore) AckCommand(context.Context, string, string) error { return nil }
func (s *tunnelStore) AppendFrames(context.Context, string, []core.ResponseFrame) (string, error) {
	return "", nil
}
func (s *tunnelStore) ReadFrames(_ context.Context, _ string, after string, _ time.Duration, _ int64) ([]core.ResponseFrame, error) {
	if after != "0-0" {
		return nil, nil
	}
	result := append([]core.ResponseFrame(nil), s.frames...)
	for i := range result {
		result[i].StreamID = string(rune('1'+i)) + "-0"
	}
	return result, nil
}
func (s *tunnelStore) RequestBelongsToManager(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestTunnelStreamsResponseFrames(t *testing.T) {
	store := &tunnelStore{connected: true, frames: []core.ResponseFrame{
		{Status: http.StatusOK, Headers: map[string][]string{"Content-Type": {"text/plain"}}},
		{Body: []byte("hello ")}, {Body: []byte("world"), Done: true},
	}}
	req, _ := http.NewRequest(http.MethodPost, "http://esm.local/remote/message?q=1", strings.NewReader("prompt"))
	req.Header.Set("Authorization", "Bearer user-secret")
	resp, err := NewTunnel(store).Do(context.Background(), "manager-a", "public", "remote", req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "hello world" {
		t.Fatalf("body = %q", got)
	}
	if store.command.ManagerID != "manager-a" || store.command.SessionID != "public" || store.command.Path != "/remote/message" || string(store.command.Body) != "prompt" {
		t.Fatalf("unexpected command: %#v", store.command)
	}
	if store.command.Headers["Authorization"] != nil {
		t.Fatal("user authorization header must not be stored in Redis command")
	}
}

func TestTunnelRejectsDisconnectedManager(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "http://esm.local/status", nil)
	_, err := NewTunnel(&tunnelStore{}).Do(context.Background(), "manager-a", "public", "remote", req)
	if err == nil {
		t.Fatal("expected disconnected manager error")
	}
}

func TestTunnelCloseEnqueuesCancellation(t *testing.T) {
	store := &tunnelStore{connected: true, frames: []core.ResponseFrame{{Status: http.StatusOK}}}
	req, _ := http.NewRequest(http.MethodGet, "http://esm.local/events", nil)
	resp, err := NewTunnel(store).Do(context.Background(), "manager-a", "public", "remote", req)
	if err != nil {
		t.Fatal(err)
	}
	requestID := store.commands[0].ID
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if len(store.commands) != 2 || store.commands[1].CancelRequestID != requestID {
		t.Fatalf("cancellation command not enqueued: %#v", store.commands)
	}
}
