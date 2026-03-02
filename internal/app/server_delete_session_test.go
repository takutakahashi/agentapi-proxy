package app

import (
	"context"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// --- Mock session ---

type mockSession struct {
	id                  string
	userID              string
	scope               entities.ResourceScope
	teamID              string
	memoryKey           map[string]string
	memorySummarizeDrft *bool
}

func (s *mockSession) ID() string                    { return s.id }
func (s *mockSession) Addr() string                  { return "" }
func (s *mockSession) UserID() string                { return s.userID }
func (s *mockSession) Scope() entities.ResourceScope { return s.scope }
func (s *mockSession) TeamID() string                { return s.teamID }
func (s *mockSession) Tags() map[string]string       { return nil }
func (s *mockSession) Status() string                { return "active" }
func (s *mockSession) StartedAt() time.Time          { return time.Time{} }
func (s *mockSession) UpdatedAt() time.Time          { return time.Time{} }
func (s *mockSession) Description() string           { return "" }
func (s *mockSession) Cancel()                       {}

// memoryConfigProvider implementation
func (s *mockSession) MemoryKey() map[string]string { return s.memoryKey }
func (s *mockSession) MemorySummarizeDrafts() *bool { return s.memorySummarizeDrft }

// --- Mock session manager ---

type mockDeleteSessionManager struct {
	session   entities.Session
	deletedID string
}

func (m *mockDeleteSessionManager) CreateSession(_ context.Context, id string, _ *entities.RunServerRequest, _ []byte) (entities.Session, error) {
	return nil, nil
}
func (m *mockDeleteSessionManager) GetSession(id string) entities.Session {
	if m.session != nil && m.session.ID() == id {
		return m.session
	}
	return nil
}
func (m *mockDeleteSessionManager) ListSessions(_ entities.SessionFilter) []entities.Session {
	return nil
}
func (m *mockDeleteSessionManager) SendMessage(_ context.Context, _ string, _ string) error {
	return nil
}
func (m *mockDeleteSessionManager) StopAgent(_ context.Context, _ string) error { return nil }
func (m *mockDeleteSessionManager) GetMessages(_ context.Context, _ string) ([]portrepos.Message, error) {
	return nil, nil
}
func (m *mockDeleteSessionManager) UpdateSlackLastMessageAt(_ string, _ time.Time) error { return nil }
func (m *mockDeleteSessionManager) Shutdown(_ time.Duration) error                       { return nil }
func (m *mockDeleteSessionManager) DeleteSession(id string) error {
	m.deletedID = id
	return nil
}

// --- Mock memory repository ---

type mockMemoryRepo struct {
	memories []*entities.Memory
	deleted  []string
}

func (r *mockMemoryRepo) Create(_ context.Context, mem *entities.Memory) error {
	r.memories = append(r.memories, mem)
	return nil
}

func (r *mockMemoryRepo) GetByID(_ context.Context, id string) (*entities.Memory, error) {
	for _, m := range r.memories {
		if m.ID() == id {
			return m, nil
		}
	}
	return nil, entities.ErrMemoryNotFound{ID: id}
}

func (r *mockMemoryRepo) List(_ context.Context, filter portrepos.MemoryFilter) ([]*entities.Memory, error) {
	var result []*entities.Memory
	for _, m := range r.memories {
		if m.MatchesTags(filter.Tags) {
			result = append(result, m)
		}
	}
	return result, nil
}

func (r *mockMemoryRepo) Update(_ context.Context, mem *entities.Memory) error {
	for i, m := range r.memories {
		if m.ID() == mem.ID() {
			r.memories[i] = mem
			return nil
		}
	}
	return entities.ErrMemoryNotFound{ID: mem.ID()}
}

func (r *mockMemoryRepo) Delete(_ context.Context, id string) error {
	r.deleted = append(r.deleted, id)
	for i, m := range r.memories {
		if m.ID() == id {
			r.memories = append(r.memories[:i], r.memories[i+1:]...)
			return nil
		}
	}
	return nil
}

// --- Helpers ---

func boolPtr(b bool) *bool { return &b }

func newServerForDeleteTest(session entities.Session, memRepo portrepos.MemoryRepository) *Server {
	return &Server{
		sessionManager: &mockDeleteSessionManager{session: session},
		memoryRepo:     memRepo,
	}
}

// --- Tests ---

func TestDeleteSessionByID_DeletesDraftMemories_WhenSummarizeDisabled(t *testing.T) {
	sessionID := "test-session-123"

	// Session with memory key set and summarize disabled (nil = false by default)
	sess := &mockSession{
		id:                  sessionID,
		userID:              "user1",
		scope:               entities.ScopeUser,
		memoryKey:           map[string]string{"project": "myproject"},
		memorySummarizeDrft: nil, // nil means disabled
	}

	repo := &mockMemoryRepo{}
	// Add a draft memory for this session
	draft := entities.NewMemoryWithTags(
		"draft-mem-1",
		"Draft: Session "+sessionID,
		"conversation content",
		entities.ScopeUser,
		"user1",
		"",
		map[string]string{
			"draft":      "true",
			"session-id": sessionID,
		},
	)
	_ = repo.Create(context.Background(), draft)

	// Add a non-draft memory (should NOT be deleted)
	mainMem := entities.NewMemoryWithTags(
		"main-mem-1",
		"Main memory",
		"important content",
		entities.ScopeUser,
		"user1",
		"",
		map[string]string{
			"project": "myproject",
		},
	)
	_ = repo.Create(context.Background(), mainMem)

	s := newServerForDeleteTest(sess, repo)

	if err := s.DeleteSessionByID(sessionID); err != nil {
		t.Fatalf("DeleteSessionByID returned error: %v", err)
	}

	// Draft memory should be deleted
	if len(repo.deleted) != 1 {
		t.Errorf("Expected 1 deleted memory, got %d: %v", len(repo.deleted), repo.deleted)
	}
	if len(repo.deleted) > 0 && repo.deleted[0] != "draft-mem-1" {
		t.Errorf("Expected draft-mem-1 to be deleted, got %s", repo.deleted[0])
	}

	// Main memory should remain
	if len(repo.memories) != 1 || repo.memories[0].ID() != "main-mem-1" {
		t.Errorf("Expected main memory to remain, got %v", repo.memories)
	}
}

func TestDeleteSessionByID_DeletesDraftMemories_WhenSummarizeExplicitlyFalse(t *testing.T) {
	sessionID := "test-session-456"

	sess := &mockSession{
		id:                  sessionID,
		userID:              "user2",
		scope:               entities.ScopeUser,
		memoryKey:           map[string]string{"repo": "myrepo"},
		memorySummarizeDrft: boolPtr(false),
	}

	repo := &mockMemoryRepo{}
	draft := entities.NewMemoryWithTags(
		"draft-mem-2",
		"Draft: Session "+sessionID,
		"content",
		entities.ScopeUser,
		"user2",
		"",
		map[string]string{"draft": "true", "session-id": sessionID},
	)
	_ = repo.Create(context.Background(), draft)

	s := newServerForDeleteTest(sess, repo)

	if err := s.DeleteSessionByID(sessionID); err != nil {
		t.Fatalf("DeleteSessionByID returned error: %v", err)
	}

	if len(repo.deleted) != 1 || repo.deleted[0] != "draft-mem-2" {
		t.Errorf("Expected draft-mem-2 to be deleted, got %v", repo.deleted)
	}
}

func TestDeleteSessionByID_DoesNotDeleteDrafts_WhenSummarizeEnabled(t *testing.T) {
	sessionID := "test-session-789"

	sess := &mockSession{
		id:                  sessionID,
		userID:              "user3",
		scope:               entities.ScopeUser,
		memoryKey:           map[string]string{"env": "prod"},
		memorySummarizeDrft: boolPtr(true), // enabled: sidecar handles it
	}

	repo := &mockMemoryRepo{}
	draft := entities.NewMemoryWithTags(
		"draft-mem-3",
		"Draft: Session "+sessionID,
		"content",
		entities.ScopeUser,
		"user3",
		"",
		map[string]string{"draft": "true", "session-id": sessionID},
	)
	_ = repo.Create(context.Background(), draft)

	s := newServerForDeleteTest(sess, repo)

	if err := s.DeleteSessionByID(sessionID); err != nil {
		t.Fatalf("DeleteSessionByID returned error: %v", err)
	}

	// Draft should NOT be deleted (summarization is handled by the sidecar)
	if len(repo.deleted) != 0 {
		t.Errorf("Expected no deletions, got %v", repo.deleted)
	}
}

func TestDeleteSessionByID_DoesNotDeleteDrafts_WhenMemoryKeyEmpty(t *testing.T) {
	sessionID := "test-session-000"

	sess := &mockSession{
		id:                  sessionID,
		userID:              "user4",
		scope:               entities.ScopeUser,
		memoryKey:           nil, // No memory key = no sidecar
		memorySummarizeDrft: nil,
	}

	repo := &mockMemoryRepo{}
	// Even if somehow there are drafts with this session-id, they should not be deleted
	draft := entities.NewMemoryWithTags(
		"draft-mem-4",
		"Draft: Session "+sessionID,
		"content",
		entities.ScopeUser,
		"user4",
		"",
		map[string]string{"draft": "true", "session-id": sessionID},
	)
	_ = repo.Create(context.Background(), draft)

	s := newServerForDeleteTest(sess, repo)

	if err := s.DeleteSessionByID(sessionID); err != nil {
		t.Fatalf("DeleteSessionByID returned error: %v", err)
	}

	// No deletions since memory key is empty
	if len(repo.deleted) != 0 {
		t.Errorf("Expected no deletions when memory key is empty, got %v", repo.deleted)
	}
}

func TestDeleteSessionByID_DeletesMultipleDraftMemories(t *testing.T) {
	sessionID := "test-session-multi"

	sess := &mockSession{
		id:                  sessionID,
		userID:              "user5",
		scope:               entities.ScopeUser,
		memoryKey:           map[string]string{"tag": "value"},
		memorySummarizeDrft: nil,
	}

	repo := &mockMemoryRepo{}
	for i, id := range []string{"draft-a", "draft-b", "draft-c"} {
		draft := entities.NewMemoryWithTags(
			id,
			"Draft "+string(rune('A'+i)),
			"content",
			entities.ScopeUser,
			"user5",
			"",
			map[string]string{"draft": "true", "session-id": sessionID},
		)
		_ = repo.Create(context.Background(), draft)
	}

	s := newServerForDeleteTest(sess, repo)

	if err := s.DeleteSessionByID(sessionID); err != nil {
		t.Fatalf("DeleteSessionByID returned error: %v", err)
	}

	if len(repo.deleted) != 3 {
		t.Errorf("Expected 3 draft deletions, got %d: %v", len(repo.deleted), repo.deleted)
	}
}

func TestDeleteSessionByID_TeamScope_DeletesDraftMemories(t *testing.T) {
	sessionID := "test-session-team"

	sess := &mockSession{
		id:                  sessionID,
		userID:              "user6",
		scope:               entities.ScopeTeam,
		teamID:              "myorg/myteam",
		memoryKey:           map[string]string{"project": "teamproject"},
		memorySummarizeDrft: nil,
	}

	repo := &mockMemoryRepo{}
	draft := entities.NewMemoryWithTags(
		"team-draft-1",
		"Draft: Session "+sessionID,
		"content",
		entities.ScopeTeam,
		"user6",
		"myorg/myteam",
		map[string]string{"draft": "true", "session-id": sessionID},
	)
	_ = repo.Create(context.Background(), draft)

	s := newServerForDeleteTest(sess, repo)

	if err := s.DeleteSessionByID(sessionID); err != nil {
		t.Fatalf("DeleteSessionByID returned error: %v", err)
	}

	if len(repo.deleted) != 1 || repo.deleted[0] != "team-draft-1" {
		t.Errorf("Expected team-draft-1 to be deleted, got %v", repo.deleted)
	}
}
