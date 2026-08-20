// Package apitoken provides shared helpers for generating opaque, random
// API token IDs and secrets, and computing safe display prefixes.
package apitoken

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const (
	// TokenIDPrefix is the prefix for the public, opaque token ID.
	TokenIDPrefix = "tok_"
	// SecretPrefix is the prefix for the plaintext token secret. New tokens
	// use "apt_"; migrated legacy tokens keep their original value (e.g.
	// "ap_") so they continue to authenticate after migration.
	SecretPrefix = "apt_"
	// displayPrefixLen is the number of characters of the secret shown in
	// list/get metadata responses. It is long enough to help a user identify
	// a key but far too short to be useful as a credential.
	displayPrefixLen = 10
)

// GenerateTokenID returns a new opaque token ID: "tok_" + 16 random bytes hex.
func GenerateTokenID() (string, error) {
	return generatePrefixed(TokenIDPrefix, 16)
}

// GenerateSecret returns a new plaintext token secret: "apt_" + 32 random
// bytes hex.
func GenerateSecret() (string, error) {
	return generatePrefixed(SecretPrefix, 32)
}

// DisplayPrefix returns a safe, short prefix of the secret for display.
func DisplayPrefix(secret string) string {
	if len(secret) <= displayPrefixLen {
		return secret
	}
	return secret[:displayPrefixLen]
}

// MigrationTokenID returns a deterministic token ID derived from a migration
// source identifier. Migration IDs are stable so the migration is idempotent
// across restarts: re-running migration against the same legacy source
// resolves to the same new token ID and Create returns
// ErrAPITokenAlreadyExists, which the migration treats as "already migrated".
//
// The returned ID is sanitized to be a valid Kubernetes resource-name suffix
// (lowercase, alphanumerics and dashes only).
func MigrationTokenID(source string) string {
	return TokenIDPrefix + "migrate-" + sanitizeForID(source)
}

func generatePrefixed(prefix string, n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

// sanitizeForID lowercases the input and replaces any character that is not a
// lowercase letter, digit, or dash with a dash, collapsing runs of dashes.
// This makes the value safe to embed in a Kubernetes resource name.
func sanitizeForID(s string) string {
	var out []byte
	prevDash := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c)
			prevDash = false
		case c >= '0' && c <= '9':
			out = append(out, c)
			prevDash = false
		default:
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	// trim trailing dash
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}
