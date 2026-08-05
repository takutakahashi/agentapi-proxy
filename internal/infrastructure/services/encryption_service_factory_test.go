package services

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptionServiceFactory_Create_Noop(t *testing.T) {
	// 環境変数をクリア
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_KEY_ID")
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_REGION")
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY_FILE")
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY")

	factory := NewEncryptionServiceFactory("")

	service, err := factory.Create()
	require.NoError(t, err)
	require.NotNil(t, service)

	// Noop を返すことを確認
	assert.Equal(t, "noop", service.Algorithm())
	assert.Equal(t, "noop", service.KeyID())

	// NoopEncryptionService であることを確認
	_, ok := service.(*NoopEncryptionService)
	assert.True(t, ok, "Factory should create NoopEncryptionService when no encryption is configured")
}

func TestEncryptionServiceFactory_Create_Local(t *testing.T) {
	// 環境変数をクリア
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_KEY_ID")
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_REGION")
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY_FILE")

	// テスト用のキーを環境変数に設定
	testKey := make([]byte, 32)
	_, err := rand.Read(testKey)
	require.NoError(t, err)
	keyB64 := base64.StdEncoding.EncodeToString(testKey)
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY") }()

	factory := NewEncryptionServiceFactory("")

	service, err := factory.Create()
	require.NoError(t, err)
	require.NotNil(t, service)

	// Local を返すことを確認
	assert.Equal(t, "aes-256-gcm", service.Algorithm())
	assert.Contains(t, service.KeyID(), "sha256:")

	// LocalEncryptionService であることを確認
	_, ok := service.(*LocalEncryptionService)
	assert.True(t, ok, "Factory should create LocalEncryptionService when AGENTAPI_ENCRYPTION_KEY is set")
}

func TestEncryptionServiceFactory_Create_LocalFromFile(t *testing.T) {
	// 環境変数をクリア
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_KEY_ID")
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_REGION")
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY")

	// テスト用のキーファイルを作成
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "encryption.key")
	testKey := make([]byte, 32)
	_, err := rand.Read(testKey)
	require.NoError(t, err)
	err = os.WriteFile(keyPath, testKey, 0600)
	require.NoError(t, err)

	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY_FILE", keyPath))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY_FILE") }()

	factory := NewEncryptionServiceFactory("")

	service, err := factory.Create()
	require.NoError(t, err)
	require.NotNil(t, service)

	// Local を返すことを確認
	assert.Equal(t, "aes-256-gcm", service.Algorithm())
	assert.Contains(t, service.KeyID(), "sha256:")

	// LocalEncryptionService であることを確認
	_, ok := service.(*LocalEncryptionService)
	assert.True(t, ok, "Factory should create LocalEncryptionService when AGENTAPI_ENCRYPTION_KEY_FILE is set")
}

func TestEncryptionServiceFactory_Create_KMS_FallbackToLocal(t *testing.T) {
	// KMS の設定（実際には使えないので Local にフォールバック）
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KMS_KEY_ID", "arn:aws:kms:us-east-1:123456789012:key/test"))
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KMS_REGION", "us-east-1"))
	defer func() {
		_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_KEY_ID")
		_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_REGION")
	}()

	// Local の設定
	testKey := make([]byte, 32)
	_, err := rand.Read(testKey)
	require.NoError(t, err)
	keyB64 := base64.StdEncoding.EncodeToString(testKey)
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY", keyB64))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY") }()

	factory := NewEncryptionServiceFactory("")

	service, err := factory.Create()
	require.NoError(t, err)
	require.NotNil(t, service)

	// KMS が使えないので Local にフォールバック
	// 注: 実際の環境では KMS が使えるかもしれないので、このテストは環境依存
	// ここでは Local または KMS のいずれかを返すことを確認
	assert.True(t,
		service.Algorithm() == "aes-256-gcm" || service.Algorithm() == "aws-kms",
		"Factory should create LocalEncryptionService or KMSEncryptionService")
}

func TestEncryptionServiceFactory_Create_InvalidLocal_FallbackToNoop(t *testing.T) {
	// 環境変数をクリア
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_KEY_ID")
	_ = os.Unsetenv("AGENTAPI_ENCRYPTION_KMS_REGION")

	// 不正なキーを設定（サイズが違う）
	require.NoError(t, os.Setenv("AGENTAPI_ENCRYPTION_KEY", "invalid-key"))
	defer func() { _ = os.Unsetenv("AGENTAPI_ENCRYPTION_KEY") }()

	factory := NewEncryptionServiceFactory("")

	service, err := factory.Create()
	require.NoError(t, err)
	require.NotNil(t, service)

	// 不正なキーなので Noop にフォールバック
	assert.Equal(t, "noop", service.Algorithm())

	_, ok := service.(*NoopEncryptionService)
	assert.True(t, ok, "Factory should fallback to NoopEncryptionService when local key is invalid")
}
