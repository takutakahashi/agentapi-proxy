package services

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
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

func TestStopAgentUsesACPCancelForACPSessions(t *testing.T) {
	m := newTestManagerForCycle(t)
	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "user1", AgentType: "codex-acp"},
		"test-deploy", "test-svc", "test-pvc", "test-ns",
		9000, nil, nil,
	)
	session.SetStatusSilent("active")
	m.mutex.Lock()
	m.sessions["test-session"] = session
	m.mutex.Unlock()

	var requestPath string
	var payload map[string]interface{}
	withRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		requestPath = req.URL.Path
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Unmarshal() error = %v", err)
		}
		return jsonResponse(http.StatusOK), nil
	})

	if err := m.StopAgent(context.Background(), "test-session"); err != nil {
		t.Fatalf("StopAgent() error = %v", err)
	}
	if requestPath != "/rpc" {
		t.Fatalf("expected /rpc, got %q", requestPath)
	}
	if payload["jsonrpc"] != "2.0" || payload["method"] != "session/cancel" {
		t.Fatalf("unexpected ACP cancel payload: %#v", payload)
	}
	params, ok := payload["params"].(map[string]interface{})
	if !ok || params["sessionId"] != "test-session" {
		t.Fatalf("unexpected params: %#v", payload["params"])
	}
}

func TestStopAgentUsesActionForAgentAPISessions(t *testing.T) {
	m := newTestManagerForCycle(t)
	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "user1", AgentType: "claude-agentapi"},
		"test-deploy", "test-svc", "test-pvc", "test-ns",
		9000, nil, nil,
	)
	session.SetStatusSilent("active")
	m.mutex.Lock()
	m.sessions["test-session"] = session
	m.mutex.Unlock()

	var requestPath string
	var body string
	withRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		requestPath = req.URL.Path
		data, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		body = string(data)
		return jsonResponse(http.StatusOK), nil
	})

	if err := m.StopAgent(context.Background(), "test-session"); err != nil {
		t.Fatalf("StopAgent() error = %v", err)
	}
	if requestPath != "/action" {
		t.Fatalf("expected /action, got %q", requestPath)
	}
	if !strings.Contains(body, `"type":"stop_agent"`) {
		t.Fatalf("unexpected action payload: %s", body)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func withRoundTripper(t *testing.T, f roundTripFunc) {
	t.Helper()
	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: f}
	t.Cleanup(func() {
		http.DefaultClient = originalClient
	})
}

func jsonResponse(statusCode int) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
		Header:     make(http.Header),
	}
}
