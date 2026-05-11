package controllers_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// ----------------------------------------------------------------------------
// Fakes
// ----------------------------------------------------------------------------

type fakeSession struct {
	id          string
	addr        string
	userID      string
	scope       entities.ResourceScope
	teamID      string
	tags        map[string]string
	status      string
	startedAt   time.Time
	updatedAt   time.Time
	description string
}

func (s *fakeSession) ID() string                    { return s.id }
func (s *fakeSession) Addr() string                  { return s.addr }
func (s *fakeSession) UserID() string                { return s.userID }
func (s *fakeSession) Scope() entities.ResourceScope { return s.scope }
func (s *fakeSession) TeamID() string                { return s.teamID }
func (s *fakeSession) Tags() map[string]string       { return s.tags }
func (s *fakeSession) Status() string                { return s.status }
func (s *fakeSession) StartedAt() time.Time          { return s.startedAt }
func (s *fakeSession) UpdatedAt() time.Time          { return s.updatedAt }
func (s *fakeSession) LastMessageAt() time.Time      { return s.updatedAt }
func (s *fakeSession) Description() string           { return s.description }
func (s *fakeSession) Cancel()                       {}

// fakeSessionManager implements repositories.SessionManager for tests.
type fakeSessionManager struct {
	sessions map[string]*fakeSession
}

func (m *fakeSessionManager) GetSession(id string) entities.Session {
	s, ok := m.sessions[id]
	if !ok {
		return nil
	}
	return s
}

func (m *fakeSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	out := make([]entities.Session, 0)
	for _, s := range m.sessions {
		if filter.UserID != "" && s.userID != filter.UserID {
			continue
		}
		out = append(out, s)
	}
	return out
}

// Stubs to satisfy repositories.SessionManager interface.
func (m *fakeSessionManager) CreateSession(_ context.Context, _ string, _ *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	return nil, nil
}
func (m *fakeSessionManager) DeleteSession(_ string) error { return nil }
func (m *fakeSessionManager) SendMessage(_ context.Context, _ string, _ string) error {
	return nil
}
func (m *fakeSessionManager) StopAgent(_ context.Context, _ string) error { return nil }
func (m *fakeSessionManager) GetMessages(_ context.Context, _ string) ([]repositories.Message, error) {
	return nil, nil
}
func (m *fakeSessionManager) Shutdown(_ time.Duration) error { return nil }

// testSessionManagerProvider adapts fakeSessionManager to controllers.SessionManagerProvider.
type testSessionManagerProvider struct {
	mgr *fakeSessionManager
}

func (p *testSessionManagerProvider) GetSessionManager() repositories.SessionManager {
	return p.mgr
}

// fakeSessionCreator tracks CreateSession and DeleteSessionByID calls.
type fakeSessionCreator struct {
	created []string
	deleted []string
}

func (c *fakeSessionCreator) CreateSession(sessionID string, req entities.StartRequest, userID, userRole string, teams []string) (entities.Session, error) {
	c.created = append(c.created, sessionID)
	return &fakeSession{id: sessionID, userID: userID, scope: req.Scope, status: "running"}, nil
}

func (c *fakeSessionCreator) DeleteSessionByID(sessionID string) error {
	c.deleted = append(c.deleted, sessionID)
	return nil
}

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

func newEchoWithACP(sessions map[string]*fakeSession) (*echo.Echo, *controllers.ACPController, *fakeSessionCreator) {
	e := echo.New()
	if sessions == nil {
		sessions = map[string]*fakeSession{}
	}
	mgr := &fakeSessionManager{sessions: sessions}
	creator := &fakeSessionCreator{}
	provider := &testSessionManagerProvider{mgr: mgr}
	ctrl := controllers.NewACPController(provider, creator)
	return e, ctrl, creator
}

func rpcBody(method string, params interface{}) string {
	p, _ := json.Marshal(params)
	return `{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + string(p) + `}`
}

func setupEchoContext(e *echo.Echo, method, path, body string, userID string) (echo.Context, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	authzCtx := &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{
			UserID:    userID,
			CanCreate: true,
			CanRead:   true,
		},
	}
	c.Set("authz_context", authzCtx)
	return c, rec
}

func parseRPCResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v\nbody: %s", err, rec.Body.String())
	}
	return resp
}

// ----------------------------------------------------------------------------
// Tests: initialize
// ----------------------------------------------------------------------------

func TestACPController_Initialize(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp", rpcBody("initialize", map[string]string{}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("HandleRPC error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	resp := parseRPCResponse(t, rec)
	if resp["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc=2.0, got %v", resp["jsonrpc"])
	}

	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("result is not an object: %v", resp["result"])
	}
	if result["protocolVersion"] != "2025-01-15" {
		t.Errorf("unexpected protocolVersion: %v", result["protocolVersion"])
	}

	caps := result["capabilities"].(map[string]interface{})
	sessionCaps := caps["sessionCapabilities"].(map[string]interface{})
	for _, key := range []string{"list", "close", "resume", "loadSession"} {
		if sessionCaps[key] != true {
			t.Errorf("expected sessionCapabilities.%s=true, got %v", key, sessionCaps[key])
		}
	}
}

// ----------------------------------------------------------------------------
// Tests: session/list
// ----------------------------------------------------------------------------

func TestACPController_SessionList_Empty(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp", rpcBody("session/list", map[string]string{}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	result := resp["result"].(map[string]interface{})
	sessions := result["sessions"].([]interface{})
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
	if _, hasNext := result["nextCursor"]; hasNext {
		t.Error("should not have nextCursor on empty result")
	}
}

func TestACPController_SessionList_ReturnsACPFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s := &fakeSession{
		id:          "sess-1",
		userID:      "user1",
		scope:       entities.ScopeUser,
		tags:        map[string]string{"cwd": "/project"},
		status:      "running",
		description: "test session",
		updatedAt:   now,
	}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-1": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp", rpcBody("session/list", map[string]string{}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	result := resp["result"].(map[string]interface{})
	sessions := result["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	sess := sessions[0].(map[string]interface{})
	if sess["sessionId"] != "sess-1" {
		t.Errorf("unexpected sessionId: %v", sess["sessionId"])
	}
	if sess["cwd"] != "/project" {
		t.Errorf("unexpected cwd: %v", sess["cwd"])
	}
	if sess["title"] != "test session" {
		t.Errorf("unexpected title: %v", sess["title"])
	}

	meta := sess["_meta"].(map[string]interface{})
	if meta["status"] != "running" {
		t.Errorf("unexpected status in _meta: %v", meta["status"])
	}
	if meta["scope"] != "user" {
		t.Errorf("unexpected scope in _meta: %v", meta["scope"])
	}
}

func TestACPController_SessionList_NoNextCursorWhenFewResults(t *testing.T) {
	sessions := map[string]*fakeSession{}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("sess-%d", i)
		sessions[id] = &fakeSession{id: id, userID: "user1", scope: entities.ScopeUser, status: "running", updatedAt: time.Now()}
	}
	e, ctrl, _ := newEchoWithACP(sessions)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp", rpcBody("session/list", map[string]string{}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	result := resp["result"].(map[string]interface{})
	if _, hasNext := result["nextCursor"]; hasNext {
		t.Error("should not have nextCursor when fewer than limit sessions")
	}
}

// ----------------------------------------------------------------------------
// Tests: session/close
// ----------------------------------------------------------------------------

func TestACPController_SessionClose_OK(t *testing.T) {
	s := &fakeSession{
		id:     "sess-del",
		userID: "user1",
		scope:  entities.ScopeUser,
		status: "running",
	}
	e, ctrl, creator := newEchoWithACP(map[string]*fakeSession{"sess-del": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/close", map[string]string{"sessionId": "sess-del"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] != nil {
		t.Errorf("unexpected error in response: %v", resp["error"])
	}
	if len(creator.deleted) != 1 || creator.deleted[0] != "sess-del" {
		t.Errorf("expected session deleted, got: %v", creator.deleted)
	}
}

func TestACPController_SessionClose_NotFound(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/close", map[string]string{"sessionId": "nonexistent"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	rpcErr := resp["error"].(map[string]interface{})
	if rpcErr["code"].(float64) != -32602 {
		t.Errorf("expected -32602, got %v", rpcErr["code"])
	}
}

func TestACPController_SessionClose_MissingSessionId(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/close", map[string]string{}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	rpcErr := resp["error"].(map[string]interface{})
	if rpcErr["code"].(float64) != -32602 {
		t.Errorf("expected -32602, got %v", rpcErr["code"])
	}
}

// ----------------------------------------------------------------------------
// Tests: session/resume
// ----------------------------------------------------------------------------

func TestACPController_SessionResume_Running(t *testing.T) {
	s := &fakeSession{id: "sess-run", userID: "user1", scope: entities.ScopeUser, status: "running"}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-run": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/resume", map[string]string{"sessionId": "sess-run"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] != nil {
		t.Errorf("unexpected error: %v", resp["error"])
	}
}

func TestACPController_SessionResume_Stable(t *testing.T) {
	s := &fakeSession{id: "sess-stable", userID: "user1", scope: entities.ScopeUser, status: "stable"}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-stable": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/resume", map[string]string{"sessionId": "sess-stable"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] != nil {
		t.Errorf("unexpected error: %v", resp["error"])
	}
}

func TestACPController_SessionResume_Stopped(t *testing.T) {
	s := &fakeSession{id: "sess-stopped", userID: "user1", scope: entities.ScopeUser, status: "stopped"}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-stopped": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/resume", map[string]string{"sessionId": "sess-stopped"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] == nil {
		t.Error("expected error for stopped session")
	}
}

func TestACPController_SessionResume_NotFound(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/resume", map[string]string{"sessionId": "ghost"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	rpcErr := resp["error"].(map[string]interface{})
	if rpcErr["code"].(float64) != -32602 {
		t.Errorf("expected -32602, got %v", rpcErr["code"])
	}
}

// ----------------------------------------------------------------------------
// Tests: session/load
// ----------------------------------------------------------------------------

func TestACPController_SessionLoad_OK(t *testing.T) {
	s := &fakeSession{id: "sess-load", userID: "user1", scope: entities.ScopeUser, status: "running"}
	e, ctrl, _ := newEchoWithACP(map[string]*fakeSession{"sess-load": s})
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/load", map[string]string{"sessionId": "sess-load"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] != nil {
		t.Errorf("unexpected error: %v", resp["error"])
	}
}

func TestACPController_SessionLoad_NotFound(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("session/load", map[string]string{"sessionId": "ghost"}), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	if resp["error"] == nil {
		t.Error("expected error for nonexistent session")
	}
}

// ----------------------------------------------------------------------------
// Tests: error cases
// ----------------------------------------------------------------------------

func TestACPController_UnknownMethod(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	c, rec := setupEchoContext(e, http.MethodPost, "/acp",
		rpcBody("unknown/method", nil), "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	rpcErr := resp["error"].(map[string]interface{})
	if rpcErr["code"].(float64) != -32601 {
		t.Errorf("expected -32601, got %v", rpcErr["code"])
	}
}

func TestACPController_InvalidJsonrpcVersion(t *testing.T) {
	e, ctrl, _ := newEchoWithACP(nil)
	body := `{"jsonrpc":"1.0","id":1,"method":"initialize","params":{}}`
	c, rec := setupEchoContext(e, http.MethodPost, "/acp", body, "user1")

	if err := ctrl.HandleRPC(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseRPCResponse(t, rec)
	rpcErr := resp["error"].(map[string]interface{})
	if rpcErr["code"].(float64) != -32600 {
		t.Errorf("expected -32600, got %v", rpcErr["code"])
	}
}
