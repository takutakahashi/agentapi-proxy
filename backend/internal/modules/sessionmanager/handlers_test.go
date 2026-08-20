package sessionmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

type proxyTestSession struct{ addr string }

func (s *proxyTestSession) ID() string                    { return "remote-1" }
func (s *proxyTestSession) Addr() string                  { return s.addr }
func (s *proxyTestSession) UserID() string                { return "user" }
func (s *proxyTestSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *proxyTestSession) TeamID() string                { return "" }
func (s *proxyTestSession) Tags() map[string]string       { return nil }
func (s *proxyTestSession) Status() string                { return "running" }
func (s *proxyTestSession) StartedAt() time.Time          { return time.Time{} }
func (s *proxyTestSession) UpdatedAt() time.Time          { return time.Time{} }
func (s *proxyTestSession) LastMessageAt() time.Time      { return time.Time{} }
func (s *proxyTestSession) Description() string           { return "" }
func (s *proxyTestSession) Cancel()                       {}
func (s *proxyTestSession) Annotations() entities.SessionAnnotations {
	return entities.SessionAnnotations{}
}
func (s *proxyTestSession) Request() *entities.RunServerRequest { return nil }

type proxyTestManager struct{ session entities.Session }

func (m *proxyTestManager) CreateSession(context.Context, string, *entities.RunServerRequest, []byte) (entities.Session, error) {
	return nil, nil
}
func (m *proxyTestManager) GetSession(id string) entities.Session {
	if id == "remote-1" {
		return m.session
	}
	return nil
}
func (m *proxyTestManager) ListSessions(entities.SessionFilter) []entities.Session { return nil }
func (m *proxyTestManager) DeleteSession(string) error                             { return nil }
func (m *proxyTestManager) SendMessage(context.Context, string, string) error      { return nil }
func (m *proxyTestManager) StopAgent(context.Context, string) error                { return nil }
func (m *proxyTestManager) GetMessages(context.Context, string) ([]repositories.Message, error) {
	return nil, nil
}
func (m *proxyTestManager) Shutdown(time.Duration) error { return nil }

func TestProxySessionUsesParentCompatiblePathAndPreservesQuery(t *testing.T) {
	var gotPath, gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("data: ready\n\n"))
	}))
	defer upstream.Close()

	const secret = "test-secret"
	e := echo.New()
	h := NewHandlers(&proxyTestManager{session: &proxyTestSession{addr: upstream.URL}}, secret)
	if err := h.RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}

	path := "/remote-1/events?since=7"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ts := hmacutil.NowTimestamp()
	req.Header.Set(hmacutil.TimestampHeader, ts)
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(secret), hmacutil.BuildMessage(http.MethodGet, path, ts, nil)))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotPath != "/events" || gotQuery != "since=7" {
		t.Fatalf("upstream request = %s?%s", gotPath, gotQuery)
	}
	if rec.Body.String() != "data: ready\n\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestProxySessionRequiresHMAC(t *testing.T) {
	e := echo.New()
	h := NewHandlers(&proxyTestManager{}, "test-secret")
	if err := h.RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/remote-1/status", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRuntimeCatchAllDoesNotShadowPreviouslyRegisteredInternalRoute(t *testing.T) {
	e := echo.New()
	e.GET("/internal/session-state/:sessionId/download-url", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	h := NewHandlers(&proxyTestManager{}, "test-secret")
	if err := h.RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/session-state/session-1/download-url", nil)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}

func TestRuntimeHMACMiddlewareDoesNotWrapUnmatchedInternalRoute(t *testing.T) {
	e := echo.New()
	h := NewHandlers(&proxyTestManager{}, "test-secret")
	if err := h.RegisterRoutes(e); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/internal/session-state/session-1/download-url", nil)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
