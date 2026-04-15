package mcpoauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// GenerateCodeVerifier generates a cryptographically random PKCE code_verifier
// per RFC 7636 (43–128 characters from the unreserved set).
func GenerateCodeVerifier() (string, error) {
	b := make([]byte, 32) // 32 bytes → 43-char base64url
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CodeChallenge derives the S256 code_challenge from a code_verifier.
func CodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
