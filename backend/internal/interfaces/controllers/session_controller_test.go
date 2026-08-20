package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type quotaErrorSessionCreator struct{}

func (quotaErrorSessionCreator) CreateSession(string, entities.StartRequest, string, string, []string) (entities.Session, error) {
	return nil, &sessionrunnercore.QuotaExceededError{
		Pool: "linux", BindingID: "binding-alice", MaxConcurrent: 2, Active: 2,
	}
}

func (quotaErrorSessionCreator) DeleteSessionByID(string) error { return nil }

func TestStartSessionReturnsQuotaExceeded(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/start", strings.NewReader(`{"scope":"user"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	user := entities.NewUser(entities.UserID("alice"), entities.UserTypeAPIKey, "alice")
	ctx.Set("authz_context", &auth.AuthorizationContext{
		User: user,
		PersonalScope: auth.PersonalScopeAuth{
			UserID: "alice", CanCreate: true, CanRead: true,
		},
	})

	controller := NewSessionController(nil, quotaErrorSessionCreator{})
	if err := controller.StartSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["binding_id"] != "binding-alice" || body["max_concurrent"] != float64(2) || body["active"] != float64(2) {
		t.Fatalf("unexpected response: %#v", body)
	}
}

func TestPopulateGitHubTokenFromAuthHeader(t *testing.T) {
	tests := []struct {
		name          string
		scope         entities.ResourceScope
		existingToken string
		credential    *auth.CredentialContext
		wantToken     string
	}{
		{name: "user scope receives authenticated GitHub token", scope: entities.ScopeUser, credential: &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "oauth-token"}, wantToken: "oauth-token"},
		{name: "API key is never treated as GitHub token", scope: entities.ScopeUser, credential: &auth.CredentialContext{Kind: auth.CredentialKindAPIKey, Token: "api-key"}, wantToken: ""},
		{name: "explicit token is preserved", scope: entities.ScopeUser, existingToken: "explicit-token", credential: &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "oauth-token"}, wantToken: "explicit-token"},
		{name: "team scope excludes user token", scope: entities.ScopeTeam, credential: &auth.CredentialContext{Kind: auth.CredentialKindGitHub, Token: "oauth-token"}, wantToken: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodPost, "/start", nil)
			req.Header.Set("Authorization", "Bearer oauth-token")
			ctx := e.NewContext(req, httptest.NewRecorder())
			auth.SetCredentialContext(ctx, tt.credential)
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
		{name: "assigned but unconfirmed session", route: &repositories.SessionRoute{SessionID: "public-id", RemoteSessionID: "remote-id"}, want: "starting"},
		{name: "confirmed session", route: &repositories.SessionRoute{SessionID: "public-id", RemoteSessionID: "remote-id", Status: "stable"}, want: "stable"},
		{name: "direct runtime pushed status", route: &repositories.SessionRoute{SessionID: "public-id", Transport: repositories.SessionRouteTransportDirectRuntime, Status: "active"}, want: "active"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routedSessionStatus(tt.route, nil); got != tt.want {
				t.Fatalf("routedSessionStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindUncreatedSessionAllocation(t *testing.T) {
	pending := &sessionListTestSession{id: "pending-id", status: "pending"}
	sessions := []entities.Session{
		&sessionListTestSession{id: "running-id", status: "running"},
		pending,
		&sessionListTestSession{id: "allocating-id", status: "allocating"},
	}

	if got := findUncreatedSessionAllocation(sessions, "pending-id"); got != pending {
		t.Fatalf("findUncreatedSessionAllocation() = %v, want pending session", got)
	}
	if got := findUncreatedSessionAllocation(sessions, "running-id"); got != nil {
		t.Fatalf("findUncreatedSessionAllocation() returned running session %v", got)
	}
	if got := findUncreatedSessionAllocation(sessions, "allocating-id"); got != sessions[2] {
		t.Fatalf("findUncreatedSessionAllocation() = %v, want allocating session", got)
	}
}
