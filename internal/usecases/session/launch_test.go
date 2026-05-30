package session

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type fakeLaunchSession struct {
	id string
}

func (s fakeLaunchSession) ID() string                    { return s.id }
func (s fakeLaunchSession) Addr() string                  { return "" }
func (s fakeLaunchSession) UserID() string                { return "" }
func (s fakeLaunchSession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s fakeLaunchSession) TeamID() string                { return "" }
func (s fakeLaunchSession) Tags() map[string]string       { return nil }
func (s fakeLaunchSession) Status() string                { return "active" }
func (s fakeLaunchSession) StartedAt() time.Time          { return time.Time{} }
func (s fakeLaunchSession) UpdatedAt() time.Time          { return time.Time{} }
func (s fakeLaunchSession) LastMessageAt() time.Time      { return time.Time{} }
func (s fakeLaunchSession) Description() string           { return "" }
func (s fakeLaunchSession) Cancel()                       {}

type fakeLaunchSessionManager struct {
	createReq *entities.RunServerRequest
}

func (m *fakeLaunchSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	m.createReq = req
	return fakeLaunchSession{id: id}, nil
}

func (m *fakeLaunchSessionManager) GetSession(id string) entities.Session { return nil }
func (m *fakeLaunchSessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	return nil
}
func (m *fakeLaunchSessionManager) DeleteSession(id string) error { return nil }
func (m *fakeLaunchSessionManager) SendMessage(_ context.Context, id string, message string) error {
	return nil
}
func (m *fakeLaunchSessionManager) StopAgent(_ context.Context, id string) error { return nil }
func (m *fakeLaunchSessionManager) GetMessages(_ context.Context, id string) ([]repositories.Message, error) {
	return nil, nil
}
func (m *fakeLaunchSessionManager) Shutdown(timeout time.Duration) error { return nil }

type fakeLaunchProfileRepo struct {
	profile *entities.SessionProfile
}

func (r *fakeLaunchProfileRepo) Create(ctx context.Context, profile *entities.SessionProfile) error {
	return nil
}
func (r *fakeLaunchProfileRepo) Get(ctx context.Context, id string) (*entities.SessionProfile, error) {
	return r.profile, nil
}
func (r *fakeLaunchProfileRepo) List(ctx context.Context, filter repositories.SessionProfileFilter) ([]*entities.SessionProfile, error) {
	return []*entities.SessionProfile{r.profile}, nil
}
func (r *fakeLaunchProfileRepo) Update(ctx context.Context, profile *entities.SessionProfile) error {
	return nil
}
func (r *fakeLaunchProfileRepo) Delete(ctx context.Context, id string) error { return nil }

func TestLaunchAppliesProfileInitialMessageTemplate(t *testing.T) {
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetInitialMessageTemplate("Review {{ .repository_full_name }}")
	profile.SetConfig(cfg)

	manager := &fakeLaunchSessionManager{}
	uc := NewLaunchUseCase(manager).WithSessionProfileRepository(&fakeLaunchProfileRepo{profile: profile})

	_, err := uc.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "user-1",
		Scope:            entities.ScopeUser,
		SessionProfileID: "profile-1",
		TemplatePayload: map[string]interface{}{
			"repository_full_name": "org/repo",
		},
	})
	if err != nil {
		t.Fatalf("Launch returned error: %v", err)
	}
	if manager.createReq == nil {
		t.Fatal("CreateSession was not called")
	}
	if got, want := manager.createReq.InitialMessage, "Review org/repo"; got != want {
		t.Fatalf("InitialMessage = %q, want %q", got, want)
	}
}

func TestLaunchExplicitInitialMessageOverridesProfileTemplate(t *testing.T) {
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	cfg := entities.NewSessionProfileConfig()
	cfg.SetInitialMessageTemplate("from profile")
	profile.SetConfig(cfg)

	manager := &fakeLaunchSessionManager{}
	uc := NewLaunchUseCase(manager).WithSessionProfileRepository(&fakeLaunchProfileRepo{profile: profile})

	_, err := uc.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "user-1",
		Scope:            entities.ScopeUser,
		SessionProfileID: "profile-1",
		InitialMessage:   "from request",
	})
	if err != nil {
		t.Fatalf("Launch returned error: %v", err)
	}
	if got, want := manager.createReq.InitialMessage, "from request"; got != want {
		t.Fatalf("InitialMessage = %q, want %q", got, want)
	}
}
