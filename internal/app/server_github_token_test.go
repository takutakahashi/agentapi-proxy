package app

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestGitHubTokenForStartRequest(t *testing.T) {
	tests := []struct {
		name      string
		request   entities.StartRequest
		wantToken string
	}{
		{
			name: "user scope forwards token",
			request: entities.StartRequest{
				Scope:  entities.ScopeUser,
				Params: &entities.SessionParams{GithubToken: "oauth-token"},
			},
			wantToken: "oauth-token",
		},
		{
			name: "team scope excludes user token",
			request: entities.StartRequest{
				Scope:  entities.ScopeTeam,
				Params: &entities.SessionParams{GithubToken: "oauth-token"},
			},
		},
		{name: "nil params", request: entities.StartRequest{Scope: entities.ScopeUser}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := githubTokenForStartRequest(tt.request); got != tt.wantToken {
				t.Fatalf("githubTokenForStartRequest() = %q, want %q", got, tt.wantToken)
			}
		})
	}
}
