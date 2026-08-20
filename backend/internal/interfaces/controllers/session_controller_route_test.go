package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

type statusWatchingSessionManager struct {
	*fakeSessionManager
	events chan repositories.SessionStatusEvent
}

func (m *statusWatchingSessionManager) SubscribeStatusEvents() (<-chan repositories.SessionStatusEvent, func()) {
	return m.events, func() {}
}

type directRuntimeTunnel struct {
	managerID string
	path      string
}

func (t *directRuntimeTunnel) IsConnected(_ context.Context, managerID string) bool {
	return managerID == "public-id"
}

func (t *directRuntimeTunnel) Do(_ context.Context, managerID, _, _ string, req *http.Request) (*http.Response, error) {
	t.managerID = managerID
	t.path = req.URL.Path
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"status":"stable"}`)),
	}, nil
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

func TestRouteToSessionDoesNotWakeLocalAliasOnGet(t *testing.T) {
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
	if len(manager.ensuredIDs) != 0 {
		t.Fatalf("GET unexpectedly woke sessions: %v", manager.ensuredIDs)
	}
}

func TestRouteToSessionDoesNotWakeRegularLocalSessionOnGet(t *testing.T) {
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
	if len(manager.ensuredIDs) != 0 {
		t.Fatalf("GET unexpectedly woke sessions: %v", manager.ensuredIDs)
	}
}

func TestResumeSessionLocalAliasRestoringReturnsPublicSessionID(t *testing.T) {
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
	ctx, rec := routeContext(echo.New(), http.MethodPost, "/sessions/public-id/resume", "public-id")

	if err := controller.ResumeSession(ctx); err != nil {
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

func TestDeleteSessionAlreadyAbsentIsIdempotent(t *testing.T) {
	manager := &fakeSessionManager{sessions: map[string]*fakeSession{}}
	controller := controllers.NewSessionController(&routeSessionManagerProvider{manager: manager}, nil)
	ctx, rec := routeContext(echo.New(), http.MethodDelete, "/sessions/missing-id", "missing-id")

	if err := controller.DeleteSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["session_id"] != "missing-id" || response["status"] != "terminated" {
		t.Fatalf("response = %#v, want missing-id terminated", response)
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

func TestRouteToSessionUsesDirectSessionRuntime(t *testing.T) {
	manager := &ensuringSessionManager{fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{}}}
	tunnel := &directRuntimeTunnel{}
	routeRepo := &fakeACPRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a", Transport: "direct_session_runtime",
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager},
		nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(tunnel),
	)
	ctx, rec := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")

	if err := controller.RouteToSession(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("response status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if tunnel.managerID != "public-id" || tunnel.path != "/status" {
		t.Fatalf("direct tunnel manager=%q path=%q", tunnel.managerID, tunnel.path)
	}
	if routeRepo.route.Status != "active" || routeRepo.route.StatusUpdatedAt.IsZero() {
		t.Fatalf("persisted route status=%q updated_at=%v, want active with timestamp", routeRepo.route.Status, routeRepo.route.StatusUpdatedAt)
	}
}

func TestRemoteStatusChangeReachesStatusWait(t *testing.T) {
	manager := &statusWatchingSessionManager{
		fakeSessionManager: &fakeSessionManager{sessions: map[string]*fakeSession{}},
		events:             make(chan repositories.SessionStatusEvent),
	}
	routeRepo := &fakeACPRouteRepo{route: &repositories.SessionRoute{
		SessionID: "public-id", RemoteSessionID: "remote-id", ManagerID: "manager-a",
		Transport: repositories.SessionRouteTransportDirectRuntime, UserID: "user-1", Scope: string(entities.ScopeUser),
	}}
	controller := controllers.NewSessionController(
		&routeSessionManagerProvider{manager: manager}, nil,
		controllers.WithSessionRouteRepository(routeRepo),
		controllers.WithESMControlTunnel(&directRuntimeTunnel{}),
	)

	waitCtx, waitRec := routeContext(echo.New(), http.MethodGet, "/sessions/status/wait?timeout=2", "")
	done := make(chan error, 1)
	go func() { done <- controller.WaitSessionsStatus(waitCtx) }()
	time.Sleep(20 * time.Millisecond)

	statusCtx, _ := routeContext(echo.New(), http.MethodGet, "/public-id/status", "public-id")
	if err := controller.RouteToSession(statusCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("remote status event was not delivered")
	}
	var evt repositories.SessionStatusEvent
	if err := json.Unmarshal(waitRec.Body.Bytes(), &evt); err != nil {
		t.Fatal(err)
	}
	if evt.SessionID != "public-id" || evt.Status != "active" {
		t.Fatalf("event = %+v, want public-id active", evt)
	}
}
