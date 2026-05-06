package services

import (
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// TestBroadcastMessageUpdate_UpdatesLastMessageAt verifies that broadcastMessageUpdate
// updates the session's lastMessageAt field so late-arriving pollers can detect missed events.
func TestBroadcastMessageUpdate_UpdatesLastMessageAt(t *testing.T) {
	m := newTestManagerForCycle(t)

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "user1"},
		"test-deploy", "test-svc", "test-pvc", "test-ns",
		9000, nil, nil,
	)
	before := time.Now()
	// Set lastMessageAt to a known past time.
	session.SetLastMessageAt(before.Add(-1 * time.Hour))

	m.mutex.Lock()
	m.sessions["test-session"] = session
	m.mutex.Unlock()

	m.broadcastMessageUpdate("test-session")

	if !session.LastMessageAt().After(before.Add(-1 * time.Hour)) {
		t.Errorf("lastMessageAt was not updated: got %v, want after %v",
			session.LastMessageAt(), before.Add(-1*time.Hour))
	}
	if session.LastMessageAt().Before(before) {
		t.Errorf("lastMessageAt %v should be >= broadcast time %v", session.LastMessageAt(), before)
	}
}

// TestSubscribeMessageEvents_ReceivesBroadcast verifies that a subscriber
// registered before broadcastMessageUpdate is called receives the event.
func TestSubscribeMessageEvents_ReceivesBroadcast(t *testing.T) {
	m := newTestManagerForCycle(t)

	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "user1"},
		"test-deploy", "test-svc", "test-pvc", "test-ns",
		9000, nil, nil,
	)
	m.mutex.Lock()
	m.sessions["test-session"] = session
	m.mutex.Unlock()

	ch, cancel := m.SubscribeMessageEvents("test-session")
	defer cancel()

	m.broadcastMessageUpdate("test-session")

	select {
	case evt := <-ch:
		if evt.SessionID != "test-session" {
			t.Errorf("unexpected session ID: %s", evt.SessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message event")
	}
}
