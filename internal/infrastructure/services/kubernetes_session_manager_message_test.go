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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestIsACPAgentTypeIncludesCursor(t *testing.T) {
	if !isACPAgentType("cursor") {
		t.Fatal("expected cursor to be treated as an ACP agent type")
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

func TestStopAgentUsesServiceAgentTypeFallbackForRestoredACPSession(t *testing.T) {
	m := newTestManagerForCycle(t)
	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "user1"},
		"test-deploy", "agentapi-session-test-session-svc", "test-pvc", "test-ns",
		9000, nil, nil,
	)
	session.SetStatusSilent("active")
	m.mutex.Lock()
	m.sessions["test-session"] = session
	m.mutex.Unlock()

	_, err := m.client.CoreV1().Services("test-ns").Create(context.Background(), &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agentapi-session-test-session-svc",
			Annotations: map[string]string{
				"agentapi.proxy/agent-type": "codex-acp",
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create service error = %v", err)
	}

	var requestPath string
	withRoundTripper(t, func(req *http.Request) (*http.Response, error) {
		requestPath = req.URL.Path
		return jsonResponse(http.StatusOK), nil
	})

	if err := m.StopAgent(context.Background(), "test-session"); err != nil {
		t.Fatalf("StopAgent() error = %v", err)
	}
	if requestPath != "/rpc" {
		t.Fatalf("expected /rpc from Service agent-type fallback, got %q", requestPath)
	}
}

func TestRestoreSessionFromServiceRestoresAgentType(t *testing.T) {
	m := newTestManagerForCycle(t)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "agentapi-session-test-session-svc",
			Labels: map[string]string{
				"agentapi.proxy/session-id": "test-session",
				"agentapi.proxy/user-id":    "user1",
				"agentapi.proxy/scope":      "user",
				"agentapi.proxy/agent-type": "codex-acp",
			},
			Annotations: map[string]string{
				"agentapi.proxy/agent-type": "codex-acp",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 9000}},
		},
	}

	session := m.restoreSessionFromService(svc)
	if session == nil || session.Request() == nil {
		t.Fatalf("expected restored session, got %#v", session)
	}
	if session.Request().AgentType != "codex-acp" {
		t.Fatalf("expected AgentType codex-acp, got %q", session.Request().AgentType)
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
