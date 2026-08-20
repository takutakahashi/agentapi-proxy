package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoopEncryptionService_Encrypt(t *testing.T) {
	service := NewNoopEncryptionService()
	ctx := context.Background()

	plaintext := "test-plaintext"
	encrypted, err := service.Encrypt(ctx, plaintext)

	require.NoError(t, err)
	assert.Equal(t, plaintext, encrypted.EncryptedValue, "Noop encryption should return plaintext as-is")
	assert.Equal(t, "noop", encrypted.Metadata.Algorithm)
	assert.Equal(t, "noop", encrypted.Metadata.KeyID)
	assert.Equal(t, "v1", encrypted.Metadata.Version)
	assert.False(t, encrypted.Metadata.EncryptedAt.IsZero())
}

func TestNoopEncryptionService_Decrypt(t *testing.T) {
	service := NewNoopEncryptionService()
	ctx := context.Background()

	// まず暗号化
	plaintext := "test-plaintext"
	encrypted, err := service.Encrypt(ctx, plaintext)
	require.NoError(t, err)

	// 復号
	decrypted, err := service.Decrypt(ctx, encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted, "Decrypted value should match original plaintext")
}

func TestNoopEncryptionService_RoundTrip(t *testing.T) {
	service := NewNoopEncryptionService()
	ctx := context.Background()

	testCases := []struct {
		name      string
		plaintext string
	}{
		{"empty string", ""},
		{"simple text", "hello world"},
		{"unicode", "こんにちは世界"},
		{"special chars", "!@#$%^&*()_+-=[]{}|;':\",./<>?"},
		{"long text", "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 暗号化
			encrypted, err := service.Encrypt(ctx, tc.plaintext)
			require.NoError(t, err)

			// 復号
			decrypted, err := service.Decrypt(ctx, encrypted)
			require.NoError(t, err)

			// 元の値と一致することを確認
			assert.Equal(t, tc.plaintext, decrypted)
		})
	}
}

func TestNoopEncryptionService_Algorithm(t *testing.T) {
	service := NewNoopEncryptionService()
	assert.Equal(t, "noop", service.Algorithm())
}

func TestNoopEncryptionService_KeyID(t *testing.T) {
	service := NewNoopEncryptionService()
	assert.Equal(t, "noop", service.KeyID())
}
