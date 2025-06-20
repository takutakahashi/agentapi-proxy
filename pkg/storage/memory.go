package storage

import (
	"fmt"
	"sync"
)

// MemoryStorage implements in-memory session storage
type MemoryStorage struct {
	sessions map[string]*SessionData
	mu       sync.RWMutex
}

// NewMemoryStorage creates a new memory storage instance
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		sessions: make(map[string]*SessionData),
	}
}

// Save stores a session in memory
func (m *MemoryStorage) Save(session *SessionData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if session.ID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}
	
	m.sessions[session.ID] = session
	return nil
}

// Load retrieves a session by ID
func (m *MemoryStorage) Load(sessionID string) (*SessionData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	session, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	
	return session, nil
}

// LoadAll retrieves all sessions
func (m *MemoryStorage) LoadAll() ([]*SessionData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	sessions := make([]*SessionData, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	
	return sessions, nil
}

// Delete removes a session from memory
func (m *MemoryStorage) Delete(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	delete(m.sessions, sessionID)
	return nil
}

// Update updates an existing session
func (m *MemoryStorage) Update(session *SessionData) error {
	return m.Save(session) // In memory, save and update are the same
}

// Close is a no-op for memory storage
func (m *MemoryStorage) Close() error {
	return nil
}