package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type ensuringSessionManager struct {
	*fakeSessionManager
	ensuredIDs []string
	restoring  bool
}

func (m *ensuringSessionManager) EnsureSessionWorkload(_ context.Context, id string) (entities.Session, bool, error) {
	m.ensuredIDs = append(m.ensuredIDs, id)
	return m.GetSession(id), m.restoring, nil
}

type routeSessionManagerProvider struct {
	manager repositories.SessionManager
}

func (p *routeSessionManagerProvider) GetSessionManager() repositories.SessionManager {
	return p.manager
}

func routeContext(e *echo.Echo, method, path, sessionID string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("sessionId", "*")
	ctx.SetParamValues(sessionID, strings.TrimPrefix(path, "/"+sessionID+"/"))
	ctx.Set("authz_context", &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{UserID: "user-1", CanRead: true},
	})
	return ctx, rec
}

func TestRouteToSessionEnsuresLocalAliasByRemoteSessionID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			t.Errorf("upstream path = %q, want /status", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"stable"}`))
	}))
	defer upstream.Close()

	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
		"remote-id": {id: "remote-id", addr: strings.TrimPrefix(upstream.URL, "http://"), userID: "user-1", scope: entities.ScopeUser},
	}}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(&fakeACPRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id",
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(manager.ensuredIDs) != 1 || manager.ensuredIDs[0] != "remote-id" {
		t.Fatalf("ensured IDs = %v, want [remote-id]", manager.ensuredIDs)
	}
}

func TestRouteToSessionEnsuresRegularLocalSessionByPublicID(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
		"local-id": {id: "local-id", addr: strings.TrimPrefix(upstream.URL, "http://"), userID: "user-1", scope: entities.ScopeUser},
	}}}
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/local-id/status", "local-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(manager.ensuredIDs) != 1 || manager.ensuredIDs[0] != "local-id" {
		t.Fatalf("ensured IDs = %v, want [local-id]", manager.ensuredIDs)
	}
}

func TestRouteToSessionLocalAliasRestoringReturnsPublicSessionID(t *testing.T) {
	manager := &ensuringSessionManager{
		fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{
			"remote-id": {id: "remote-id", userID: "user-1", scope: entities.ScopeUser},
		}},
		restoring: true,
	}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(&fakeACPRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id",
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusAccepted {
		t.Fatalf("response status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["session_id"] != "public-id" {
		t.Fatalf("response session_id = %v, want public-id", response["session_id"])
	}
	if len(manager.ensuredIDs) != 1 || manager.ensuredIDs[0] != "remote-id" {
		t.Fatalf("ensured IDs = %v, want [remote-id]", manager.ensuredIDs)
	}
}

func TestRouteToSessionExternalRouteBypassesLocalEnsurer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/remote-id/status" {
			t.Errorf("upstream path = %q, want /remote-id/status", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(&fakeACPRouteRepo{route: &repositories.SessionRoute{
			SessionID: "public-id", RemoteSessionID: "remote-id", ProxyURL: upstream.URL,
		}}),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(manager.ensuredIDs) != 0 {
		t.Fatalf("external route unexpectedly ensured local IDs %v", manager.ensuredIDs)
	}
}
