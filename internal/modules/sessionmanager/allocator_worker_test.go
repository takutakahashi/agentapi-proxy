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
	created   int
	createErr error
}

func (m *fakeAllocatorSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return m.CreateSessionDirect(ctx, id, req, webhookPayload)
}

func (m *fakeAllocatorSessionManager) CreateSessionDirect(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	m.created++
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
