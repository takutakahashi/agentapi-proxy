package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewKMSEncryptionService_ValidParams(t *testing.T) {
	keyID := "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012"
	region := "us-east-1"

	service, err := NewKMSEncryptionService(keyID, region)
	require.NoError(t, err)
	require.NotNil(t, service)

	assert.Equal(t, "aws-kms", service.Algorithm())
	assert.Equal(t, keyID, service.KeyID())
	assert.Equal(t, region, service.region)
}

func TestNewKMSEncryptionService_EmptyKeyID(t *testing.T) {
	_, err := NewKMSEncryptionService("", "us-east-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "KMS key ID is required")
}

func TestNewKMSEncryptionService_EmptyRegion(t *testing.T) {
	keyID := "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012"
	_, err := NewKMSEncryptionService(keyID, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AWS region is required")
}

func TestKMSEncryptionService_Algorithm(t *testing.T) {
	keyID := "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012"
	region := "us-east-1"

	service, err := NewKMSEncryptionService(keyID, region)
	require.NoError(t, err)

	assert.Equal(t, "aws-kms", service.Algorithm())
}

func TestKMSEncryptionService_KeyID(t *testing.T) {
	keyID := "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012"
	region := "us-east-1"

	service, err := NewKMSEncryptionService(keyID, region)
	require.NoError(t, err)

	assert.Equal(t, keyID, service.KeyID())
}

// 注意: 実際の暗号化・復号化のテストは AWS KMS が必要なため、
// ここでは基本的なインターフェースのテストのみ実施
// 実際の KMS を使ったテストは統合テストで行う
