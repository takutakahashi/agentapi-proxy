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
func (s *mockSession) Scope() ResourceScope    { return ScopeUser }
func (s *mockSession) TeamID() string          { return "" }
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

func TestDeleteSessionByID_DeletesShareLink(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	// Set custom session manager
	mockManager := newMockSessionManager()
	proxy.SetSessionManager(mockManager)

	// Create a session
	sessionID := "test-session-with-share"
	_, err := proxy.CreateSession(sessionID, StartRequest{
		Environment: map[string]string{"TEST": "value"},
	}, "test-user", "user", nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Create a share link for the session
	shareRepo := proxy.GetShareRepository()
	share := NewSessionShare(sessionID, "test-user")
	err = shareRepo.Save(share)
	if err != nil {
		t.Fatalf("Failed to save share: %v", err)
	}

	// Verify share exists
	_, err = shareRepo.FindBySessionID(sessionID)
	if err != nil {
		t.Fatalf("Share should exist before session deletion: %v", err)
	}

	// Delete the session
	err = proxy.DeleteSessionByID(sessionID)
	if err != nil {
		t.Fatalf("DeleteSessionByID failed: %v", err)
	}

	// Verify share is also deleted
	_, err = shareRepo.FindBySessionID(sessionID)
	if err == nil {
		t.Error("Share should be deleted when session is deleted")
	}
}

func TestDeleteSessionByID_NoShareExists(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	// Set custom session manager
	mockManager := newMockSessionManager()
	proxy.SetSessionManager(mockManager)

	// Create a session without a share link
	sessionID := "test-session-no-share"
	_, err := proxy.CreateSession(sessionID, StartRequest{
		Environment: map[string]string{"TEST": "value"},
	}, "test-user", "user", nil)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Delete the session (should not error even without share)
	err = proxy.DeleteSessionByID(sessionID)
	if err != nil {
		t.Fatalf("DeleteSessionByID should not fail when no share exists: %v", err)
	}

	mockManager.mu.Lock()
	if !mockManager.deleteCalled {
		t.Error("Mock session manager DeleteSession was not called")
	}
	mockManager.mu.Unlock()
}

func TestLocalSessionManager_ListSessions_ScopeFilter(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false

	manager := NewLocalSessionManager(cfg, false, nil, 9000)

	// Manually add sessions with different scopes to the internal map
	manager.mutex.Lock()
	manager.sessions["session-user-1"] = &localSession{
		id:        "session-user-1",
		startedAt: time.Now(),
		status:    "active",
		request: &RunServerRequest{
			UserID: "user-a",
			Scope:  ScopeUser,
			TeamID: "",
		},
	}
	manager.sessions["session-user-2"] = &localSession{
		id:        "session-user-2",
		startedAt: time.Now(),
		status:    "active",
		request: &RunServerRequest{
			UserID: "user-b",
			Scope:  ScopeUser,
			TeamID: "",
		},
	}
	manager.sessions["session-team-1"] = &localSession{
		id:        "session-team-1",
		startedAt: time.Now(),
		status:    "active",
		request: &RunServerRequest{
			UserID: "user-a",
			Scope:  ScopeTeam,
			TeamID: "org/team-alpha",
		},
	}
	manager.sessions["session-team-2"] = &localSession{
		id:        "session-team-2",
		startedAt: time.Now(),
		status:    "active",
		request: &RunServerRequest{
			UserID: "user-b",
			Scope:  ScopeTeam,
			TeamID: "org/team-alpha",
		},
	}
	manager.sessions["session-team-3"] = &localSession{
		id:        "session-team-3",
		startedAt: time.Now(),
		status:    "active",
		request: &RunServerRequest{
			UserID: "user-c",
			Scope:  ScopeTeam,
			TeamID: "org/team-beta",
		},
	}
	manager.mutex.Unlock()

	// Test: List all sessions
	allSessions := manager.ListSessions(SessionFilter{})
	if len(allSessions) != 5 {
		t.Errorf("Expected 5 sessions, got %d", len(allSessions))
	}

	// Test: Filter by scope=user
	userScopedSessions := manager.ListSessions(SessionFilter{Scope: ScopeUser})
	if len(userScopedSessions) != 2 {
		t.Errorf("Expected 2 user-scoped sessions, got %d", len(userScopedSessions))
	}

	// Test: Filter by scope=team
	teamScopedSessions := manager.ListSessions(SessionFilter{Scope: ScopeTeam})
	if len(teamScopedSessions) != 3 {
		t.Errorf("Expected 3 team-scoped sessions, got %d", len(teamScopedSessions))
	}

	// Test: Filter by specific team_id
	teamAlphaSessions := manager.ListSessions(SessionFilter{TeamID: "org/team-alpha"})
	if len(teamAlphaSessions) != 2 {
		t.Errorf("Expected 2 sessions for org/team-alpha, got %d", len(teamAlphaSessions))
	}

	// Test: Filter by TeamIDs (user's teams)
	// TeamIDs filter only applies to team-scoped sessions
	// User-scoped sessions are not filtered out by TeamIDs
	userTeamsSessions := manager.ListSessions(SessionFilter{
		TeamIDs: []string{"org/team-alpha", "org/team-gamma"},
	})
	// Should return: 2 user-scoped + 2 team-alpha sessions = 4
	// (team-beta is filtered out)
	if len(userTeamsSessions) != 4 {
		t.Errorf("Expected 4 sessions for user's teams filter, got %d", len(userTeamsSessions))
	}

	// Test: Combined filter - scope=team and team_id
	teamAlphaTeamScoped := manager.ListSessions(SessionFilter{
		Scope:  ScopeTeam,
		TeamID: "org/team-alpha",
	})
	if len(teamAlphaTeamScoped) != 2 {
		t.Errorf("Expected 2 team-scoped sessions for org/team-alpha, got %d", len(teamAlphaTeamScoped))
	}

	// Verify session scope and team_id are correctly stored
	for _, session := range allSessions {
		if session.ID() == "session-team-1" {
			if session.Scope() != ScopeTeam {
				t.Errorf("Expected session-team-1 to have scope 'team', got '%s'", session.Scope())
			}
			if session.TeamID() != "org/team-alpha" {
				t.Errorf("Expected session-team-1 to have team_id 'org/team-alpha', got '%s'", session.TeamID())
			}
		}
		if session.ID() == "session-user-1" {
			if session.Scope() != ScopeUser {
				t.Errorf("Expected session-user-1 to have scope 'user', got '%s'", session.Scope())
			}
		}
	}
}
