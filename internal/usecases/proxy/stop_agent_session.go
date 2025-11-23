package proxy

import (
	"context"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// StopAgentSessionUseCase handles stopping agent sessions
type StopAgentSessionUseCase struct {
	sessionRepo   repositories.SessionRepository
	agentService  services.AgentService
	notifyService services.NotificationService
}

// NewStopAgentSessionUseCase creates a new StopAgentSessionUseCase
func NewStopAgentSessionUseCase(
	sessionRepo repositories.SessionRepository,
	agentService services.AgentService,
	notifyService services.NotificationService,
) *StopAgentSessionUseCase {
	return &StopAgentSessionUseCase{
		sessionRepo:   sessionRepo,
		agentService:  agentService,
		notifyService: notifyService,
	}
}

// StopAgentSessionRequest represents the request to stop an agent session
type StopAgentSessionRequest struct {
	SessionID string
	UserID    string
}

// Execute stops an agent session
func (u *StopAgentSessionUseCase) Execute(ctx context.Context, req StopAgentSessionRequest) error {
	// Get session
	session, err := u.sessionRepo.FindByID(ctx, entities.SessionID(req.SessionID))
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if session == nil {
		return fmt.Errorf("session not found: %s", req.SessionID)
	}

	// Check ownership
	if string(session.UserID()) != req.UserID {
		return fmt.Errorf("session does not belong to user")
	}

	// Check if session can be terminated
	if !session.CanBeTerminated() {
		return fmt.Errorf("session cannot be terminated from status: %s", session.Status())
	}

	// Terminate session
	if err := session.Terminate(); err != nil {
		return fmt.Errorf("failed to terminate session: %w", err)
	}

	// Stop agent process if running
	if processInfo := session.ProcessInfo(); processInfo != nil {
		if err := u.agentService.StopAgent(ctx, processInfo.PID()); err != nil {
			// Log error but don't fail the operation
			// The process might already be dead
		}
	}

	// Mark session as stopped
	if err := session.MarkStopped(); err != nil {
		return fmt.Errorf("failed to mark session as stopped: %w", err)
	}

	// Save session
	if err := u.sessionRepo.Save(ctx, session); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	return nil
}
