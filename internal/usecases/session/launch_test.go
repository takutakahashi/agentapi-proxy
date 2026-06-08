package session

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type recordingSessionManager struct {
	req *entities.RunServerRequest
}

func (m *recordingSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	m.req = req
	return &launchTestSession{id: id, userID: req.UserID, status: "active"}, nil
}

func (m *recordingSessionManager) GetSession(string) entities.Session { return nil }
func (m *recordingSessionManager) ListSessions(entities.SessionFilter) []entities.Session {
	return nil
}
func (m *recordingSessionManager) DeleteSession(string) error { return nil }
func (m *recordingSessionManager) SendMessage(context.Context, string, string) error {
	return nil
}
func (m *recordingSessionManager) StopAgent(context.Context, string) error { return nil }
func (m *recordingSessionManager) GetMessages(context.Context, string) ([]repositories.Message, error) {
	return nil, nil
}
func (m *recordingSessionManager) Shutdown(time.Duration) error { return nil }

type launchTestSession struct {
	id     string
	userID string
	status string
}

func (s *launchTestSession) ID() string                    { return s.id }
func (s *launchTestSession) Addr() string                  { return "" }
func (s *launchTestSession) UserID() string                { return s.userID }
func (s *launchTestSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *launchTestSession) TeamID() string                { return "" }
func (s *launchTestSession) Tags() map[string]string       { return nil }
func (s *launchTestSession) Status() string                { return s.status }
func (s *launchTestSession) StartedAt() time.Time          { return time.Time{} }
func (s *launchTestSession) UpdatedAt() time.Time          { return time.Time{} }
func (s *launchTestSession) LastMessageAt() time.Time      { return time.Time{} }
func (s *launchTestSession) Description() string           { return "" }
func (s *launchTestSession) Cancel()                       {}

type fakeSessionProfileRepo struct {
	profiles []*entities.SessionProfile
}

func (r *fakeSessionProfileRepo) Create(context.Context, *entities.SessionProfile) error {
	return nil
}
func (r *fakeSessionProfileRepo) Get(_ context.Context, id string) (*entities.SessionProfile, error) {
	for _, p := range r.profiles {
		if p.ID() == id {
			return p, nil
		}
	}
	return nil, entities.ErrSessionProfileNotFound{ID: id}
}
func (r *fakeSessionProfileRepo) List(_ context.Context, filter repositories.SessionProfileFilter) ([]*entities.SessionProfile, error) {
	var result []*entities.SessionProfile
	for _, p := range r.profiles {
		if p.UserID() != filter.UserID || p.Scope() != filter.Scope {
			continue
		}
		if filter.Scope == entities.ScopeTeam && p.TeamID() != filter.TeamID {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}
func (r *fakeSessionProfileRepo) Update(context.Context, *entities.SessionProfile) error {
	return nil
}
func (r *fakeSessionProfileRepo) Delete(context.Context, string) error { return nil }

func TestLaunchAppliesDefaultProfileDocker(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		Docker:     &entities.DockerParams{Enabled: true},
		SessionTTL: "48h",
	})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil {
		t.Fatal("CreateSession was not called")
	}
	if sessionManager.req.Docker == nil || !sessionManager.req.Docker.Enabled {
		t.Fatalf("expected profile Docker.Enabled to be applied, got %#v", sessionManager.req.Docker)
	}
	if sessionManager.req.SessionTTL != "48h" {
		t.Fatalf("expected profile SessionTTL to be applied, got %q", sessionManager.req.SessionTTL)
	}
}

func TestLaunchExplicitDockerOverridesProfileDocker(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		Docker: &entities.DockerParams{Enabled: true},
	})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		Docker: &entities.DockerParams{Enabled: false},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil || sessionManager.req.Docker == nil {
		t.Fatalf("expected explicit Docker params, got %#v", sessionManager.req)
	}
	if sessionManager.req.Docker.Enabled {
		t.Fatalf("expected explicit Docker.Enabled=false to override profile, got %#v", sessionManager.req.Docker)
	}
}
