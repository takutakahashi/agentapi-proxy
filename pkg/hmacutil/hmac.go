// Package hmacutil provides shared HMAC-SHA256 signing and verification utilities
// for Proxy A ↔ Proxy B communication.
//
// Signature format:
//
//	HMAC-SHA256(secret, "METHOD\nPATH?QUERY\nTIMESTAMP\nBODY")
//
// where TIMESTAMP is a Unix epoch in seconds (string), and BODY is the raw request body
// (may be empty). The resulting signature is sent as the X-Hub-Signature-256 header
// ("sha256=<hex>"), and the timestamp is sent separately as the X-Timestamp header.
//
// Timestamp validation rejects requests whose timestamp deviates by more than
// MaxTimestampSkew from the server clock, preventing replay attacks.
package hmacutil

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// TimestampHeader is the HTTP header name for the Unix timestamp included in signing.
	TimestampHeader = "X-Timestamp"

	// MaxTimestampSkew is the maximum allowed difference between the timestamp in the
	// request and the server's current time. Requests outside this window are rejected.
	MaxTimestampSkew = 5 * time.Minute
)

// BuildMessage constructs the canonical signing message in the form:
//
//	METHOD\nPATH?QUERY\nTIMESTAMP\nBODY
//
// pathWithQuery should be the full request URI including query string (e.g.
// "/api/v1/sessions?user_id=alice"). body may be nil or empty.
func BuildMessage(method, pathWithQuery, timestamp string, body []byte) []byte {
	var sb strings.Builder
	sb.WriteString(strings.ToUpper(method))
	sb.WriteByte('\n')
	sb.WriteString(pathWithQuery)
	sb.WriteByte('\n')
	sb.WriteString(timestamp)
	sb.WriteByte('\n')
	msg := []byte(sb.String())
	if len(body) > 0 {
		msg = append(msg, body...)
	}
	return msg
}

// Sign computes HMAC-SHA256 over message and returns the signature as "sha256=<hex>".
func Sign(secret, message []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(message)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify returns true if sig matches the expected HMAC-SHA256 signature of message.
// The comparison is constant-time to prevent timing attacks.
func Verify(secret, message []byte, sig string) bool {
	expected := Sign(secret, message)
	return hmac.Equal([]byte(sig), []byte(expected))
}

// NowTimestamp returns the current Unix epoch as a decimal string.
func NowTimestamp() string {
	return strconv.FormatInt(time.Now().Unix(), 10)
}

// ValidateTimestamp parses ts as a Unix epoch (decimal string) and verifies that
// it falls within ±MaxTimestampSkew of the current time.
//
// If ts is empty, the function returns an error – callers that need backward
// compatibility should handle the empty-string case themselves before calling
// ValidateTimestamp.
func ValidateTimestamp(ts string) error {
	if ts == "" {
		return fmt.Errorf("missing timestamp")
	}
	epoch, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp %q: %w", ts, err)
	}
	diff := time.Since(time.Unix(epoch, 0))
	if diff < 0 {
		diff = -diff
	}
	if diff > MaxTimestampSkew {
		return fmt.Errorf("timestamp %q is outside allowed skew (%s)", ts, MaxTimestampSkew)
	}
	return nil
}
