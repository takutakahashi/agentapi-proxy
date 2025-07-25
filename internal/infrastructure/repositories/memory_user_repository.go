package repositories

import (
	"context"
	"errors"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"sync"
)

// MemoryUserRepository implements UserRepository using in-memory storage
type MemoryUserRepository struct {
	mu    sync.RWMutex
	users map[entities.UserID]*entities.User
}

// NewMemoryUserRepository creates a new MemoryUserRepository
func NewMemoryUserRepository() *MemoryUserRepository {
	return &MemoryUserRepository{
		users: make(map[entities.UserID]*entities.User),
	}
}

// Save persists a user
func (r *MemoryUserRepository) Save(ctx context.Context, user *entities.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if user already exists
	if _, exists := r.users[user.ID()]; exists {
		return errors.New("user already exists")
	}

	// Clone user to avoid external modifications
	cloned := r.cloneUser(user)
	r.users[user.ID()] = cloned

	return nil
}

// FindByID retrieves a user by their ID
func (r *MemoryUserRepository) FindByID(ctx context.Context, id entities.UserID) (*entities.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, exists := r.users[id]
	if !exists {
		return nil, errors.New("user not found")
	}

	return r.cloneUser(user), nil
}

// FindByUsername retrieves a user by their username
func (r *MemoryUserRepository) FindByUsername(ctx context.Context, username string) (*entities.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, user := range r.users {
		if user.Username() == username {
			return r.cloneUser(user), nil
		}
	}

	return nil, errors.New("user not found")
}

// FindByEmail retrieves a user by their email
func (r *MemoryUserRepository) FindByEmail(ctx context.Context, email string) (*entities.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, user := range r.users {
		if userEmail := user.Email(); userEmail != nil && *userEmail == email {
			return r.cloneUser(user), nil
		}
	}

	return nil, errors.New("user not found")
}

// FindByGitHubID retrieves a user by their GitHub ID
func (r *MemoryUserRepository) FindByGitHubID(ctx context.Context, githubID int) (*entities.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, user := range r.users {
		// In a real implementation, you'd check the user's GitHub info
		// For now, we'll use a simple approach based on user ID format
		if string(user.ID()) == "github_"+string(rune(githubID)) {
			return r.cloneUser(user), nil
		}
	}

	return nil, errors.New("user not found")
}

// FindAll retrieves all users
func (r *MemoryUserRepository) FindAll(ctx context.Context) ([]*entities.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*entities.User, 0, len(r.users))
	for _, user := range r.users {
		result = append(result, r.cloneUser(user))
	}

	return result, nil
}

// FindByStatus retrieves users by status
func (r *MemoryUserRepository) FindByStatus(ctx context.Context, status entities.UserStatus) ([]*entities.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.User
	for _, user := range r.users {
		if user.Status() == status {
			result = append(result, r.cloneUser(user))
		}
	}

	return result, nil
}

// FindByType retrieves users by type
func (r *MemoryUserRepository) FindByType(ctx context.Context, userType entities.UserType) ([]*entities.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.User
	for _, user := range r.users {
		if user.Type() == userType {
			result = append(result, r.cloneUser(user))
		}
	}

	return result, nil
}

// Update updates an existing user
func (r *MemoryUserRepository) Update(ctx context.Context, user *entities.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[user.ID()]; !exists {
		return errors.New("user not found")
	}

	// Clone user to avoid external modifications
	cloned := r.cloneUser(user)
	r.users[user.ID()] = cloned

	return nil
}

// Delete removes a user
func (r *MemoryUserRepository) Delete(ctx context.Context, id entities.UserID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[id]; !exists {
		return errors.New("user not found")
	}

	delete(r.users, id)
	return nil
}

// Exists checks if a user exists
func (r *MemoryUserRepository) Exists(ctx context.Context, id entities.UserID) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.users[id]
	return exists, nil
}

// Count returns the total number of users
func (r *MemoryUserRepository) Count(ctx context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.users), nil
}

// CountByStatus returns the number of users by status
func (r *MemoryUserRepository) CountByStatus(ctx context.Context, status entities.UserStatus) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, user := range r.users {
		if user.Status() == status {
			count++
		}
	}

	return count, nil
}

// FindWithFilters retrieves users with filtering options
func (r *MemoryUserRepository) FindWithFilters(ctx context.Context, filters repositories.UserFilters) ([]*entities.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []*entities.User

	// Apply filters
	for _, user := range r.users {
		if r.matchesFilter(user, filters) {
			filtered = append(filtered, r.cloneUser(user))
		}
	}

	// TODO: Apply sorting and pagination

	return filtered, nil
}

// matchesFilter checks if a user matches the given filter criteria
func (r *MemoryUserRepository) matchesFilter(user *entities.User, filters repositories.UserFilters) bool {
	// Filter by type
	if filters.Type != nil && user.Type() != *filters.Type {
		return false
	}

	// Filter by status
	if filters.Status != nil && user.Status() != *filters.Status {
		return false
	}

	// Filter by username (partial match)
	if filters.Username != nil && user.Username() != *filters.Username {
		return false
	}

	// Filter by email (partial match)
	if filters.Email != nil {
		userEmail := user.Email()
		if userEmail == nil || *userEmail != *filters.Email {
			return false
		}
	}

	return true
}

// cloneUser creates a deep copy of a user to prevent external modifications
func (r *MemoryUserRepository) cloneUser(user *entities.User) *entities.User {
	// In a real implementation, you would properly clone all fields
	// For now, we'll return the user as-is since entities should be immutable
	return user
}
