package storage

import (
	"time"
)

// SessionData represents the persistable session data
type SessionData struct {
	ID          string            `json:"id"`
	Port        int               `json:"port"`
	StartedAt   time.Time         `json:"started_at"`
	UserID      string            `json:"user_id"`
	Status      string            `json:"status"`
	Environment map[string]string `json:"environment"`
	Tags        map[string]string `json:"tags"`
	ProcessID   int               `json:"process_id,omitempty"` // For validation on recovery
	Command     []string          `json:"command,omitempty"`    // To recreate process if needed
	WorkingDir  string            `json:"working_dir,omitempty"`
}

// Storage defines the interface for session persistence
type Storage interface {
	// Save persists a session to storage
	Save(session *SessionData) error

	// Load retrieves a session by ID
	Load(sessionID string) (*SessionData, error)

	// LoadAll retrieves all sessions
	LoadAll() ([]*SessionData, error)

	// Delete removes a session from storage
	Delete(sessionID string) error

	// Update updates an existing session
	Update(session *SessionData) error

	// Close cleans up any resources
	Close() error
}

// StorageConfig holds configuration for storage backends
type StorageConfig struct {
	Type string `json:"type"` // "memory", "file", "sqlite"

	// File storage config
	FilePath       string `json:"file_path,omitempty"`
	SyncInterval   int    `json:"sync_interval_seconds,omitempty"`
	EncryptSecrets bool   `json:"encrypt_sensitive_data,omitempty"`

	// Future: Database config
	DatabaseURL string `json:"database_url,omitempty"`
}
