package proxy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewSessionShare(t *testing.T) {
	share := NewSessionShare("session-123", "user-456")

	assert.NotEmpty(t, share.Token())
	assert.Equal(t, 32, len(share.Token())) // hex encoded 16 bytes = 32 chars
	assert.Equal(t, "session-123", share.SessionID())
	assert.Equal(t, "user-456", share.CreatedBy())
	assert.False(t, share.CreatedAt().IsZero())
	assert.Nil(t, share.ExpiresAt())
	assert.False(t, share.IsExpired())
}

func TestSessionShare_Expiration(t *testing.T) {
	share := NewSessionShare("session-123", "user-456")

	// Not expired when no expiration set
	assert.False(t, share.IsExpired())

	// Set expiration in the future
	future := time.Now().Add(time.Hour)
	share.SetExpiresAt(&future)
	assert.False(t, share.IsExpired())

	// Set expiration in the past
	past := time.Now().Add(-time.Hour)
	share.SetExpiresAt(&past)
	assert.True(t, share.IsExpired())
}

func TestNewSessionShareWithToken(t *testing.T) {
	createdAt := time.Now().Add(-time.Hour)
	expiresAt := time.Now().Add(time.Hour)

	share := NewSessionShareWithToken("token123", "session-456", "user-789", createdAt, &expiresAt)

	assert.Equal(t, "token123", share.Token())
	assert.Equal(t, "session-456", share.SessionID())
	assert.Equal(t, "user-789", share.CreatedBy())
	assert.Equal(t, createdAt, share.CreatedAt())
	assert.Equal(t, &expiresAt, share.ExpiresAt())
}

func TestShareToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)

	for i := 0; i < 100; i++ {
		share := NewSessionShare("session", "user")
		token := share.Token()
		assert.Equal(t, 32, len(token))
		assert.False(t, tokens[token], "Token should be unique")
		tokens[token] = true
	}
}
