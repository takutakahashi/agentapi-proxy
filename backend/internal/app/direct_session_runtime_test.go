package app

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestNewDirectRuntimeTokenReturnsMatchingHashAndUniqueTokens(t *testing.T) {
	first, firstHash, err := newDirectRuntimeToken()
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := newDirectRuntimeToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first == second {
		t.Fatalf("runtime tokens are empty or repeated")
	}
	digest := sha256.Sum256([]byte(first))
	if got := hex.EncodeToString(digest[:]); got != firstHash {
		t.Fatalf("hash=%q, want %q", firstHash, got)
	}
}
