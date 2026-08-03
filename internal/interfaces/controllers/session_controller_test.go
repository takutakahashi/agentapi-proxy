package controllers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestPopulateGitHubTokenFromAuthHeader(t *testing.T) {
	tests := []struct {
		name          string
		scope         entities.ResourceScope
		existingToken string
		wantToken     string
	}{
		{name: "user scope receives header token", scope: entities.ScopeUser, wantToken: "oauth-token"},
		{name: "explicit token is preserved", scope: entities.ScopeUser, existingToken: "explicit-token", wantToken: "explicit-token"},
		{name: "team scope excludes user token", scope: entities.ScopeTeam, wantToken: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/start", nil)
			req.Header.Set("Authorization", "Bearer oauth-token")
			ctx := e.NewContext(req, httptest.NewRecorder())
			ctx.Set("config", &config.Config{Auth: config.AuthConfig{GitHub: &config.GitHubAuthConfig{
				Enabled:     true,
				TokenHeader: "Authorization",
			}}})
			startReq := entities.StartRequest{Scope: tt.scope, Params: &entities.SessionParams{GithubToken: tt.existingToken}}

			populateGitHubTokenFromAuthHeader(ctx, &startReq)

			if startReq.Params.GithubToken != tt.wantToken {
				t.Fatalf("GithubToken = %q, want %q", startReq.Params.GithubToken, tt.wantToken)
			}
		})
	}
}

type sessionListTestSession struct {
	id     string
	status string
}

func (s *sessionListTestSession) ID() string                    { return s.id }
func (s *sessionListTestSession) Addr() string                  { return "" }
func (s *sessionListTestSession) UserID() string                { return "user-1" }
func (s *sessionListTestSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *sessionListTestSession) TeamID() string                { return "" }
func (s *sessionListTestSession) Tags() map[string]string       { return nil }
func (s *sessionListTestSession) Status() string {
	if s.status == "" {
		return "running"
	}
	return s.status
}
func (s *sessionListTestSession) StartedAt() time.Time     { return time.Time{} }
func (s *sessionListTestSession) UpdatedAt() time.Time     { return time.Time{} }
func (s *sessionListTestSession) LastMessageAt() time.Time { return time.Time{} }
func (s *sessionListTestSession) Description() string      { return "" }
func (s *sessionListTestSession) Cancel()                  {}

func TestExcludeAllocatedSessions(t *testing.T) {
	sessions := []entities.Session{
		&sessionListTestSession{id: "public-id"},
		&sessionListTestSession{id: "allocated-id"},
		&sessionListTestSession{id: "local-id"},
	}
	routes := []*repositories.SessionRoute{
		{SessionID: "public-id", RemoteSessionID: "allocated-id"},
	}

	got := excludeAllocatedSessions(sessions, routes)
	if len(got) != 2 {
		t.Fatalf("excludeAllocatedSessions() returned %d sessions, want 2", len(got))
	}
	if got[0].ID() != "public-id" || got[1].ID() != "local-id" {
		t.Fatalf("excludeAllocatedSessions() returned IDs %q and %q, want public-id and local-id", got[0].ID(), got[1].ID())
	}
}

func TestIndexAllocatedSessionsPreservesRuntimeStatus(t *testing.T) {
	sessions := []entities.Session{
		&sessionListTestSession{id: "allocated-running", status: "running"},
		&sessionListTestSession{id: "allocated-stable", status: "stable"},
		&sessionListTestSession{id: "local-id", status: "active"},
	}
	routes := []*repositories.SessionRoute{
		{SessionID: "public-running", RemoteSessionID: "allocated-running"},
		{SessionID: "public-stable", RemoteSessionID: "allocated-stable"},
	}

	got := indexAllocatedSessions(sessions, routes)
	if len(got) != 2 {
		t.Fatalf("indexAllocatedSessions() returned %d sessions, want 2", len(got))
	}
	if got["allocated-running"].Status() != "running" {
		t.Fatalf("running session status = %q, want running", got["allocated-running"].Status())
	}
	if got["allocated-stable"].Status() != "stable" {
		t.Fatalf("stable session status = %q, want stable", got["allocated-stable"].Status())
	}
	if status := routedSessionStatus(routes[0], got); status != "running" {
		t.Fatalf("public running session status = %q, want running", status)
	}
	if status := routedSessionStatus(routes[1], got); status != "stable" {
		t.Fatalf("public stable session status = %q, want stable", status)
	}
}

func TestRoutedSessionStatusFallbacks(t *testing.T) {
	tests := []struct {
		name  string
		route *repositories.SessionRoute
		want  string
	}{
		{name: "allocation pending", route: &repositories.SessionRoute{SessionID: "public-id"}, want: "creating"},
		{name: "remote session", route: &repositories.SessionRoute{SessionID: "public-id", RemoteSessionID: "remote-id"}, want: "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routedSessionStatus(tt.route, nil); got != tt.want {
				t.Fatalf("routedSessionStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindPendingSessionAllocation(t *testing.T) {
	pending := &sessionListTestSession{id: "pending-id", status: "pending"}
	sessions := []entities.Session{
		&sessionListTestSession{id: "running-id", status: "running"},
		pending,
		&sessionListTestSession{id: "allocating-id", status: "allocating"},
	}

	if got := findPendingSessionAllocation(sessions, "pending-id"); got != pending {
		t.Fatalf("findPendingSessionAllocation() = %v, want pending session", got)
	}
	if got := findPendingSessionAllocation(sessions, "running-id"); got != nil {
		t.Fatalf("findPendingSessionAllocation() returned running session %v", got)
	}
	if got := findPendingSessionAllocation(sessions, "allocating-id"); got != nil {
		t.Fatalf("findPendingSessionAllocation() returned allocating session %v", got)
	}
}
