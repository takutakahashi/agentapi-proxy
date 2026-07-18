package controllers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type sessionListTestSession struct {
	id string
}

type staleRouteSessionManager struct{}

func (staleRouteSessionManager) CreateSession(context.Context, string, *entities.RunServerRequest, []byte) (entities.Session, error) {
	return nil, nil
}
func (staleRouteSessionManager) GetSession(string) entities.Session { return nil }
func (staleRouteSessionManager) ListSessions(entities.SessionFilter) []entities.Session {
	return nil
}
func (staleRouteSessionManager) DeleteSession(string) error { return nil }
func (staleRouteSessionManager) SendMessage(context.Context, string, string) error {
	return nil
}
func (staleRouteSessionManager) StopAgent(context.Context, string) error { return nil }
func (staleRouteSessionManager) GetMessages(context.Context, string) ([]repositories.Message, error) {
	return nil, nil
}
func (staleRouteSessionManager) Shutdown(time.Duration) error { return nil }

type staleRouteProvider struct{ manager repositories.SessionManager }

func (p staleRouteProvider) GetSessionManager() repositories.SessionManager { return p.manager }

type staleRouteRepository struct {
	route     *repositories.SessionRoute
	deletedID string
}

func (r *staleRouteRepository) Save(context.Context, *repositories.SessionRoute) error { return nil }
func (r *staleRouteRepository) Get(context.Context, string) (*repositories.SessionRoute, error) {
	return r.route, nil
}
func (r *staleRouteRepository) List(context.Context, string) ([]*repositories.SessionRoute, error) {
	return []*repositories.SessionRoute{r.route}, nil
}
func (r *staleRouteRepository) Delete(_ context.Context, sessionID string) error {
	r.deletedID = sessionID
	return nil
}

func staleRouteDeleteContext(sessionID, userID string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodDelete, "/sessions/"+sessionID, nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues(sessionID)
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: userID},
	})
	return ctx, rec
}

func TestDeleteSession_RemovesStaleLocalAlias(t *testing.T) {
	const sessionID = "public-session"
	repo := &staleRouteRepository{route: &repositories.SessionRoute{
		SessionID: sessionID, RemoteSessionID: "missing-local-session", UserID: "user-1", Scope: "user",
	}}
	controller := NewSessionController(
		staleRouteProvider{manager: staleRouteSessionManager{}}, nil, WithSessionRouteRepository(repo),
	)
	ctx, rec := staleRouteDeleteContext(sessionID, "user-1")

	require.NoError(t, controller.DeleteSession(ctx))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, sessionID, repo.deletedID)
	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "terminated", response["status"])
}

func TestDeleteSession_RejectsStaleLocalAliasOwnedByAnotherUser(t *testing.T) {
	const sessionID = "public-session"
	repo := &staleRouteRepository{route: &repositories.SessionRoute{
		SessionID: sessionID, RemoteSessionID: "missing-local-session", UserID: "owner", Scope: "user",
	}}
	controller := NewSessionController(
		staleRouteProvider{manager: staleRouteSessionManager{}}, nil, WithSessionRouteRepository(repo),
	)
	ctx, _ := staleRouteDeleteContext(sessionID, "other-user")

	err := controller.DeleteSession(ctx)
	var httpErr *echo.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, http.StatusForbidden, httpErr.Code)
	require.Empty(t, repo.deletedID)
}

func (s *sessionListTestSession) ID() string                    { return s.id }
func (s *sessionListTestSession) Addr() string                  { return "" }
func (s *sessionListTestSession) UserID() string                { return "user-1" }
func (s *sessionListTestSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *sessionListTestSession) TeamID() string                { return "" }
func (s *sessionListTestSession) Tags() map[string]string       { return nil }
func (s *sessionListTestSession) Status() string                { return "running" }
func (s *sessionListTestSession) StartedAt() time.Time          { return time.Time{} }
func (s *sessionListTestSession) UpdatedAt() time.Time          { return time.Time{} }
func (s *sessionListTestSession) LastMessageAt() time.Time      { return time.Time{} }
func (s *sessionListTestSession) Description() string           { return "" }
func (s *sessionListTestSession) Cancel()                       {}

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
