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

// e2eSession implements the Session interface for e2e testing
type e2eSession struct {
	id        string
	port      int
	userID    string
	tags      map[string]string
	status    string
	startedAt time.Time
	cancel    context.CancelFunc
}

func (s *e2eSession) ID() string              { return s.id }
func (s *e2eSession) Addr() string            { return fmt.Sprintf("localhost:%d", s.port) }
func (s *e2eSession) UserID() string          { return s.userID }
func (s *e2eSession) Tags() map[string]string { return s.tags }
func (s *e2eSession) Status() string          { return s.status }
func (s *e2eSession) StartedAt() time.Time    { return s.startedAt }
func (s *e2eSession) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

// e2eSessionManager is a SessionManager for e2e testing that uses mock agentapi servers
type e2eSessionManager struct {
	sessions    map[string]*e2eSession
	mockServers map[string]*mockAgentAPIServer
	mutex       sync.RWMutex
}

func newE2ESessionManager() *e2eSessionManager {
	return &e2eSessionManager{
		sessions:    make(map[string]*e2eSession),
		mockServers: make(map[string]*mockAgentAPIServer),
	}
}

func (m *e2eSessionManager) CreateSession(ctx context.Context, id string, req *RunServerRequest) (Session, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Create mock server
	mockServer := newMockAgentAPIServer()
	m.mockServers[id] = mockServer

	// Create session context
	sessionCtx, cancel := context.WithCancel(context.Background())

	session := &e2eSession{
		id:        id,
		port:      mockServer.Port(),
		userID:    req.UserID,
		tags:      req.Tags,
		status:    "active",
		startedAt: time.Now(),
		cancel:    cancel,
	}
	m.sessions[id] = session

	// Start goroutine to cleanup on context cancellation
	go func() {
		<-sessionCtx.Done()
		m.mutex.Lock()
		if server, ok := m.mockServers[id]; ok {
			server.Close()
			delete(m.mockServers, id)
		}
		delete(m.sessions, id)
		m.mutex.Unlock()
	}()

	fmt.Printf("E2E Mock server started on port %d for session %s\n", session.port, id)
	return session, nil
}

func (m *e2eSessionManager) GetSession(id string) Session {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if session, ok := m.sessions[id]; ok {
		return session
	}
	return nil
}

func (m *e2eSessionManager) ListSessions(filter SessionFilter) []Session {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var result []Session
	for _, session := range m.sessions {
		// Apply filters
		if filter.UserID != "" && session.userID != filter.UserID {
			continue
		}
		if filter.Status != "" && session.status != filter.Status {
			continue
		}
		if len(filter.Tags) > 0 {
			matchAllTags := true
			for key, value := range filter.Tags {
				if session.tags[key] != value {
					matchAllTags = false
					break
				}
			}
			if !matchAllTags {
				continue
			}
		}
		result = append(result, session)
	}
	return result
}

func (m *e2eSessionManager) DeleteSession(id string) error {
	m.mutex.RLock()
	session, exists := m.sessions[id]
	m.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("session not found")
	}

	session.Cancel()

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)
	return nil
}

func (m *e2eSessionManager) Shutdown(timeout time.Duration) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for id, session := range m.sessions {
		session.Cancel()
		if server, ok := m.mockServers[id]; ok {
			server.Close()
		}
	}
	m.sessions = make(map[string]*e2eSession)
	m.mockServers = make(map[string]*mockAgentAPIServer)
	return nil
}

// TestE2ESessionLifecycle tests the complete session lifecycle with a mock agentapi
func TestE2ESessionLifecycle(t *testing.T) {
	// Create proxy with minimal config
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	cfg.StartPort = 19000
	proxy := NewProxy(cfg, false)

	// Set up e2e session manager
	e2eManager := newE2ESessionManager()
	proxy.SetSessionManager(e2eManager)

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
		Params: &SessionParams{
			Message: "Hello from e2e test",
		},
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

	// Set up e2e session manager
	e2eManager := newE2ESessionManager()
	proxy.SetSessionManager(e2eManager)

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
	sessions := e2eManager.ListSessions(SessionFilter{})
	if len(sessions) != numSessions {
		t.Errorf("Expected %d active sessions, got %d", numSessions, len(sessions))
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
