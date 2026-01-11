package webhook

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"strings"
)

// SignatureVerifier provides HMAC signature verification for webhooks
type SignatureVerifier struct{}

// NewSignatureVerifier creates a new SignatureVerifier
func NewSignatureVerifier() *SignatureVerifier {
	return &SignatureVerifier{}
}

// SignatureConfig contains configuration for signature verification
type SignatureConfig struct {
	// HeaderName is the name of the HTTP header containing the signature
	// Examples: "X-Hub-Signature-256", "X-Signature"
	HeaderName string

	// Secret is the shared secret used for HMAC computation
	Secret string

	// Algorithm specifies the hash algorithm to use
	// Supported values: "sha256", "sha1", "sha512"
	Algorithm string
}

// Verify verifies an HMAC signature against a payload
// Returns true if the signature is valid, false otherwise
func (v *SignatureVerifier) Verify(payload []byte, signatureHeader string, config SignatureConfig) bool {
	if signatureHeader == "" || config.Secret == "" {
		return false
	}

	// Select hash algorithm
	var h hash.Hash
	switch config.Algorithm {
	case "sha256":
		h = hmac.New(sha256.New, []byte(config.Secret))
	case "sha1":
		h = hmac.New(sha1.New, []byte(config.Secret))
	case "sha512":
		h = hmac.New(sha512.New, []byte(config.Secret))
	default:
		return false
	}

	// Compute expected signature
	h.Write(payload)
	expected := hex.EncodeToString(h.Sum(nil))

	// Handle GitHub-style format (algorithm=signature)
	signature := signatureHeader
	if strings.Contains(signatureHeader, "=") {
		parts := strings.SplitN(signatureHeader, "=", 2)
		if len(parts) == 2 {
			signature = parts[1]
		}
	}

	// Constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(expected), []byte(signature))
}

// VerifyGitHubSignature is a convenience method for verifying GitHub webhook signatures
// It handles both X-Hub-Signature (SHA1) and X-Hub-Signature-256 (SHA256)
func (v *SignatureVerifier) VerifyGitHubSignature(payload []byte, signatureHeader, secret string) bool {
	if signatureHeader == "" || secret == "" {
		return false
	}

	// Parse algorithm from header (e.g., "sha256=..." or "sha1=...")
	parts := strings.SplitN(signatureHeader, "=", 2)
	if len(parts) != 2 {
		return false
	}

	algorithm := parts[0]
	config := SignatureConfig{
		Secret:    secret,
		Algorithm: algorithm,
	}

	return v.Verify(payload, signatureHeader, config)
}
