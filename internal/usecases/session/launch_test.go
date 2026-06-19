package session

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type recordingSessionManager struct {
	req             *entities.RunServerRequest
	existing        []entities.Session
	sendMessageErr  error
	stopAgentErr    error
	sendMessageID   string
	sendMessageBody string
	stopAgentID     string
	calls           []string
	createdID       string
}

func (m *recordingSessionManager) CreateSession(_ context.Context, id string, req *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	m.req = req
	m.createdID = id
	return &launchTestSession{id: id, userID: req.UserID, status: "active"}, nil
}

func (m *recordingSessionManager) GetSession(string) entities.Session { return nil }
func (m *recordingSessionManager) ListSessions(entities.SessionFilter) []entities.Session {
	return m.existing
}
func (m *recordingSessionManager) DeleteSession(string) error { return nil }
func (m *recordingSessionManager) SendMessage(_ context.Context, id string, message string) error {
	m.calls = append(m.calls, "send")
	m.sendMessageID = id
	m.sendMessageBody = message
	return m.sendMessageErr
}
func (m *recordingSessionManager) StopAgent(_ context.Context, id string) error {
	m.calls = append(m.calls, "stop")
	m.stopAgentID = id
	return m.stopAgentErr
}
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

func TestLaunchAppliesDefaultProfileAuthProxy(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		AuthProxy: boolPointer(true),
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
	if sessionManager.req == nil || sessionManager.req.AuthProxy == nil || !*sessionManager.req.AuthProxy {
		t.Fatalf("expected profile AuthProxy=true to be applied, got %#v", sessionManager.req)
	}
}

func TestLaunchExplicitAuthProxyOverridesProfileAuthProxy(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	profile := entities.NewSessionProfile("profile-1", "default", "user-1")
	profile.SetIsDefault(true)
	cfg := entities.NewSessionProfileConfig()
	cfg.SetParams(&entities.SessionParams{
		AuthProxy: boolPointer(true),
	})
	profile.SetConfig(cfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{profile}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:    "user-1",
		Scope:     entities.ScopeUser,
		AuthProxy: boolPointer(false),
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req == nil || sessionManager.req.AuthProxy == nil || *sessionManager.req.AuthProxy {
		t.Fatalf("expected explicit AuthProxy=false to override profile, got %#v", sessionManager.req)
	}
}

func TestLaunchAppliesProfileSelectedByTags(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	defaultProfile := entities.NewSessionProfile("profile-default", "default", "user-1")
	defaultProfile.SetIsDefault(true)
	defaultCfg := entities.NewSessionProfileConfig()
	defaultCfg.SetParams(&entities.SessionParams{AgentType: "claude"})
	defaultProfile.SetConfig(defaultCfg)

	selectedProfile := entities.NewSessionProfile("profile-selected", "selected", "user-1")
	selectedProfile.SetSelectorTags(map[string]string{"env": "dev"})
	selectedCfg := entities.NewSessionProfileConfig()
	selectedCfg.SetParams(&entities.SessionParams{AgentType: "codex"})
	selectedCfg.SetTags(map[string]string{"profile": "selected", "env": "profile"})
	selectedProfile.SetConfig(selectedCfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{
			defaultProfile,
			selectedProfile,
		}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		Tags:   map[string]string{"env": "dev", "request": "kept"},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req.AgentType != "codex" {
		t.Fatalf("expected tag-selected profile agent type, got %q", sessionManager.req.AgentType)
	}
	if sessionManager.req.Tags["env"] != "dev" || sessionManager.req.Tags["profile"] != "selected" || sessionManager.req.Tags["request"] != "kept" {
		t.Fatalf("unexpected merged tags: %#v", sessionManager.req.Tags)
	}
}

func TestLaunchExplicitProfileIDOverridesTagSelector(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	explicitProfile := entities.NewSessionProfile("profile-explicit", "explicit", "user-1")
	explicitCfg := entities.NewSessionProfileConfig()
	explicitCfg.SetParams(&entities.SessionParams{AgentType: "claude"})
	explicitProfile.SetConfig(explicitCfg)

	selectedProfile := entities.NewSessionProfile("profile-selected", "selected", "user-1")
	selectedProfile.SetSelectorTags(map[string]string{"env": "dev"})
	selectedCfg := entities.NewSessionProfileConfig()
	selectedCfg.SetParams(&entities.SessionParams{AgentType: "codex"})
	selectedProfile.SetConfig(selectedCfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{
			explicitProfile,
			selectedProfile,
		}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID:           "user-1",
		Scope:            entities.ScopeUser,
		Tags:             map[string]string{"env": "dev"},
		SessionProfileID: "profile-explicit",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req.AgentType != "claude" {
		t.Fatalf("expected explicit profile agent type, got %q", sessionManager.req.AgentType)
	}
}

func TestLaunchChoosesMostSpecificTagSelectedProfile(t *testing.T) {
	sessionManager := &recordingSessionManager{}
	genericProfile := entities.NewSessionProfile("profile-generic", "generic", "user-1")
	genericProfile.SetSelectorTags(map[string]string{"env": "dev"})
	genericCfg := entities.NewSessionProfileConfig()
	genericCfg.SetParams(&entities.SessionParams{AgentType: "claude"})
	genericProfile.SetConfig(genericCfg)

	specificProfile := entities.NewSessionProfile("profile-specific", "specific", "user-1")
	specificProfile.SetSelectorTags(map[string]string{"env": "dev", "repo": "owner/repo"})
	specificCfg := entities.NewSessionProfileConfig()
	specificCfg.SetParams(&entities.SessionParams{AgentType: "codex"})
	specificProfile.SetConfig(specificCfg)

	launcher := NewLaunchUseCase(sessionManager).
		WithSessionProfileRepository(&fakeSessionProfileRepo{profiles: []*entities.SessionProfile{
			genericProfile,
			specificProfile,
		}})

	_, err := launcher.Launch(context.Background(), "session-1", LaunchRequest{
		UserID: "user-1",
		Scope:  entities.ScopeUser,
		Tags:   map[string]string{"env": "dev", "repo": "owner/repo"},
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if sessionManager.req.AgentType != "codex" {
		t.Fatalf("expected most specific selected profile, got %q", sessionManager.req.AgentType)
	}
}

func TestLaunchReuseRoutesMessageToExistingSession(t *testing.T) {
	sessionManager := &recordingSessionManager{
		existing: []entities.Session{&launchTestSession{id: "existing-1", userID: "user-1", status: "active"}},
	}
	launcher := NewLaunchUseCase(sessionManager)

	result, err := launcher.Launch(context.Background(), "new-1", LaunchRequest{
		UserID:         "user-1",
		Scope:          entities.ScopeUser,
		InitialMessage: "initial",
		ReuseSession:   true,
		ReuseMatchTags: map[string]string{"webhook_id": "webhook-1"},
		ReuseMessage:   "reuse",
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !result.SessionReused || result.SessionID != "existing-1" {
		t.Fatalf("expected reused existing session, got %#v", result)
	}
	if sessionManager.req != nil {
		t.Fatal("CreateSession should not be called when reuse succeeds")
	}
	if sessionManager.sendMessageID != "existing-1" || sessionManager.sendMessageBody != "reuse" {
		t.Fatalf("unexpected SendMessage call: id=%q body=%q", sessionManager.sendMessageID, sessionManager.sendMessageBody)
	}
}

func TestLaunchReuseStopsExistingSessionBeforeSendingWhenRequested(t *testing.T) {
	sessionManager := &recordingSessionManager{
		existing: []entities.Session{&launchTestSession{id: "existing-1", userID: "user-1", status: "active"}},
	}
	launcher := NewLaunchUseCase(sessionManager)

	result, err := launcher.Launch(context.Background(), "new-1", LaunchRequest{
		UserID:          "user-1",
		Scope:           entities.ScopeUser,
		InitialMessage:  "initial",
		ReuseSession:    true,
		ReuseMatchTags:  map[string]string{"webhook_id": "webhook-1"},
		ReuseMessage:    "reuse",
		StopBeforeReuse: true,
	})
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if !result.SessionReused || result.SessionID != "existing-1" {
		t.Fatalf("expected reused existing session, got %#v", result)
	}
	if sessionManager.req != nil {
		t.Fatal("CreateSession should not be called when reuse succeeds")
	}
	if sessionManager.stopAgentID != "existing-1" {
		t.Fatalf("unexpected StopAgent id: %q", sessionManager.stopAgentID)
	}
	if sessionManager.sendMessageID != "existing-1" || sessionManager.sendMessageBody != "reuse" {
		t.Fatalf("unexpected SendMessage call: id=%q body=%q", sessionManager.sendMessageID, sessionManager.sendMessageBody)
	}
	if !reflect.DeepEqual(sessionManager.calls, []string{"stop", "send"}) {
		t.Fatalf("unexpected call order: %#v", sessionManager.calls)
	}
}

func TestLaunchReuseReturnsErrorWhenStopBeforeReuseFails(t *testing.T) {
	sessionManager := &recordingSessionManager{
		existing:     []entities.Session{&launchTestSession{id: "existing-1", userID: "user-1", status: "active"}},
		stopAgentErr: errors.New("stop failed"),
	}
	launcher := NewLaunchUseCase(sessionManager)

	_, err := launcher.Launch(context.Background(), "new-1", LaunchRequest{
		UserID:          "user-1",
		Scope:           entities.ScopeUser,
		InitialMessage:  "initial",
		ReuseSession:    true,
		ReuseMatchTags:  map[string]string{"webhook_id": "webhook-1"},
		StopBeforeReuse: true,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if sessionManager.sendMessageID != "" {
		t.Fatal("SendMessage should not be called when StopAgent fails")
	}
	if sessionManager.req != nil {
		t.Fatal("CreateSession should not be called when StopAgent fails")
	}
}

func TestLaunchReuseReturnsErrorForNonStaleSendFailure(t *testing.T) {
	sessionManager := &recordingSessionManager{
		existing:       []entities.Session{&launchTestSession{id: "existing-1", userID: "user-1", status: "active"}},
		sendMessageErr: errors.New("connection refused"),
	}
	launcher := NewLaunchUseCase(sessionManager)

	_, err := launcher.Launch(context.Background(), "new-1", LaunchRequest{
		UserID:         "user-1",
		Scope:          entities.ScopeUser,
		InitialMessage: "initial",
		ReuseSession:   true,
		ReuseMatchTags: map[string]string{"webhook_id": "webhook-1"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if sessionManager.req != nil {
		t.Fatal("CreateSession should not be called for non-stale reuse errors")
	}
}

func boolPointer(v bool) *bool {
	return &v
}
