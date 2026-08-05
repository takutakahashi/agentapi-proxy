package auth

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func newAuthzContext(userID string, isAdmin bool, teams []string) *AuthorizationContext {
	user := entities.NewUser(
		entities.UserID(userID),
		entities.UserTypeAPIKey,
		userID,
	)
	if isAdmin {
		_ = user.SetRoles([]entities.Role{entities.RoleAdmin})
	} else {
		_ = user.SetRoles([]entities.Role{entities.RoleUser})
	}

	teamPerms := make(map[string]TeamPermissions)
	for _, t := range teams {
		teamPerms[t] = TeamPermissions{
			TeamID:    t,
			CanCreate: true,
			CanRead:   true,
			CanUpdate: true,
			CanDelete: true,
		}
	}

	return &AuthorizationContext{
		User: user,
		PersonalScope: PersonalScopeAuth{
			UserID:    userID,
			CanCreate: true,
			CanRead:   true,
			CanUpdate: true,
			CanDelete: true,
		},
		TeamScope: TeamScopeAuth{
			Teams:           teams,
			TeamPermissions: teamPerms,
			IsAdmin:         isAdmin,
		},
	}
}

func TestCanAccessResource_PersonalScope(t *testing.T) {
	tests := []struct {
		name        string
		authzUserID string
		isAdmin     bool
		ownerUserID string
		scope       string
		teamID      string
		want        bool
	}{
		{
			name:        "owner can access own personal session",
			authzUserID: "user-1",
			isAdmin:     false,
			ownerUserID: "user-1",
			scope:       "user",
			teamID:      "",
			want:        true,
		},
		{
			name:        "non-admin cannot access another user's personal session",
			authzUserID: "user-1",
			isAdmin:     false,
			ownerUserID: "user-2",
			scope:       "user",
			teamID:      "",
			want:        false,
		},
		{
			name:        "admin cannot access another user's personal session",
			authzUserID: "admin-user",
			isAdmin:     true,
			ownerUserID: "user-2",
			scope:       "user",
			teamID:      "",
			want:        false,
		},
		{
			name:        "admin can access own personal session",
			authzUserID: "admin-user",
			isAdmin:     true,
			ownerUserID: "admin-user",
			scope:       "user",
			teamID:      "",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authzCtx := newAuthzContext(tt.authzUserID, tt.isAdmin, nil)
			got := authzCtx.CanAccessResource(tt.ownerUserID, tt.scope, tt.teamID)
			if got != tt.want {
				t.Errorf("CanAccessResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newServiceAccountAuthzContext(userID, teamID string) *AuthorizationContext {
	user := entities.NewServiceAccountUser(
		entities.UserID(userID),
		teamID,
		[]entities.Permission{
			entities.PermissionSessionCreate,
			entities.PermissionSessionRead,
			entities.PermissionSessionUpdate,
			entities.PermissionSessionDelete,
		},
	)

	teamPerms := make(map[string]TeamPermissions)
	if teamID != "" {
		teamPerms[teamID] = TeamPermissions{
			TeamID:    teamID,
			CanCreate: true,
			CanRead:   true,
			CanUpdate: true,
			CanDelete: true,
		}
	}

	return &AuthorizationContext{
		User: user,
		PersonalScope: PersonalScopeAuth{
			UserID:    userID,
			CanCreate: true,
			CanRead:   true,
			CanUpdate: true,
			CanDelete: true,
		},
		TeamScope: TeamScopeAuth{
			Teams:           []string{teamID},
			TeamPermissions: teamPerms,
			IsAdmin:         false,
		},
	}
}

func TestIsServiceAccount(t *testing.T) {
	tests := []struct {
		name    string
		buildFn func() *AuthorizationContext
		want    bool
	}{
		{
			name: "service account returns true",
			buildFn: func() *AuthorizationContext {
				return newServiceAccountAuthzContext("sa-org-team", "org/team")
			},
			want: true,
		},
		{
			name: "regular user returns false",
			buildFn: func() *AuthorizationContext {
				return newAuthzContext("user-1", false, []string{"org/team"})
			},
			want: false,
		},
		{
			name: "admin user returns false",
			buildFn: func() *AuthorizationContext {
				return newAuthzContext("admin-user", true, nil)
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authzCtx := tt.buildFn()
			got := authzCtx.IsServiceAccount()
			if got != tt.want {
				t.Errorf("IsServiceAccount() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceAccountTeamID(t *testing.T) {
	tests := []struct {
		name    string
		buildFn func() *AuthorizationContext
		want    string
	}{
		{
			name: "service account returns its team ID",
			buildFn: func() *AuthorizationContext {
				return newServiceAccountAuthzContext("sa-org-team", "org/team")
			},
			want: "org/team",
		},
		{
			name: "service account with no team returns empty string",
			buildFn: func() *AuthorizationContext {
				return newServiceAccountAuthzContext("sa-no-team", "")
			},
			want: "",
		},
		{
			name: "regular user returns empty string",
			buildFn: func() *AuthorizationContext {
				return newAuthzContext("user-1", false, []string{"org/team"})
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authzCtx := tt.buildFn()
			got := authzCtx.ServiceAccountTeamID()
			if got != tt.want {
				t.Errorf("ServiceAccountTeamID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveUserScope(t *testing.T) {
	tests := []struct {
		name        string
		user        func() *entities.User
		inputScope  string
		inputTeamID string
		wantScope   string
		wantTeamID  string
	}{
		{
			name: "service account with empty scope routes to team scope",
			user: func() *entities.User {
				return entities.NewServiceAccountUser("sa-org-team", "org/team", nil)
			},
			inputScope:  "",
			inputTeamID: "",
			wantScope:   "team",
			wantTeamID:  "org/team",
		},
		{
			name: "service account with user scope routes to team scope",
			user: func() *entities.User {
				return entities.NewServiceAccountUser("sa-org-team", "org/team", nil)
			},
			inputScope:  "user",
			inputTeamID: "",
			wantScope:   "team",
			wantTeamID:  "org/team",
		},
		{
			name: "service account with team scope is unchanged",
			user: func() *entities.User {
				return entities.NewServiceAccountUser("sa-org-team", "org/team", nil)
			},
			inputScope:  "team",
			inputTeamID: "org/other-team",
			wantScope:   "team",
			wantTeamID:  "org/other-team",
		},
		{
			name: "regular user with user scope is unchanged",
			user: func() *entities.User {
				return entities.NewUser("user-1", entities.UserTypeAPIKey, "user-1")
			},
			inputScope:  "user",
			inputTeamID: "",
			wantScope:   "user",
			wantTeamID:  "",
		},
		{
			name: "regular user with team scope is unchanged",
			user: func() *entities.User {
				return entities.NewUser("user-1", entities.UserTypeAPIKey, "user-1")
			},
			inputScope:  "team",
			inputTeamID: "org/team",
			wantScope:   "team",
			wantTeamID:  "org/team",
		},
		{
			name: "nil user is unchanged",
			user: func() *entities.User {
				return nil
			},
			inputScope:  "user",
			inputTeamID: "",
			wantScope:   "user",
			wantTeamID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotScope, gotTeamID := ResolveUserScope(tt.user(), tt.inputScope, tt.inputTeamID)
			if gotScope != tt.wantScope {
				t.Errorf("ResolveUserScope() scope = %q, want %q", gotScope, tt.wantScope)
			}
			if gotTeamID != tt.wantTeamID {
				t.Errorf("ResolveUserScope() teamID = %q, want %q", gotTeamID, tt.wantTeamID)
			}
		})
	}
}

func TestAuthorizationContext_ResolveScope(t *testing.T) {
	tests := []struct {
		name        string
		buildFn     func() *AuthorizationContext
		inputScope  string
		inputTeamID string
		wantScope   string
		wantTeamID  string
	}{
		{
			name: "service account authz context routes user scope to team",
			buildFn: func() *AuthorizationContext {
				return newServiceAccountAuthzContext("sa-org-team", "org/team")
			},
			inputScope:  "user",
			inputTeamID: "",
			wantScope:   "team",
			wantTeamID:  "org/team",
		},
		{
			name: "regular user authz context is unchanged",
			buildFn: func() *AuthorizationContext {
				return newAuthzContext("user-1", false, []string{"org/team"})
			},
			inputScope:  "user",
			inputTeamID: "",
			wantScope:   "user",
			wantTeamID:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authzCtx := tt.buildFn()
			gotScope, gotTeamID := authzCtx.ResolveScope(tt.inputScope, tt.inputTeamID)
			if gotScope != tt.wantScope {
				t.Errorf("ResolveScope() scope = %q, want %q", gotScope, tt.wantScope)
			}
			if gotTeamID != tt.wantTeamID {
				t.Errorf("ResolveScope() teamID = %q, want %q", gotTeamID, tt.wantTeamID)
			}
		})
	}
}

func TestCanAccessResource_TeamScope(t *testing.T) {
	tests := []struct {
		name        string
		authzUserID string
		isAdmin     bool
		teams       []string
		ownerUserID string
		scope       string
		teamID      string
		want        bool
	}{
		{
			name:        "member can access team session",
			authzUserID: "user-1",
			isAdmin:     false,
			teams:       []string{"org/team-a"},
			ownerUserID: "user-2",
			scope:       "team",
			teamID:      "org/team-a",
			want:        true,
		},
		{
			name:        "non-member cannot access team session",
			authzUserID: "user-1",
			isAdmin:     false,
			teams:       []string{"org/team-b"},
			ownerUserID: "user-2",
			scope:       "team",
			teamID:      "org/team-a",
			want:        false,
		},
		{
			name:        "admin can access any team session",
			authzUserID: "admin-user",
			isAdmin:     true,
			teams:       nil,
			ownerUserID: "user-2",
			scope:       "team",
			teamID:      "org/team-a",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authzCtx := newAuthzContext(tt.authzUserID, tt.isAdmin, tt.teams)
			got := authzCtx.CanAccessResource(tt.ownerUserID, tt.scope, tt.teamID)
			if got != tt.want {
				t.Errorf("CanAccessResource() = %v, want %v", got, tt.want)
			}
		})
	}
}
