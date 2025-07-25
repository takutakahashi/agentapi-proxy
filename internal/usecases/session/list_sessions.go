package session

import (
	"context"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// ListSessionsUseCase handles the listing and searching of sessions
type ListSessionsUseCase struct {
	sessionRepo repositories.SessionRepository
	userRepo    repositories.UserRepository
}

// NewListSessionsUseCase creates a new ListSessionsUseCase
func NewListSessionsUseCase(
	sessionRepo repositories.SessionRepository,
	userRepo repositories.UserRepository,
) *ListSessionsUseCase {
	return &ListSessionsUseCase{
		sessionRepo: sessionRepo,
		userRepo:    userRepo,
	}
}

// ListSessionsRequest represents the input for listing sessions
type ListSessionsRequest struct {
	UserID     entities.UserID
	Status     *entities.SessionStatus // Filter by status (optional)
	Tags       entities.Tags           // Filter by tags (optional)
	Repository *entities.Repository    // Filter by repository (optional)
	Limit      int                     // Number of sessions to return (0 = no limit)
	Offset     int                     // Offset for pagination
	SortBy     SortField               // Sort field
	SortOrder  SortOrder               // Sort order
}

// ListSessionsResponse represents the output of listing sessions
type ListSessionsResponse struct {
	Sessions   []*entities.Session
	TotalCount int
	HasMore    bool
}

// SortField represents the field to sort by
type SortField string

const (
	SortFieldCreatedAt SortField = "created_at"
	SortFieldStatus    SortField = "status"
	SortFieldUserID    SortField = "user_id"
	SortFieldPort      SortField = "port"
)

// SortOrder represents the sort order
type SortOrder string

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

// Execute lists sessions based on the request criteria
func (uc *ListSessionsUseCase) Execute(ctx context.Context, req *ListSessionsRequest) (*ListSessionsResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Verify user exists and is authorized
	user, err := uc.userRepo.FindByID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}
	if !user.IsActive() {
		return nil, errors.New("user is not active")
	}

	// Build query filter
	filter := &repositories.SessionFilter{
		UserID:     req.UserID,
		Status:     req.Status,
		Tags:       req.Tags,
		Repository: req.Repository,
		Limit:      req.Limit,
		Offset:     req.Offset,
		SortBy:     string(req.SortBy),
		SortOrder:  string(req.SortOrder),
	}

	// If user is not admin, restrict to their own sessions
	if !user.IsAdmin() {
		filter.UserID = req.UserID
	} else if filter.UserID == "" {
		// Admin can see all sessions if no specific user requested
		filter.UserID = ""
	}

	// Get sessions from repository
	sessions, totalCount, err := uc.sessionRepo.FindWithFilter(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve sessions: %w", err)
	}

	// Calculate if there are more sessions
	hasMore := false
	if req.Limit > 0 {
		hasMore = totalCount > req.Offset+len(sessions)
	}

	// Update user's last used timestamp
	user.UpdateLastUsed()
	_ = uc.userRepo.Update(ctx, user)

	return &ListSessionsResponse{
		Sessions:   sessions,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// validateRequest validates the list sessions request
func (uc *ListSessionsUseCase) validateRequest(req *ListSessionsRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.UserID == "" {
		return errors.New("user ID cannot be empty")
	}

	if req.Limit < 0 {
		return errors.New("limit cannot be negative")
	}

	if req.Offset < 0 {
		return errors.New("offset cannot be negative")
	}

	// Validate sort field
	validSortFields := map[SortField]bool{
		SortFieldCreatedAt: true,
		SortFieldStatus:    true,
		SortFieldUserID:    true,
		SortFieldPort:      true,
	}
	if req.SortBy != "" && !validSortFields[req.SortBy] {
		return fmt.Errorf("invalid sort field: %s", req.SortBy)
	}

	// Validate sort order
	validSortOrders := map[SortOrder]bool{
		SortOrderAsc:  true,
		SortOrderDesc: true,
	}
	if req.SortOrder != "" && !validSortOrders[req.SortOrder] {
		return fmt.Errorf("invalid sort order: %s", req.SortOrder)
	}

	// Set defaults
	if req.SortBy == "" {
		req.SortBy = SortFieldCreatedAt
	}
	if req.SortOrder == "" {
		req.SortOrder = SortOrderDesc
	}

	return nil
}

// GetSessionByIDUseCase handles retrieving a specific session by ID
type GetSessionByIDUseCase struct {
	sessionRepo repositories.SessionRepository
	userRepo    repositories.UserRepository
}

// NewGetSessionByIDUseCase creates a new GetSessionByIDUseCase
func NewGetSessionByIDUseCase(
	sessionRepo repositories.SessionRepository,
	userRepo repositories.UserRepository,
) *GetSessionByIDUseCase {
	return &GetSessionByIDUseCase{
		sessionRepo: sessionRepo,
		userRepo:    userRepo,
	}
}

// GetSessionByIDRequest represents the input for getting a session by ID
type GetSessionByIDRequest struct {
	SessionID entities.SessionID
	UserID    entities.UserID // For authorization
}

// GetSessionByIDResponse represents the output of getting a session by ID
type GetSessionByIDResponse struct {
	Session *entities.Session
}

// Execute retrieves a session by ID
func (uc *GetSessionByIDUseCase) Execute(ctx context.Context, req *GetSessionByIDRequest) (*GetSessionByIDResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Get the session
	session, err := uc.sessionRepo.FindByID(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to find session: %w", err)
	}

	// Verify user authorization
	if err := uc.checkAuthorization(ctx, session, req.UserID); err != nil {
		return nil, fmt.Errorf("authorization failed: %w", err)
	}

	return &GetSessionByIDResponse{
		Session: session,
	}, nil
}

// validateRequest validates the get session by ID request
func (uc *GetSessionByIDUseCase) validateRequest(req *GetSessionByIDRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.SessionID == "" {
		return errors.New("session ID cannot be empty")
	}

	if req.UserID == "" {
		return errors.New("user ID cannot be empty")
	}

	return nil
}

// checkAuthorization checks if the user is authorized to access the session
func (uc *GetSessionByIDUseCase) checkAuthorization(ctx context.Context, session *entities.Session, userID entities.UserID) error {
	// Get the user
	user, err := uc.userRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to find user: %w", err)
	}

	if !user.IsActive() {
		return errors.New("user is not active")
	}

	// Check if user can access the session
	if !user.CanAccessSession(session.UserID()) {
		return errors.New("user does not have permission to access this session")
	}

	return nil
}
