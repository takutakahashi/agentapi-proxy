package services

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitHubTokenBroker_CachesUntilMargin(t *testing.T) {
	var calls int32
	expires := time.Now().Add(1 * time.Hour).UTC()
	b := NewGitHubTokenBroker(func(repo string) (string, time.Time, error) {
		atomic.AddInt32(&calls, 1)
		return "ghs_token", expires, nil
	})

	for i := 0; i < 3; i++ {
		tok, exp, err := b.IssueToken("octo/repo")
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if tok != "ghs_token" {
			t.Fatalf("call %d: token = %q, want ghs_token", i, tok)
		}
		if !exp.Equal(expires) {
			t.Fatalf("call %d: expiry mismatch", i)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("resolver called %d times, want 1 (cached)", got)
	}
}

func TestGitHubTokenBroker_RefreshesAfterExpiry(t *testing.T) {
	now := time.Now()
	b := NewGitHubTokenBroker(func(repo string) (string, time.Time, error) {
		return "ghs_fresh", now.Add(1 * time.Hour), nil
	})
	// Force the cache to hold an already-expired (within margin) entry.
	b.mu.Lock()
	b.cache["octo/repo"] = cachedToken{token: "ghs_stale", expiresAt: now.Add(1 * time.Second)}
	b.mu.Unlock()

	tok, _, err := b.IssueToken("octo/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "ghs_fresh" {
		t.Fatalf("token = %q, want refreshed ghs_fresh", tok)
	}
}

func TestGitHubTokenBroker_PersonalTokenZeroExpiryCached(t *testing.T) {
	var calls int32
	b := NewGitHubTokenBroker(func(repo string) (string, time.Time, error) {
		atomic.AddInt32(&calls, 1)
		return "ghp_personal", time.Time{}, nil // personal tokens have no known expiry
	})
	for i := 0; i < 2; i++ {
		if _, _, err := b.IssueToken(""); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("resolver called %d times, want 1", got)
	}
}

func TestGitHubTokenBroker_ResolverErrorSanitized(t *testing.T) {
	const secret = "ghs_supersecret"
	b := NewGitHubTokenBroker(func(repo string) (string, time.Time, error) {
		return "", time.Time{}, fmt.Errorf("upstream rejected token %s -----BEGIN PRIVATE KEY-----", secret)
	})
	_, _, err := b.IssueToken("octo/repo")
	if err == nil {
		t.Fatalf("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret material: %v", err)
	}
	if strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Fatalf("error leaked PEM material: %v", err)
	}
}

func TestGitHubTokenBroker_EmptyTokenTreatedAsFailure(t *testing.T) {
	b := NewGitHubTokenBroker(func(repo string) (string, time.Time, error) {
		return "", time.Now().Add(time.Hour), nil
	})
	if _, _, err := b.IssueToken("octo/repo"); err == nil {
		t.Fatalf("empty token must surface as an error, not silently succeed")
	}
}

func TestGitHubTokenBroker_NilResolverDisabled(t *testing.T) {
	var b *GitHubTokenBroker
	if _, _, err := b.IssueToken("octo/repo"); err == nil {
		t.Fatalf("nil broker must error")
	}
	b2 := &GitHubTokenBroker{}
	if _, _, err := b2.IssueToken("octo/repo"); err == nil {
		t.Fatalf("broker with nil resolver must error")
	}
}
