package proxy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestMemoryShareRepository_Save(t *testing.T) {
	repo := NewMemoryShareRepository()

	share := NewSessionShare("session-123", "user-456")
	err := repo.Save(share)
	require.NoError(t, err)

	// Verify share can be found
	found, err := repo.FindByToken(share.Token())
	require.NoError(t, err)
	assert.Equal(t, share.SessionID(), found.SessionID())
}

func TestMemoryShareRepository_FindByToken(t *testing.T) {
	repo := NewMemoryShareRepository()

	// Not found
	_, err := repo.FindByToken("nonexistent")
	assert.Error(t, err)

	// Save and find
	share := NewSessionShare("session-123", "user-456")
	err = repo.Save(share)
	require.NoError(t, err)

	found, err := repo.FindByToken(share.Token())
	require.NoError(t, err)
	assert.Equal(t, share.SessionID(), found.SessionID())
	assert.Equal(t, share.CreatedBy(), found.CreatedBy())
}

func TestMemoryShareRepository_FindBySessionID(t *testing.T) {
	repo := NewMemoryShareRepository()

	// Not found
	_, err := repo.FindBySessionID("nonexistent")
	assert.Error(t, err)

	// Save and find
	share := NewSessionShare("session-123", "user-456")
	err = repo.Save(share)
	require.NoError(t, err)

	found, err := repo.FindBySessionID("session-123")
	require.NoError(t, err)
	assert.Equal(t, share.Token(), found.Token())
}

func TestMemoryShareRepository_Delete(t *testing.T) {
	repo := NewMemoryShareRepository()

	share := NewSessionShare("session-123", "user-456")
	err := repo.Save(share)
	require.NoError(t, err)

	// Delete
	err = repo.Delete("session-123")
	require.NoError(t, err)

	// Verify deleted
	_, err = repo.FindBySessionID("session-123")
	assert.Error(t, err)

	_, err = repo.FindByToken(share.Token())
	assert.Error(t, err)
}

func TestMemoryShareRepository_DeleteByToken(t *testing.T) {
	repo := NewMemoryShareRepository()

	share := NewSessionShare("session-123", "user-456")
	err := repo.Save(share)
	require.NoError(t, err)

	// Delete by token
	err = repo.DeleteByToken(share.Token())
	require.NoError(t, err)

	// Verify deleted
	_, err = repo.FindBySessionID("session-123")
	assert.Error(t, err)
}

func TestMemoryShareRepository_SaveReplacesOldShare(t *testing.T) {
	repo := NewMemoryShareRepository()

	// Create first share
	share1 := NewSessionShare("session-123", "user-456")
	err := repo.Save(share1)
	require.NoError(t, err)

	oldToken := share1.Token()

	// Create second share for same session (should replace)
	share2 := NewSessionShare("session-123", "user-789")
	err = repo.Save(share2)
	require.NoError(t, err)

	// Old token should not work
	_, err = repo.FindByToken(oldToken)
	assert.Error(t, err)

	// New token should work
	found, err := repo.FindByToken(share2.Token())
	require.NoError(t, err)
	assert.Equal(t, "user-789", found.CreatedBy())

	// Find by session should return new share
	found, err = repo.FindBySessionID("session-123")
	require.NoError(t, err)
	assert.Equal(t, share2.Token(), found.Token())
}

func TestGenerateShareToken_Uniqueness(t *testing.T) {
	tokens := make(map[string]bool)

	for i := 0; i < 100; i++ {
		token := generateShareToken()
		assert.Equal(t, 32, len(token))
		assert.False(t, tokens[token], "Token should be unique")
		tokens[token] = true
	}
}

func TestMemoryShareRepository_CleanupExpired(t *testing.T) {
	repo := NewMemoryShareRepository()

	// Create a non-expired share
	share1 := NewSessionShare("session-1", "user-1")
	err := repo.Save(share1)
	require.NoError(t, err)

	// Create an expired share
	share2 := NewSessionShare("session-2", "user-2")
	past := time.Now().Add(-time.Hour)
	share2.SetExpiresAt(&past)
	err = repo.Save(share2)
	require.NoError(t, err)

	// Create another expired share
	share3 := NewSessionShare("session-3", "user-3")
	pastAgain := time.Now().Add(-time.Minute)
	share3.SetExpiresAt(&pastAgain)
	err = repo.Save(share3)
	require.NoError(t, err)

	// Cleanup should remove 2 expired shares
	count, err := repo.CleanupExpired()
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	// Non-expired share should still be accessible
	found, err := repo.FindByToken(share1.Token())
	require.NoError(t, err)
	assert.Equal(t, "session-1", found.SessionID())

	// Expired shares should be gone
	_, err = repo.FindByToken(share2.Token())
	assert.Error(t, err)

	_, err = repo.FindByToken(share3.Token())
	assert.Error(t, err)

	// Running cleanup again should return 0
	count, err = repo.CleanupExpired()
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}
