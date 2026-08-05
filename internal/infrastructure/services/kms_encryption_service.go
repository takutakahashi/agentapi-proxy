package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/services"
)

// KMSEncryptionService は AWS KMS を使用した暗号化サービス
type KMSEncryptionService struct {
	client *kms.Client
	keyID  string
	region string
}

// NewKMSEncryptionService は KMSEncryptionService を作成する
func NewKMSEncryptionService(keyID, region string) (*KMSEncryptionService, error) {
	if keyID == "" {
		return nil, fmt.Errorf("KMS key ID is required")
	}
	if region == "" {
		return nil, fmt.Errorf("AWS region is required")
	}

	// AWS SDK の設定を読み込み
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &KMSEncryptionService{
		client: kms.NewFromConfig(cfg),
		keyID:  keyID,
		region: region,
	}, nil
}

// Encrypt は平文を AWS KMS で暗号化する
func (s *KMSEncryptionService) Encrypt(ctx context.Context, plaintext string) (*services.EncryptedData, error) {
	result, err := s.client.Encrypt(ctx, &kms.EncryptInput{
		KeyId:     aws.String(s.keyID),
		Plaintext: []byte(plaintext),
	})
	if err != nil {
		return nil, fmt.Errorf("KMS encryption failed: %w", err)
	}

	return &services.EncryptedData{
		EncryptedValue: base64.StdEncoding.EncodeToString(result.CiphertextBlob),
		Metadata: services.EncryptionMetadata{
			Algorithm:   "aws-kms",
			KeyID:       s.keyID,
			EncryptedAt: time.Now(),
			Version:     "v1",
		},
	}, nil
}

// Decrypt は AWS KMS で暗号化されたデータを復号する
func (s *KMSEncryptionService) Decrypt(ctx context.Context, encrypted *services.EncryptedData) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted.EncryptedValue)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	result, err := s.client.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: ciphertext,
	})
	if err != nil {
		return "", fmt.Errorf("KMS decryption failed: %w", err)
	}

	return string(result.Plaintext), nil
}

// Algorithm は "aws-kms" を返す
func (s *KMSEncryptionService) Algorithm() string {
	return "aws-kms"
}

// KeyID は KMS キー ID を返す
func (s *KMSEncryptionService) KeyID() string {
	return s.keyID
}

// コンパイル時にインターフェースを実装していることを確認
var _ services.EncryptionService = (*KMSEncryptionService)(nil)
