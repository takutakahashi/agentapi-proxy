package entities

import (
	"testing"
)

func makeGitHubUser(teamMemberships []GitHubTeamMembership) *User {
	u := &User{
		userType: UserTypeGitHub,
	}
	info := NewGitHubUserInfo(12345, "testuser", "Test User", "test@example.com", "", "", "")
	u.SetGitHubInfo(info, teamMemberships)
	return u
}

func TestIsMemberOfTeam(t *testing.T) {
	tests := []struct {
		name     string
		user     *User
		teamID   string
		expected bool
	}{
		{
			name:     "service account: matching team",
			user:     NewServiceAccountUser("sa-org-myteam", "org/myteam", nil),
			teamID:   "org/myteam",
			expected: true,
		},
		{
			name:     "service account: different team",
			user:     NewServiceAccountUser("sa-org-myteam", "org/myteam", nil),
			teamID:   "org/otherteam",
			expected: false,
		},
		{
			name:     "service account: empty teamID arg",
			user:     NewServiceAccountUser("sa-org-myteam", "org/myteam", nil),
			teamID:   "",
			expected: false,
		},
		{
			name:     "service account: no team configured",
			user:     NewServiceAccountUser("sa-noteam", "", nil),
			teamID:   "org/myteam",
			expected: false,
		},
		{
			name: "github user: member of team",
			user: makeGitHubUser([]GitHubTeamMembership{
				{Organization: "org", TeamSlug: "myteam"},
			}),
			teamID:   "org/myteam",
			expected: true,
		},
		{
			name: "github user: not member of team",
			user: makeGitHubUser([]GitHubTeamMembership{
				{Organization: "org", TeamSlug: "otherteam"},
			}),
			teamID:   "org/myteam",
			expected: false,
		},
		{
			name:     "github user: no githubInfo",
			user:     &User{userType: UserTypeGitHub},
			teamID:   "org/myteam",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.user.IsMemberOfTeam(tt.teamID)
			if got != tt.expected {
				t.Errorf("IsMemberOfTeam(%q) = %v, want %v", tt.teamID, got, tt.expected)
			}
		})
	}
}

func TestCanAccessResource_ServiceAccount(t *testing.T) {
	sa := NewServiceAccountUser("sa-org-myteam", "org/myteam", nil)

	tests := []struct {
		name        string
		ownerUserID UserID
		scope       string
		teamID      string
		expected    bool
	}{
		{
			name:        "team scope: matching team",
			ownerUserID: "other-user",
			scope:       "team",
			teamID:      "org/myteam",
			expected:    true,
		},
		{
			name:        "team scope: different team",
			ownerUserID: "other-user",
			scope:       "team",
			teamID:      "org/otherteam",
			expected:    false,
		},
		{
			name:        "user scope: own resource",
			ownerUserID: "sa-org-myteam",
			scope:       "user",
			teamID:      "",
			expected:    true,
		},
		{
			name:        "user scope: other user's resource",
			ownerUserID: "another-user",
			scope:       "user",
			teamID:      "",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sa.CanAccessResource(tt.ownerUserID, tt.scope, tt.teamID)
			if got != tt.expected {
				t.Errorf("CanAccessResource() = %v, want %v", got, tt.expected)
			}
		})
	}
}
