package crypto

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// decodeBase64 decodes a base64 encoded string
func decodeBase64(data string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(data)
}

// IsEncrypted checks if a value is encrypted (has RSA: prefix)
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, "RSA:")
}

// GetEncryptedValue extracts the encrypted value without the RSA: prefix
func GetEncryptedValue(value string) string {
	if IsEncrypted(value) {
		return value[4:]
	}
	return value
}

// AddEncryptionPrefix adds the RSA: prefix to mark a value as encrypted
func AddEncryptionPrefix(value string) string {
	return fmt.Sprintf("RSA:%s", value)
}