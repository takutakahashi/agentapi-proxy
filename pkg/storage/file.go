package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

// FileStorage implements file-based session persistence
type FileStorage struct {
	filePath       string
	encryptSecrets bool
	sessions       map[string]*SessionData
	mu             sync.RWMutex
	syncInterval   time.Duration
	stopSync       chan struct{}
}

// NewFileStorage creates a new file storage instance
func NewFileStorage(filePath string, syncInterval int, encryptSecrets bool) (*FileStorage, error) {
	fs := &FileStorage{
		filePath:       filePath,
		encryptSecrets: encryptSecrets,
		sessions:       make(map[string]*SessionData),
		syncInterval:   time.Duration(syncInterval) * time.Second,
		stopSync:       make(chan struct{}),
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Load existing sessions
	if err := fs.loadFromFile(); err != nil {
		if !os.IsNotExist(err) {
			// For corrupted files or other errors, return the error
			return nil, fmt.Errorf("failed to load existing sessions: %w", err)
		} else {
			fmt.Printf("[FileStorage] No existing sessions file found at %s, starting fresh\n", filePath)
		}
	} else {
		fmt.Printf("[FileStorage] Loaded %d sessions from %s\n", len(fs.sessions), filePath)
	}

	// Start periodic sync if interval is set
	if syncInterval > 0 {
		go fs.periodicSync()
	}

	return fs, nil
}

// Save stores a session and syncs to file
func (fs *FileStorage) Save(session *SessionData) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if session.ID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}

	// Encrypt sensitive data if enabled
	if fs.encryptSecrets {
		if encrypted, err := encryptSessionSecrets(session); err != nil {
			// Log warning but continue with unencrypted session
		} else {
			session = encrypted
		}
	}

	fs.sessions[session.ID] = session
	return fs.syncToFile()
}

// Load retrieves a session by ID
func (fs *FileStorage) Load(sessionID string) (*SessionData, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	session, exists := fs.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	// Decrypt sensitive data if needed
	if fs.encryptSecrets {
		if decrypted, err := decryptSessionSecrets(session); err != nil {
			// Log warning but return session as-is
		} else {
			session = decrypted
		}
	}

	return session, nil
}

// LoadAll retrieves all sessions
func (fs *FileStorage) LoadAll() ([]*SessionData, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	sessions := make([]*SessionData, 0, len(fs.sessions))
	for _, session := range fs.sessions {
		// Decrypt sensitive data if needed
		if fs.encryptSecrets {
			if decrypted, err := decryptSessionSecrets(session); err != nil {
				// Log warning but continue with encrypted session
			} else {
				session = decrypted
			}
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// Delete removes a session and syncs to file
func (fs *FileStorage) Delete(sessionID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	delete(fs.sessions, sessionID)
	return fs.syncToFile()
}

// Update updates an existing session
func (fs *FileStorage) Update(session *SessionData) error {
	return fs.Save(session)
}

// Close stops the sync routine and performs final sync
func (fs *FileStorage) Close() error {
	close(fs.stopSync)

	fs.mu.Lock()
	defer fs.mu.Unlock()

	return fs.syncToFile()
}

// periodicSync runs periodic synchronization to disk
func (fs *FileStorage) periodicSync() {
	ticker := time.NewTicker(fs.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fs.mu.Lock()
			if err := fs.syncToFile(); err != nil {
				// Log error but continue
				fmt.Printf("Error syncing sessions to file: %v\n", err)
			}
			fs.mu.Unlock()
		case <-fs.stopSync:
			return
		}
	}
}

// syncToFile writes sessions to disk
func (fs *FileStorage) syncToFile() error {
	// Skip sync if no sessions to save
	if len(fs.sessions) == 0 {
		fmt.Printf("[FileStorage] No sessions to sync\n")
		return nil
	}

	// Prepare data structure
	data := struct {
		Sessions  []*SessionData `json:"sessions"`
		UpdatedAt time.Time      `json:"updated_at"`
	}{
		Sessions:  make([]*SessionData, 0, len(fs.sessions)),
		UpdatedAt: time.Now(),
	}

	for _, session := range fs.sessions {
		data.Sessions = append(data.Sessions, session)
	}

	// Use atomic write with JSON formatting
	jsonOptions := utils.JSONWriteOptions{
		Indent:   "  ",
		FileMode: 0644,
		Atomic:   true,
	}

	if err := utils.WriteJSONFile(fs.filePath, data, jsonOptions); err != nil {
		return fmt.Errorf("failed to write sessions file: %w", err)
	}

	fmt.Printf("[FileStorage] Successfully synced %d sessions to %s\n", len(data.Sessions), fs.filePath)
	return nil
}

// loadFromFile reads sessions from disk
func (fs *FileStorage) loadFromFile() error {
	file, err := os.Open(fs.filePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	var data struct {
		Sessions []*SessionData `json:"sessions"`
	}

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return fmt.Errorf("failed to decode sessions: %w", err)
	}

	fs.sessions = make(map[string]*SessionData, len(data.Sessions))
	for _, session := range data.Sessions {
		fs.sessions[session.ID] = session
	}

	return nil
}
