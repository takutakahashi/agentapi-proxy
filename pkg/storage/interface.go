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
	Type string `json:"type"` // "memory", "file", "sqlite", "s3"

	// File storage config
	FilePath       string `json:"file_path,omitempty"`
	SyncInterval   int    `json:"sync_interval_seconds,omitempty"`
	EncryptSecrets bool   `json:"encrypt_sensitive_data,omitempty"`

	// S3 storage config
	S3Bucket    string `json:"s3_bucket,omitempty"`
	S3Region    string `json:"s3_region,omitempty"`
	S3Prefix    string `json:"s3_prefix,omitempty"`
	S3Endpoint  string `json:"s3_endpoint,omitempty"` // For custom S3-compatible services
	S3AccessKey string `json:"s3_access_key,omitempty"`
	S3SecretKey string `json:"s3_secret_key,omitempty"`

	// Future: Database config
	DatabaseURL string `json:"database_url,omitempty"`
}
