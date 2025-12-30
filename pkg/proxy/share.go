package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// SessionShare represents a shared session link
type SessionShare struct {
	token     string
	sessionID string
	createdBy string
	createdAt time.Time
	expiresAt *time.Time
}

// NewSessionShare creates a new SessionShare
func NewSessionShare(sessionID, createdBy string) *SessionShare {
	return &SessionShare{
		token:     generateShareToken(),
		sessionID: sessionID,
		createdBy: createdBy,
		createdAt: time.Now(),
	}
}

// NewSessionShareWithToken creates a SessionShare with a specific token (for loading from storage)
func NewSessionShareWithToken(token, sessionID, createdBy string, createdAt time.Time, expiresAt *time.Time) *SessionShare {
	return &SessionShare{
		token:     token,
		sessionID: sessionID,
		createdBy: createdBy,
		createdAt: createdAt,
		expiresAt: expiresAt,
	}
}

// Token returns the share token
func (s *SessionShare) Token() string {
	return s.token
}

// SessionID returns the session ID
func (s *SessionShare) SessionID() string {
	return s.sessionID
}

// CreatedBy returns the user who created the share
func (s *SessionShare) CreatedBy() string {
	return s.createdBy
}

// CreatedAt returns when the share was created
func (s *SessionShare) CreatedAt() time.Time {
	return s.createdAt
}

// ExpiresAt returns when the share expires (nil if no expiration)
func (s *SessionShare) ExpiresAt() *time.Time {
	return s.expiresAt
}

// SetExpiresAt sets the expiration time
func (s *SessionShare) SetExpiresAt(expiresAt *time.Time) {
	s.expiresAt = expiresAt
}

// IsExpired returns true if the share has expired
func (s *SessionShare) IsExpired() bool {
	if s.expiresAt == nil {
		return false
	}
	return time.Now().After(*s.expiresAt)
}

// generateShareToken generates a random 32-character hex token
func generateShareToken() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based token if crypto/rand fails
		return hex.EncodeToString([]byte(time.Now().String()))[:32]
	}
	return hex.EncodeToString(bytes)
}

// ShareRepository defines the interface for share storage
type ShareRepository interface {
	// Save persists a session share
	Save(share *SessionShare) error

	// FindByToken retrieves a share by its token
	FindByToken(token string) (*SessionShare, error)

	// FindBySessionID retrieves a share by session ID
	FindBySessionID(sessionID string) (*SessionShare, error)

	// Delete removes a share by session ID
	Delete(sessionID string) error

	// DeleteByToken removes a share by token
	DeleteByToken(token string) error
}
