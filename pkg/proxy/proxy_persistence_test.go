package proxy

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/storage"
)

// TestProxySessionPersistence tests session persistence integration with proxy
func TestProxySessionPersistence(t *testing.T) {
	t.Run("SessionSavedOnCreation", func(t *testing.T) {
		// Create test config with persistence enabled
		tmpDir := t.TempDir()
		cfg := createTestConfigWithPersistence(tmpDir)

		proxy := NewProxy(cfg, false)
		defer func() {
			if proxy.storage != nil {
				proxy.storage.Close()
			}
		}()

		// Verify storage was initialized
		if proxy.storage == nil {
			t.Fatal("Storage should be initialized when persistence is enabled")
		}

		// Create a test session directly (simulating session creation)
		session := &AgentSession{
			ID:          "test-session-123",
			Port:        9001,
			StartedAt:   time.Now(),
			UserID:      "test-user",
			Status:      "active",
			Environment: map[string]string{"TEST_VAR": "test-value"},
			Tags:        map[string]string{"test": "true"},
		}

		// Add session to proxy
		proxy.sessionsMutex.Lock()
		proxy.sessions[session.ID] = session
		proxy.sessionsMutex.Unlock()

		// Save session to storage
		proxy.saveSession(session)

		// Verify session was saved to storage
		savedSession, err := proxy.storage.Load(session.ID)
		if err != nil {
			t.Fatalf("Failed to load saved session: %v", err)
		}

		if savedSession.ID != session.ID {
			t.Errorf("Expected session ID %s, got %s", session.ID, savedSession.ID)
		}
		if savedSession.UserID != session.UserID {
			t.Errorf("Expected user ID %s, got %s", session.UserID, savedSession.UserID)
		}
		if savedSession.Environment["TEST_VAR"] != "test-value" {
			t.Errorf("Expected TEST_VAR=test-value, got %s", savedSession.Environment["TEST_VAR"])
		}
	})

	t.Run("SessionDeletedFromStorage", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := createTestConfigWithPersistence(tmpDir)

		proxy := NewProxy(cfg, false)
		defer func() {
			if proxy.storage != nil {
				proxy.storage.Close()
			}
		}()

		// Create and save a session
		session := &AgentSession{
			ID:     "delete-test-session",
			Port:   9002,
			UserID: "test-user",
			Status: "active",
		}

		proxy.sessionsMutex.Lock()
		proxy.sessions[session.ID] = session
		proxy.sessionsMutex.Unlock()

		proxy.saveSession(session)

		// Verify session exists in storage
		_, err := proxy.storage.Load(session.ID)
		if err != nil {
			t.Fatalf("Session should exist in storage: %v", err)
		}

		// Delete session
		proxy.sessionsMutex.Lock()
		delete(proxy.sessions, session.ID)
		proxy.sessionsMutex.Unlock()

		proxy.deleteSessionFromStorage(session.ID)

		// Verify session was deleted from storage
		_, err = proxy.storage.Load(session.ID)
		if err == nil {
			t.Error("Session should have been deleted from storage")
		}
	})

	t.Run("SessionUpdatedInStorage", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := createTestConfigWithPersistence(tmpDir)

		proxy := NewProxy(cfg, false)
		defer func() {
			if proxy.storage != nil {
				proxy.storage.Close()
			}
		}()

		// Create and save a session
		session := &AgentSession{
			ID:     "update-test-session",
			Port:   9003,
			UserID: "test-user",
			Status: "active",
		}

		proxy.sessionsMutex.Lock()
		proxy.sessions[session.ID] = session
		proxy.sessionsMutex.Unlock()

		proxy.saveSession(session)

		// Update session
		session.Status = "updated"
		session.Environment = map[string]string{"NEW_VAR": "new-value"}

		proxy.updateSession(session)

		// Verify session was updated in storage
		updatedSession, err := proxy.storage.Load(session.ID)
		if err != nil {
			t.Fatalf("Failed to load updated session: %v", err)
		}

		if updatedSession.Status != "updated" {
			t.Errorf("Expected status 'updated', got %s", updatedSession.Status)
		}
		if updatedSession.Environment["NEW_VAR"] != "new-value" {
			t.Errorf("Expected NEW_VAR=new-value, got %s", updatedSession.Environment["NEW_VAR"])
		}
	})
}

// TestProxySessionRecovery tests session recovery functionality
func TestProxySessionRecovery(t *testing.T) {
	t.Run("ValidSessionRecovery", func(t *testing.T) {
		tmpDir := t.TempDir()
		storageFile := filepath.Join(tmpDir, "recovery_test.json")

		// Create initial storage with test data
		testSessions := []*storage.SessionData{
			{
				ID:          "recover-session-1",
				Port:        9004,
				StartedAt:   time.Now().Add(-1 * time.Hour), // 1 hour ago
				UserID:      "user1",
				Status:      "active",
				Environment: map[string]string{"VAR1": "value1"},
				Tags:        map[string]string{"env": "test"},
			},
			{
				ID:          "recover-session-2",
				Port:        9005,
				StartedAt:   time.Now().Add(-2 * time.Hour), // 2 hours ago
				UserID:      "user2",
				Status:      "active",
				Environment: map[string]string{"VAR2": "value2"},
			},
		}

		// Create storage and save test sessions
		initialStorage, err := storage.NewFileStorage(storageFile, 0, false)
		if err != nil {
			t.Fatalf("Failed to create initial storage: %v", err)
		}

		for _, session := range testSessions {
			err = initialStorage.Save(session)
			if err != nil {
				t.Fatalf("Failed to save test session: %v", err)
			}
		}
		initialStorage.Close()

		// Create proxy with persistence (should recover sessions)
		cfg := createTestConfigWithPersistence(tmpDir)
		cfg.Persistence.FilePath = storageFile

		proxy := NewProxy(cfg, false)
		defer func() {
			if proxy.storage != nil {
				proxy.storage.Close()
			}
		}()

		// Verify sessions were recovered
		proxy.sessionsMutex.RLock()
		recoveredCount := len(proxy.sessions)
		proxy.sessionsMutex.RUnlock()

		if recoveredCount == 0 {
			t.Error("No sessions were recovered")
		}

		// Check specific sessions
		proxy.sessionsMutex.RLock()
		session1, exists1 := proxy.sessions["recover-session-1"]
		session2, exists2 := proxy.sessions["recover-session-2"]
		proxy.sessionsMutex.RUnlock()

		if !exists1 || !exists2 {
			t.Error("Not all sessions were recovered")
		}

		if session1.Status != "recovered" {
			t.Errorf("Expected recovered status, got %s", session1.Status)
		}

		if session2.Environment["VAR2"] != "value2" {
			t.Errorf("Session environment not recovered correctly")
		}

		// Verify next port was updated
		if proxy.nextPort <= 9005 {
			t.Errorf("Next port should be updated after recovery, got %d", proxy.nextPort)
		}
	})

}

// TestProxyPersistenceConfiguration tests persistence configuration scenarios
func TestProxyPersistenceConfiguration(t *testing.T) {
	t.Run("PersistenceDisabled", func(t *testing.T) {
		cfg := &config.Config{
			StartPort: 9000,
			Persistence: config.PersistenceConfig{
				Enabled: false, // Disabled
			},
		}

		proxy := NewProxy(cfg, false)

		// Should use memory storage
		if proxy.storage == nil {
			t.Error("Storage should be initialized even when persistence is disabled")
		}

		// Should be memory storage (not file storage)
		_, isMemory := proxy.storage.(*storage.MemoryStorage)
		if !isMemory {
			t.Error("Should use memory storage when persistence is disabled")
		}
	})

	t.Run("InvalidStorageBackend", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := createTestConfigWithPersistence(tmpDir)
		cfg.Persistence.Backend = "invalid-backend"

		proxy := NewProxy(cfg, false)
		defer func() {
			if proxy.storage != nil {
				proxy.storage.Close()
			}
		}()

		// Should fall back to memory storage
		_, isMemory := proxy.storage.(*storage.MemoryStorage)
		if !isMemory {
			t.Error("Should fall back to memory storage for invalid backend")
		}
	})

	t.Run("EncryptionEnabled", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := createTestConfigWithPersistence(tmpDir)
		cfg.Persistence.EncryptSecrets = true

		proxy := NewProxy(cfg, false)
		defer func() {
			if proxy.storage != nil {
				proxy.storage.Close()
			}
		}()

		// Test session with sensitive data
		session := &AgentSession{
			ID:     "encryption-test",
			Port:   9007,
			UserID: "user",
			Status: "active",
			Environment: map[string]string{
				"GITHUB_TOKEN": "secret-token-123",
				"NORMAL_VAR":   "normal-value",
			},
		}

		proxy.saveSession(session)

		// Load and verify data is decrypted correctly
		loadedSession, err := proxy.storage.Load(session.ID)
		if err != nil {
			t.Fatalf("Failed to load encrypted session: %v", err)
		}

		if loadedSession.Environment["GITHUB_TOKEN"] != "secret-token-123" {
			t.Error("Sensitive data not decrypted correctly")
		}
		if loadedSession.Environment["NORMAL_VAR"] != "normal-value" {
			t.Error("Normal data not preserved correctly")
		}
	})
}

// TestProxyPersistenceAPI tests persistence through API endpoints
func TestProxyPersistenceAPI(t *testing.T) {
	t.Run("StartSessionWithPersistence", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := createTestConfigWithPersistence(tmpDir)

		proxy := NewProxy(cfg, false)
		defer func() {
			if proxy.storage != nil {
				proxy.storage.Close()
			}
		}()

		// Mock the start session request
		reqBody := `{
			"environment": {"TEST_VAR": "test-value"},
			"tags": {"test": "true"}
		}`

		req := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-User-ID", "test-user")

		rec := httptest.NewRecorder()
		_ = echo.New().NewContext(req, rec)

		// Note: This test is simplified since we can't easily mock the full agentapi process
		// In a real scenario, you'd need to mock the process execution

		// Verify that the proxy has persistence enabled
		if proxy.storage == nil {
			t.Error("Storage should be initialized")
		}
	})
}

// Helper function to create test config with persistence enabled
func createTestConfigWithPersistence(tmpDir string) *config.Config {
	return &config.Config{
		StartPort: 9000,
		Auth: config.AuthConfig{
			Enabled: false,
		},
		Persistence: config.PersistenceConfig{
			Enabled:        true,
			Backend:        "file",
			FilePath:       filepath.Join(tmpDir, "sessions.json"),
			SyncInterval:   0, // Disable periodic sync for tests
			EncryptSecrets: false,
		},
	}
}

// TestSessionConversion tests conversion between AgentSession and SessionData
func TestSessionConversion(t *testing.T) {
	cfg := &config.Config{
		StartPort: 9000,
		Persistence: config.PersistenceConfig{
			Enabled: false,
		},
	}

	proxy := NewProxy(cfg, false)

	// Create test session
	agentSession := &AgentSession{
		ID:        "conversion-test",
		Port:      9008,
		StartedAt: time.Now(),
		UserID:    "test-user",
		Status:    "active",
		Environment: map[string]string{
			"VAR1": "value1",
			"VAR2": "value2",
		},
		Tags: map[string]string{
			"tag1": "tagvalue1",
			"tag2": "tagvalue2",
		},
	}

	// Convert to storage format
	sessionData := proxy.sessionToStorage(agentSession)

	// Verify conversion
	if sessionData.ID != agentSession.ID {
		t.Errorf("ID conversion failed: expected %s, got %s", agentSession.ID, sessionData.ID)
	}

	if sessionData.Port != agentSession.Port {
		t.Errorf("Port conversion failed: expected %d, got %d", agentSession.Port, sessionData.Port)
	}

	if len(sessionData.Environment) != len(agentSession.Environment) {
		t.Errorf("Environment conversion failed: expected %d items, got %d", len(agentSession.Environment), len(sessionData.Environment))
	}

	// Convert back to agent session
	convertedBack := proxy.sessionFromStorage(sessionData)

	// Verify round-trip conversion
	if convertedBack.ID != agentSession.ID {
		t.Errorf("Round-trip ID conversion failed: expected %s, got %s", agentSession.ID, convertedBack.ID)
	}

	if convertedBack.UserID != agentSession.UserID {
		t.Errorf("Round-trip UserID conversion failed: expected %s, got %s", agentSession.UserID, convertedBack.UserID)
	}

	if len(convertedBack.Environment) != len(agentSession.Environment) {
		t.Errorf("Round-trip Environment conversion failed: expected %d items, got %d", len(agentSession.Environment), len(convertedBack.Environment))
	}

	for key, value := range agentSession.Environment {
		if convertedBack.Environment[key] != value {
			t.Errorf("Round-trip Environment value failed for key %s: expected %s, got %s", key, value, convertedBack.Environment[key])
		}
	}
}
