package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

const (
	keySize = 2048
	keyDir  = "./keys"
)

// RSAEncryption handles RSA key generation and encryption/decryption
type RSAEncryption struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	mutex      sync.RWMutex
}

// NewRSAEncryption creates a new RSA encryption instance
func NewRSAEncryption() *RSAEncryption {
	return &RSAEncryption{}
}

// Initialize generates or loads RSA keys
func (e *RSAEncryption) Initialize() error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// Create key directory if it doesn't exist
	if err := os.MkdirAll(keyDir, 0700); err != nil {
		return fmt.Errorf("failed to create key directory: %v", err)
	}

	privateKeyPath := filepath.Join(keyDir, "private.pem")
	publicKeyPath := filepath.Join(keyDir, "public.pem")

	// Try to load existing keys
	if err := e.loadKeys(privateKeyPath, publicKeyPath); err == nil {
		log.Printf("Loaded existing RSA keys from %s", keyDir)
		return nil
	}

	// Generate new keys if loading failed
	log.Printf("Generating new RSA keys...")
	if err := e.generateKeys(); err != nil {
		return fmt.Errorf("failed to generate RSA keys: %v", err)
	}

	// Save keys to files
	if err := e.saveKeys(privateKeyPath, publicKeyPath); err != nil {
		return fmt.Errorf("failed to save RSA keys: %v", err)
	}

	log.Printf("Generated and saved new RSA keys to %s", keyDir)
	return nil
}

// generateKeys generates new RSA key pair
func (e *RSAEncryption) generateKeys() error {
	privateKey, err := rsa.GenerateKey(rand.Reader, keySize)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %v", err)
	}

	e.privateKey = privateKey
	e.publicKey = &privateKey.PublicKey
	return nil
}

// loadKeys loads RSA keys from files
func (e *RSAEncryption) loadKeys(privateKeyPath, publicKeyPath string) error {
	// Load private key
	privateKeyData, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key file: %v", err)
	}

	block, _ := pem.Decode(privateKeyData)
	if block == nil {
		return fmt.Errorf("failed to decode PEM block for private key")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %v", err)
	}

	// Load public key
	publicKeyData, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key file: %v", err)
	}

	block, _ = pem.Decode(publicKeyData)
	if block == nil {
		return fmt.Errorf("failed to decode PEM block for public key")
	}

	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse public key: %v", err)
	}

	rsaPublicKey, ok := publicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("public key is not RSA")
	}

	e.privateKey = privateKey
	e.publicKey = rsaPublicKey
	return nil
}

// saveKeys saves RSA keys to files
func (e *RSAEncryption) saveKeys(privateKeyPath, publicKeyPath string) error {
	// Save private key
	privateKeyDER := x509.MarshalPKCS1PrivateKey(e.privateKey)
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyDER,
	})

	if err := os.WriteFile(privateKeyPath, privateKeyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write private key: %v", err)
	}

	// Save public key
	publicKeyDER, err := x509.MarshalPKIXPublicKey(e.publicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %v", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyDER,
	})

	if err := os.WriteFile(publicKeyPath, publicKeyPEM, 0644); err != nil {
		return fmt.Errorf("failed to write public key: %v", err)
	}

	return nil
}

// GetPublicKeyPEM returns the public key in PEM format
func (e *RSAEncryption) GetPublicKeyPEM() (string, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.publicKey == nil {
		return "", fmt.Errorf("public key not initialized")
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(e.publicKey)
	if err != nil {
		return "", fmt.Errorf("failed to marshal public key: %v", err)
	}

	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyDER,
	})

	return string(publicKeyPEM), nil
}

// DecryptValue decrypts a value using the private key
func (e *RSAEncryption) DecryptValue(encryptedValue string) (string, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if e.privateKey == nil {
		return "", fmt.Errorf("private key not initialized")
	}

	// Decode base64 encoded encrypted value
	encryptedData, err := decodeBase64(encryptedValue)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %v", err)
	}

	// Try RSA OAEP first (Web Crypto API standard)
	hash := sha256.New()
	decryptedData, err := rsa.DecryptOAEP(hash, rand.Reader, e.privateKey, encryptedData, nil)
	if err != nil {
		// Fall back to PKCS1v15 for compatibility
		decryptedData, err = rsa.DecryptPKCS1v15(rand.Reader, e.privateKey, encryptedData)
		if err != nil {
			return "", fmt.Errorf("failed to decrypt with both OAEP and PKCS1v15: %v", err)
		}
	}

	return string(decryptedData), nil
}

// IsInitialized checks if the encryption is initialized
func (e *RSAEncryption) IsInitialized() bool {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.privateKey != nil && e.publicKey != nil
}