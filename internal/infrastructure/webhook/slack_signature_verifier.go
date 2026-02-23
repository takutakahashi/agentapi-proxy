package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrTimestampExpired is returned when the Slack request timestamp is too old
var ErrTimestampExpired = errors.New("slack request timestamp expired (possible replay attack)")

// SlackSignatureVerifier verifies Slack's v0 HMAC-SHA256 webhook signatures.
//
// Slack signature format:
//   - Header X-Slack-Signature: v0=<hex>
//   - Header X-Slack-Request-Timestamp: <unix timestamp>
//   - Base string: "v0:" + timestamp + ":" + body
//   - Signature: "v0=" + HMAC-SHA256(signingSecret, baseString)
type SlackSignatureVerifier struct {
	// maxAge is the maximum age of a request (default: 5 minutes)
	maxAge time.Duration
}

// NewSlackSignatureVerifier creates a new SlackSignatureVerifier
func NewSlackSignatureVerifier() *SlackSignatureVerifier {
	return &SlackSignatureVerifier{
		maxAge: 5 * time.Minute,
	}
}

// Verify verifies a Slack webhook signature.
// Returns (true, nil) on success.
// Returns (false, ErrTimestampExpired) if the timestamp is too old.
// Returns (false, nil) if the signature does not match.
func (v *SlackSignatureVerifier) Verify(
	body []byte,
	timestamp string, // X-Slack-Request-Timestamp header value
	signature string, // X-Slack-Signature header value (e.g. "v0=abc123...")
	signingSecret string,
) (bool, error) {
	if timestamp == "" || signature == "" || signingSecret == "" {
		return false, nil
	}

	// Validate and check timestamp freshness
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid timestamp: %w", err)
	}
	requestTime := time.Unix(ts, 0)
	if time.Since(requestTime).Abs() > v.maxAge {
		return false, ErrTimestampExpired
	}

	// Build the signature base string
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, string(body))

	// Compute HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(baseString))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	// Strip the "v0=" prefix from the provided signature for comparison
	sig := strings.TrimPrefix(signature, "v0=")
	expectedHex := expected[3:] // strip "v0=" from expected

	// Constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(expectedHex), []byte(sig)), nil
}
