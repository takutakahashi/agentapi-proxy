package logger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogger(t *testing.T) {
	// Create temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "logger-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set LOG_DIR environment variable
	originalLogDir := os.Getenv("LOG_DIR")
	os.Setenv("LOG_DIR", tmpDir)
	defer os.Setenv("LOG_DIR", originalLogDir)

	logger := NewLogger()

	// Test session start logging
	sessionID := "test-session-123"
	repository := "owner/repo"
	
	err = logger.LogSessionStart(sessionID, repository)
	if err != nil {
		t.Fatalf("Failed to log session start: %v", err)
	}

	// Check if log file was created
	logPath := filepath.Join(tmpDir, sessionID+".json")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Fatalf("Log file was not created: %s", logPath)
	}

	// Test session end logging
	messageCount := 5
	err = logger.LogSessionEnd(sessionID, messageCount)
	if err != nil {
		t.Fatalf("Failed to log session end: %v", err)
	}

	// Read and verify the final log
	sessionLog, err := logger.readSessionLog(sessionID)
	if err != nil {
		t.Fatalf("Failed to read session log: %v", err)
	}

	if sessionLog.SessionID != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, sessionLog.SessionID)
	}

	if sessionLog.Repository != repository {
		t.Errorf("Expected repository %s, got %s", repository, sessionLog.Repository)
	}

	if sessionLog.MessageCount != messageCount {
		t.Errorf("Expected message count %d, got %d", messageCount, sessionLog.MessageCount)
	}

	if sessionLog.DeletedAt == nil {
		t.Error("Expected DeletedAt to be set")
	}

	if sessionLog.StartedAt.IsZero() {
		t.Error("Expected StartedAt to be set")
	}
}