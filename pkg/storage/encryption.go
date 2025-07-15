package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

// getEncryptionKey generates a consistent encryption key from environment variable
func getEncryptionKey() []byte {
	// Get encryption key from environment variable
	key := os.Getenv("AGENTAPI_ENCRYPTION_KEY")
	if key == "" {
		// Fallback to default for development/testing
		// This should never be used in production
		log.Printf("WARNING: Using default encryption key. Set AGENTAPI_ENCRYPTION_KEY environment variable for production!")
		key = "agentapi-session-encryption-key"
	}

	hash := sha256.Sum256([]byte(key))
	return hash[:]
}

// encryptSessionSecrets encrypts sensitive fields in session data
func encryptSessionSecrets(session *SessionData) (*SessionData, error) {
	// Create a copy to avoid modifying the original
	encrypted := *session
	encrypted.Environment = make(map[string]string)

	// List of sensitive environment variable patterns
	sensitivePatterns := []string{
		"TOKEN", "KEY", "SECRET", "PASSWORD", "CREDENTIAL",
	}

	for key, value := range session.Environment {
		isSensitive := false
		for _, pattern := range sensitivePatterns {
			if containsIgnoreCase(key, pattern) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			encryptedValue, err := encryptString(value)
			if err == nil {
				encrypted.Environment[key] = "ENC:" + encryptedValue
			} else {
				// If encryption fails, store empty value
				encrypted.Environment[key] = "ENC:ERROR"
			}
		} else {
			encrypted.Environment[key] = value
		}
	}

	return &encrypted, nil
}

// decryptSessionSecrets decrypts sensitive fields in session data
func decryptSessionSecrets(session *SessionData) (*SessionData, error) {
	// Create a copy to avoid modifying the original
	decrypted := *session
	decrypted.Environment = make(map[string]string)

	for key, value := range session.Environment {
		if len(value) > 4 && value[:4] == "ENC:" {
			decryptedValue, err := decryptString(value[4:])
			if err == nil {
				decrypted.Environment[key] = decryptedValue
			} else {
				// If decryption fails, keep the encrypted value
				decrypted.Environment[key] = value
			}
		} else {
			decrypted.Environment[key] = value
		}
	}

	return &decrypted, nil
}

// encryptString encrypts a string using AES
func encryptString(text string) (string, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	plaintext := []byte(text)
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]

	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], plaintext)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptString decrypts a string using AES
func decryptString(cryptoText string) (string, error) {
	key := getEncryptionKey()
	ciphertext, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	if len(ciphertext) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)

	return string(ciphertext), nil
}

// containsIgnoreCase checks if a string contains another string (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToUpper(s), strings.ToUpper(substr))
}
