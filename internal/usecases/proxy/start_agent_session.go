package proxy

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// StartAgentSessionUseCase handles starting new agent sessions
type StartAgentSessionUseCase struct {
	sessionRepo   repositories.SessionRepository
	userRepo      repositories.UserRepository
	agentService  services.AgentService
	notifyService services.NotificationService
}

// NewStartAgentSessionUseCase creates a new StartAgentSessionUseCase
func NewStartAgentSessionUseCase(
	sessionRepo repositories.SessionRepository,
	userRepo repositories.UserRepository,
	agentService services.AgentService,
	notifyService services.NotificationService,
) *StartAgentSessionUseCase {
	return &StartAgentSessionUseCase{
		sessionRepo:   sessionRepo,
		userRepo:      userRepo,
		agentService:  agentService,
		notifyService: notifyService,
	}
}

// StartAgentSessionRequest represents the request to start an agent session
type StartAgentSessionRequest struct {
	UserID      string
	Environment map[string]string
	Tags        map[string]string
	Repository  *entities.Repository
}

// StartAgentSessionResponse represents the response after starting an agent session
type StartAgentSessionResponse struct {
	Session *entities.Session
	Port    int
}

// Execute starts a new agent session
func (u *StartAgentSessionUseCase) Execute(ctx context.Context, req StartAgentSessionRequest) (*StartAgentSessionResponse, error) {
	// Validate user
	user, err := u.userRepo.FindByID(ctx, entities.UserID(req.UserID))
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found: %s", req.UserID)
	}

	// Allocate port
	port, err := u.agentService.AllocatePort(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate port: %w", err)
	}

	// Create session
	sessionID := entities.SessionID(u.generateSessionID())
	session := entities.NewSession(
		sessionID,
		user.ID(),
		entities.Port(port),
		req.Environment,
		req.Tags,
		req.Repository,
	)

	// Validate session
	if err := session.Validate(); err != nil {
		return nil, fmt.Errorf("invalid session: %w", err)
	}

	// Start agent process
	processInfo, err := u.agentService.StartAgent(ctx, port, req.Environment, req.Repository)
	if err != nil {
		return nil, fmt.Errorf("failed to start agent: %w", err)
	}

	// Mark session as started
	if err := session.Start(processInfo); err != nil {
		// Cleanup process if session start fails
		if stopErr := u.agentService.StopAgent(ctx, processInfo.PID()); stopErr != nil {
			// Log cleanup error but continue with original error
			_ = stopErr // Explicitly ignore cleanup error for lint
		}
		return nil, fmt.Errorf("failed to mark session as started: %w", err)
	}

	// Save session
	if err := u.sessionRepo.Save(ctx, session); err != nil {
		// Cleanup process if save fails
		if stopErr := u.agentService.StopAgent(ctx, processInfo.PID()); stopErr != nil {
			// Log cleanup error but continue with original error
			_ = stopErr // Explicitly ignore cleanup error for lint
		}
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	return &StartAgentSessionResponse{
		Session: session,
		Port:    port,
	}, nil
}

// generateSessionID generates a unique session ID
func (u *StartAgentSessionUseCase) generateSessionID() string {
	return uuid.New().String()
}
