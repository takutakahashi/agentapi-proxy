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
