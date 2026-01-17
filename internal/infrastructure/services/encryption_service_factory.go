package services

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
)

// EncryptionServiceFactory は EncryptionService の実装を作成するファクトリー
type EncryptionServiceFactory struct{}

// NewEncryptionServiceFactory は EncryptionServiceFactory を作成する
func NewEncryptionServiceFactory() *EncryptionServiceFactory {
	return &EncryptionServiceFactory{}
}

// Create は EncryptionService の実装を作成する
// 現時点では NoopEncryptionService のみを返す
func (f *EncryptionServiceFactory) Create() (services.EncryptionService, error) {
	// TODO: 環境変数から暗号化方式を選択する実装を追加
	// - KMS が設定されていれば KMSEncryptionService
	// - ローカルキーが設定されていれば LocalEncryptionService
	// - どちらも設定されていなければ NoopEncryptionService
	return NewNoopEncryptionService(), nil
}
