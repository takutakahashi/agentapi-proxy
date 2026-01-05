package session

import (
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestValidateTeamScope(t *testing.T) {
	uc := NewValidateTeamAccessUseCase()

	tests := []struct {
		name            string
		scope           entities.ResourceScope
		teamID          string
		userTeams       []string
		isAuthenticated bool
		wantErr         bool
		errContains     string
	}{
		{
			name:            "user scope - should pass",
			scope:           entities.ScopeUser,
			teamID:          "",
			userTeams:       nil,
			isAuthenticated: true,
			wantErr:         false,
		},
		{
			name:            "team scope without team_id - should fail",
			scope:           entities.ScopeTeam,
			teamID:          "",
			userTeams:       []string{"org/team-a"},
			isAuthenticated: true,
			wantErr:         true,
			errContains:     "team_id is required",
		},
		{
			name:            "team scope without authentication - should fail",
			scope:           entities.ScopeTeam,
			teamID:          "org/team-a",
			userTeams:       nil,
			isAuthenticated: false,
			wantErr:         true,
			errContains:     "authentication required",
		},
		{
			name:            "team scope user not member - should fail",
			scope:           entities.ScopeTeam,
			teamID:          "org/team-b",
			userTeams:       []string{"org/team-a"},
			isAuthenticated: true,
			wantErr:         true,
			errContains:     "you are not a member of this team",
		},
		{
			name:            "team scope user is member - should pass",
			scope:           entities.ScopeTeam,
			teamID:          "org/team-a",
			userTeams:       []string{"org/team-a", "org/team-b"},
			isAuthenticated: true,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := uc.ValidateTeamScope(tt.scope, tt.teamID, tt.userTeams, tt.isAuthenticated)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTeamScope() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("ValidateTeamScope() error = %v, want error containing %q", err, tt.errContains)
				}
			}
		})
	}
}
