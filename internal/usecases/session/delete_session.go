package session

import (
	"context"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// DeleteSessionUseCase handles the deletion of sessions
type DeleteSessionUseCase struct {
	sessionRepo  repositories.SessionRepository
	userRepo     repositories.UserRepository
	agentService services.AgentService
}

// NewDeleteSessionUseCase creates a new DeleteSessionUseCase
func NewDeleteSessionUseCase(
	sessionRepo repositories.SessionRepository,
	userRepo repositories.UserRepository,
	agentService services.AgentService,
) *DeleteSessionUseCase {
	return &DeleteSessionUseCase{
		sessionRepo:  sessionRepo,
		userRepo:     userRepo,
		agentService: agentService,
	}
}

// DeleteSessionRequest represents the input for deleting a session
type DeleteSessionRequest struct {
	SessionID entities.SessionID
	UserID    entities.UserID // For authorization
	Force     bool            // Force termination even if graceful shutdown fails
}

// DeleteSessionResponse represents the output of deleting a session
type DeleteSessionResponse struct {
	SessionID entities.SessionID
	Success   bool
	Message   string
}

// Execute deletes a session
func (uc *DeleteSessionUseCase) Execute(ctx context.Context, req *DeleteSessionRequest) (*DeleteSessionResponse, error) {
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

	// Check if session can be terminated
	if !session.CanBeTerminated() {
		return &DeleteSessionResponse{
			SessionID: req.SessionID,
			Success:   false,
			Message:   fmt.Sprintf("session cannot be terminated from status: %s", session.Status()),
		}, nil
	}

	var terminationError error

	// If session is active, terminate the agent process
	if session.IsActive() && session.ProcessInfo() != nil {
		// Mark session as terminating
		if err := session.Terminate(); err != nil {
			return nil, fmt.Errorf("failed to mark session as terminating: %w", err)
		}

		// Update session status
		if err := uc.sessionRepo.Update(ctx, session); err != nil {
			return nil, fmt.Errorf("failed to update session status: %w", err)
		}

		// Try graceful shutdown first
		err = uc.agentService.StopAgent(ctx, session.ProcessInfo())
		if err != nil {
			terminationError = err

			// If force is enabled, try to kill the process
			if req.Force {
				killErr := uc.agentService.KillProcess(ctx, session.ProcessInfo())
				if killErr != nil {
					terminationError = fmt.Errorf("graceful stop failed: %w, kill also failed: %w", err, killErr)
				} else {
					terminationError = nil // Kill succeeded
				}
			}
		}
	}

	// Mark session as stopped if termination was successful
	if terminationError == nil {
		if err := session.MarkStopped(); err != nil {
			return nil, fmt.Errorf("failed to mark session as stopped: %w", err)
		}

		// Update session
		if err := uc.sessionRepo.Update(ctx, session); err != nil {
			return nil, fmt.Errorf("failed to update session: %w", err)
		}
	} else {
		// Mark session as failed if termination failed
		session.MarkFailed(terminationError.Error())
		if err := uc.sessionRepo.Update(ctx, session); err != nil {
			return nil, fmt.Errorf("failed to update session after termination failure: %w", err)
		}

		return &DeleteSessionResponse{
			SessionID: req.SessionID,
			Success:   false,
			Message:   fmt.Sprintf("failed to terminate session: %v", terminationError),
		}, nil
	}

	// Optional: Remove session from repository (or keep for audit purposes)
	// For now, we'll keep the session in stopped state for audit trail
	// If you want to physically delete: uc.sessionRepo.Delete(ctx, req.SessionID)

	return &DeleteSessionResponse{
		SessionID: req.SessionID,
		Success:   true,
		Message:   "session terminated successfully",
	}, nil
}

// validateRequest validates the delete session request
func (uc *DeleteSessionUseCase) validateRequest(req *DeleteSessionRequest) error {
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

// checkAuthorization checks if the user is authorized to delete the session
func (uc *DeleteSessionUseCase) checkAuthorization(ctx context.Context, session *entities.Session, userID entities.UserID) error {
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
		return errors.New("user does not have permission to delete this session")
	}

	return nil
}

// ForceDeleteSessionUseCase handles force deletion of sessions (admin only)
type ForceDeleteSessionUseCase struct {
	sessionRepo  repositories.SessionRepository
	userRepo     repositories.UserRepository
	agentService services.AgentService
}

// NewForceDeleteSessionUseCase creates a new ForceDeleteSessionUseCase
func NewForceDeleteSessionUseCase(
	sessionRepo repositories.SessionRepository,
	userRepo repositories.UserRepository,
	agentService services.AgentService,
) *ForceDeleteSessionUseCase {
	return &ForceDeleteSessionUseCase{
		sessionRepo:  sessionRepo,
		userRepo:     userRepo,
		agentService: agentService,
	}
}

// Execute forcefully deletes a session (admin only)
func (uc *ForceDeleteSessionUseCase) Execute(ctx context.Context, req *DeleteSessionRequest) (*DeleteSessionResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Verify admin authorization
	user, err := uc.userRepo.FindByID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	if !user.IsAdmin() {
		return nil, errors.New("only administrators can force delete sessions")
	}

	// Get the session
	session, err := uc.sessionRepo.FindByID(ctx, req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to find session: %w", err)
	}

	// Force terminate if needed
	if session.ProcessInfo() != nil {
		_ = uc.agentService.KillProcess(ctx, session.ProcessInfo())
	}

	// Delete session from repository
	if err := uc.sessionRepo.Delete(ctx, req.SessionID); err != nil {
		return nil, fmt.Errorf("failed to delete session: %w", err)
	}

	return &DeleteSessionResponse{
		SessionID: req.SessionID,
		Success:   true,
		Message:   "session force deleted successfully",
	}, nil
}

// validateRequest validates the force delete session request
func (uc *ForceDeleteSessionUseCase) validateRequest(req *DeleteSessionRequest) error {
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
