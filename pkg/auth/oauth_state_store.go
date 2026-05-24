package auth

import "context"

// OAuthStateStore defines the interface for storing OAuth state parameters.
// The default in-memory implementation is suitable for single-pod deployments.
// For multi-pod deployments, use a shared backend (e.g. ConfigMap) via SetStateStore.
type OAuthStateStore interface {
	// Store saves a state entry.
	Store(ctx context.Context, state string, entry *OAuthState) error

	// Load retrieves a state entry. Returns (entry, true, nil) if found.
	Load(ctx context.Context, state string) (*OAuthState, bool, error)

	// Delete removes a state entry.
	Delete(ctx context.Context, state string) error

	// Range iterates over all entries. The callback returns false to stop iteration.
	Range(ctx context.Context, fn func(state string, entry *OAuthState) bool) error
}
