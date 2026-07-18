package controllers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestDeviceAuthCredentialName(t *testing.T) {
	user := entities.NewGitHubUser("alice", "alice", "alice@example.com", nil)
	user.SetGitHubInfo(entities.NewGitHubUserInfo(1, "alice", "Alice", "alice@example.com", "", "", ""), []entities.GitHubTeamMembership{
		{Organization: "acme", TeamSlug: "platform"},
	})

	tests := []struct {
		name       string
		req        StartDeviceAuthRequest
		want       string
		wantStatus int
	}{
		{name: "default user scope", want: "alice"},
		{name: "explicit user scope", req: StartDeviceAuthRequest{Scope: "user"}, want: "alice"},
		{name: "team scope", req: StartDeviceAuthRequest{Scope: "team", TeamID: "acme/platform"}, want: "acme/platform"},
		{name: "team id with user scope", req: StartDeviceAuthRequest{Scope: "user", TeamID: "acme/platform"}, wantStatus: http.StatusBadRequest},
		{name: "missing team id", req: StartDeviceAuthRequest{Scope: "team"}, wantStatus: http.StatusBadRequest},
		{name: "unknown team", req: StartDeviceAuthRequest{Scope: "team", TeamID: "acme/security"}, wantStatus: http.StatusForbidden},
		{name: "invalid scope", req: StartDeviceAuthRequest{Scope: "organization"}, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deviceAuthCredentialName(user, tt.req)
			if tt.wantStatus != 0 {
				require.Error(t, err)
				var httpErr *echo.HTTPError
				require.True(t, errors.As(err, &httpErr))
				assert.Equal(t, tt.wantStatus, httpErr.Code)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
