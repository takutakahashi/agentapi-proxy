package storage

import (
	"path/filepath"
	"testing"
	"time"
)

// TestMemoryStorage tests the memory storage implementation
func TestMemoryStorage(t *testing.T) {
	storage := NewMemoryStorage()
	testStorageInterface(t, storage)
}

// TestFileStorage tests the file storage implementation
func TestFileStorage(t *testing.T) {
	// Create temporary file for testing
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_sessions.json")
	
	storage, err := NewFileStorage(tmpFile, 0, false) // Disable sync for testing
	if err != nil {
		t.Fatalf("Failed to create file storage: %v", err)
	}
	defer storage.Close()
	
	testStorageInterface(t, storage)
}

// TestFileStorageWithEncryption tests the file storage with encryption
func TestFileStorageWithEncryption(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_sessions_encrypted.json")
	
	storage, err := NewFileStorage(tmpFile, 0, true) // Enable encryption
	if err != nil {
		t.Fatalf("Failed to create file storage with encryption: %v", err)
	}
	defer storage.Close()
	
	// Test with sensitive environment variables
	sessionData := &SessionData{
		ID:        "test-session-encrypted",
		Port:      9001,
		StartedAt: time.Now(),
		UserID:    "test-user",
		Status:    "active",
		Environment: map[string]string{
			"GITHUB_TOKEN": "sensitive-token-123",
			"API_KEY":      "secret-key-456",
			"NORMAL_VAR":   "not-sensitive",
		},
		Tags: map[string]string{
			"environment": "test",
		},
	}
	
	// Save session
	err = storage.Save(sessionData)
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}
	
	// Load session
	loaded, err := storage.Load(sessionData.ID)
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}
	
	// Check that sensitive data is properly decrypted
	if loaded.Environment["GITHUB_TOKEN"] != "sensitive-token-123" {
		t.Errorf("Expected GITHUB_TOKEN to be decrypted, got: %s", loaded.Environment["GITHUB_TOKEN"])
	}
	if loaded.Environment["API_KEY"] != "secret-key-456" {
		t.Errorf("Expected API_KEY to be decrypted, got: %s", loaded.Environment["API_KEY"])
	}
	if loaded.Environment["NORMAL_VAR"] != "not-sensitive" {
		t.Errorf("Expected NORMAL_VAR to remain unchanged, got: %s", loaded.Environment["NORMAL_VAR"])
	}
}

// TestFileStoragePersistence tests that data persists across storage instances
func TestFileStoragePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_sessions_persist.json")
	
	sessionData := &SessionData{
		ID:        "persist-test",
		Port:      9002,
		StartedAt: time.Now(),
		UserID:    "test-user",
		Status:    "active",
		Environment: map[string]string{
			"TEST_VAR": "test-value",
		},
		Tags: map[string]string{
			"test": "true",
		},
	}
	
	// Create first storage instance and save data
	storage1, err := NewFileStorage(tmpFile, 0, false)
	if err != nil {
		t.Fatalf("Failed to create first storage instance: %v", err)
	}
	
	err = storage1.Save(sessionData)
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}
	
	storage1.Close()
	
	// Create second storage instance and verify data exists
	storage2, err := NewFileStorage(tmpFile, 0, false)
	if err != nil {
		t.Fatalf("Failed to create second storage instance: %v", err)
	}
	defer storage2.Close()
	
	loaded, err := storage2.Load(sessionData.ID)
	if err != nil {
		t.Fatalf("Failed to load session from second instance: %v", err)
	}
	
	if loaded.ID != sessionData.ID {
		t.Errorf("Expected ID %s, got %s", sessionData.ID, loaded.ID)
	}
	if loaded.Environment["TEST_VAR"] != "test-value" {
		t.Errorf("Expected TEST_VAR to be 'test-value', got: %s", loaded.Environment["TEST_VAR"])
	}
}

// testStorageInterface runs common tests for any Storage implementation
func testStorageInterface(t *testing.T, storage Storage) {
	// Test data
	sessionData := &SessionData{
		ID:        "test-session-123",
		Port:      9000,
		StartedAt: time.Now(),
		UserID:    "test-user",
		Status:    "active",
		Environment: map[string]string{
			"KEY1": "value1",
			"KEY2": "value2",
		},
		Tags: map[string]string{
			"environment": "test",
			"version":     "1.0",
		},
		ProcessID:  12345,
		Command:    []string{"agentapi", "--port", "9000"},
		WorkingDir: "/tmp",
	}
	
	// Test Save
	err := storage.Save(sessionData)
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}
	
	// Test Load
	loaded, err := storage.Load(sessionData.ID)
	if err != nil {
		t.Fatalf("Failed to load session: %v", err)
	}
	
	// Verify loaded data
	if loaded.ID != sessionData.ID {
		t.Errorf("Expected ID %s, got %s", sessionData.ID, loaded.ID)
	}
	if loaded.Port != sessionData.Port {
		t.Errorf("Expected Port %d, got %d", sessionData.Port, loaded.Port)
	}
	if loaded.UserID != sessionData.UserID {
		t.Errorf("Expected UserID %s, got %s", sessionData.UserID, loaded.UserID)
	}
	if loaded.Status != sessionData.Status {
		t.Errorf("Expected Status %s, got %s", sessionData.Status, loaded.Status)
	}
	if len(loaded.Environment) != len(sessionData.Environment) {
		t.Errorf("Expected %d environment variables, got %d", len(sessionData.Environment), len(loaded.Environment))
	}
	for key, expectedValue := range sessionData.Environment {
		if actualValue, exists := loaded.Environment[key]; !exists || actualValue != expectedValue {
			t.Errorf("Expected environment[%s] = %s, got %s", key, expectedValue, actualValue)
		}
	}
	
	// Test LoadAll
	sessions, err := storage.LoadAll()
	if err != nil {
		t.Fatalf("Failed to load all sessions: %v", err)
	}
	
	found := false
	for _, session := range sessions {
		if session.ID == sessionData.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Session %s not found in LoadAll results", sessionData.ID)
	}
	
	// Test Update
	sessionData.Status = "updated"
	sessionData.Environment["NEW_KEY"] = "new_value"
	
	err = storage.Update(sessionData)
	if err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}
	
	// Verify update
	updated, err := storage.Load(sessionData.ID)
	if err != nil {
		t.Fatalf("Failed to load updated session: %v", err)
	}
	
	if updated.Status != "updated" {
		t.Errorf("Expected updated status 'updated', got %s", updated.Status)
	}
	if updated.Environment["NEW_KEY"] != "new_value" {
		t.Errorf("Expected NEW_KEY to be 'new_value', got %s", updated.Environment["NEW_KEY"])
	}
	
	// Test Delete
	err = storage.Delete(sessionData.ID)
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}
	
	// Verify deletion
	_, err = storage.Load(sessionData.ID)
	if err == nil {
		t.Errorf("Expected error when loading deleted session, but got none")
	}
	
	// Test Load non-existent session
	_, err = storage.Load("non-existent-session")
	if err == nil {
		t.Errorf("Expected error when loading non-existent session, but got none")
	}
}

// TestStorageFactory tests the storage factory function
func TestStorageFactory(t *testing.T) {
	// Test memory storage
	memConfig := &StorageConfig{Type: "memory"}
	memStorage, err := NewStorage(memConfig)
	if err != nil {
		t.Fatalf("Failed to create memory storage: %v", err)
	}
	if _, ok := memStorage.(*MemoryStorage); !ok {
		t.Errorf("Expected MemoryStorage, got %T", memStorage)
	}
	
	// Test file storage
	tmpDir := t.TempDir()
	fileConfig := &StorageConfig{
		Type:         "file",
		FilePath:     filepath.Join(tmpDir, "test.json"),
		SyncInterval: 30,
	}
	fileStorage, err := NewStorage(fileConfig)
	if err != nil {
		t.Fatalf("Failed to create file storage: %v", err)
	}
	if _, ok := fileStorage.(*FileStorage); !ok {
		t.Errorf("Expected FileStorage, got %T", fileStorage)
	}
	fileStorage.Close()
	
	// Test unknown storage type
	unknownConfig := &StorageConfig{Type: "unknown"}
	_, err = NewStorage(unknownConfig)
	if err == nil {
		t.Errorf("Expected error for unknown storage type, but got none")
	}
}

// TestEncryptionDecryption tests the encryption/decryption functions
func TestEncryptionDecryption(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_encrypt.json")
	
	storage, err := NewFileStorage(tmpFile, 0, true)
	if err != nil {
		t.Fatalf("Failed to create file storage: %v", err)
	}
	defer storage.Close()
	
	// Test that encryption/decryption works by saving and loading sensitive data
	sessionData := &SessionData{
		ID:        "test-encryption",
		Port:      9003,
		StartedAt: time.Now(),
		UserID:    "test-user",
		Status:    "active",
		Environment: map[string]string{
			"SECRET_TOKEN": "secret-value-123",
			"NORMAL_VAR":   "normal-value",
		},
	}
	
	// Save session (should encrypt sensitive data)
	err = storage.Save(sessionData)
	if err != nil {
		t.Fatalf("Failed to save session with encryption: %v", err)
	}
	
	// Load session (should decrypt sensitive data)
	loaded, err := storage.Load(sessionData.ID)
	if err != nil {
		t.Fatalf("Failed to load session with encryption: %v", err)
	}
	
	// Verify data is correctly decrypted
	if loaded.Environment["SECRET_TOKEN"] != "secret-value-123" {
		t.Errorf("Expected SECRET_TOKEN to be decrypted to 'secret-value-123', got: %s", loaded.Environment["SECRET_TOKEN"])
	}
	if loaded.Environment["NORMAL_VAR"] != "normal-value" {
		t.Errorf("Expected NORMAL_VAR to remain 'normal-value', got: %s", loaded.Environment["NORMAL_VAR"])
	}
}