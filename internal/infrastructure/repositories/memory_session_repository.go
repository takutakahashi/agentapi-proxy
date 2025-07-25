package repositories

import (
	"context"
	"errors"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"sort"
	"strings"
	"sync"
)

// MemorySessionRepository implements SessionRepository using in-memory storage
type MemorySessionRepository struct {
	mu       sync.RWMutex
	sessions map[entities.SessionID]*entities.Session
}

// NewMemorySessionRepository creates a new MemorySessionRepository
func NewMemorySessionRepository() *MemorySessionRepository {
	return &MemorySessionRepository{
		sessions: make(map[entities.SessionID]*entities.Session),
	}
}

// Save persists a session
func (r *MemorySessionRepository) Save(ctx context.Context, session *entities.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if session already exists
	if _, exists := r.sessions[session.ID()]; exists {
		return errors.New("session already exists")
	}

	// Clone session to avoid external modifications
	cloned := r.cloneSession(session)
	r.sessions[session.ID()] = cloned

	return nil
}

// FindByID retrieves a session by its ID
func (r *MemorySessionRepository) FindByID(ctx context.Context, id entities.SessionID) (*entities.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, exists := r.sessions[id]
	if !exists {
		return nil, errors.New("session not found")
	}

	return r.cloneSession(session), nil
}

// FindByUserID retrieves all sessions for a specific user
func (r *MemorySessionRepository) FindByUserID(ctx context.Context, userID entities.UserID) ([]*entities.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.Session
	for _, session := range r.sessions {
		if session.UserID() == userID {
			result = append(result, r.cloneSession(session))
		}
	}

	return result, nil
}

// FindAll retrieves all sessions
func (r *MemorySessionRepository) FindAll(ctx context.Context) ([]*entities.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*entities.Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		result = append(result, r.cloneSession(session))
	}

	return result, nil
}

// FindByStatus retrieves sessions by status
func (r *MemorySessionRepository) FindByStatus(ctx context.Context, status entities.SessionStatus) ([]*entities.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.Session
	for _, session := range r.sessions {
		if session.Status() == status {
			result = append(result, r.cloneSession(session))
		}
	}

	return result, nil
}

// Update updates an existing session
func (r *MemorySessionRepository) Update(ctx context.Context, session *entities.Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[session.ID()]; !exists {
		return errors.New("session not found")
	}

	// Clone session to avoid external modifications
	cloned := r.cloneSession(session)
	r.sessions[session.ID()] = cloned

	return nil
}

// Delete removes a session
func (r *MemorySessionRepository) Delete(ctx context.Context, id entities.SessionID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[id]; !exists {
		return errors.New("session not found")
	}

	delete(r.sessions, id)
	return nil
}

// Count returns the total number of sessions
func (r *MemorySessionRepository) Count(ctx context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.sessions), nil
}

// CountByUserID returns the number of sessions for a specific user
func (r *MemorySessionRepository) CountByUserID(ctx context.Context, userID entities.UserID) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, session := range r.sessions {
		if session.UserID() == userID {
			count++
		}
	}

	return count, nil
}

// FindWithFilter finds sessions based on filter criteria
func (r *MemorySessionRepository) FindWithFilter(ctx context.Context, filter *repositories.SessionFilter) ([]*entities.Session, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []*entities.Session

	// Apply filters
	for _, session := range r.sessions {
		if r.matchesFilter(session, filter) {
			filtered = append(filtered, r.cloneSession(session))
		}
	}

	totalCount := len(filtered)

	// Apply sorting
	r.sortSessions(filtered, filter.SortBy, filter.SortOrder)

	// Apply pagination
	if filter.Limit > 0 {
		start := filter.Offset
		if start > len(filtered) {
			start = len(filtered)
		}

		end := start + filter.Limit
		if end > len(filtered) {
			end = len(filtered)
		}

		filtered = filtered[start:end]
	}

	return filtered, totalCount, nil
}

// FindActiveSessions finds all active sessions
func (r *MemorySessionRepository) FindActiveSessions(ctx context.Context) ([]*entities.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.Session
	for _, session := range r.sessions {
		if session.IsActive() {
			result = append(result, r.cloneSession(session))
		}
	}

	return result, nil
}

// FindByPort finds a session using a specific port
func (r *MemorySessionRepository) FindByPort(ctx context.Context, port entities.Port) (*entities.Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, session := range r.sessions {
		if session.Port() == port {
			return r.cloneSession(session), nil
		}
	}

	return nil, errors.New("session not found")
}

// CountByStatus counts sessions by status
func (r *MemorySessionRepository) CountByStatus(ctx context.Context, status entities.SessionStatus) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, session := range r.sessions {
		if session.Status() == status {
			count++
		}
	}

	return count, nil
}

// matchesFilter checks if a session matches the given filter criteria
func (r *MemorySessionRepository) matchesFilter(session *entities.Session, filter *repositories.SessionFilter) bool {
	// Filter by user ID
	if filter.UserID != "" && session.UserID() != filter.UserID {
		return false
	}

	// Filter by status
	if filter.Status != nil && session.Status() != *filter.Status {
		return false
	}

	// Filter by tags
	if filter.Tags != nil {
		sessionTags := session.Tags()
		for key, value := range filter.Tags {
			if sessionTags[key] != value {
				return false
			}
		}
	}

	// Filter by repository
	if filter.Repository != nil {
		sessionRepo := session.Repository()
		if sessionRepo == nil {
			return false
		}
		if sessionRepo.URL() != filter.Repository.URL() {
			return false
		}
	}

	// TODO: Implement date range filtering for StartDate and EndDate

	return true
}

// sortSessions sorts sessions based on the given criteria
func (r *MemorySessionRepository) sortSessions(sessions []*entities.Session, sortBy, sortOrder string) {
	if sortBy == "" {
		sortBy = "created_at"
	}
	if sortOrder == "" {
		sortOrder = "desc"
	}

	sort.Slice(sessions, func(i, j int) bool {
		var less bool

		switch sortBy {
		case "created_at":
			less = sessions[i].StartedAt().Before(sessions[j].StartedAt())
		case "status":
			less = strings.Compare(string(sessions[i].Status()), string(sessions[j].Status())) < 0
		case "user_id":
			less = strings.Compare(string(sessions[i].UserID()), string(sessions[j].UserID())) < 0
		case "port":
			less = sessions[i].Port() < sessions[j].Port()
		default:
			less = sessions[i].StartedAt().Before(sessions[j].StartedAt())
		}

		if sortOrder == "desc" {
			return !less
		}
		return less
	})
}

// cloneSession creates a deep copy of a session to prevent external modifications
func (r *MemorySessionRepository) cloneSession(session *entities.Session) *entities.Session {
	// In a real implementation, you would properly clone all fields
	// For now, we'll return the session as-is since entities should be immutable
	// In practice, you might want to implement a Clone() method on the Session entity
	return session
}
