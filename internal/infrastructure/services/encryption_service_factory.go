package services

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
)

// EncryptionServiceFactory は EncryptionService の実装を作成するファクトリー
type EncryptionServiceFactory struct {
	kmsKeyID     string
	kmsRegion    string
	localKeyPath string
}

// NewEncryptionServiceFactory は EncryptionServiceFactory を作成する
// 環境変数から設定を読み込む
func NewEncryptionServiceFactory() *EncryptionServiceFactory {
	return &EncryptionServiceFactory{
		kmsKeyID:     os.Getenv("AGENTAPI_ENCRYPTION_KMS_KEY_ID"),
		kmsRegion:    os.Getenv("AGENTAPI_ENCRYPTION_KMS_REGION"),
		localKeyPath: os.Getenv("AGENTAPI_ENCRYPTION_KEY_FILE"),
	}
}

// Create は EncryptionService の実装を作成する
// 優先順位: KMS → Local → Noop
func (f *EncryptionServiceFactory) Create() (services.EncryptionService, error) {
	// 1. KMS が設定されていれば KMS を優先
	if f.kmsKeyID != "" && f.kmsRegion != "" {
		service, err := NewKMSEncryptionService(f.kmsKeyID, f.kmsRegion)
		if err == nil {
			// KMS が利用可能かテスト
			if err := f.testKMSAvailability(service); err == nil {
				log.Printf("[ENCRYPTION] Using AWS KMS encryption (key: %s, region: %s)", f.kmsKeyID, f.kmsRegion)
				return service, nil
			}
			// KMS が利用不可の場合はフォールバック
			log.Printf("[ENCRYPTION] KMS unavailable, falling back to next option: %v", err)
		} else {
			log.Printf("[ENCRYPTION] Failed to create KMS service: %v", err)
		}
	}

	// 2. ローカル暗号化が設定されていれば使用
	if f.localKeyPath != "" || os.Getenv("AGENTAPI_ENCRYPTION_KEY") != "" {
		service, err := NewLocalEncryptionService(f.localKeyPath)
		if err == nil {
			log.Printf("[ENCRYPTION] Using local AES-256-GCM encryption (key fingerprint: %s)", service.KeyID())
			return service, nil
		}
		log.Printf("[ENCRYPTION] Failed to create local encryption service: %v", err)
	}

	// 3. どちらも設定されていなければ Noop（暗号化なし）
	log.Printf("[ENCRYPTION] No encryption configured, using noop encryption (plaintext)")
	return NewNoopEncryptionService(), nil
}

// testKMSAvailability は KMS が利用可能かテストする
func (f *EncryptionServiceFactory) testKMSAvailability(service *KMSEncryptionService) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// テスト用の暗号化を実行
	_, err := service.Encrypt(ctx, "test")
	return err
}
