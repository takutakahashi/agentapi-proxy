package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileStorage implements file-based session persistence
type FileStorage struct {
	filePath       string
	encryptSecrets bool
	encryptionKey  []byte
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

	if encryptSecrets {
		// Generate encryption key from a fixed string for now
		// In production, this should come from a secure key management system
		hash := sha256.Sum256([]byte("agentapi-session-encryption-key"))
		fs.encryptionKey = hash[:]
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Load existing sessions
	if err := fs.loadFromFile(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load existing sessions: %w", err)
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
		session = fs.encryptSensitiveData(session)
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
		session = fs.decryptSensitiveData(session)
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
			session = fs.decryptSensitiveData(session)
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
	// Create temporary file
	tempFile := fs.filePath + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer file.Close()

	// Encode sessions
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	
	data := struct {
		Sessions []*SessionData `json:"sessions"`
		UpdatedAt time.Time     `json:"updated_at"`
	}{
		Sessions:  make([]*SessionData, 0, len(fs.sessions)),
		UpdatedAt: time.Now(),
	}

	for _, session := range fs.sessions {
		data.Sessions = append(data.Sessions, session)
	}

	if err := encoder.Encode(&data); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to encode sessions: %w", err)
	}

	// Sync to disk
	if err := file.Sync(); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, fs.filePath); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// loadFromFile reads sessions from disk
func (fs *FileStorage) loadFromFile() error {
	file, err := os.Open(fs.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

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

// encryptSensitiveData encrypts sensitive fields in session data
func (fs *FileStorage) encryptSensitiveData(session *SessionData) *SessionData {
	// Create a copy to avoid modifying the original
	encrypted := *session
	encrypted.Environment = make(map[string]string)

	// List of sensitive environment variable patterns
	sensitivePatterns := []string{
		"TOKEN", "KEY", "SECRET", "PASSWORD", "CREDENTIAL",
	}

	for key, value := range session.Environment {
		isSensitive := false
		for _, pattern := range sensitivePatterns {
			if containsIgnoreCase(key, pattern) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			encryptedValue, err := fs.encrypt(value)
			if err == nil {
				encrypted.Environment[key] = "ENC:" + encryptedValue
			} else {
				// If encryption fails, store empty value
				encrypted.Environment[key] = "ENC:ERROR"
			}
		} else {
			encrypted.Environment[key] = value
		}
	}

	return &encrypted
}

// decryptSensitiveData decrypts sensitive fields in session data
func (fs *FileStorage) decryptSensitiveData(session *SessionData) *SessionData {
	// Create a copy to avoid modifying the original
	decrypted := *session
	decrypted.Environment = make(map[string]string)

	for key, value := range session.Environment {
		if len(value) > 4 && value[:4] == "ENC:" {
			decryptedValue, err := fs.decrypt(value[4:])
			if err == nil {
				decrypted.Environment[key] = decryptedValue
			} else {
				// If decryption fails, keep the encrypted value
				decrypted.Environment[key] = value
			}
		} else {
			decrypted.Environment[key] = value
		}
	}

	return &decrypted
}

// encrypt encrypts a string using AES
func (fs *FileStorage) encrypt(text string) (string, error) {
	block, err := aes.NewCipher(fs.encryptionKey)
	if err != nil {
		return "", err
	}

	plaintext := []byte(text)
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], plaintext)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts a string using AES
func (fs *FileStorage) decrypt(cryptoText string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(fs.encryptionKey)
	if err != nil {
		return "", err
	}

	if len(ciphertext) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)

	return string(ciphertext), nil
}

// containsIgnoreCase checks if a string contains another string (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	s = string([]rune(s))
	substr = string([]rune(substr))
	
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] && s[i+j] != substr[j]+32 && s[i+j] != substr[j]-32 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	
	return false
}