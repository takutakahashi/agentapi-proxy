package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// ReconcileAPITokens must drop a named token that was deleted on another
// replica (no longer present in the repository) so that replica cannot keep
// accepting it indefinitely. Legacy static/personal API keys (the apiKeys map)
// must remain untouched.
func TestSimpleAuthService_Reconcile_DropsDeletedTokenPreservesLegacy(t *testing.T) {
	auth := NewSimpleAuthService()
	repo := newMemTokenRepo()

	// A named personal token + a legacy personal API key coexist.
	named := makeToken("apt_named_secret", entities.APITokenScopeUser,
		entities.UserID("u1"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil)
	require.NoError(t, repo.Create(context.Background(), named))

	pk := entities.NewPersonalAPIKey(entities.UserID("legacy-user"), "ap_legacy_value")
	require.NoError(t, auth.LoadPersonalAPIKey(context.Background(), pk))
	require.NoError(t, auth.LoadAPIToken(context.Background(), named))

	auth.SetAPITokenRepository(repo)

	// Both authenticate before reconciliation.
	if _, err := auth.ValidateAPIKey(context.Background(), "apt_named_secret"); err != nil {
		t.Fatalf("named token should authenticate before reconcile: %v", err)
	}
	if _, err := auth.ValidateAPIKey(context.Background(), "ap_legacy_value"); err != nil {
		t.Fatalf("legacy key should authenticate before reconcile: %v", err)
	}

	// Simulate another replica deleting the named token from the store.
	require.NoError(t, repo.Delete(context.Background(), named.ID()))

	// Reconciliation rebuilds the named-token map from the store; the deleted
	// token is gone.
	require.NoError(t, auth.ReconcileAPITokens(context.Background()))

	if _, err := auth.ValidateAPIKey(context.Background(), "apt_named_secret"); err == nil {
		t.Error("deleted named token must not authenticate after reconcile")
	}
	if !auth.IsAPITokenLoaded("apt_named_secret") {
		// expected: it was dropped
	} else {
		t.Error("deleted named token should be removed from in-memory map")
	}

	// Legacy key is unaffected by reconciliation.
	user, err := auth.ValidateAPIKey(context.Background(), "ap_legacy_value")
	require.NoError(t, err, "legacy personal API key must remain authenticatable")
	assert.Equal(t, entities.UserID("legacy-user"), user.ID())
}

// ReconcileAPITokens must also pick up tokens created on another replica.
func TestSimpleAuthService_Reconcile_PicksUpTokensCreatedElsewhere(t *testing.T) {
	auth := NewSimpleAuthService()
	repo := newMemTokenRepo()

	named := makeToken("apt_other_replica", entities.APITokenScopeUser,
		entities.UserID("u2"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil)
	require.NoError(t, repo.Create(context.Background(), named))

	auth.SetAPITokenRepository(repo)
	// This replica never loaded the token yet.
	if _, err := auth.ValidateAPIKey(context.Background(), "apt_other_replica"); err == nil {
		t.Fatal("token should not authenticate before reconcile on this replica")
	}
	require.NoError(t, auth.ReconcileAPITokens(context.Background()))
	if _, err := auth.ValidateAPIKey(context.Background(), "apt_other_replica"); err != nil {
		t.Fatalf("token should authenticate after reconcile: %v", err)
	}
}

func TestSimpleAuthService_Reconcile_NoRepoNoOp(t *testing.T) {
	auth := NewSimpleAuthService()
	// No repository wired: reconcile is a no-op and never panics.
	require.NoError(t, auth.ReconcileAPITokens(context.Background()))
}
