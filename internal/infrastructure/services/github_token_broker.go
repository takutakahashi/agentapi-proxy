package services

import (
	"fmt"
	"sync"
	"time"
)

// GitHubTokenResolver produces a short-lived GitHub installation token (and its
// expiration) for a given repository full name. It is backed by the proxy's own
// GitHub App credentials (or a proxy-level personal token) and is overridable in
// tests. The returned expiration may be the zero value when the credential has no
// known expiry (e.g. a personal access token).
type GitHubTokenResolver func(repoFullName string) (token string, expiresAt time.Time, err error)

// githubBrokerRefreshMargin is how long before expiry a cached token is considered
// stale and re-issued. GitHub installation tokens are short-lived (~1h); refreshing
// a few minutes early avoids serving a token that expires during an in-flight
// operation while minimizing upstream API calls.
const githubBrokerRefreshMargin = 5 * time.Minute

// cachedToken holds an installation token together with its expiration.
type cachedToken struct {
	token     string
	expiresAt time.Time
}

// GitHubTokenBroker is the proxy-side issuer of short-lived GitHub installation
// tokens for session Pods. It validates that callers are authorized for a specific
// session (per-session HMAC credential, handled by KubernetesSessionManager) and
// scopes tokens to the session's repository.
//
// Tokens are cached per repository and refreshed before they expire. The broker
// never logs or returns token / PEM material in errors; failures surface as a
// fixed, secret-free error so callers can distinguish "unavailable" from "wrong
// repo" without learning secret material.
type GitHubTokenBroker struct {
	resolver GitHubTokenResolver
	margin   time.Duration

	mu    sync.Mutex
	cache map[string]cachedToken
	now   func() time.Time
}

// NewGitHubTokenBroker creates a broker backed by the given resolver. If resolver
// is nil the broker is considered disabled and IssueToken returns a clear error.
func NewGitHubTokenBroker(resolver GitHubTokenResolver) *GitHubTokenBroker {
	return &GitHubTokenBroker{
		resolver: resolver,
		margin:   githubBrokerRefreshMargin,
		cache:    make(map[string]cachedToken),
		now:      time.Now,
	}
}

// IssueToken returns a valid GitHub token for the given repository, minting a new
// one (or reusing the cache) as needed. repoFullName may be empty when the session
// has no repository scope; in that case the resolver decides whether a token can
// be issued (subject to REPOSITORY_RESTRICTION).
//
// On resolver failure the error is a fixed, secret-free message; the underlying
// error (which may contain credential material echoed by an upstream API) is never
// propagated to callers or logs.
func (b *GitHubTokenBroker) IssueToken(repoFullName string) (string, time.Time, error) {
	if b == nil || b.resolver == nil {
		return "", time.Time{}, fmt.Errorf("github token broker is not configured")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	key := repoFullName
	if entry, ok := b.cache[key]; ok && b.fresh(entry) {
		return entry.token, entry.expiresAt, nil
	}

	token, expiresAt, err := b.resolver(repoFullName)
	if err != nil {
		// Never leak the underlying error: it may reference credential material.
		return "", time.Time{}, fmt.Errorf("failed to issue GitHub token")
	}
	if token == "" {
		return "", time.Time{}, fmt.Errorf("failed to issue GitHub token")
	}

	b.cache[key] = cachedToken{token: token, expiresAt: expiresAt}
	return token, expiresAt, nil
}

// fresh reports whether a cached token is still valid with the refresh margin.
// Tokens without a known expiry (zero expiresAt, e.g. personal tokens) are always
// considered fresh and reused.
func (b *GitHubTokenBroker) fresh(entry cachedToken) bool {
	if entry.expiresAt.IsZero() {
		return true
	}
	return b.now().Add(b.margin).Before(entry.expiresAt)
}

// Invalidate removes a cached token so the next IssueToken re-mints. Used in tests
// to exercise cache boundaries.
func (b *GitHubTokenBroker) Invalidate(repoFullName string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.cache, repoFullName)
}
