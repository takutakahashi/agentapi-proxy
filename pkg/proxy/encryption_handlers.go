package proxy

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/crypto"
)

// EncryptionHandlers handles encryption-related endpoints
type EncryptionHandlers struct {
	rsaEncryption *crypto.RSAEncryption
	enabled       bool
}

// NewEncryptionHandlers creates new encryption handlers
func NewEncryptionHandlers(enabled bool) *EncryptionHandlers {
	handlers := &EncryptionHandlers{
		enabled: enabled,
	}

	if enabled {
		handlers.rsaEncryption = crypto.NewRSAEncryption()
		if err := handlers.rsaEncryption.Initialize(); err != nil {
			// Log error but don't fail startup
			// The endpoint will return an error if encryption is not available
			handlers.enabled = false
		}
	}

	return handlers
}

// GetPublicKey handles GET /encryption.pub requests
func (h *EncryptionHandlers) GetPublicKey(c echo.Context) error {
	if !h.enabled {
		return echo.NewHTTPError(http.StatusNotFound, "Encryption is not enabled")
	}

	if !h.rsaEncryption.IsInitialized() {
		return echo.NewHTTPError(http.StatusInternalServerError, "Encryption not initialized")
	}

	publicKeyPEM, err := h.rsaEncryption.GetPublicKeyPEM()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get public key")
	}

	// Return the public key as plain text
	return c.String(http.StatusOK, publicKeyPEM)
}

// DecryptValue decrypts a value using the private key
func (h *EncryptionHandlers) DecryptValue(encryptedValue string) (string, error) {
	if !h.enabled || !h.rsaEncryption.IsInitialized() {
		return encryptedValue, nil // Return original value if encryption is not enabled
	}

	return h.rsaEncryption.DecryptValue(encryptedValue)
}

// IsEnabled returns whether encryption is enabled
func (h *EncryptionHandlers) IsEnabled() bool {
	return h.enabled && h.rsaEncryption != nil && h.rsaEncryption.IsInitialized()
}