package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
)

// LocalEncryptionService は AES-256-GCM を使用したローカル暗号化サービス
type LocalEncryptionService struct {
	key            []byte
	keyFingerprint string
}

// NewLocalEncryptionService は LocalEncryptionService を作成する
// keyPath が指定されていない場合、環境変数から読み込む
// keyEnvVar が空の場合は "AGENTAPI_ENCRYPTION_KEY" を使用
func NewLocalEncryptionService(keyPath string, keyEnvVar string) (*LocalEncryptionService, error) {
	if keyEnvVar == "" {
		keyEnvVar = "AGENTAPI_ENCRYPTION_KEY"
	}

	var key []byte
	var err error

	if keyPath != "" {
		// ファイルから読み込み
		key, err = os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read encryption key from file: %w", err)
		}
	} else {
		// 環境変数から読み込み
		keyB64 := os.Getenv(keyEnvVar)
		if keyB64 == "" {
			return nil, fmt.Errorf("encryption key not found: neither keyPath nor %s is set", keyEnvVar)
		}
		key, err = base64.StdEncoding.DecodeString(keyB64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode encryption key: %w", err)
		}
	}

	// キーのサイズチェック（AES-256 には 32 バイト必要）
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption key must be 32 bytes for AES-256, got %d bytes", len(key))
	}

	// キーのフィンガープリントを生成
	hash := sha256.Sum256(key)
	fingerprint := fmt.Sprintf("sha256:%x", hash[:8])

	return &LocalEncryptionService{
		key:            key,
		keyFingerprint: fingerprint,
	}, nil
}

// Encrypt は平文を AES-256-GCM で暗号化する
func (s *LocalEncryptionService) Encrypt(ctx context.Context, plaintext string) (*services.EncryptedData, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// ノンスを生成
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// 暗号化（ノンスを先頭に付加）
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	return &services.EncryptedData{
		EncryptedValue: base64.StdEncoding.EncodeToString(ciphertext),
		Metadata: services.EncryptionMetadata{
			Algorithm:   "aes-256-gcm",
			KeyID:       s.keyFingerprint,
			EncryptedAt: time.Now(),
			Version:     "v1",
		},
	}, nil
}

// Decrypt は AES-256-GCM で暗号化されたデータを復号する
func (s *LocalEncryptionService) Decrypt(ctx context.Context, encrypted *services.EncryptedData) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted.EncryptedValue)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short: %d bytes, expected at least %d bytes", len(ciphertext), nonceSize)
	}

	// ノンスと暗号文を分離
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	// 復号
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// Algorithm は "aes-256-gcm" を返す
func (s *LocalEncryptionService) Algorithm() string {
	return "aes-256-gcm"
}

// KeyID はキーのフィンガープリントを返す
func (s *LocalEncryptionService) KeyID() string {
	return s.keyFingerprint
}

// コンパイル時にインターフェースを実装していることを確認
var _ services.EncryptionService = (*LocalEncryptionService)(nil)
