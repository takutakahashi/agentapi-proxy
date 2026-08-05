package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEncryptionServiceRegistry(t *testing.T) {
	primary := NewNoopEncryptionService()
	registry := NewEncryptionServiceRegistry(primary)

	assert.NotNil(t, registry)
	assert.Equal(t, primary, registry.GetForEncryption())
}

func TestEncryptionServiceRegistry_Register(t *testing.T) {
	registry := NewEncryptionServiceRegistry(nil)

	noop := NewNoopEncryptionService()
	registry.Register(noop)

	// Should be registered by algorithm
	assert.NotNil(t, registry.servicesByAlgorithm["noop"])
	assert.NotNil(t, registry.servicesByAlgorithmAndKey["noop:noop"])
}

func TestEncryptionServiceRegistry_GetForEncryption(t *testing.T) {
	primary := NewNoopEncryptionService()
	registry := NewEncryptionServiceRegistry(primary)

	// Add another service
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	keyB64 := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY") }()

	local, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)
	registry.Register(local)

	// GetForEncryption should always return primary
	assert.Equal(t, primary, registry.GetForEncryption())
}

func TestEncryptionServiceRegistry_GetForDecryption_ExactMatch(t *testing.T) {
	registry := NewEncryptionServiceRegistry(nil)

	// Create and register a local service
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	keyB64 := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY") }()

	local, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)
	registry.Register(local)

	// Encrypt something to get metadata
	ctx := context.Background()
	encrypted, err := local.Encrypt(ctx, "test")
	require.NoError(t, err)

	// GetForDecryption should return the exact service
	service := registry.GetForDecryption(encrypted.Metadata)
	assert.Equal(t, local, service)
}

func TestEncryptionServiceRegistry_GetForDecryption_AlgorithmMatch(t *testing.T) {
	registry := NewEncryptionServiceRegistry(nil)

	// Register a noop service
	noop := NewNoopEncryptionService()
	registry.Register(noop)

	// Create metadata with same algorithm but different keyID
	metadata := noop.createMetadata()
	metadata.KeyID = "different-key-id"

	// Should match by algorithm even with different keyID
	service := registry.GetForDecryption(metadata)
	assert.Equal(t, noop, service)
}

func TestEncryptionServiceRegistry_GetForDecryption_FallbackToPrimary(t *testing.T) {
	primary := NewNoopEncryptionService()
	registry := NewEncryptionServiceRegistry(primary)

	// Create metadata for a service that's not registered
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	keyB64 := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY") }()

	local, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)

	ctx := context.Background()
	encrypted, err := local.Encrypt(ctx, "test")
	require.NoError(t, err)

	// GetForDecryption should fallback to primary (noop) since local is not registered
	service := registry.GetForDecryption(encrypted.Metadata)
	assert.Equal(t, primary, service)
}

func TestEncryptionServiceRegistry_SetPrimary(t *testing.T) {
	noop := NewNoopEncryptionService()
	registry := NewEncryptionServiceRegistry(noop)

	// Create a new local service
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	keyB64 := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY") }()

	local, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)

	// Set as primary
	registry.SetPrimary(local)

	// Should be the new primary
	assert.Equal(t, local, registry.GetForEncryption())

	// Should also be registered
	assert.NotNil(t, registry.servicesByAlgorithm["aes-256-gcm"])
}

func TestEncryptionServiceRegistry_MultipleServices(t *testing.T) {
	// Create registry with noop as primary
	noop := NewNoopEncryptionService()
	registry := NewEncryptionServiceRegistry(noop)

	// Add local service
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	keyB64 := base64.StdEncoding.EncodeToString(key)
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY") }()

	local, err := NewLocalEncryptionService("", "")
	require.NoError(t, err)
	registry.Register(local)

	ctx := context.Background()

	// Encrypt with noop (primary)
	noopEncrypted, err := registry.GetForEncryption().Encrypt(ctx, "test")
	require.NoError(t, err)

	// Should decrypt with noop
	noopService := registry.GetForDecryption(noopEncrypted.Metadata)
	assert.Equal(t, "noop", noopService.Algorithm())

	// Encrypt with local (simulate old data)
	localEncrypted, err := local.Encrypt(ctx, "test")
	require.NoError(t, err)

	// Should decrypt with local
	localService := registry.GetForDecryption(localEncrypted.Metadata)
	assert.Equal(t, "aes-256-gcm", localService.Algorithm())
}
