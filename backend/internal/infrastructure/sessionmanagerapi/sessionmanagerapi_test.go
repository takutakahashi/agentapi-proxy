package sessionmanagerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const testBearerToken = "session-manager-only-token"

type fakeSession struct {
	id            string
	addr          string
	userID        string
	scope         entities.ResourceScope
	teamID        string
	tags          map[string]string
	status        string
	startedAt     time.Time
	updatedAt     time.Time
	lastMessageAt time.Time
	description   string
	annotations   entities.SessionAnnotations
	request       *entities.RunServerRequest
}

func (s *fakeSession) ID() string                               { return s.id }
func (s *fakeSession) Addr() string                             { return s.addr }
func (s *fakeSession) UserID() string                           { return s.userID }
func (s *fakeSession) Scope() entities.ResourceScope            { return s.scope }
func (s *fakeSession) TeamID() string                           { return s.teamID }
func (s *fakeSession) Tags() map[string]string                  { return s.tags }
func (s *fakeSession) Status() string                           { return s.status }
func (s *fakeSession) StartedAt() time.Time                     { return s.startedAt }
func (s *fakeSession) UpdatedAt() time.Time                     { return s.updatedAt }
func (s *fakeSession) LastMessageAt() time.Time                 { return s.lastMessageAt }
func (s *fakeSession) Description() string                      { return s.description }
func (s *fakeSession) Cancel()                                  {}
func (s *fakeSession) Annotations() entities.SessionAnnotations { return s.annotations }
func (s *fakeSession) Request() *entities.RunServerRequest      { return s.request }

type fakeManager struct {
	sessions map[string]entities.Session

	createdID      string
	createdRequest *entities.RunServerRequest
	createdPayload []byte
	deletedID      string
	sentID         string
	sentMessage    string
	stoppedID      string
	lastFilter     entities.SessionFilter
	messages       []portrepos.Message

	ensureSession   entities.Session
	ensureRestoring bool
	annotationPatch entities.UpdateSessionAnnotationsRequest

	stockCount       int
	stockCreatedDind bool
	stockPurged      bool
	pendingDeleted   string
	provisionDeleted string

	submittedManagerID string
	submittedSessionID string
	submittedSettings  *sessionsettings.SessionSettings
	submittedRequest   *entities.RunServerRequest
	submittedRuntime   *coreallocation.RuntimeBootstrap
	nextLocal          *coreallocation.AllocationRequest
	nextExternal       *coreallocation.AllocationRequest
	nextExternalID     string
	completedLocal     coreallocation.AllocationResult
	completedExternal  coreallocation.AllocationResult
}

func newFakeManager() *fakeManager {
	return &fakeManager{sessions: make(map[string]entities.Session)}
}

func (m *fakeManager) CreateSession(_ context.Context, id string, request *entities.RunServerRequest, payload []byte) (entities.Session, error) {
	m.createdID = id
	m.createdRequest = request
	m.createdPayload = append([]byte(nil), payload...)
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	session := &fakeSession{
		id:            id,
		addr:          "agentapi-session-" + id + ".sessions.svc:9000",
		userID:        request.UserID,
		scope:         request.Scope,
		teamID:        request.TeamID,
		tags:          request.Tags,
		status:        "creating",
		startedAt:     now,
		updatedAt:     now.Add(time.Minute),
		lastMessageAt: now.Add(2 * time.Minute),
		description:   "rich session",
		annotations:   entities.SessionAnnotations{PRURL: "https://example.test/pull/1", Description: "annotated"},
		request:       request,
	}
	m.sessions[id] = session
	return session, nil
}

func (m *fakeManager) GetSession(id string) entities.Session { return m.sessions[id] }

func (m *fakeManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	m.lastFilter = filter
	result := make([]entities.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		result = append(result, session)
	}
	return result
}

func (m *fakeManager) DeleteSession(id string) error {
	m.deletedID = id
	delete(m.sessions, id)
	return nil
}

func (m *fakeManager) SendMessage(_ context.Context, id, message string) error {
	m.sentID, m.sentMessage = id, message
	return nil
}

func (m *fakeManager) StopAgent(_ context.Context, id string) error {
	m.stoppedID = id
	return nil
}

func (m *fakeManager) GetMessages(context.Context, string) ([]portrepos.Message, error) {
	return m.messages, nil
}

func (m *fakeManager) Shutdown(time.Duration) error { return nil }

func (m *fakeManager) EnsureSessionWorkload(context.Context, string) (entities.Session, bool, error) {
	return m.ensureSession, m.ensureRestoring, nil
}

func (m *fakeManager) UpdateSessionAnnotations(_ context.Context, _ string, patch entities.UpdateSessionAnnotationsRequest) (entities.SessionAnnotations, error) {
	m.annotationPatch = patch
	result := entities.SessionAnnotations{}
	if patch.Description != nil {
		result.Description = *patch.Description
	}
	return result, nil
}

func (m *fakeManager) CreateStockSession(_ context.Context, dind bool) error {
	m.stockCreatedDind = dind
	return nil
}

func (m *fakeManager) CountStockSessions(context.Context, bool) (int, error) {
	return m.stockCount, nil
}

func (m *fakeManager) PurgeStaleStockSessions(context.Context) error {
	m.stockPurged = true
	return nil
}

func (m *fakeManager) DeletePendingSessionAllocation(_ context.Context, id string) (bool, error) {
	m.pendingDeleted = id
	return true, nil
}

func (m *fakeManager) DeleteProvisionRequest(_ context.Context, id string) error {
	m.provisionDeleted = id
	return nil
}

func (m *fakeManager) SubmitExternalSessionAllocation(_ context.Context, managerID, sessionID string, settings *sessionsettings.SessionSettings, request *entities.RunServerRequest, runtime *coreallocation.RuntimeBootstrap) error {
	m.submittedManagerID = managerID
	m.submittedSessionID = sessionID
	m.submittedSettings = settings
	m.submittedRequest = request
	m.submittedRuntime = runtime
	return nil
}

func (m *fakeManager) NextSessionAllocation(context.Context, time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	allocation := m.nextLocal
	m.nextLocal = nil
	return allocation, allocation != nil, nil
}

func (m *fakeManager) CompleteSessionAllocation(_ context.Context, id string, result coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	m.completedLocal = result
	return &coreallocation.AllocationRequest{SessionID: id, Status: result.Status}, nil
}

func (m *fakeManager) NextExternalSessionAllocation(_ context.Context, managerID string, _ time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	m.nextExternalID = managerID
	allocation := m.nextExternal
	m.nextExternal = nil
	return allocation, allocation != nil, nil
}

func (m *fakeManager) CompleteExternalSessionAllocation(_ context.Context, id string, result coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	m.completedExternal = result
	return &coreallocation.AllocationRequest{SessionID: id, Status: result.Status}, nil
}

func newTestClient(t *testing.T, manager *fakeManager) (*Client, *httptest.Server) {
	t.Helper()
	handler, err := NewHandler(manager, testBearerToken)
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	handler.RegisterRoutes(e)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, testBearerToken)
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func TestClientSubscribeStatusEventsEmitsInitialSnapshot(t *testing.T) {
	manager := newFakeManager()
	now := time.Now().UTC()
	manager.sessions["session-error"] = &fakeSession{
		id: "session-error", userID: "user-1", scope: entities.ScopeUser,
		status: "error", startedAt: now, updatedAt: now, lastMessageAt: now,
	}
	client, _ := newTestClient(t, manager)

	events, cancel := client.SubscribeStatusEvents()
	defer cancel()

	select {
	case event := <-events:
		if event.SessionID != "session-error" || event.Status != "error" {
			t.Fatalf("unexpected initial status event: %#v", event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for initial status snapshot")
	}
}

func TestHandlerAuthenticatesEveryPrivateRoute(t *testing.T) {
	manager := newFakeManager()
	_, server := newTestClient(t, manager)

	for name, authorization := range map[string]struct {
		header string
		code   int
	}{
		"missing": {code: http.StatusUnauthorized},
		"wrong":   {header: "Bearer wrong", code: http.StatusUnauthorized},
		"valid":   {header: "Bearer " + testBearerToken, code: http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, server.URL+RoutePrefix+"/health", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", authorization.header)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close() //nolint:errcheck
			if resp.StatusCode != authorization.code {
				t.Fatalf("status = %d, want %d", resp.StatusCode, authorization.code)
			}
		})
	}

	if _, err := NewHandler(manager, ""); err == nil {
		t.Fatal("NewHandler accepted an empty token")
	}
}

func TestClientRoundTripsRichSessionLifecycle(t *testing.T) {
	manager := newFakeManager()
	manager.messages = []portrepos.Message{{Role: "assistant", Content: "done", Timestamp: time.Now()}}
	client, _ := newTestClient(t, manager)
	ctx := context.Background()

	request := &entities.RunServerRequest{
		UserID:     "user-1",
		Scope:      entities.ScopeTeam,
		TeamID:     "org/team",
		Tags:       map[string]string{"source": "test"},
		SessionTTL: "6h",
		Sandbox:    &entities.SandboxParams{PolicyID: "sandbox-policy-1"},
	}
	session, err := client.CreateSession(ctx, "caller-selected-id", request, []byte("webhook"))
	if err != nil {
		t.Fatal(err)
	}
	if manager.createdID != "caller-selected-id" {
		t.Fatalf("created ID = %q", manager.createdID)
	}
	if string(manager.createdPayload) != "webhook" {
		t.Fatalf("webhook payload = %q", manager.createdPayload)
	}
	if session.Addr() != "agentapi-session-caller-selected-id.sessions.svc:9000" {
		t.Fatalf("session addr = %q", session.Addr())
	}
	if session.UpdatedAt().Sub(session.StartedAt()) != time.Minute {
		t.Fatalf("updated time was not preserved: start=%s update=%s", session.StartedAt(), session.UpdatedAt())
	}
	if session.Description() != "rich session" || session.Tags()["session_ttl"] != "6h" {
		t.Fatalf("rich metadata missing: description=%q tags=%v", session.Description(), session.Tags())
	}
	annotated, ok := session.(interface {
		Annotations() entities.SessionAnnotations
	})
	if !ok || annotated.Annotations().PRURL != "https://example.test/pull/1" {
		t.Fatalf("annotations were not preserved: %#v", annotated)
	}
	sandboxed, ok := session.(interface{ SandboxPolicyID() string })
	if !ok || sandboxed.SandboxPolicyID() != "sandbox-policy-1" {
		t.Fatalf("sandbox policy was not preserved: %#v", sandboxed)
	}

	got, err := client.GetSessionContext(ctx, "caller-selected-id")
	if err != nil || got == nil || got.UserID() != "user-1" {
		t.Fatalf("GetSessionContext() = (%#v, %v)", got, err)
	}
	filter := entities.SessionFilter{
		UserID: "user-1", Scope: entities.ScopeTeam, TeamID: "org/team",
		TeamIDs: []string{"org/team", "org/other"}, Tags: map[string]string{"source": "test"},
	}
	listed, err := client.ListSessionsContext(ctx, filter)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListSessionsContext() = (%d sessions, %v)", len(listed), err)
	}
	if manager.lastFilter.TeamIDs[1] != "org/other" || manager.lastFilter.Tags["source"] != "test" {
		t.Fatalf("filter was not preserved: %#v", manager.lastFilter)
	}

	if err := client.SendMessage(ctx, session.ID(), "hello"); err != nil {
		t.Fatal(err)
	}
	if manager.sentID != session.ID() || manager.sentMessage != "hello" {
		t.Fatalf("send = (%q, %q)", manager.sentID, manager.sentMessage)
	}
	if err := client.StopAgent(ctx, session.ID()); err != nil || manager.stoppedID != session.ID() {
		t.Fatalf("StopAgent() err=%v stopped=%q", err, manager.stoppedID)
	}
	messages, err := client.GetMessages(ctx, session.ID())
	if err != nil || len(messages) != 1 || messages[0].Content != "done" {
		t.Fatalf("GetMessages() = (%#v, %v)", messages, err)
	}

	manager.ensureSession, manager.ensureRestoring = session, true
	ensured, restoring, err := client.EnsureSessionWorkload(ctx, session.ID())
	if err != nil || !restoring || ensured == nil || ensured.Addr() != session.Addr() {
		t.Fatalf("EnsureSessionWorkload() = (%#v, %t, %v)", ensured, restoring, err)
	}
	description := "new description"
	annotations, err := client.UpdateSessionAnnotations(ctx, session.ID(), entities.UpdateSessionAnnotationsRequest{Description: &description})
	if err != nil || annotations.Description != description || manager.annotationPatch.Description == nil {
		t.Fatalf("UpdateSessionAnnotations() = (%#v, %v)", annotations, err)
	}

	if err := client.DeleteSessionContext(ctx, session.ID()); err != nil || manager.deletedID != session.ID() {
		t.Fatalf("DeleteSessionContext() err=%v deleted=%q", err, manager.deletedID)
	}
	missing, err := client.GetSessionContext(ctx, session.ID())
	if err != nil || missing != nil {
		t.Fatalf("missing session = (%#v, %v)", missing, err)
	}
}

func TestClientRoundTripsStockCleanupAndAllocationQueue(t *testing.T) {
	manager := newFakeManager()
	manager.stockCount = 3
	manager.nextLocal = &coreallocation.AllocationRequest{SessionID: "local-allocation", Status: coreallocation.StatusPending}
	manager.nextExternal = &coreallocation.AllocationRequest{SessionID: "external-allocation", ManagerID: "manager-1", Status: coreallocation.StatusPending}
	client, _ := newTestClient(t, manager)
	ctx := context.Background()

	count, err := client.CountStockSessions(ctx, true)
	if err != nil || count != 3 {
		t.Fatalf("CountStockSessions() = (%d, %v)", count, err)
	}
	if err := client.CreateStockSession(ctx, true); err != nil || !manager.stockCreatedDind {
		t.Fatalf("CreateStockSession() err=%v dind=%t", err, manager.stockCreatedDind)
	}
	if err := client.PurgeStaleStockSessions(ctx); err != nil || !manager.stockPurged {
		t.Fatalf("PurgeStaleStockSessions() err=%v purged=%t", err, manager.stockPurged)
	}

	deleted, err := client.DeletePendingSessionAllocation(ctx, "pending-1")
	if err != nil || !deleted || manager.pendingDeleted != "pending-1" {
		t.Fatalf("DeletePendingSessionAllocation() = (%t, %v), id=%q", deleted, err, manager.pendingDeleted)
	}
	if err := client.DeleteProvisionRequest(ctx, "provision-1"); err != nil || manager.provisionDeleted != "provision-1" {
		t.Fatalf("DeleteProvisionRequest() err=%v id=%q", err, manager.provisionDeleted)
	}

	settings := &sessionsettings.SessionSettings{InitialMessage: "hello"}
	runRequest := &entities.RunServerRequest{UserID: "user-1"}
	runtime := &coreallocation.RuntimeBootstrap{Token: "runtime-token", Generation: 2}
	if err := client.SubmitExternalSessionAllocation(ctx, "manager-1", "external-submit", settings, runRequest, runtime); err != nil {
		t.Fatal(err)
	}
	if manager.submittedManagerID != "manager-1" || manager.submittedSessionID != "external-submit" || manager.submittedRuntime.Generation != 2 {
		t.Fatalf("submitted allocation was not preserved: manager=%q session=%q runtime=%#v", manager.submittedManagerID, manager.submittedSessionID, manager.submittedRuntime)
	}

	local, found, err := client.NextSessionAllocation(ctx, 5*time.Second)
	if err != nil || !found || local.SessionID != "local-allocation" {
		t.Fatalf("NextSessionAllocation() = (%#v, %t, %v)", local, found, err)
	}
	if empty, found, err := client.NextSessionAllocation(ctx, 0); err != nil || found || empty != nil {
		t.Fatalf("empty NextSessionAllocation() = (%#v, %t, %v)", empty, found, err)
	}
	completed, err := client.CompleteSessionAllocation(ctx, local.SessionID, coreallocation.AllocationResult{Status: coreallocation.StatusAssigned, AllocatedSessionID: "runtime-1"})
	if err != nil || completed.SessionID != local.SessionID || manager.completedLocal.AllocatedSessionID != "runtime-1" {
		t.Fatalf("CompleteSessionAllocation() = (%#v, %v), result=%#v", completed, err, manager.completedLocal)
	}

	external, found, err := client.NextExternalSessionAllocation(ctx, "manager-1", 3*time.Second)
	if err != nil || !found || external.SessionID != "external-allocation" || manager.nextExternalID != "manager-1" {
		t.Fatalf("NextExternalSessionAllocation() = (%#v, %t, %v), manager=%q", external, found, err, manager.nextExternalID)
	}
	completedExternal, err := client.CompleteExternalSessionAllocation(ctx, external.SessionID, coreallocation.AllocationResult{Status: coreallocation.StatusError, Message: "failed"})
	if err != nil || completedExternal.SessionID != external.SessionID || manager.completedExternal.Message != "failed" {
		t.Fatalf("CompleteExternalSessionAllocation() = (%#v, %v), result=%#v", completedExternal, err, manager.completedExternal)
	}
}

func TestClientReturnsTypedAuthenticationError(t *testing.T) {
	manager := newFakeManager()
	_, server := newTestClient(t, manager)
	client, err := NewClient(server.URL, "wrong-token")
	if err != nil {
		t.Fatal(err)
	}
	err = client.Health(context.Background())
	httpErr, ok := err.(*HTTPError)
	if !ok || httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("Health() error = %#v", err)
	}
	var response errorResponse
	if err := json.Unmarshal([]byte(`{"error":"unauthorized"}`), &response); err != nil || response.Error == "" {
		t.Fatal("error response contract is not JSON-decodable")
	}
}
