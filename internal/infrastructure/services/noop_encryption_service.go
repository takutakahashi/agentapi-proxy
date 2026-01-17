package services

import (
	"context"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
)

// NoopEncryptionService は暗号化を行わないダミーの実装
// インターフェースが通る状態を作るために使用する
type NoopEncryptionService struct{}

// NewNoopEncryptionService は NoopEncryptionService を作成する
func NewNoopEncryptionService() *NoopEncryptionService {
	return &NoopEncryptionService{}
}

// Encrypt は平文をそのまま返す（暗号化しない）
func (s *NoopEncryptionService) Encrypt(ctx context.Context, plaintext string) (*services.EncryptedData, error) {
	return &services.EncryptedData{
		EncryptedValue: plaintext, // 平文をそのまま格納
		Metadata: services.EncryptionMetadata{
			Algorithm:   "noop",
			KeyID:       "noop",
			EncryptedAt: time.Now(),
			Version:     "v1",
		},
	}, nil
}

// Decrypt は暗号化されたデータをそのまま返す（復号しない）
func (s *NoopEncryptionService) Decrypt(ctx context.Context, encrypted *services.EncryptedData) (string, error) {
	return encrypted.EncryptedValue, nil // そのまま返す
}

// Algorithm は "noop" を返す
func (s *NoopEncryptionService) Algorithm() string {
	return "noop"
}

// KeyID は "noop" を返す
func (s *NoopEncryptionService) KeyID() string {
	return "noop"
}

// createMetadata は metadata を作成する（テスト用）
func (s *NoopEncryptionService) createMetadata() services.EncryptionMetadata {
	return services.EncryptionMetadata{
		Algorithm:   "noop",
		KeyID:       "noop",
		EncryptedAt: time.Now(),
		Version:     "v1",
	}
}

// コンパイル時にインターフェースを実装していることを確認
var _ services.EncryptionService = (*NoopEncryptionService)(nil)
