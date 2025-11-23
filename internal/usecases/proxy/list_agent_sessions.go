package proxy

import (
	"context"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// ListAgentSessionsUseCase handles listing agent sessions
type ListAgentSessionsUseCase struct {
	sessionRepo repositories.SessionRepository
}

// NewListAgentSessionsUseCase creates a new ListAgentSessionsUseCase
func NewListAgentSessionsUseCase(sessionRepo repositories.SessionRepository) *ListAgentSessionsUseCase {
	return &ListAgentSessionsUseCase{
		sessionRepo: sessionRepo,
	}
}

// ListAgentSessionsRequest represents the request to list agent sessions
type ListAgentSessionsRequest struct {
	UserID string
}

// ListAgentSessionsResponse represents the response with agent sessions
type ListAgentSessionsResponse struct {
	Sessions []*entities.Session
}

// Execute lists agent sessions for a user
func (u *ListAgentSessionsUseCase) Execute(ctx context.Context, req ListAgentSessionsRequest) (*ListAgentSessionsResponse, error) {
	sessions, err := u.sessionRepo.FindByUserID(ctx, entities.UserID(req.UserID))
	if err != nil {
		return nil, err
	}

	return &ListAgentSessionsResponse{
		Sessions: sessions,
	}, nil
}
