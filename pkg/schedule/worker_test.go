package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"k8s.io/client-go/kubernetes/fake"
)

// mockProxySessionManager implements portrepos.SessionManager for testing
type mockProxySessionManager struct {
	sessions map[string]*mockProxySession
}

type mockProxySession struct {
	id        string
	userID    string
	status    string
	addr      string
	tags      map[string]string
	startedAt time.Time
	updatedAt time.Time
}

func (s *mockProxySession) ID() string                    { return s.id }
func (s *mockProxySession) Addr() string                  { return s.addr }
func (s *mockProxySession) UserID() string                { return s.userID }
func (s *mockProxySession) Tags() map[string]string       { return s.tags }
func (s *mockProxySession) Status() string                { return s.status }
func (s *mockProxySession) StartedAt() time.Time          { return s.startedAt }
func (s *mockProxySession) UpdatedAt() time.Time          { return s.updatedAt }
func (s *mockProxySession) Description() string           { return "" }
func (s *mockProxySession) Scope() entities.ResourceScope { return entities.ScopeUser }
func (s *mockProxySession) TeamID() string                { return "" }
func (s *mockProxySession) Cancel()                       {}

func newMockProxySessionManager() *mockProxySessionManager {
	return &mockProxySessionManager{sessions: make(map[string]*mockProxySession)}
}

func (m *mockProxySessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	now := time.Now()
	session := &mockProxySession{
		id:        id,
		userID:    req.UserID,
		status:    "active",
		addr:      "localhost:9000",
		tags:      req.Tags,
		startedAt: now,
		updatedAt: now,
	}
	m.sessions[id] = session
	return session, nil
}

func (m *mockProxySessionManager) GetSession(id string) entities.Session {
	s, ok := m.sessions[id]
	if !ok {
		return nil
	}
	return s
}

func (m *mockProxySessionManager) ListSessions(filter entities.SessionFilter) []entities.Session {
	var result []entities.Session
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

func (m *mockProxySessionManager) DeleteSession(id string) error {
	delete(m.sessions, id)
	return nil
}

func (m *mockProxySessionManager) ResumeSession(ctx context.Context, id string) error {
	return nil
}

func (m *mockProxySessionManager) SendMessage(ctx context.Context, id string, message string) error {
	return nil
}

func (m *mockProxySessionManager) GetMessages(ctx context.Context, id string) ([]repositories.Message, error) {
	return nil, nil
}

func (m *mockProxySessionManager) Shutdown(timeout time.Duration) error {
	return nil
}

func TestWorker_StartStop(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start worker
	err := worker.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Stop worker
	worker.Stop()
}

func TestWorker_ProcessSchedules(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create a due schedule
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "test-schedule",
		Name:            "Test Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		ScheduledAt:     &past,
		NextExecutionAt: &past,
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config)

	// Process schedules
	worker.processSchedules(ctx)

	// Verify session was created
	if len(sessionManager.sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessionManager.sessions))
	}

	// Verify execution was recorded
	updated, _ := manager.Get(ctx, "test-schedule")
	if updated.LastExecution == nil {
		t.Error("expected execution to be recorded")
	}
	if updated.LastExecution.Status != "success" {
		t.Errorf("expected status 'success', got %q", updated.LastExecution.Status)
	}
	if updated.ExecutionCount != 1 {
		t.Errorf("expected execution count 1, got %d", updated.ExecutionCount)
	}
}

func TestWorker_SkipActiveSession(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create an active session
	prevSession := &mockProxySession{
		id:        "prev-session",
		userID:    "test-user",
		status:    "active",
		addr:      "localhost:9000",
		startedAt: time.Now(),
	}
	sessionManager.sessions["prev-session"] = prevSession

	// Create a due schedule with previous execution
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "test-schedule",
		Name:            "Test Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		CronExpr:        "* * * * *",
		NextExecutionAt: &past,
		LastExecution: &ExecutionRecord{
			ExecutedAt: past,
			SessionID:  "prev-session",
			Status:     "success",
		},
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config)

	// Process schedules
	worker.processSchedules(ctx)

	// Verify no new session was created (only prev-session exists)
	if len(sessionManager.sessions) != 1 {
		t.Errorf("expected 1 session (only prev), got %d", len(sessionManager.sessions))
	}

	// Verify skipped execution was recorded
	updated, _ := manager.Get(ctx, "test-schedule")
	if updated.LastExecution == nil {
		t.Error("expected execution to be recorded")
	}
	if updated.LastExecution.Status != "skipped" {
		t.Errorf("expected status 'skipped', got %q", updated.LastExecution.Status)
	}
}

func TestWorker_OneTimeScheduleCompleted(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create a one-time schedule
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "one-time-schedule",
		Name:            "One Time Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		ScheduledAt:     &past,
		NextExecutionAt: &past,
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config)

	// Process schedules
	worker.processSchedules(ctx)

	// Verify schedule is marked as completed
	updated, _ := manager.Get(ctx, "one-time-schedule")
	if updated.Status != ScheduleStatusCompleted {
		t.Errorf("expected status %q, got %q", ScheduleStatusCompleted, updated.Status)
	}
}

func TestWorker_RecurringScheduleNextExecution(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create a recurring schedule
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "recurring-schedule",
		Name:            "Recurring Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		CronExpr:        "0 9 * * *", // Daily at 9am
		NextExecutionAt: &past,
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config)

	// Process schedules
	worker.processSchedules(ctx)

	// Verify next execution is updated (should be in the future)
	updated, _ := manager.Get(ctx, "recurring-schedule")
	if updated.NextExecutionAt == nil {
		t.Error("expected next execution time to be set")
	}
	if !updated.NextExecutionAt.After(now) {
		t.Errorf("expected next execution after now, got %v", updated.NextExecutionAt)
	}

	// Status should still be active
	if updated.Status != ScheduleStatusActive {
		t.Errorf("expected status %q, got %q", ScheduleStatusActive, updated.Status)
	}
}

func TestWorker_DeletePreviousInactiveSession(t *testing.T) {
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	sessionManager := newMockProxySessionManager()

	ctx := context.Background()

	// Create a previous session that is NOT active (stopped)
	prevSession := &mockProxySession{
		id:        "prev-session",
		userID:    "test-user",
		status:    "stopped", // Not active
		addr:      "localhost:9000",
		startedAt: time.Now().Add(-time.Hour),
	}
	sessionManager.sessions["prev-session"] = prevSession

	// Create a due schedule with previous execution
	now := time.Now()
	past := now.Add(-time.Hour)
	schedule := &Schedule{
		ID:              "test-schedule",
		Name:            "Test Schedule",
		UserID:          "test-user",
		Status:          ScheduleStatusActive,
		CronExpr:        "* * * * *",
		NextExecutionAt: &past,
		LastExecution: &ExecutionRecord{
			ExecutedAt: past,
			SessionID:  "prev-session",
			Status:     "success",
		},
		SessionConfig: SessionConfig{
			Tags: map[string]string{"test": "true"},
		},
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	config := WorkerConfig{
		CheckInterval: 100 * time.Millisecond,
		Enabled:       true,
	}

	worker := NewWorker(manager, sessionManager, config)

	// Process schedules
	worker.processSchedules(ctx)

	// Verify previous session was deleted
	if _, exists := sessionManager.sessions["prev-session"]; exists {
		t.Error("expected previous session to be deleted")
	}

	// Verify new session was created (should have 1 session, the new one)
	if len(sessionManager.sessions) != 1 {
		t.Errorf("expected 1 session (new one), got %d", len(sessionManager.sessions))
	}

	// Verify execution was recorded as success
	updated, _ := manager.Get(ctx, "test-schedule")
	if updated.LastExecution == nil {
		t.Error("expected execution to be recorded")
	}
	if updated.LastExecution.Status != "success" {
		t.Errorf("expected status 'success', got %q", updated.LastExecution.Status)
	}
}
