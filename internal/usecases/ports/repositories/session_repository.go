package repositories

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// SessionRepository defines the interface for session data persistence
type SessionRepository interface {
	// Save persists a session
	Save(ctx context.Context, session *entities.Session) error

	// FindByID retrieves a session by its ID
	FindByID(ctx context.Context, id entities.SessionID) (*entities.Session, error)

	// FindByUserID retrieves all sessions for a specific user
	FindByUserID(ctx context.Context, userID entities.UserID) ([]*entities.Session, error)

	// FindAll retrieves all sessions
	FindAll(ctx context.Context) ([]*entities.Session, error)

	// FindByStatus retrieves sessions by status
	FindByStatus(ctx context.Context, status entities.SessionStatus) ([]*entities.Session, error)

	// Update updates an existing session
	Update(ctx context.Context, session *entities.Session) error

	// Delete removes a session
	Delete(ctx context.Context, id entities.SessionID) error

	// Count returns the total number of sessions
	Count(ctx context.Context) (int, error)

	// CountByUserID returns the number of sessions for a specific user
	CountByUserID(ctx context.Context, userID entities.UserID) (int, error)

	// FindWithFilter finds sessions based on filter criteria
	FindWithFilter(ctx context.Context, filter *SessionFilter) ([]*entities.Session, int, error)

	// FindActiveSessions finds all active sessions
	FindActiveSessions(ctx context.Context) ([]*entities.Session, error)

	// FindByPort finds a session using a specific port
	FindByPort(ctx context.Context, port entities.Port) (*entities.Session, error)

	// CountByStatus counts sessions by status
	CountByStatus(ctx context.Context, status entities.SessionStatus) (int, error)
}

// SessionFilter represents filter criteria for session queries
type SessionFilter struct {
	UserID     entities.UserID         // Filter by user ID
	Status     *entities.SessionStatus // Filter by status
	Tags       entities.Tags           // Filter by tags
	Repository *entities.Repository    // Filter by repository
	StartDate  *string                 // Filter by start date (ISO format)
	EndDate    *string                 // Filter by end date (ISO format)
	Limit      int                     // Number of results to return (0 = no limit)
	Offset     int                     // Offset for pagination
	SortBy     string                  // Sort field (created_at, status, user_id, port)
	SortOrder  string                  // Sort order (asc, desc)
}
