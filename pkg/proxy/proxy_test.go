package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestNewProxyFixed(t *testing.T) {
	cfg := &config.Config{
		StartPort: 9000,
		Auth: config.AuthConfig{
			Enabled: false,
		},
	}

	proxy := NewProxy(cfg, false)
	if proxy == nil {
		t.Fatal("NewProxy returned nil")
	}

	if proxy.config != cfg {
		t.Error("Proxy config not set correctly")
	}

	if proxy.echo == nil {
		t.Error("Echo instance not initialized")
	}

	if proxy.sessionManager == nil {
		t.Error("Session manager not initialized")
	}
}

func TestHealthEndpointFixed(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for /health endpoint, got %d", http.StatusOK, w.Code)
	}
}

func TestExtractRepoFullNameFromURLFixed(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL",
			url:      "https://github.com/owner/repo",
			expected: "owner/repo",
		},
		{
			name:     "HTTPS URL with .git",
			url:      "https://github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "SSH URL",
			url:      "git@github.com:owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "Invalid URL format",
			url:      "invalid-url",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := extractRepoFullNameFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestHealthEndpointWithoutAuth(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for /health endpoint, got %d", http.StatusOK, w.Code)
	}
}

// mockSessionManager is a mock implementation of SessionManager for testing
type mockSessionManager struct {
	mu             sync.Mutex
	createCalled   bool
	getCalled      bool
	listCalled     bool
	deleteCalled   bool
	shutdownCalled bool
	sessions       map[string]*mockSession
	lastCreatedID  string
	lastDeletedID  string
}

type mockSession struct {
	id        string
	port      int
	userID    string
	tags      map[string]string
	status    string
	startedAt time.Time
	cancelled bool
}

func (s *mockSession) ID() string              { return s.id }
func (s *mockSession) Addr() string            { return fmt.Sprintf("localhost:%d", s.port) }
func (s *mockSession) UserID() string          { return s.userID }
func (s *mockSession) Tags() map[string]string { return s.tags }
func (s *mockSession) Status() string          { return s.status }
func (s *mockSession) StartedAt() time.Time    { return s.startedAt }
func (s *mockSession) Description() string     { return "" }
func (s *mockSession) Cancel()                 { s.cancelled = true }

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		sessions: make(map[string]*mockSession),
	}
}

func (m *mockSessionManager) CreateSession(ctx context.Context, id string, req *RunServerRequest) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalled = true
	m.lastCreatedID = id
	session := &mockSession{
		id:        id,
		port:      req.Port,
		userID:    req.UserID,
		tags:      req.Tags,
		status:    "active",
		startedAt: time.Now(),
	}
	m.sessions[id] = session
	return session, nil
}

func (m *mockSessionManager) GetSession(id string) Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getCalled = true
	if session, ok := m.sessions[id]; ok {
		return session
	}
	return nil
}

func (m *mockSessionManager) ListSessions(filter SessionFilter) []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalled = true
	var result []Session
	for _, session := range m.sessions {
		result = append(result, session)
	}
	return result
}

func (m *mockSessionManager) DeleteSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalled = true
	m.lastDeletedID = id
	delete(m.sessions, id)
	return nil
}

func (m *mockSessionManager) Shutdown(timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdownCalled = true
	return nil
}

func TestCustomSessionManager(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	// Set custom session manager
	mockManager := newMockSessionManager()
	proxy.SetSessionManager(mockManager)

	// Create a session
	session, err := proxy.CreateSession("test-session", StartRequest{
		Environment: map[string]string{"TEST": "value"},
	}, "test-user", "user", nil)

	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session == nil {
		t.Fatal("Session should not be nil")
	}

	mockManager.mu.Lock()
	if !mockManager.createCalled {
		t.Error("Mock session manager CreateSession was not called")
	}
	if mockManager.lastCreatedID != "test-session" {
		t.Errorf("Expected session ID 'test-session', got '%s'", mockManager.lastCreatedID)
	}
	mockManager.mu.Unlock()

	// Get the session
	retrievedSession := proxy.GetSessionManager().GetSession("test-session")
	if retrievedSession == nil {
		t.Error("GetSession should return the session")
	}

	mockManager.mu.Lock()
	if !mockManager.getCalled {
		t.Error("Mock session manager GetSession was not called")
	}
	mockManager.mu.Unlock()

	// Delete the session
	err = proxy.DeleteSessionByID("test-session")
	if err != nil {
		t.Fatalf("DeleteSessionByID failed: %v", err)
	}

	mockManager.mu.Lock()
	if !mockManager.deleteCalled {
		t.Error("Mock session manager DeleteSession was not called")
	}
	if mockManager.lastDeletedID != "test-session" {
		t.Errorf("Expected deleted session ID 'test-session', got '%s'", mockManager.lastDeletedID)
	}
	mockManager.mu.Unlock()
}

func TestDefaultSessionManager(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	// Verify default session manager is returned
	manager := proxy.GetSessionManager()
	if manager == nil {
		t.Error("GetSessionManager should return a session manager")
	}

	// Check if it's an instance of LocalSessionManager
	if _, ok := manager.(*LocalSessionManager); !ok {
		t.Error("Default session manager should be of type LocalSessionManager")
	}
}

func TestProxyShutdown(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	// Set custom session manager
	mockManager := newMockSessionManager()
	proxy.SetSessionManager(mockManager)

	// Shutdown
	err := proxy.Shutdown(5 * time.Second)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	mockManager.mu.Lock()
	if !mockManager.shutdownCalled {
		t.Error("Mock session manager Shutdown was not called")
	}
	mockManager.mu.Unlock()
}
