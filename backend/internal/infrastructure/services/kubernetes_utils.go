package services

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// SanitizeLabelKey sanitizes a string to be used as a Kubernetes label key
func SanitizeLabelKey(s string) string {
	// Label keys must be 63 characters or less
	// Must start and end with alphanumeric character
	// Can contain dashes, underscores, dots, and alphanumerics
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	sanitized := re.ReplaceAllString(s, "-")
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}
	// Trim non-alphanumeric characters from start and end
	sanitized = strings.Trim(sanitized, "-_.")
	return sanitized
}

// SanitizeLabelValue sanitizes a string to be used as a Kubernetes label value
func SanitizeLabelValue(s string) string {
	// Label values must be 63 characters or less
	// Must start and end with alphanumeric character (or be empty)
	// Can contain dashes, underscores, dots, and alphanumerics
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]`)
	sanitized := re.ReplaceAllString(s, "-")
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}
	// Trim non-alphanumeric characters from start and end
	sanitized = strings.Trim(sanitized, "-_.")
	return sanitized
}

// HashLabelValue creates a sha256 hash of a value for use as a Kubernetes label value
// This allows querying by values that may contain invalid characters (e.g., "/" in team IDs)
// The hash is truncated to 16 characters for brevity while maintaining uniqueness
func HashLabelValue(value string) string {
	if value == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])[:16]
}

// HashTeamID creates a sha256 hash of the team ID for use as a Kubernetes label value
// This allows querying by team_id without sanitization issues (e.g., "/" in team IDs)
// The hash is truncated to 63 characters to fit within Kubernetes label value limits
func HashTeamID(teamID string) string {
	if teamID == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(teamID))
	hexHash := hex.EncodeToString(hash[:])
	// Truncate to 63 characters (Kubernetes label value limit)
	if len(hexHash) > 63 {
		hexHash = hexHash[:63]
	}
	return hexHash
}

// SanitizeSecretName sanitizes a string to be used as a Kubernetes Secret name
// Secret names must be lowercase, alphanumeric, and may contain dashes
// Example: "myorg/backend-team" -> "myorg-backend-team"
func SanitizeSecretName(s string) string {
	// Convert to lowercase
	sanitized := strings.ToLower(s)
	// Replace non-alphanumeric characters (except dash) with dash
	re := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	// Remove consecutive dashes
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}
	// Secret names must be 253 characters or less
	if len(sanitized) > 253 {
		sanitized = sanitized[:253]
	}
	// Trim dashes from start and end
	sanitized = strings.Trim(sanitized, "-")
	return sanitized
}

// Int64Ptr returns a pointer to an int64
func Int64Ptr(i int64) *int64 {
	return &i
}

// BoolPtr returns a pointer to a bool
func BoolPtr(b bool) *bool {
	return &b
}

// sanitizeLabelValue is an unexported wrapper for SanitizeLabelValue.
func sanitizeLabelValue(s string) string {
	return SanitizeLabelValue(s)
}

// sanitizeSecretName is an unexported wrapper for SanitizeSecretName.
func sanitizeSecretName(s string) string {
	return SanitizeSecretName(s)
}
