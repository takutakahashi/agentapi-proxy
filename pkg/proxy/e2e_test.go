package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// StartResponse represents the response from starting a session
type StartResponse struct {
	SessionID string `json:"session_id"`
}

// SessionInfo represents session information for search results
type SessionInfo struct {
	SessionID string            `json:"session_id"`
	UserID    string            `json:"user_id"`
	Status    string            `json:"status"`
	StartedAt time.Time         `json:"started_at"`
	Port      int               `json:"port"`
	Tags      map[string]string `json:"tags"`
}

// SearchResponse represents the response from searching sessions
type SearchResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// mockAgentAPIServer simulates an agentapi server for testing
type mockAgentAPIServer struct {
	server   *httptest.Server
	messages []map[string]interface{}
	mu       sync.Mutex
}

func newMockAgentAPIServer() *mockAgentAPIServer {
	m := &mockAgentAPIServer{
		messages: make([]map[string]interface{}, 0),
	}

	// Create Echo instance for the mock server
	e := echo.New()

	// Health endpoint
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Messages endpoint
	e.GET("/messages", func(c echo.Context) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		return c.JSON(http.StatusOK, m.messages)
	})

	// Message endpoint
	e.POST("/message", func(c echo.Context) error {
		var msg map[string]interface{}
		if err := c.Bind(&msg); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid message"})
		}

		m.mu.Lock()
		m.messages = append(m.messages, msg)
		m.mu.Unlock()

		// Simulate response
		response := map[string]interface{}{
			"id":      fmt.Sprintf("msg-%d", len(m.messages)),
			"content": fmt.Sprintf("Response to: %v", msg["content"]),
			"type":    "assistant",
		}

		return c.JSON(http.StatusOK, response)
	})

	// Status endpoint
	e.GET("/status", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"status":         "ready",
			"messages_count": len(m.messages),
		})
	})

	m.server = httptest.NewServer(e)
	return m
}

func (m *mockAgentAPIServer) URL() string {
	return m.server.URL
}

func (m *mockAgentAPIServer) Port() int {
	// Extract port from server URL
	listener := m.server.Listener
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		return tcpAddr.Port
	}
	return 0
}

func (m *mockAgentAPIServer) Close() {
	m.server.Close()
}

// e2eServerRunnerFactory creates individual runners for each session
type e2eServerRunnerFactory struct {
	proxy *Proxy
}

func (f *e2eServerRunnerFactory) Run(ctx context.Context, session *AgentSession) {
	// Create individual runner for this session
	runner := &e2eServerRunner{proxy: f.proxy}
	runner.Run(ctx, session)
}

// e2eServerRunner runs a mock agentapi server instead of the real one
type e2eServerRunner struct {
	mockServer *mockAgentAPIServer
	proxy      *Proxy
}

func (r *e2eServerRunner) Run(ctx context.Context, session *AgentSession) {
	// Create and start mock server on the session's port
	r.mockServer = newMockAgentAPIServer()

	// Store the mock server port in the session request
	session.Request.Port = r.mockServer.Port()

	// Log for debugging
	fmt.Printf("E2E Mock server started on port %d for session %s\n", session.Request.Port, session.ID)

	// Wait for context cancellation
	<-ctx.Done()

	// Cleanup
	if r.mockServer != nil {
		r.mockServer.Close()
	}

	// Clean up session
	r.proxy.sessionsMutex.Lock()
	delete(r.proxy.sessions, session.ID)
	r.proxy.sessionsMutex.Unlock()
}

// TestE2ESessionLifecycle tests the complete session lifecycle with a mock agentapi
func TestE2ESessionLifecycle(t *testing.T) {
	// Create proxy with minimal config
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	cfg.StartPort = 19000
	proxy := NewProxy(cfg, false)

	// Set up e2e server runner factory
	factory := &e2eServerRunnerFactory{proxy: proxy}
	proxy.SetServerRunner(factory)

	// Start proxy server
	server := httptest.NewServer(proxy.GetEcho())
	defer server.Close()

	// Test 1: Start a session
	startReq := StartRequest{
		Tags: map[string]string{
			"test": "e2e",
		},
		Environment: map[string]string{
			"TEST_ENV": "e2e_test",
		},
		Message: "Hello from e2e test",
	}

	body, _ := json.Marshal(startReq)
	req, _ := http.NewRequest("POST", server.URL+"/start", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "test-user")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to start session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, body)
	}

	var startResp StartResponse
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		t.Fatalf("Failed to decode start response: %v", err)
	}

	sessionID := startResp.SessionID
	t.Logf("Started session: %s", sessionID)

	// Wait for mock server to be ready
	time.Sleep(200 * time.Millisecond)

	// Test 2: Send a message through proxy
	msgReq := map[string]interface{}{
		"content": "Test message",
		"type":    "user",
	}

	body, _ = json.Marshal(msgReq)
	req, _ = http.NewRequest("POST", fmt.Sprintf("%s/%s/message", server.URL, sessionID), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200 for message, got %d: %s", resp.StatusCode, body)
	}

	var msgResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		t.Fatalf("Failed to decode message response: %v", err)
	}

	t.Logf("Message response: %v", msgResp)

	// Test 3: Get messages through proxy
	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/%s/messages", server.URL, sessionID), nil)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get messages: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200 for messages, got %d: %s", resp.StatusCode, body)
	}

	var messages []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		t.Fatalf("Failed to decode messages: %v", err)
	}

	if len(messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(messages))
	}

	// Test 4: Get status through proxy
	req, _ = http.NewRequest("GET", fmt.Sprintf("%s/%s/status", server.URL, sessionID), nil)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200 for status, got %d: %s", resp.StatusCode, body)
	}

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("Failed to decode status: %v", err)
	}

	t.Logf("Session status: %v", status)

	// Test 5: Search sessions
	req, _ = http.NewRequest("GET", server.URL+"/search", nil)
	req.Header.Set("X-User-ID", "test-user")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to search sessions: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200 for search, got %d: %s", resp.StatusCode, body)
	}

	var searchResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		t.Fatalf("Failed to decode sessions: %v", err)
	}

	sessions, ok := searchResp["sessions"].([]interface{})
	if !ok {
		t.Fatalf("Sessions field not found or wrong type")
	}

	if len(sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(sessions))
	}

	// Test 6: Delete session
	req, _ = http.NewRequest("DELETE", fmt.Sprintf("%s/sessions/%s", server.URL, sessionID), nil)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200 for delete, got %d: %s", resp.StatusCode, body)
	}

	// Verify session is deleted
	req, _ = http.NewRequest("GET", server.URL+"/search", nil)
	req.Header.Set("X-User-ID", "test-user")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to search sessions after delete: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var finalSearchResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&finalSearchResp); err != nil {
		t.Fatalf("Failed to decode sessions after delete: %v", err)
	}

	finalSessions, ok := finalSearchResp["sessions"].([]interface{})
	if !ok {
		t.Fatalf("Sessions field not found or wrong type after delete")
	}

	if len(finalSessions) != 0 {
		t.Errorf("Expected 0 sessions after delete, got %d", len(finalSessions))
	}

	t.Log("E2E test completed successfully!")
}

// TestE2EConcurrentSessions tests multiple concurrent sessions
func TestE2EConcurrentSessions(t *testing.T) {
	// Create proxy with minimal config
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	cfg.StartPort = 20000
	proxy := NewProxy(cfg, false)

	// Set up e2e server runner factory
	factory := &e2eServerRunnerFactory{proxy: proxy}
	proxy.SetServerRunner(factory)

	// Start proxy server
	server := httptest.NewServer(proxy.GetEcho())
	defer server.Close()

	// Start multiple sessions concurrently
	numSessions := 5
	var wg sync.WaitGroup
	sessionIDs := make([]string, numSessions)

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			startReq := StartRequest{
				Tags: map[string]string{
					"test":  "concurrent",
					"index": fmt.Sprintf("%d", index),
				},
			}

			body, _ := json.Marshal(startReq)
			req, _ := http.NewRequest("POST", server.URL+"/start", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-User-ID", fmt.Sprintf("user-%d", index))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("Failed to start session %d: %v", index, err)
				return
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Session %d: expected status 200, got %d", index, resp.StatusCode)
				return
			}

			var startResp StartResponse
			if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
				t.Errorf("Session %d: failed to decode response: %v", index, err)
				return
			}

			sessionIDs[index] = startResp.SessionID
			t.Logf("Started session %d: %s", index, startResp.SessionID)
		}(i)
	}

	wg.Wait()

	// Verify all sessions were created
	activeCount := 0
	proxy.sessionsMutex.RLock()
	activeCount = len(proxy.sessions)
	proxy.sessionsMutex.RUnlock()

	if activeCount != numSessions {
		t.Errorf("Expected %d active sessions, got %d", numSessions, activeCount)
	}

	// Clean up all sessions
	for _, sessionID := range sessionIDs {
		if sessionID != "" {
			req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/sessions/%s", server.URL, sessionID), nil)
			resp, _ := http.DefaultClient.Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
		}
	}

	t.Log("Concurrent sessions test completed successfully!")
}
