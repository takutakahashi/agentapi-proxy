package kubelabel

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashValue creates a short sha256 hash suitable for Kubernetes label values.
// This allows querying by values that may contain invalid characters such as "/" in team IDs.
func HashValue(value string) string {
	if value == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])[:16]
}
