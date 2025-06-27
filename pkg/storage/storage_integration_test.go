package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFileStorageErrorHandling tests error scenarios for file storage
func TestFileStorageErrorHandling(t *testing.T) {
	t.Run("InvalidFilePath", func(t *testing.T) {
		// Try to create storage with invalid path
		_, err := NewFileStorage("/root/nonexistent/invalid.json", 0, false)
		if err == nil {
			t.Error("Expected error for invalid file path")
		}
	})

	t.Run("CorruptedJSONFile", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "corrupted.json")

		// Create corrupted JSON file
		err := os.WriteFile(tmpFile, []byte(`{"sessions": [invalid json`), 0644)
		if err != nil {
			t.Fatalf("Failed to create corrupted file: %v", err)
		}

		// Try to load storage with corrupted file
		_, err = NewFileStorage(tmpFile, 0, false)
		if err == nil {
			t.Error("Expected error for corrupted JSON file")
		}
	})

	t.Run("ReadOnlyDirectory", func(t *testing.T) {
		tmpDir := t.TempDir()
		readOnlyDir := filepath.Join(tmpDir, "readonly")

		// Create read-only directory
		err := os.Mkdir(readOnlyDir, 0444)
		if err != nil {
			t.Fatalf("Failed to create read-only directory: %v", err)
		}

		tmpFile := filepath.Join(readOnlyDir, "sessions.json")

		// Try to create storage in read-only directory
		storage, err := NewFileStorage(tmpFile, 0, false)
		if err == nil {
			defer func() { _ = storage.Close() }()

			// Try to save session (should fail)
			session := &SessionData{
				ID:     "test",
				Port:   9000,
				UserID: "user",
				Status: "active",
			}

			err = storage.Save(session)
			if err == nil {
				t.Error("Expected error when saving to read-only directory")
			}
		}
	})
}

// TestEncryptionErrorHandling tests encryption error scenarios
func TestEncryptionErrorHandling(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_encryption_errors.json")

	storage, err := NewFileStorage(tmpFile, 0, true)
	if err != nil {
		t.Fatalf("Failed to create file storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	t.Run("EmptySensitiveValue", func(t *testing.T) {
		session := &SessionData{
			ID:     "test-empty",
			Port:   9000,
			UserID: "user",
			Status: "active",
			Environment: map[string]string{
				"SECRET_TOKEN": "", // Empty sensitive value
				"NORMAL_VAR":   "value",
			},
		}

		// Should handle empty values gracefully
		err := storage.Save(session)
		if err != nil {
			t.Errorf("Failed to save session with empty sensitive value: %v", err)
		}

		loaded, err := storage.Load(session.ID)
		if err != nil {
			t.Errorf("Failed to load session with empty sensitive value: %v", err)
		}

		if loaded.Environment["SECRET_TOKEN"] != "" {
			t.Errorf("Expected empty SECRET_TOKEN, got: %s", loaded.Environment["SECRET_TOKEN"])
		}
	})

	t.Run("SpecialCharactersSensitiveValue", func(t *testing.T) {
		specialValue := "token-with-special-chars!@#$%^&*(){}[]|\\:;\"'<>,.?/~`"
		session := &SessionData{
			ID:     "test-special",
			Port:   9001,
			UserID: "user",
			Status: "active",
			Environment: map[string]string{
				"API_KEY": specialValue,
			},
		}

		err := storage.Save(session)
		if err != nil {
			t.Errorf("Failed to save session with special characters: %v", err)
		}

		loaded, err := storage.Load(session.ID)
		if err != nil {
			t.Errorf("Failed to load session with special characters: %v", err)
		}

		if loaded.Environment["API_KEY"] != specialValue {
			t.Errorf("Special characters not preserved. Expected: %s, Got: %s", specialValue, loaded.Environment["API_KEY"])
		}
	})
}

// TestStorageConcurrency tests concurrent access to storage
func TestStorageConcurrency(t *testing.T) {
	t.Run("MemoryStorageConcurrency", func(t *testing.T) {
		storage := NewMemoryStorage()
		testConcurrentAccess(t, storage)
	})

	t.Run("FileStorageConcurrency", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "concurrent_test.json")

		storage, err := NewFileStorage(tmpFile, 0, false)
		if err != nil {
			t.Fatalf("Failed to create file storage: %v", err)
		}
		defer func() { _ = storage.Close() }()

		testConcurrentAccess(t, storage)
	})
}

func testConcurrentAccess(t *testing.T, storage Storage) {
	const numGoroutines = 10
	const numOperations = 50

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numOperations)

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				session := &SessionData{
					ID:     fmt.Sprintf("session-%d-%d", workerID, j),
					Port:   9000 + workerID*100 + j,
					UserID: fmt.Sprintf("user-%d", workerID),
					Status: "active",
					Environment: map[string]string{
						"WORKER_ID": fmt.Sprintf("%d", workerID),
						"OP_ID":     fmt.Sprintf("%d", j),
					},
				}

				if err := storage.Save(session); err != nil {
					errors <- fmt.Errorf("worker %d op %d save failed: %v", workerID, j, err)
				}

				// Randomly read or update
				if j%2 == 0 {
					if _, err := storage.Load(session.ID); err != nil {
						errors <- fmt.Errorf("worker %d op %d load failed: %v", workerID, j, err)
					}
				} else {
					// Create a copy to avoid race condition
					updateSession := &SessionData{
						ID:          session.ID,
						Port:        session.Port,
						StartedAt:   session.StartedAt,
						UserID:      session.UserID,
						Status:      "updated",
						Environment: make(map[string]string),
						Tags:        make(map[string]string),
						ProcessID:   session.ProcessID,
						Command:     append([]string{}, session.Command...),
						WorkingDir:  session.WorkingDir,
					}
					// Copy maps to avoid race
					for k, v := range session.Environment {
						updateSession.Environment[k] = v
					}
					for k, v := range session.Tags {
						updateSession.Tags[k] = v
					}
					if err := storage.Update(updateSession); err != nil {
						errors <- fmt.Errorf("worker %d op %d update failed: %v", workerID, j, err)
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errorList []string
	for err := range errors {
		errorList = append(errorList, err.Error())
	}

	if len(errorList) > 0 {
		t.Errorf("Concurrent access errors: %s", strings.Join(errorList, "; "))
	}

	// Verify final state
	sessions, err := storage.LoadAll()
	if err != nil {
		t.Errorf("Failed to load all sessions after concurrent test: %v", err)
	}

	if len(sessions) != numGoroutines*numOperations {
		t.Errorf("Expected %d sessions, got %d", numGoroutines*numOperations, len(sessions))
	}
}

// TestStorageEdgeCases tests edge cases for storage implementations
func TestStorageEdgeCases(t *testing.T) {
	t.Run("VeryLargeSessionData", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "large_data.json")

		storage, err := NewFileStorage(tmpFile, 0, false)
		if err != nil {
			t.Fatalf("Failed to create file storage: %v", err)
		}
		defer func() { _ = storage.Close() }()

		// Create session with large environment data
		largeValue := strings.Repeat("a", 100000) // 100KB string
		session := &SessionData{
			ID:     "large-session",
			Port:   9000,
			UserID: "user",
			Status: "active",
			Environment: map[string]string{
				"LARGE_DATA": largeValue,
			},
		}

		err = storage.Save(session)
		if err != nil {
			t.Errorf("Failed to save large session: %v", err)
		}

		loaded, err := storage.Load(session.ID)
		if err != nil {
			t.Errorf("Failed to load large session: %v", err)
		}

		if loaded.Environment["LARGE_DATA"] != largeValue {
			t.Error("Large data not preserved correctly")
		}
	})

	t.Run("UnicodeInSessionData", func(t *testing.T) {
		storage := NewMemoryStorage()

		// Test various unicode characters
		unicodeStrings := []string{
			"日本語",                // Japanese
			"🚀🔥💻",                // Emojis
			"Ñiño, café, piñata", // Spanish with accents
			"Москва",             // Russian
			"北京",                 // Chinese
		}

		for i, unicode := range unicodeStrings {
			session := &SessionData{
				ID:     fmt.Sprintf("unicode-%d", i),
				Port:   9000 + i,
				UserID: unicode,
				Status: "active",
				Environment: map[string]string{
					"UNICODE_VAR": unicode,
				},
				Tags: map[string]string{
					"unicode": unicode,
				},
			}

			err := storage.Save(session)
			if err != nil {
				t.Errorf("Failed to save unicode session %d: %v", i, err)
				continue
			}

			loaded, err := storage.Load(session.ID)
			if err != nil {
				t.Errorf("Failed to load unicode session %d: %v", i, err)
				continue
			}

			if loaded.UserID != unicode {
				t.Errorf("Unicode in UserID not preserved: expected %s, got %s", unicode, loaded.UserID)
			}

			if loaded.Environment["UNICODE_VAR"] != unicode {
				t.Errorf("Unicode in Environment not preserved: expected %s, got %s", unicode, loaded.Environment["UNICODE_VAR"])
			}

			if loaded.Tags["unicode"] != unicode {
				t.Errorf("Unicode in Tags not preserved: expected %s, got %s", unicode, loaded.Tags["unicode"])
			}
		}
	})

	t.Run("EmptySessionCollections", func(t *testing.T) {
		storage := NewMemoryStorage()

		session := &SessionData{
			ID:          "empty-collections",
			Port:        9000,
			UserID:      "user",
			Status:      "active",
			Environment: make(map[string]string), // Empty map
			Tags:        make(map[string]string), // Empty map
			Command:     []string{},              // Empty slice
		}

		err := storage.Save(session)
		if err != nil {
			t.Errorf("Failed to save session with empty collections: %v", err)
		}

		loaded, err := storage.Load(session.ID)
		if err != nil {
			t.Errorf("Failed to load session with empty collections: %v", err)
		}

		if loaded.Environment == nil || len(loaded.Environment) != 0 {
			t.Error("Empty Environment map not preserved")
		}

		if loaded.Tags == nil || len(loaded.Tags) != 0 {
			t.Error("Empty Tags map not preserved")
		}

		if loaded.Command == nil || len(loaded.Command) != 0 {
			t.Error("Empty Command slice not preserved")
		}
	})
}

// TestFileStoragePeriodicSync tests the periodic sync functionality
func TestFileStoragePeriodicSync(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "periodic_sync.json")

	// Create storage with short sync interval
	storage, err := NewFileStorage(tmpFile, 1, false) // 1 second sync
	if err != nil {
		t.Fatalf("Failed to create file storage: %v", err)
	}
	defer func() { _ = storage.Close() }()

	session := &SessionData{
		ID:     "sync-test",
		Port:   9000,
		UserID: "user",
		Status: "active",
	}

	// Save session
	err = storage.Save(session)
	if err != nil {
		t.Fatalf("Failed to save session: %v", err)
	}

	// Wait for sync
	time.Sleep(2 * time.Second)

	// Verify file exists and contains data
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Error("Storage file was not created")
	}

	// Read file directly and verify content
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read storage file: %v", err)
	}

	var fileData struct {
		Sessions []SessionData `json:"sessions"`
	}

	err = json.Unmarshal(data, &fileData)
	if err != nil {
		t.Fatalf("Failed to parse storage file: %v", err)
	}

	if len(fileData.Sessions) != 1 {
		t.Errorf("Expected 1 session in file, got %d", len(fileData.Sessions))
	}

	if fileData.Sessions[0].ID != session.ID {
		t.Errorf("Session ID mismatch: expected %s, got %s", session.ID, fileData.Sessions[0].ID)
	}
}

// TestStorageConfigValidation tests configuration validation scenarios
func TestStorageConfigValidation(t *testing.T) {
	t.Run("InvalidStorageType", func(t *testing.T) {
		config := &StorageConfig{
			Type: "invalid-type",
		}

		_, err := NewStorage(config)
		if err == nil {
			t.Error("Expected error for invalid storage type")
		}
	})

	t.Run("EmptyFilePathWithDefaults", func(t *testing.T) {
		config := &StorageConfig{
			Type:     "file",
			FilePath: "", // Should use default
		}

		storage, err := NewStorage(config)
		if err != nil {
			t.Errorf("Failed to create storage with empty file path: %v", err)
		}
		if storage != nil {
			_ = storage.Close()
		}
	})

	t.Run("ZeroSyncIntervalWithDefaults", func(t *testing.T) {
		tmpDir := t.TempDir()
		config := &StorageConfig{
			Type:         "file",
			FilePath:     filepath.Join(tmpDir, "default_sync.json"),
			SyncInterval: 0, // Should use default
		}

		storage, err := NewStorage(config)
		if err != nil {
			t.Errorf("Failed to create storage with zero sync interval: %v", err)
		}
		if storage != nil {
			_ = storage.Close()
		}
	})
}
