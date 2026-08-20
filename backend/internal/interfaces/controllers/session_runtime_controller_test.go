package controllers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type runtimeControllerStore struct {
	touched  string
	requests []core.Command
}

func (s *runtimeControllerStore) TouchManager(_ context.Context, id, _ string) error {
	s.touched = id
	return nil
}
func (s *runtimeControllerStore) IsManagerConnected(context.Context, string) (bool, error) {
	return true, nil
}
func (s *runtimeControllerStore) EnqueueCommand(context.Context, string, core.Command) (string, error) {
	return "1-0", nil
}
func (s *runtimeControllerStore) ReadCommands(context.Context, string, string, time.Duration, int64) ([]core.Command, error) {
	return s.requests, nil
}
func (s *runtimeControllerStore) AckCommand(context.Context, string, string) error { return nil }
func (s *runtimeControllerStore) AppendFrames(context.Context, string, []core.ResponseFrame) (string, error) {
	return "2-0", nil
}
func (s *runtimeControllerStore) ReadFrames(context.Context, string, string, time.Duration, int64) ([]core.ResponseFrame, error) {
	return nil, nil
}
func (s *runtimeControllerStore) RequestBelongsToManager(context.Context, string, string) (bool, error) {
	return true, nil
}

type runtimeRouteRepo struct{ route *repositories.SessionRoute }

type runtimeStatusRecorder struct{ status string }

func (r *runtimeStatusRecorder) RecordRemoteSessionStatus(_ context.Context, _ *repositories.SessionRoute, status string) error {
	r.status = status
	return nil
}

func (r *runtimeRouteRepo) Save(context.Context, *repositories.SessionRoute) error { return nil }
func (r *runtimeRouteRepo) Get(context.Context, string) (*repositories.SessionRoute, error) {
	return r.route, nil
}

func TestSessionRuntimeUpdateStatus(t *testing.T) {
	store := &runtimeControllerStore{}
	route := &repositories.SessionRoute{SessionID: "session-a", Transport: repositories.SessionRouteTransportDirectRuntime, RuntimeTokenHash: runtimeTokenHash("secret"), Generation: 3}
	recorder := &runtimeStatusRecorder{}
	controller := NewSessionRuntimeController(store, &runtimeRouteRepo{route: route}, recorder)
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/internal/session-runtime/session-a/status?generation=3", bytes.NewBufferString(`{"status":"stable"}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("session-a")
	if err := controller.UpdateStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusNoContent || recorder.status != "stable" {
		t.Fatalf("code=%d status=%q", rec.Code, recorder.status)
	}
}
func (r *runtimeRouteRepo) List(context.Context, string) ([]*repositories.SessionRoute, error) {
	return nil, nil
}
func (r *runtimeRouteRepo) Delete(context.Context, string) error { return nil }

func runtimeTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func runtimeControllerContext(method, target, token string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("sessionId")
	ctx.SetParamValues("session-a")
	return ctx, rec
}

func TestSessionRuntimeWaitRequestsAuthenticatesGeneration(t *testing.T) {
	store := &runtimeControllerStore{requests: []core.Command{{ID: "request-a", StreamID: "1-0", Method: http.MethodGet, Path: "/status"}}}
	controller := NewSessionRuntimeController(store, &runtimeRouteRepo{route: &repositories.SessionRoute{
		SessionID: "session-a", Transport: "direct_session_runtime", RuntimeTokenHash: runtimeTokenHash("secret"), Generation: 3,
	}})
	ctx, rec := runtimeControllerContext(http.MethodGet, "/internal/session-runtime/session-a/requests?generation=3", "secret")

	if err := controller.WaitRequests(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || store.touched != "session-a" {
		t.Fatalf("code=%d touched=%q body=%s", rec.Code, store.touched, rec.Body.String())
	}
}

func TestSessionRuntimeRejectsWrongTokenOrGeneration(t *testing.T) {
	controller := NewSessionRuntimeController(&runtimeControllerStore{}, &runtimeRouteRepo{route: &repositories.SessionRoute{
		SessionID: "session-a", Transport: "direct_session_runtime", RuntimeTokenHash: runtimeTokenHash("secret"), Generation: 3,
	}})
	for _, target := range []string{
		"/internal/session-runtime/session-a/requests?generation=2",
		"/internal/session-runtime/session-a/requests?generation=3",
	} {
		token := "secret"
		if target[len(target)-1] == '3' {
			token = "wrong"
		}
		ctx, rec := runtimeControllerContext(http.MethodGet, target, token)
		if err := controller.WaitRequests(ctx); err != nil {
			t.Fatal(err)
		}
		want := http.StatusConflict
		if token == "wrong" {
			want = http.StatusUnauthorized
		}
		if rec.Code != want {
			t.Fatalf("target=%s code=%d, want %d", target, rec.Code, want)
		}
	}
}
