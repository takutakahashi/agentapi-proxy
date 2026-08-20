package hmacutil_test

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

func TestBuildMessage(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		pathWithQuery string
		timestamp     string
		body          []byte
		wantPrefix    string
	}{
		{
			name:          "GET without query or body",
			method:        "GET",
			pathWithQuery: "/api/v1/sessions",
			timestamp:     "1711681200",
			body:          nil,
			wantPrefix:    "GET\n/api/v1/sessions\n1711681200\n",
		},
		{
			name:          "GET with query string",
			method:        "GET",
			pathWithQuery: "/api/v1/sessions?user_id=alice&scope=user",
			timestamp:     "1711681200",
			body:          nil,
			wantPrefix:    "GET\n/api/v1/sessions?user_id=alice&scope=user\n1711681200\n",
		},
		{
			name:          "POST with body",
			method:        "POST",
			pathWithQuery: "/api/v1/sessions",
			timestamp:     "1711681200",
			body:          []byte(`{"user_id":"alice"}`),
			wantPrefix:    "POST\n/api/v1/sessions\n1711681200\n",
		},
		{
			name:          "method is normalised to upper case",
			method:        "post",
			pathWithQuery: "/api/v1/sessions",
			timestamp:     "1711681200",
			body:          nil,
			wantPrefix:    "POST\n/api/v1/sessions\n1711681200\n",
		},
		{
			name:          "DELETE without body",
			method:        "DELETE",
			pathWithQuery: "/api/v1/sessions/abc123",
			timestamp:     "1711681200",
			body:          []byte{},
			wantPrefix:    "DELETE\n/api/v1/sessions/abc123\n1711681200\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := hmacutil.BuildMessage(tc.method, tc.pathWithQuery, tc.timestamp, tc.body)
			got := string(msg)
			if len(tc.body) > 0 {
				want := tc.wantPrefix + string(tc.body)
				if got != want {
					t.Errorf("BuildMessage() = %q, want %q", got, want)
				}
			} else {
				if got != tc.wantPrefix {
					t.Errorf("BuildMessage() = %q, want %q", got, tc.wantPrefix)
				}
			}
		})
	}
}

func TestSignAndVerify_RoundTrip(t *testing.T) {
	secret := []byte("supersecret")
	msg := hmacutil.BuildMessage("POST", "/api/v1/sessions", "1711681200", []byte(`{"user_id":"alice"}`))

	sig := hmacutil.Sign(secret, msg)
	if sig == "" {
		t.Fatal("Sign() returned empty string")
	}
	if len(sig) < 8 || sig[:7] != "sha256=" {
		t.Errorf("Sign() result %q does not start with 'sha256='", sig)
	}

	if !hmacutil.Verify(secret, msg, sig) {
		t.Error("Verify() returned false for valid signature")
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	msg := hmacutil.BuildMessage("GET", "/api/v1/sessions", "1711681200", nil)
	sig := hmacutil.Sign([]byte("correct-secret"), msg)

	if hmacutil.Verify([]byte("wrong-secret"), msg, sig) {
		t.Error("Verify() returned true with wrong secret")
	}
}

func TestVerify_TamperedPath(t *testing.T) {
	secret := []byte("mysecret")
	// Sign with user_id=alice
	msg1 := hmacutil.BuildMessage("GET", "/api/v1/sessions?user_id=alice", "1711681200", nil)
	sig := hmacutil.Sign(secret, msg1)

	// Attempt to reuse the same signature with user_id=bob
	msg2 := hmacutil.BuildMessage("GET", "/api/v1/sessions?user_id=bob", "1711681200", nil)
	if hmacutil.Verify(secret, msg2, sig) {
		t.Error("Verify() accepted signature for tampered query string")
	}
}

func TestVerify_TamperedTimestamp(t *testing.T) {
	secret := []byte("mysecret")
	msg1 := hmacutil.BuildMessage("GET", "/api/v1/sessions", "1711681200", nil)
	sig := hmacutil.Sign(secret, msg1)

	msg2 := hmacutil.BuildMessage("GET", "/api/v1/sessions", "9999999999", nil)
	if hmacutil.Verify(secret, msg2, sig) {
		t.Error("Verify() accepted signature with different timestamp")
	}
}

func TestValidateTimestamp(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name    string
		ts      string
		wantErr bool
	}{
		{
			name:    "current time",
			ts:      strconv.FormatInt(now, 10),
			wantErr: false,
		},
		{
			name:    "4 minutes ago (within skew)",
			ts:      strconv.FormatInt(now-240, 10),
			wantErr: false,
		},
		{
			name:    "4 minutes in the future (within skew)",
			ts:      strconv.FormatInt(now+240, 10),
			wantErr: false,
		},
		{
			name:    "6 minutes ago (outside skew)",
			ts:      strconv.FormatInt(now-360, 10),
			wantErr: true,
		},
		{
			name:    "6 minutes in the future (outside skew)",
			ts:      strconv.FormatInt(now+360, 10),
			wantErr: true,
		},
		{
			name:    "empty string",
			ts:      "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			ts:      "not-a-number",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := hmacutil.ValidateTimestamp(tc.ts)
			if tc.wantErr && err == nil {
				t.Error("ValidateTimestamp() expected error but got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("ValidateTimestamp() unexpected error: %v", err)
			}
		})
	}
}

func TestNowTimestamp(t *testing.T) {
	before := time.Now().Unix()
	ts := hmacutil.NowTimestamp()
	after := time.Now().Unix()

	epoch, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		t.Fatalf("NowTimestamp() returned non-numeric value %q: %v", ts, err)
	}
	if epoch < before || epoch > after {
		t.Errorf("NowTimestamp() = %d, want between %d and %d", epoch, before, after)
	}
}

// TestSignDifferentEachCall ensures two calls with different timestamps produce different signatures.
func TestSignDifferentEachCall(t *testing.T) {
	secret := []byte("mysecret")
	ts1 := fmt.Sprintf("%d", time.Now().Unix())
	ts2 := fmt.Sprintf("%d", time.Now().Unix()+1)

	msg1 := hmacutil.BuildMessage("GET", "/api/v1/sessions", ts1, nil)
	msg2 := hmacutil.BuildMessage("GET", "/api/v1/sessions", ts2, nil)

	sig1 := hmacutil.Sign(secret, msg1)
	sig2 := hmacutil.Sign(secret, msg2)

	if sig1 == sig2 {
		t.Error("Sign() produced identical signatures for different timestamps")
	}
}
