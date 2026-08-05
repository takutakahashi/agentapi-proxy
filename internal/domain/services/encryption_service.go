package services

import (
	"context"
	"time"
)

// EncryptionService は暗号化・復号化を行うサービスのインターフェース
type EncryptionService interface {
	// Encrypt は平文を暗号化する
	Encrypt(ctx context.Context, plaintext string) (*EncryptedData, error)

	// Decrypt は暗号化されたデータを復号する
	Decrypt(ctx context.Context, encrypted *EncryptedData) (string, error)

	// Algorithm は暗号化方式の名前を返す
	Algorithm() string

	// KeyID はキーIDを返す
	KeyID() string
}

// EncryptedData は暗号化されたデータとメタデータを保持する
type EncryptedData struct {
	EncryptedValue string
	Metadata       EncryptionMetadata
}

// EncryptionMetadata は暗号化のメタデータ
type EncryptionMetadata struct {
	Algorithm   string    // 暗号化アルゴリズム ("noop", "aws-kms", "aes-256-gcm")
	KeyID       string    // キーID
	EncryptedAt time.Time // 暗号化日時
	Version     string    // バージョン
}
