package mcpoauth

import (
	"sync"
	"time"
)

const stateTTL = 15 * time.Minute

// PendingState holds the transient data needed between the authorization
// redirect and the callback.
type PendingState struct {
	State        string
	CodeVerifier string
	UserID       string
	ServerName   string
	MCPServerURL string
	RedirectURI  string
	ClientID     string
	ClientSecret string // only for confidential clients
	TokenURL     string
	CreatedAt    time.Time
}

// StateStore is a thread-safe in-memory store for pending OAuth states.
type StateStore struct {
	mu    sync.Mutex
	items map[string]*PendingState
}

// NewStateStore creates a new StateStore.
func NewStateStore() *StateStore {
	return &StateStore{items: make(map[string]*PendingState)}
}

// Store saves a pending state.
func (s *StateStore) Store(state string, p *PendingState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup()
	s.items[state] = p
}

// Load retrieves and removes a pending state. Returns nil when not found or expired.
func (s *StateStore) Load(state string) (*PendingState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.items[state]
	if !ok {
		return nil, false
	}
	if time.Since(p.CreatedAt) > stateTTL {
		delete(s.items, state)
		return nil, false
	}
	delete(s.items, state)
	return p, true
}

// cleanup removes expired entries. Must be called with s.mu held.
func (s *StateStore) cleanup() {
	for k, v := range s.items {
		if time.Since(v.CreatedAt) > stateTTL {
			delete(s.items, k)
		}
	}
}
