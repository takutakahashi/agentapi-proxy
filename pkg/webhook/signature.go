package webhook

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"strings"
)

// VerifyGitHubSignature verifies a GitHub webhook signature
// GitHub sends the signature in the X-Hub-Signature-256 header in the format "sha256=<signature>"
func VerifyGitHubSignature(payload []byte, signatureHeader, secret string) bool {
	if signatureHeader == "" || secret == "" {
		return false
	}

	// Parse the signature header
	// Format: "sha256=<hex-encoded-signature>" or "sha1=<hex-encoded-signature>"
	parts := strings.SplitN(signatureHeader, "=", 2)
	if len(parts) != 2 {
		return false
	}

	algorithm := parts[0]
	signature := parts[1]

	var h hash.Hash
	switch algorithm {
	case "sha256":
		h = hmac.New(sha256.New, []byte(secret))
	case "sha1":
		h = hmac.New(sha1.New, []byte(secret))
	default:
		return false
	}

	h.Write(payload)
	expected := hex.EncodeToString(h.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

// VerifyCustomSignature verifies a custom webhook signature with configurable header and algorithm
func VerifyCustomSignature(payload []byte, signature, secret, algorithm string) bool {
	if signature == "" || secret == "" {
		return false
	}

	var h hash.Hash
	switch strings.ToLower(algorithm) {
	case "sha256":
		h = hmac.New(sha256.New, []byte(secret))
	case "sha1":
		h = hmac.New(sha1.New, []byte(secret))
	default:
		return false
	}

	h.Write(payload)
	expected := hex.EncodeToString(h.Sum(nil))

	// Handle signature with or without algorithm prefix
	actualSignature := signature
	if parts := strings.SplitN(signature, "=", 2); len(parts) == 2 {
		actualSignature = parts[1]
	}

	return hmac.Equal([]byte(expected), []byte(actualSignature))
}
