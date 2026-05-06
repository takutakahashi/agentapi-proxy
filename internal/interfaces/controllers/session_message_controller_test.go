package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// --- minimal mock session ---

type mockWaitSession struct {
	id            string
	userID        string
	lastMessageAt time.Time
}

func (s *mockWaitSession) ID() string                        { return s.id }
func (s *mockWaitSession) Addr() string                      { return "localhost:9000" }
func (s *mockWaitSession) UserID() string                    { return s.userID }
func (s *mockWaitSession) Scope() entities.ResourceScope     { return entities.ScopeUser }
func (s *mockWaitSession) TeamID() string                    { return "" }
func (s *mockWaitSession) Tags() map[string]string           { return nil }
func (s *mockWaitSession) Status() string                    { return "active" }
func (s *mockWaitSession) StartedAt() time.Time              { return time.Time{} }
func (s *mockWaitSession) UpdatedAt() time.Time              { return time.Time{} }
func (s *mockWaitSession) LastMessageAt() time.Time          { return s.lastMessageAt }
func (s *mockWaitSession) Description() string               { return "" }
func (s *mockWaitSession) Cancel()                           {}

// --- mock session manager that also implements ProxyMessageWatcher ---

type mockWaitSessionManager struct {
	session   entities.Session
	eventCh   chan services.SessionMessageEvent
	cancelFn  func()
}

func newMockWaitSessionManager(session entities.Session) *mockWaitSessionManager {
	ch := make(chan services.SessionMessageEvent, 1)
	return &mockWaitSessionManager{
		session:  session,
		eventCh:  ch,
		cancelFn: func() {},
	}
}

func (m *mockWaitSessionManager) CreateSession(_ context.Context, _ string, _ *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	return nil, nil
}
func (m *mockWaitSessionManager) GetSession(id string) entities.Session {
	if m.session != nil && m.session.ID() == id {
		return m.session
	}
	return nil
}
func (m *mockWaitSessionManager) ListSessions(_ entities.SessionFilter) []entities.Session {
	return nil
}
func (m *mockWaitSessionManager) DeleteSession(_ string) error { return nil }
func (m *mockWaitSessionManager) SendMessage(_ context.Context, _ string, _ string) error {
	return nil
}
func (m *mockWaitSessionManager) StopAgent(_ context.Context, _ string) error { return nil }
func (m *mockWaitSessionManager) GetMessages(_ context.Context, _ string) ([]portrepos.Message, error) {
	return nil, nil
}
func (m *mockWaitSessionManager) Shutdown(_ time.Duration) error { return nil }

// ProxyMessageWatcher
func (m *mockWaitSessionManager) SubscribeMessageEvents(_ string) (<-chan services.SessionMessageEvent, func()) {
	return m.eventCh, m.cancelFn
}

// --- mock SessionManagerProvider ---

type mockWaitProvider struct {
	manager portrepos.SessionManager
}

func (p *mockWaitProvider) GetSessionManager() portrepos.SessionManager { return p.manager }

// --- helpers ---

func makeWaitEchoContext(t *testing.T, sessionID string, queryParams map[string]string, userID string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	url := fmt.Sprintf("/sessions/%s/messages/wait", sessionID)
	if len(queryParams) > 0 {
		url += "?"
		first := true
		for k, v := range queryParams {
			if !first {
				url += "&"
			}
			url += k + "=" + v
			first = false
		}
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("sessionId")
	c.SetParamValues(sessionID)
	for k, v := range queryParams {
		c.QueryParams().Set(k, v)
	}

	// Set authz context so the handler can authorize the request.
	authzCtx := &auth.AuthorizationContext{
		PersonalScope: auth.PersonalScopeAuth{
			UserID:  userID,
			CanRead: true,
		},
		TeamScope: auth.TeamScopeAuth{
			TeamPermissions: make(map[string]auth.TeamPermissions),
		},
	}
	c.Set("authz_context", authzCtx)
	return c, rec
}

// --- tests ---

// TestWaitSessionMessages_Since_ImmediateReturn verifies that when `since` is before
// the session's lastMessageAt, the handler returns immediately with updated=true
// without waiting for a new event.
func TestWaitSessionMessages_Since_ImmediateReturn(t *testing.T) {
	lastMsg := time.Now().Add(-5 * time.Minute)
	session := &mockWaitSession{
		id:            "sess-1",
		userID:        "user-1",
		lastMessageAt: lastMsg,
	}
	mgr := newMockWaitSessionManager(session)
	provider := &mockWaitProvider{manager: mgr}
	controller := NewSessionController(provider, nil)

	// since is 10 minutes ago, session was updated 5 minutes ago → should return immediately
	sinceMs := fmt.Sprintf("%d", time.Now().Add(-10*time.Minute).UnixMilli())
	c, rec := makeWaitEchoContext(t, "sess-1", map[string]string{"since": sinceMs, "timeout": "1"}, "user-1")

	err := controller.WaitSessionMessages(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["updated"])
	assert.Equal(t, "sess-1", resp["session_id"])
}

// TestWaitSessionMessages_Since_NoUpdate verifies that when `since` is after
// the session's lastMessageAt, the handler waits and returns updated=false on timeout.
func TestWaitSessionMessages_Since_NoUpdate(t *testing.T) {
	lastMsg := time.Now().Add(-10 * time.Minute)
	session := &mockWaitSession{
		id:            "sess-2",
		userID:        "user-1",
		lastMessageAt: lastMsg,
	}
	mgr := newMockWaitSessionManager(session)
	provider := &mockWaitProvider{manager: mgr}
	controller := NewSessionController(provider, nil)

	// since is 5 minutes ago, session was last updated 10 minutes ago → no catch-up
	sinceMs := fmt.Sprintf("%d", time.Now().Add(-5*time.Minute).UnixMilli())
	c, rec := makeWaitEchoContext(t, "sess-2", map[string]string{"since": sinceMs, "timeout": "1"}, "user-1")

	err := controller.WaitSessionMessages(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["updated"])
}

// TestWaitSessionMessages_NoSince_ReceivesEvent verifies that without a since parameter
// the handler blocks and returns updated=true when an event arrives.
func TestWaitSessionMessages_NoSince_ReceivesEvent(t *testing.T) {
	session := &mockWaitSession{
		id:            "sess-3",
		userID:        "user-1",
		lastMessageAt: time.Now().Add(-1 * time.Hour),
	}
	mgr := newMockWaitSessionManager(session)
	provider := &mockWaitProvider{manager: mgr}
	controller := NewSessionController(provider, nil)

	// Send an event before the handler runs (buffered channel).
	now := time.Now()
	mgr.eventCh <- services.SessionMessageEvent{SessionID: "sess-3", Timestamp: now}

	c, rec := makeWaitEchoContext(t, "sess-3", map[string]string{"timeout": "5"}, "user-1")

	err := controller.WaitSessionMessages(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["updated"])
}
