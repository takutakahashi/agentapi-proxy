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
