package githubsync

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/kms"
)

const (
	// encPrefix identifies encrypted field values in YAML exports
	encPrefix = "enc:v1:"
	// dekSize is 256-bit AES key
	dekSize = 32
	// nonceSize is 96-bit GCM nonce
	nonceSize = 12
)

// sensitiveKeyPatterns are substrings that mark a key as sensitive
var sensitiveKeyPatterns = []string{
	"SECRET", "TOKEN", "KEY", "PASSWORD", "PASS",
	"CREDENTIAL", "AUTH", "PRIVATE", "API_KEY",
}

// SyncEncryptor manages fixed-DEK AWS KMS envelope encryption for GitHub sync.
// The DEK is generated once per GitSyncConfig and stored encrypted in Settings.
type SyncEncryptor struct {
	kmsClient *kms.Client
	kmsKeyARN string
}

// NewSyncEncryptor creates a SyncEncryptor using AWS default credentials
// (IRSA > env vars > ~/.aws).
func NewSyncEncryptor(ctx context.Context, kmsKeyARN, awsRegion string) (*SyncEncryptor, error) {
	if kmsKeyARN == "" {
		return nil, fmt.Errorf("kms_key_arn is required for GitHub sync encryption")
	}
	if awsRegion == "" {
		return nil, fmt.Errorf("aws_region is required for GitHub sync encryption")
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(awsRegion))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &SyncEncryptor{
		kmsClient: kms.NewFromConfig(cfg),
		kmsKeyARN: kmsKeyARN,
	}, nil
}

// GenerateAndEncryptDEK generates a fresh random 256-bit DEK, encrypts it with KMS,
// and returns both the raw DEK bytes (for immediate use) and the base64 encrypted DEK
// (for storage in GitSyncConfig.Encryption.EncryptedDEK).
func (e *SyncEncryptor) GenerateAndEncryptDEK(ctx context.Context) (dek []byte, encryptedDEK string, err error) {
	dek = make([]byte, dekSize)
	if _, err = rand.Read(dek); err != nil {
		return nil, "", fmt.Errorf("failed to generate DEK: %w", err)
	}

	result, err := e.kmsClient.Encrypt(ctx, &kms.EncryptInput{
		KeyId:     aws.String(e.kmsKeyARN),
		Plaintext: dek,
	})
	if err != nil {
		return nil, "", fmt.Errorf("KMS encrypt DEK failed: %w", err)
	}

	encryptedDEK = base64.StdEncoding.EncodeToString(result.CiphertextBlob)
	return dek, encryptedDEK, nil
}

// DecryptDEK decrypts the base64-encoded encrypted DEK stored in GitSyncConfig.
func (e *SyncEncryptor) DecryptDEK(ctx context.Context, encryptedDEK string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedDEK)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode encryptedDEK: %w", err)
	}

	result, err := e.kmsClient.Decrypt(ctx, &kms.DecryptInput{
		CiphertextBlob: ciphertext,
	})
	if err != nil {
		return nil, fmt.Errorf("KMS decrypt DEK failed: %w", err)
	}

	return result.Plaintext, nil
}

// EncryptField encrypts a plaintext value with AES-256-GCM using a deterministic nonce.
// The nonce = HMAC-SHA256(DEK, plaintext)[:12] — same plaintext+DEK always yields the
// same ciphertext, keeping git diffs clean.
// Returns "enc:v1:<base64(nonce||ciphertext)>".
func EncryptField(dek []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", fmt.Errorf("AES cipher init failed: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("GCM init failed: %w", err)
	}

	mac := hmac.New(sha256.New, dek)
	mac.Write([]byte(plaintext))
	nonce := mac.Sum(nil)[:nonceSize]

	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	combined := make([]byte, nonceSize+len(ciphertext))
	copy(combined[:nonceSize], nonce)
	copy(combined[nonceSize:], ciphertext)
	return encPrefix + base64.StdEncoding.EncodeToString(combined), nil
}

// DecryptField decrypts a value produced by EncryptField.
// If the value does not have the enc:v1: prefix it is returned as-is (plaintext pass-through).
func DecryptField(dek []byte, value string) (string, error) {
	if !strings.HasPrefix(value, encPrefix) {
		return value, nil
	}

	encoded := strings.TrimPrefix(value, encPrefix)
	combined, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to base64-decode encrypted field: %w", err)
	}
	if len(combined) < nonceSize {
		return "", fmt.Errorf("encrypted field too short")
	}

	nonce := combined[:nonceSize]
	ciphertext := combined[nonceSize:]

	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", fmt.Errorf("AES cipher init failed: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("GCM init failed: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("AES-GCM decryption failed: %w", err)
	}

	return string(plaintext), nil
}

// IsEncrypted returns true when the value carries the enc:v1: prefix.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, encPrefix)
}

// IsSensitiveKey returns true when an env-var or header key name suggests its value is secret.
func IsSensitiveKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, p := range sensitiveKeyPatterns {
		if strings.Contains(upper, p) {
			return true
		}
	}
	return false
}
