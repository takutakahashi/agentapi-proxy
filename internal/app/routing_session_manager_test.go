package app

import (
	"context"
	"testing"
	"time"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type routingTestSessionManager struct {
	sessionallocation.Queue
	req     *entities.RunServerRequest
	payload []byte
	queued  string
}

func (m *routingTestSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, payload []byte) (entities.Session, error) {
	m.req = req
	m.payload = payload
	return entities.NewProxySession(id, req.UserID, req.Scope, req.TeamID, req.Tags, time.Now()), nil
}
func (m *routingTestSessionManager) GetSession(string) entities.Session { return nil }
func (m *routingTestSessionManager) ListSessions(entities.SessionFilter) []entities.Session {
	return nil
}
func (m *routingTestSessionManager) DeleteSession(string) error { return nil }
func (m *routingTestSessionManager) SendMessage(context.Context, string, string) error {
	return nil
}
func (m *routingTestSessionManager) StopAgent(context.Context, string) error { return nil }
func (m *routingTestSessionManager) GetMessages(context.Context, string) ([]portrepos.Message, error) {
	return nil, nil
}
func (m *routingTestSessionManager) Shutdown(time.Duration) error { return nil }
func (m *routingTestSessionManager) SubmitExternalSessionAllocation(_ context.Context, managerID, _ string, _ *sessionsettings.SessionSettings, req *entities.RunServerRequest) error {
	m.queued = managerID
	m.req = req
	return nil
}

type routingTestSettingsRepository struct {
	settings map[string]*entities.Settings
}

func (r *routingTestSettingsRepository) Save(_ context.Context, settings *entities.Settings) error {
	if r.settings == nil {
		r.settings = map[string]*entities.Settings{}
	}
	r.settings[settings.Name()] = settings
	return nil
}
func (r *routingTestSettingsRepository) FindByName(_ context.Context, name string) (*entities.Settings, error) {
	return r.settings[name], nil
}
func (r *routingTestSettingsRepository) Delete(_ context.Context, name string) error {
	delete(r.settings, name)
	return nil
}
func (r *routingTestSettingsRepository) Exists(_ context.Context, name string) (bool, error) {
	_, ok := r.settings[name]
	return ok, nil
}
func (r *routingTestSettingsRepository) List(context.Context) ([]*entities.Settings, error) {
	return nil, nil
}

func TestCreateRoutedSessionFallsBackToLocalManager(t *testing.T) {
	local := &routingTestSessionManager{}
	server := &Server{sessionManager: local}
	req := &entities.RunServerRequest{UserID: "user-1", Scope: entities.ScopeUser}
	payload := []byte("payload")

	_, err := server.createRoutedSession(context.Background(), "session-1", req, payload)
	if err != nil {
		t.Fatalf("createRoutedSession() error = %v", err)
	}
	if local.req != req || string(local.payload) != string(payload) {
		t.Fatalf("local manager received req=%#v payload=%q", local.req, local.payload)
	}
}

func TestCreateRoutedSessionRejectsUnknownExplicitManager(t *testing.T) {
	server := &Server{sessionManager: &routingTestSessionManager{}}
	_, err := server.createRoutedSession(context.Background(), "session-1", &entities.RunServerRequest{
		UserID:    "user-1",
		Scope:     entities.ScopeUser,
		ManagerID: "missing-manager",
	}, nil)
	if err == nil {
		t.Fatal("expected unknown explicit manager to fail")
	}
}

func TestCreateRoutedSessionRejectsUnmatchedAllocatorSelector(t *testing.T) {
	server := &Server{sessionManager: &routingTestSessionManager{}}
	_, err := server.createRoutedSession(context.Background(), "session-1", &entities.RunServerRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		Tags:   map[string]string{"allocator.os": "linux"},
	}, nil)
	if err == nil {
		t.Fatal("expected unmatched allocator selector to fail")
	}
}

func TestCreateRoutedSessionQueuesProfileSelectedManager(t *testing.T) {
	manager := &routingTestSessionManager{}
	settings := entities.NewSettings("user-1")
	settings.SetExternalSessionManagers([]entities.ExternalSessionManagerEntry{{
		ID:   "manager-profile",
		Name: "Profile manager",
	}})
	server := &Server{
		sessionManager: manager,
		settingsRepo: &routingTestSettingsRepository{settings: map[string]*entities.Settings{
			"user-1": settings,
		}},
	}

	_, err := server.createRoutedSession(context.Background(), "session-1", &entities.RunServerRequest{
		UserID:    "user-1",
		Scope:     entities.ScopeUser,
		ManagerID: "manager-profile",
	}, nil)
	if err != nil {
		t.Fatalf("createRoutedSession() error = %v", err)
	}
	if manager.queued != "manager-profile" || manager.req == nil || manager.req.ManagerID != "manager-profile" {
		t.Fatalf("external allocation manager=%q req=%#v", manager.queued, manager.req)
	}
}
