package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptionServiceFactory_Create(t *testing.T) {
	factory := NewEncryptionServiceFactory()

	service, err := factory.Create()
	require.NoError(t, err)
	require.NotNil(t, service)

	// 現時点では Noop を返すことを確認
	assert.Equal(t, "noop", service.Algorithm())
	assert.Equal(t, "noop", service.KeyID())

	// NoopEncryptionService であることを確認
	_, ok := service.(*NoopEncryptionService)
	assert.True(t, ok, "Factory should create NoopEncryptionService")
}
