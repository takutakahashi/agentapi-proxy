package sessionmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestAllocatorWorkerCompletesAssignedAllocation(t *testing.T) {
	client := &fakeExternalAllocatorClient{}
	manager := &fakeAllocatorSessionManager{}
	worker := NewAllocatorWorkerWithClient(manager, client, "https://esm.example")

	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "parent-session",
		ProvisionSettings: &sessionsettings.SessionSettings{
			Session: sessionsettings.SessionMeta{
				UserID:    "user-1",
				Scope:     string(entities.ScopeUser),
				AgentType: "codex",
			},
		},
	})

	if len(client.completed) != 1 {
		t.Fatalf("completed results = %d, want 1", len(client.completed))
	}
	result := client.completed[0]
	if result.sessionID != "parent-session" {
		t.Fatalf("completed sessionID = %q, want parent-session", result.sessionID)
	}
	if result.result.Status != sessionallocation.StatusAssigned {
		t.Fatalf("status = %q, want assigned", result.result.Status)
	}
	if result.result.AllocatedSessionID == "" {
		t.Fatalf("allocated_session_id is empty")
	}
	if result.result.ProxyURL != "https://esm.example" {
		t.Fatalf("proxy_url = %q, want https://esm.example", result.result.ProxyURL)
	}
	if manager.created == 0 {
		t.Fatalf("local session was not created")
	}
}

func TestAllocatorWorkerPointsOneshotDeletionAtUpstreamProxy(t *testing.T) {
	client := &fakeExternalAllocatorClient{}
	manager := &fakeAllocatorSessionManager{}
	worker := newAllocatorWorkerWithClient(manager, client, "https://proxy.example", "https://esm.example")
	settings := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{UserID: "user-1", Scope: string(entities.ScopeUser), Oneshot: true},
	}

	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID:         "public-session",
		ProvisionSettings: settings,
	})

	if got := settings.Env["AGENTAPI_PROXY_ENDPOINT"]; got != "https://proxy.example" {
		t.Fatalf("AGENTAPI_PROXY_ENDPOINT = %q, want upstream proxy URL", got)
	}
}

func TestAllocatorWorkerInjectsDirectParentRuntime(t *testing.T) {
	worker := newAllocatorWorkerWithClient(&fakeAllocatorSessionManager{}, &fakeExternalAllocatorClient{}, "https://parent.example", "")
	settings := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{UserID: "user-1", Scope: string(entities.ScopeUser)}}
	request := &entities.RunServerRequest{}
	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "public-session", ManagerID: "manager-a", ProvisionSettings: settings, Request: request,
		Runtime: &sessionallocation.RuntimeBootstrap{Token: "runtime-secret", Generation: 2},
	})

	if settings.ParentRuntime == nil {
		t.Fatal("parent runtime was not injected into provision settings")
	}
	if got := settings.ParentRuntime.Endpoint; got != "https://parent.example" {
		t.Fatalf("endpoint=%q", got)
	}
	if settings.ParentRuntime.SessionID != "public-session" || settings.ParentRuntime.ManagerID != "manager-a" || settings.ParentRuntime.Generation != 2 {
		t.Fatalf("unexpected runtime config: %#v", settings.ParentRuntime)
	}
	if request.ParentRuntime != settings.ParentRuntime {
		t.Fatal("request did not receive the same runtime bootstrap")
	}
}

func TestAllocatorWorkerAppliesParentRuntimeProfileBeforeCreatingSession(t *testing.T) {
	client := &fakeExternalAllocatorClient{}
	manager := &fakeAllocatorSessionManager{}
	worker := NewAllocatorWorkerWithClient(manager, client, "https://esm.example")
	profile := &sessionsettings.RuntimeProfile{Version: 1, Scia: sessionsettings.SciaRuntimeProfile{Enabled: true, SessionSidecarImage: "scia:parent"}}

	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "parent-session",
		ProvisionSettings: &sessionsettings.SessionSettings{
			Session: sessionsettings.SessionMeta{UserID: "user-1", Scope: string(entities.ScopeUser)},
		},
		RuntimeProfile: profile,
	})

	if manager.appliedProfile != profile {
		t.Fatalf("applied profile = %#v, want %#v", manager.appliedProfile, profile)
	}
	if manager.created != 1 {
		t.Fatalf("created = %d, want 1", manager.created)
	}
}

func TestAllocatorWorkerCanIgnoreParentRuntimeProfile(t *testing.T) {
	client := &fakeExternalAllocatorClient{}
	manager := &fakeAllocatorSessionManager{profileErr: errors.New("must not be called")}
	worker := NewAllocatorWorkerWithClient(manager, client, "https://native.example")
	worker.applyRuntimeProfile = false

	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "parent-session",
		ProvisionSettings: &sessionsettings.SessionSettings{
			Session: sessionsettings.SessionMeta{UserID: "user-1", Scope: string(entities.ScopeUser)},
		},
		RuntimeProfile: &sessionsettings.RuntimeProfile{Version: 1},
	})

	if manager.appliedProfile != nil {
		t.Fatalf("applied profile = %#v, want nil", manager.appliedProfile)
	}
	if manager.created != 1 {
		t.Fatalf("created = %d, want 1", manager.created)
	}
}

func TestAllocatorWorkerSynchronizesChangedRuntimeProfileRevision(t *testing.T) {
	profile := &sessionsettings.RuntimeProfile{Version: 1}
	client := &fakeRuntimeProfileClient{
		fakeExternalAllocatorClient: fakeExternalAllocatorClient{},
		revision:                    "revision-1",
		snapshot: &sessionallocation.RuntimeProfileSnapshot{
			Revision: "revision-1",
			Profile:  profile,
		},
	}
	manager := &fakeAllocatorSessionManager{}
	worker := NewAllocatorWorkerWithClient(manager, client, "https://esm.example")

	worker.syncRuntimeProfile(context.Background(), false)
	worker.syncRuntimeProfile(context.Background(), false)
	if client.profileGets != 1 {
		t.Fatalf("profile gets = %d, want 1", client.profileGets)
	}
	if manager.appliedProfile != profile || worker.appliedProfileRevision != "revision-1" {
		t.Fatalf("profile synchronization failed: manager=%#v revision=%q", manager.appliedProfile, worker.appliedProfileRevision)
	}

	worker.syncRuntimeProfile(context.Background(), true)
	if client.profileGets != 2 {
		t.Fatalf("profile gets after reconnect = %d, want 2", client.profileGets)
	}
}

func TestAllocatorWorkerRejectsAllocationWhenRuntimeProfileCannotBeApplied(t *testing.T) {
	client := &fakeExternalAllocatorClient{}
	manager := &fakeAllocatorSessionManager{profileErr: errors.New("rbac denied")}
	worker := NewAllocatorWorkerWithClient(manager, client, "https://esm.example")

	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "parent-session",
		ProvisionSettings: &sessionsettings.SessionSettings{
			Session: sessionsettings.SessionMeta{UserID: "user-1", Scope: string(entities.ScopeUser)},
		},
		RuntimeProfile: &sessionsettings.RuntimeProfile{Version: 1},
	})

	if manager.created != 0 {
		t.Fatalf("created = %d, want 0", manager.created)
	}
	if len(client.completed) != 1 || client.completed[0].result.Status != sessionallocation.StatusError {
		t.Fatalf("completion = %#v", client.completed)
	}
}

func TestAllocatorWorkerLeavesRegularSessionEndpointUnchanged(t *testing.T) {
	worker := newAllocatorWorkerWithClient(&fakeAllocatorSessionManager{}, &fakeExternalAllocatorClient{}, "https://proxy.example", "https://esm.example")
	settings := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{UserID: "user-1", Scope: string(entities.ScopeUser)},
		Env:     map[string]string{"AGENTAPI_PROXY_ENDPOINT": "https://custom.example"},
	}

	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID:         "public-session",
		ProvisionSettings: settings,
	})

	if got := settings.Env["AGENTAPI_PROXY_ENDPOINT"]; got != "https://custom.example" {
		t.Fatalf("AGENTAPI_PROXY_ENDPOINT = %q, want existing endpoint", got)
	}
}

func TestAllocatorWorkerCompletesErrorWhenProvisionSettingsMissing(t *testing.T) {
	client := &fakeExternalAllocatorClient{}
	worker := NewAllocatorWorkerWithClient(&fakeAllocatorSessionManager{}, client, "https://esm.example")

	worker.process(context.Background(), &sessionallocation.AllocationRequest{SessionID: "parent-session"})

	if len(client.completed) != 1 {
		t.Fatalf("completed results = %d, want 1", len(client.completed))
	}
	result := client.completed[0].result
	if result.Status != sessionallocation.StatusError {
		t.Fatalf("status = %q, want error", result.Status)
	}
	if result.Message != "provision_settings is required" {
		t.Fatalf("message = %q, want provision_settings is required", result.Message)
	}
}

func TestNativeAllocatorUsesMetadataAndPublicSessionID(t *testing.T) {
	client := &fakeExternalAllocatorClient{}
	manager := &fakeNativeAllocatorSessionManager{}
	worker := NewAllocatorWorkerWithClient(manager, client, "https://native.example")
	metadata := &entities.RunServerRequest{UserID: "user-1", Scope: entities.ScopeUser, AgentType: "codex-acp"}

	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "public-session",
		Request:   metadata,
	})

	if manager.lastID != "public-session" || manager.lastReq != metadata {
		t.Fatalf("native create = id %q request %#v", manager.lastID, manager.lastReq)
	}
	if len(client.completed) != 1 || client.completed[0].result.Status != sessionallocation.StatusAssigned {
		t.Fatalf("completion = %#v", client.completed)
	}
}

func TestAllocatorWorkerCompletesErrorWhenLocalSessionCreationFails(t *testing.T) {
	client := &fakeExternalAllocatorClient{}
	manager := &fakeAllocatorSessionManager{createErr: errors.New("create failed")}
	worker := NewAllocatorWorkerWithClient(manager, client, "https://esm.example")

	worker.process(context.Background(), &sessionallocation.AllocationRequest{
		SessionID: "parent-session",
		ProvisionSettings: &sessionsettings.SessionSettings{
			Session: sessionsettings.SessionMeta{UserID: "user-1", Scope: string(entities.ScopeUser)},
		},
	})

	if len(client.completed) != 1 {
		t.Fatalf("completed results = %d, want 1", len(client.completed))
	}
	result := client.completed[0].result
	if result.Status != sessionallocation.StatusError {
		t.Fatalf("status = %q, want error", result.Status)
	}
	if result.Message != "create failed" {
		t.Fatalf("message = %q, want create failed", result.Message)
	}
}

type fakeExternalAllocatorClient struct {
	completed []completedAllocation
}

type fakeRuntimeProfileClient struct {
	fakeExternalAllocatorClient
	revision    string
	snapshot    *sessionallocation.RuntimeProfileSnapshot
	profileGets int
}

func (c *fakeRuntimeProfileClient) RuntimeProfileRevision() string { return c.revision }

func (c *fakeRuntimeProfileClient) GetRuntimeProfile(context.Context) (*sessionallocation.RuntimeProfileSnapshot, error) {
	c.profileGets++
	return c.snapshot, nil
}

type completedAllocation struct {
	sessionID string
	result    sessionallocation.AllocationResult
}

func (c *fakeExternalAllocatorClient) NextExternal(context.Context, time.Duration) (*sessionallocation.AllocationRequest, bool, error) {
	return nil, false, nil
}

func (c *fakeExternalAllocatorClient) CompleteExternal(_ context.Context, sessionID string, result sessionallocation.AllocationResult) error {
	c.completed = append(c.completed, completedAllocation{sessionID: sessionID, result: result})
	return nil
}

type fakeAllocatorSessionManager struct {
	created        int
	createErr      error
	profileErr     error
	appliedProfile *sessionsettings.RuntimeProfile
	lastID         string
	lastReq        *entities.RunServerRequest
}

func (m *fakeAllocatorSessionManager) ApplyRuntimeProfile(_ context.Context, profile *sessionsettings.RuntimeProfile) error {
	m.appliedProfile = profile
	return m.profileErr
}

func (m *fakeAllocatorSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return m.CreateSessionDirect(ctx, id, req, webhookPayload)
}

func (m *fakeAllocatorSessionManager) CreateSessionDirect(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	m.created++
	m.lastID, m.lastReq = id, req
	if m.createErr != nil {
		return nil, m.createErr
	}
	return entities.NewProxySession(id, req.UserID, req.Scope, req.TeamID, req.Tags, time.Now()), nil
}

func (m *fakeAllocatorSessionManager) GetSession(string) entities.Session { return nil }
func (m *fakeAllocatorSessionManager) ListSessions(entities.SessionFilter) []entities.Session {
	return nil
}
func (m *fakeAllocatorSessionManager) DeleteSession(string) error { return nil }
func (m *fakeAllocatorSessionManager) SendMessage(context.Context, string, string) error {
	return nil
}
func (m *fakeAllocatorSessionManager) StopAgent(context.Context, string) error { return nil }
func (m *fakeAllocatorSessionManager) GetMessages(context.Context, string) ([]portrepos.Message, error) {
	return nil, nil
}
func (m *fakeAllocatorSessionManager) Shutdown(time.Duration) error { return nil }

type fakeNativeAllocatorSessionManager struct{ fakeAllocatorSessionManager }

func (m *fakeNativeAllocatorSessionManager) UsesRemoteProvisioner() bool { return true }
