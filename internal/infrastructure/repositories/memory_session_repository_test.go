package repositories

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"testing"
)

func TestMemorySessionRepository_Save(t *testing.T) {
	repo := NewMemorySessionRepository()
	ctx := context.Background()

	// Create test session
	session := entities.NewSession(
		entities.SessionID("test_session"),
		entities.UserID("test_user"),
		entities.Port(8080),
		entities.Environment{"TEST": "true"},
		entities.Tags{"test": "session"},
		nil,
	)

	// Save session
	err := repo.Save(ctx, session)
	if err != nil {
		t.Fatal("Failed to save session:", err)
	}

	// Retrieve session
	retrieved, err := repo.FindByID(ctx, session.ID())
	if err != nil {
		t.Fatal("Failed to find session:", err)
	}

	if retrieved.ID() != session.ID() {
		t.Errorf("Expected session ID %s, got %s", session.ID(), retrieved.ID())
	}

	if retrieved.UserID() != session.UserID() {
		t.Errorf("Expected user ID %s, got %s", session.UserID(), retrieved.UserID())
	}

	if retrieved.Port() != session.Port() {
		t.Errorf("Expected port %d, got %d", session.Port(), retrieved.Port())
	}
}

func TestMemorySessionRepository_FindByUserID(t *testing.T) {
	repo := NewMemorySessionRepository()
	ctx := context.Background()

	userID := entities.UserID("test_user")

	// Create test sessions
	session1 := entities.NewSession(
		entities.SessionID("session1"),
		userID,
		entities.Port(8080),
		entities.Environment{},
		entities.Tags{},
		nil,
	)

	session2 := entities.NewSession(
		entities.SessionID("session2"),
		userID,
		entities.Port(8081),
		entities.Environment{},
		entities.Tags{},
		nil,
	)

	session3 := entities.NewSession(
		entities.SessionID("session3"),
		entities.UserID("other_user"),
		entities.Port(8082),
		entities.Environment{},
		entities.Tags{},
		nil,
	)

	// Save sessions
	_ = repo.Save(ctx, session1)
	_ = repo.Save(ctx, session2)
	_ = repo.Save(ctx, session3)

	// Find sessions by user ID
	sessions, err := repo.FindByUserID(ctx, userID)
	if err != nil {
		t.Fatal("Failed to find sessions by user ID:", err)
	}

	if len(sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(sessions))
	}

	// Verify all sessions belong to the user
	for _, session := range sessions {
		if session.UserID() != userID {
			t.Errorf("Session %s should belong to user %s, but belongs to %s",
				session.ID(), userID, session.UserID())
		}
	}
}
