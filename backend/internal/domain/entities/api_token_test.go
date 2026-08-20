package entities

import (
	"testing"
	"time"
)

func TestNewAPIToken_Personal(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	tok := NewAPIToken("tok_1", "apt_secret", "apt_sec", "my key",
		APITokenScopeUser, UserID("u1"), "",
		[]Permission{PermissionSessionRead}, &exp, UserID("u1"))

	if tok.ID() != "tok_1" {
		t.Errorf("ID = %q", tok.ID())
	}
	if tok.Secret() != "apt_secret" {
		t.Errorf("Secret = %q", tok.Secret())
	}
	if tok.Scope() != APITokenScopeUser {
		t.Errorf("Scope = %q", tok.Scope())
	}
	if tok.UserID() != UserID("u1") {
		t.Errorf("UserID = %q", tok.UserID())
	}
	if tok.TeamID() != "" {
		t.Errorf("TeamID = %q want empty", tok.TeamID())
	}
	if tok.CreatedBy() != UserID("u1") {
		t.Errorf("CreatedBy = %q", tok.CreatedBy())
	}
	if !tok.HasPermission(PermissionSessionRead) {
		t.Error("expected session:read permission")
	}
	if tok.HasPermission(PermissionSessionCreate) {
		t.Error("should not have session:create")
	}
	if tok.IsExpired() {
		t.Error("token should not be expired")
	}
}

func TestNewAPIToken_Team(t *testing.T) {
	tok := NewAPIToken("tok_2", "apt_team", "apt_t", "team key",
		APITokenScopeTeam, UserID("sa-org-team"), "org/team",
		[]Permission{PermissionSessionCreate, PermissionSessionRead}, nil, UserID("creator"))

	if tok.Scope() != APITokenScopeTeam {
		t.Errorf("Scope = %q", tok.Scope())
	}
	if tok.TeamID() != "org/team" {
		t.Errorf("TeamID = %q", tok.TeamID())
	}
	if tok.IsExpired() {
		t.Error("nil expiry should never be expired")
	}
}

func TestAPIToken_IsExpired(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	tok := NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "",
		[]Permission{PermissionSessionRead}, &past, UserID("u"))
	if !tok.IsExpired() {
		t.Error("expected expired token")
	}
}

func TestAPIToken_PermissionsCopied(t *testing.T) {
	perms := []Permission{PermissionSessionRead}
	tok := NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "", perms, nil, UserID("u"))
	perms[0] = PermissionSessionCreate
	if tok.Permissions()[0] != PermissionSessionRead {
		t.Error("permissions should be defensively copied")
	}
}

func TestAPIToken_AdminPermissionGrantsAll(t *testing.T) {
	tok := NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "",
		[]Permission{PermissionAdmin}, nil, UserID("u"))
	if !tok.HasPermission(PermissionSessionCreate) {
		t.Error("admin should grant all permissions")
	}
}

func TestAPIToken_Validate(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	valid := NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "",
		[]Permission{PermissionSessionRead}, &exp, UserID("u"))
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid token failed: %v", err)
	}

	invalidCases := []struct {
		name string
		tok  *APIToken
	}{
		{
			name: "empty id",
			tok:  NewAPIToken("", "s", "s", "n", APITokenScopeUser, UserID("u"), "", []Permission{PermissionSessionRead}, nil, UserID("u")),
		},
		{
			name: "empty secret",
			tok:  NewAPIToken("tok", "", "s", "n", APITokenScopeUser, UserID("u"), "", []Permission{PermissionSessionRead}, nil, UserID("u")),
		},
		{
			name: "bad scope",
			tok:  NewAPIToken("tok", "s", "s", "n", APITokenScope("bad"), UserID("u"), "", []Permission{PermissionSessionRead}, nil, UserID("u")),
		},
		{
			name: "empty user id",
			tok:  NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID(""), "", []Permission{PermissionSessionRead}, nil, UserID("u")),
		},
		{
			name: "team scope without team id",
			tok:  NewAPIToken("tok", "s", "s", "n", APITokenScopeTeam, UserID("sa-x"), "", []Permission{PermissionSessionRead}, nil, UserID("u")),
		},
		{
			name: "user scope with team id",
			tok:  NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "org/team", []Permission{PermissionSessionRead}, nil, UserID("u")),
		},
		{
			name: "empty created by",
			tok:  NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "", []Permission{PermissionSessionRead}, nil, UserID("")),
		},
		{
			name: "empty permissions",
			tok:  NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "", []Permission{}, nil, UserID("u")),
		},
		{
			name: "zero created at",
			tok: func() *APIToken {
				tk := NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "", []Permission{PermissionSessionRead}, nil, UserID("u"))
				tk.SetCreatedAt(time.Time{})
				return tk
			}(),
		},
	}
	for _, c := range invalidCases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.tok.Validate(); err == nil {
				t.Errorf("expected validation error for %s", c.name)
			}
		})
	}
}

func TestAPIToken_SetName(t *testing.T) {
	tok := NewAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "", []Permission{PermissionSessionRead}, nil, UserID("u"))
	before := tok.UpdatedAt()
	time.Sleep(time.Millisecond)
	tok.SetName("new")
	if tok.Name() != "new" {
		t.Errorf("Name = %q", tok.Name())
	}
	if !tok.UpdatedAt().After(before) {
		t.Error("UpdatedAt not advanced")
	}
}

func TestRestoreAPIToken_PreservesTimestamps(t *testing.T) {
	created := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	tok := RestoreAPIToken("tok", "s", "s", "n", APITokenScopeUser, UserID("u"), "",
		[]Permission{PermissionSessionRead}, nil, UserID("u"), created, updated)
	if tok.CreatedAt() != created {
		t.Errorf("CreatedAt = %v want %v", tok.CreatedAt(), created)
	}
	if tok.UpdatedAt() != updated {
		t.Errorf("UpdatedAt = %v want %v", tok.UpdatedAt(), updated)
	}
}
