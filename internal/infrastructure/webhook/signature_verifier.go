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

	// Prefix specifies the exact prefix to strip from the header value before comparing.
	// When empty, auto-detection is used: if the header value contains "=",
	// the part before and including "=" is stripped (e.g., "sha256=<hex>" → "<hex>").
	// When set to a non-empty string, that exact prefix is stripped.
	// Use this for services that send plain hex digests without any prefix (e.g., Sentry).
	// Example: "" (auto-detect), "sha256=" (GitHub-style), "v0=" (Slack-style)
	Prefix string
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

	// Strip prefix to extract the raw hex digest
	signature := signatureHeader
	if config.Prefix != "" {
		// Explicit prefix configured: strip exactly this prefix
		if !strings.HasPrefix(signatureHeader, config.Prefix) {
			return false
		}
		signature = signatureHeader[len(config.Prefix):]
	} else {
		// Auto-detect: handle format like "algorithm=signature" (e.g., GitHub "sha256=<hex>")
		if strings.Contains(signatureHeader, "=") {
			parts := strings.SplitN(signatureHeader, "=", 2)
			if len(parts) == 2 {
				signature = parts[1]
			}
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
