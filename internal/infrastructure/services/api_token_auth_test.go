package services

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func makeToken(secret string, scope entities.APITokenScope, userID entities.UserID, teamID string, perms []entities.Permission, exp *time.Time) *entities.APIToken {
	return entities.NewAPIToken("tok_"+secret, secret, secret[:8], "n",
		scope, userID, teamID, perms, exp, userID)
}

func TestSimpleAuthService_AuthenticatesAPIToken_Personal(t *testing.T) {
	s := NewSimpleAuthService()
	tok := makeToken("apt_personal_secret", entities.APITokenScopeUser,
		entities.UserID("u1"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil)
	if err := s.LoadAPIToken(context.Background(), tok); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !s.IsAPITokenLoaded(tok.Secret()) {
		t.Fatal("token not loaded")
	}
	user, err := s.ValidateAPIKey(context.Background(), tok.Secret())
	if err != nil {
		t.Fatalf("ValidateAPIKey: %v", err)
	}
	if user.ID() != entities.UserID("u1") {
		t.Errorf("user id = %q", user.ID())
	}
	if user.UserType() != entities.UserTypeRegular {
		t.Errorf("user type = %q", user.UserType())
	}
	if !user.HasPermission(entities.PermissionSessionRead) {
		t.Error("missing session:read")
	}
	if user.HasPermission(entities.PermissionSessionCreate) {
		t.Error("should not have session:create")
	}
}

func TestSimpleAuthService_AuthenticatesAPIToken_Team(t *testing.T) {
	s := NewSimpleAuthService()
	tok := makeToken("apt_team_secret", entities.APITokenScopeTeam,
		entities.UserID("sa-org-team"), "org/team",
		[]entities.Permission{entities.PermissionSessionCreate, entities.PermissionSessionRead}, nil)
	if err := s.LoadAPIToken(context.Background(), tok); err != nil {
		t.Fatalf("Load: %v", err)
	}
	user, err := s.ValidateAPIKey(context.Background(), tok.Secret())
	if err != nil {
		t.Fatalf("ValidateAPIKey: %v", err)
	}
	if user.UserType() != entities.UserTypeServiceAccount {
		t.Errorf("user type = %q", user.UserType())
	}
	if user.TeamID() != "org/team" {
		t.Errorf("team = %q", user.TeamID())
	}
}

func TestSimpleAuthService_RevokedAPITokenFails(t *testing.T) {
	s := NewSimpleAuthService()
	tok := makeToken("apt_revoke", entities.APITokenScopeUser, entities.UserID("u"), "",
		[]entities.Permission{entities.PermissionSessionRead}, nil)
	_ = s.LoadAPIToken(context.Background(), tok)
	if _, err := s.ValidateAPIKey(context.Background(), tok.Secret()); err != nil {
		t.Fatalf("before revoke should work: %v", err)
	}
	s.RevokeAPIToken(tok.Secret())
	if s.IsAPITokenLoaded(tok.Secret()) {
		t.Error("token still loaded after revoke")
	}
	if _, err := s.ValidateAPIKey(context.Background(), tok.Secret()); err == nil {
		t.Error("revoked token should not authenticate")
	}
}

func TestSimpleAuthService_ExpiredAPITokenFails(t *testing.T) {
	s := NewSimpleAuthService()
	past := time.Now().Add(-time.Minute)
	tok := makeToken("apt_expired", entities.APITokenScopeUser, entities.UserID("u"), "",
		[]entities.Permission{entities.PermissionSessionRead}, &past)
	_ = s.LoadAPIToken(context.Background(), tok)
	if _, err := s.ValidateAPIKey(context.Background(), tok.Secret()); err == nil {
		t.Error("expired token should not authenticate")
	}
}

func TestSimpleAuthService_UnknownSecretFails(t *testing.T) {
	s := NewSimpleAuthService()
	if _, err := s.ValidateAPIKey(context.Background(), "totally_unknown"); err == nil {
		t.Error("unknown secret should not authenticate")
	}
}

func TestSimpleAuthService_LegacyAPIKeyStillWorks(t *testing.T) {
	// A legacy key loaded via LoadPersonalAPIKey must still authenticate after
	// the new token system is wired in (backward compatibility).
	s := NewSimpleAuthService()
	pk := entities.NewPersonalAPIKey(entities.UserID("legacy-user"), "ap_legacy_value")
	if err := s.LoadPersonalAPIKey(context.Background(), pk); err != nil {
		t.Fatalf("LoadPersonalAPIKey: %v", err)
	}
	user, err := s.ValidateAPIKey(context.Background(), "ap_legacy_value")
	if err != nil {
		t.Fatalf("legacy token should authenticate: %v", err)
	}
	if user.ID() != entities.UserID("legacy-user") {
		t.Errorf("user id = %q", user.ID())
	}
}

func TestSimpleAuthService_RevokeUnknownSafe(t *testing.T) {
	s := NewSimpleAuthService()
	s.RevokeAPIToken("never-existed") // must not panic
	s.RevokeAPIToken("")
}

// TestSimpleAuthService_MigratedPersonalLegacyKeyRevocation is the central
// regression for the migration/revocation bug. A legacy personal API key that
// has been migrated into a named token must stop authenticating the moment
// the named token is revoked, even though the legacy apiKeys entry (loaded by
// the legacy bootstrap and possibly reloaded by the legacy
// GET/POST /users/me/api-key controller) is still present in memory, and
// must stay revoked across reconciliation.
func TestSimpleAuthService_MigratedPersonalLegacyKeyRevocation(t *testing.T) {
	s := NewSimpleAuthService()
	const legacySecret = "ap_legacy_personal_value"
	userID := entities.UserID("legacy-user")

	// 1. Legacy bootstrap loads the legacy personal key into apiKeys. Before
	//    migration it authenticates via the legacy path.
	pk := entities.NewPersonalAPIKey(userID, legacySecret)
	if err := s.LoadPersonalAPIKey(context.Background(), pk); err != nil {
		t.Fatalf("LoadPersonalAPIKey: %v", err)
	}
	if s.IsShadowedLegacySecret(legacySecret) {
		t.Error("pre-migration legacy key should not yet be shadowed")
	}
	if _, err := s.ValidateAPIKey(context.Background(), legacySecret); err != nil {
		t.Fatalf("legacy key should authenticate before migration: %v", err)
	}

	// 2. Migration loads the same secret as a named token. The secret is now
	//    shadowed and authenticates through apiTokens.
	migrated := makeToken(legacySecret, entities.APITokenScopeUser, userID, "",
		[]entities.Permission{entities.PermissionSessionRead}, nil)
	if err := s.LoadAPIToken(context.Background(), migrated); err != nil {
		t.Fatalf("LoadAPIToken: %v", err)
	}
	if !s.IsShadowedLegacySecret(legacySecret) {
		t.Fatal("migrated secret must be shadowed after LoadAPIToken")
	}
	user, err := s.ValidateAPIKey(context.Background(), legacySecret)
	if err != nil {
		t.Fatalf("migrated secret should authenticate: %v", err)
	}
	if user.ID() != userID {
		t.Errorf("user id = %q, want %q", user.ID(), userID)
	}

	// 3. Re-load the legacy credential (simulating the legacy controller or a
	//    repeated bootstrap). It must NOT unshadow the secret.
	if err := s.LoadPersonalAPIKey(context.Background(), pk); err != nil {
		t.Fatalf("re-LoadPersonalAPIKey: %v", err)
	}
	if !s.IsShadowedLegacySecret(legacySecret) {
		t.Fatal("reloading legacy credential must not unshadow it")
	}

	// 4. Revoke the migrated named token. The secret must fail authentication
	//    immediately, despite the legacy apiKeys entry still being present.
	s.RevokeAPIToken(legacySecret)
	if s.IsAPITokenLoaded(legacySecret) {
		t.Error("named token should be removed from apiTokens after revoke")
	}
	if !s.IsShadowedLegacySecret(legacySecret) {
		t.Error("revoked secret must remain shadowed")
	}
	if _, err := s.ValidateAPIKey(context.Background(), legacySecret); err == nil {
		t.Fatal("revoked migrated secret must not authenticate via legacy fallback")
	}

	// 5. Reconciliation must keep it invalid: the deleted named token is gone
	//    from the store, and the shadow prevents the legacy apiKeys fallback.
	repo := newMemTokenRepo()
	s.SetAPITokenRepository(repo) // store is empty -> reconcile drops all named tokens
	if err := s.ReconcileAPITokens(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !s.IsShadowedLegacySecret(legacySecret) {
		t.Error("shadow must survive reconcile")
	}
	if _, err := s.ValidateAPIKey(context.Background(), legacySecret); err == nil {
		t.Fatal("revoked migrated secret must stay invalid after reconcile")
	}
}

// TestSimpleAuthService_MigratedTeamLegacyKeyRevocation mirrors the personal
// regression for team service-account tokens: a migrated team legacy key must
// fail authentication once its named token is revoked, with no fallback to the
// legacy apiKeys entry loaded from TeamConfig, and must stay invalid across
// reconciliation and legacy reload.
func TestSimpleAuthService_MigratedTeamLegacyKeyRevocation(t *testing.T) {
	s := NewSimpleAuthService()
	const legacySecret = "ap_legacy_team_value"
	const teamID = "org/team"
	saUserID := entities.UserID("sa-org-team")
	perms := []entities.Permission{entities.PermissionSessionCreate, entities.PermissionSessionRead}

	// 1. Legacy bootstrap loads the team service account into apiKeys.
	sa := entities.NewServiceAccount(teamID, saUserID, legacySecret, perms)
	tc := entities.NewTeamConfig(teamID, sa, nil)
	if err := s.LoadServiceAccountFromTeamConfig(context.Background(), tc); err != nil {
		t.Fatalf("LoadServiceAccountFromTeamConfig: %v", err)
	}
	if _, err := s.ValidateAPIKey(context.Background(), legacySecret); err != nil {
		t.Fatalf("legacy team key should authenticate before migration: %v", err)
	}

	// 2. Migration loads the same secret as a team-scoped named token.
	migrated := makeToken(legacySecret, entities.APITokenScopeTeam, saUserID, teamID, perms, nil)
	if err := s.LoadAPIToken(context.Background(), migrated); err != nil {
		t.Fatalf("LoadAPIToken: %v", err)
	}
	if !s.IsShadowedLegacySecret(legacySecret) {
		t.Fatal("migrated team secret must be shadowed")
	}
	user, err := s.ValidateAPIKey(context.Background(), legacySecret)
	if err != nil {
		t.Fatalf("migrated team secret should authenticate: %v", err)
	}
	if user.UserType() != entities.UserTypeServiceAccount {
		t.Errorf("user type = %q, want service account", user.UserType())
	}
	if user.TeamID() != teamID {
		t.Errorf("team = %q, want %q", user.TeamID(), teamID)
	}

	// 3. Re-loading the legacy team config must not unshadow.
	if err := s.LoadServiceAccountFromTeamConfig(context.Background(), tc); err != nil {
		t.Fatalf("re-load team config: %v", err)
	}
	if !s.IsShadowedLegacySecret(legacySecret) {
		t.Fatal("reloading legacy team credential must not unshadow it")
	}

	// 4. Revoke the migrated named token -> secret must fail immediately.
	s.RevokeAPIToken(legacySecret)
	if _, err := s.ValidateAPIKey(context.Background(), legacySecret); err == nil {
		t.Fatal("revoked migrated team secret must not authenticate via legacy fallback")
	}

	// 5. Reconciliation must keep it invalid.
	repo := newMemTokenRepo()
	s.SetAPITokenRepository(repo)
	if err := s.ReconcileAPITokens(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !s.IsShadowedLegacySecret(legacySecret) {
		t.Error("team shadow must survive reconcile")
	}
	if _, err := s.ValidateAPIKey(context.Background(), legacySecret); err == nil {
		t.Fatal("revoked migrated team secret must stay invalid after reconcile")
	}
}

// TestSimpleAuthService_NonMigratedLegacyKeyUnaffected ensures that shadowing
// only applies to secrets that were actually registered as named tokens. A
// purely-legacy key that was never migrated must keep authenticating through
// the legacy apiKeys path, including after a reconcile that touches nothing.
func TestSimpleAuthService_NonMigratedLegacyKeyUnaffected(t *testing.T) {
	s := NewSimpleAuthService()
	pk := entities.NewPersonalAPIKey(entities.UserID("plain-user"), "ap_plain_legacy")
	if err := s.LoadPersonalAPIKey(context.Background(), pk); err != nil {
		t.Fatalf("LoadPersonalAPIKey: %v", err)
	}
	if s.IsShadowedLegacySecret("ap_plain_legacy") {
		t.Error("non-migrated legacy key should not be shadowed")
	}
	repo := newMemTokenRepo()
	s.SetAPITokenRepository(repo)
	if err := s.ReconcileAPITokens(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	user, err := s.ValidateAPIKey(context.Background(), "ap_plain_legacy")
	if err != nil {
		t.Fatalf("non-migrated legacy key must still authenticate: %v", err)
	}
	if user.ID() != entities.UserID("plain-user") {
		t.Errorf("user id = %q", user.ID())
	}
}
