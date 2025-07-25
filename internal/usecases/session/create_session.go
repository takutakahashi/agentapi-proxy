package session

import (
	"context"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// CreateSessionUseCase handles the creation of new sessions
type CreateSessionUseCase struct {
	sessionRepo  repositories.SessionRepository
	userRepo     repositories.UserRepository
	agentService services.AgentService
	proxyService services.ProxyService
}

// NewCreateSessionUseCase creates a new CreateSessionUseCase
func NewCreateSessionUseCase(
	sessionRepo repositories.SessionRepository,
	userRepo repositories.UserRepository,
	agentService services.AgentService,
	proxyService services.ProxyService,
) *CreateSessionUseCase {
	return &CreateSessionUseCase{
		sessionRepo:  sessionRepo,
		userRepo:     userRepo,
		agentService: agentService,
		proxyService: proxyService,
	}
}

// CreateSessionRequest represents the input for creating a session
type CreateSessionRequest struct {
	UserID      entities.UserID
	Environment entities.Environment
	Tags        entities.Tags
	Repository  *entities.Repository
	Port        *entities.Port // Optional: if not provided, will auto-assign
}

// CreateSessionResponse represents the output of creating a session
type CreateSessionResponse struct {
	Session *entities.Session
	URL     string
}

// Execute creates a new session
func (uc *CreateSessionUseCase) Execute(ctx context.Context, req *CreateSessionRequest) (*CreateSessionResponse, error) {
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

	// Check user's session limit (business rule)
	if err := uc.checkSessionLimit(ctx, req.UserID); err != nil {
		return nil, fmt.Errorf("session limit exceeded: %w", err)
	}

	// Assign port if not provided
	port := req.Port
	if port == nil {
		availablePort, err := uc.agentService.GetAvailablePort(ctx, 9000, 9999)
		if err != nil {
			return nil, fmt.Errorf("failed to get available port: %w", err)
		}
		port = &availablePort
	}

	// Verify port is available
	available, err := uc.agentService.IsPortAvailable(ctx, *port)
	if err != nil {
		return nil, fmt.Errorf("failed to check port availability: %w", err)
	}
	if !available {
		return nil, fmt.Errorf("port %d is not available", *port)
	}

	// Generate session ID
	sessionID := uc.generateSessionID()

	// Create session entity
	session := entities.NewSession(sessionID, req.UserID, *port, req.Environment, req.Tags, req.Repository)

	// Validate session
	if err := session.Validate(); err != nil {
		return nil, fmt.Errorf("invalid session: %w", err)
	}

	// Save session in starting state
	if err := uc.sessionRepo.Save(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// Start the agent process
	agentConfig := &services.AgentConfig{
		SessionID:   sessionID,
		UserID:      req.UserID,
		Port:        *port,
		Environment: req.Environment,
		Repository:  req.Repository,
	}

	processInfo, err := uc.agentService.StartAgent(ctx, agentConfig)
	if err != nil {
		// Mark session as failed and update
		session.MarkFailed(err.Error())
		_ = uc.sessionRepo.Update(ctx, session)
		return nil, fmt.Errorf("failed to start agent: %w", err)
	}

	// Mark session as active
	if err := session.Start(processInfo); err != nil {
		// Try to stop the agent if session start failed
		_ = uc.agentService.StopAgent(ctx, processInfo)
		session.MarkFailed(err.Error())
		_ = uc.sessionRepo.Update(ctx, session)
		return nil, fmt.Errorf("failed to start session: %w", err)
	}

	// Update session with process info
	if err := uc.sessionRepo.Update(ctx, session); err != nil {
		// Try to stop the agent if update failed
		_ = uc.agentService.StopAgent(ctx, processInfo)
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	// Get session URL
	sessionURL, err := uc.proxyService.GetSessionURL(ctx, sessionID, *port)
	if err != nil {
		// Log warning but don't fail the session creation
		sessionURL = fmt.Sprintf("http://localhost:%d", *port)
	}

	// Update user's last used timestamp
	user.UpdateLastUsed()
	_ = uc.userRepo.Update(ctx, user)

	return &CreateSessionResponse{
		Session: session,
		URL:     sessionURL,
	}, nil
}

// validateRequest validates the create session request
func (uc *CreateSessionUseCase) validateRequest(req *CreateSessionRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.UserID == "" {
		return errors.New("user ID cannot be empty")
	}

	if req.Port != nil && (*req.Port <= 0 || *req.Port > 65535) {
		return fmt.Errorf("invalid port: %d", *req.Port)
	}

	if req.Repository != nil {
		if err := req.Repository.Validate(); err != nil {
			return fmt.Errorf("invalid repository: %w", err)
		}
	}

	return nil
}

// checkSessionLimit checks if the user has exceeded their session limit
func (uc *CreateSessionUseCase) checkSessionLimit(ctx context.Context, userID entities.UserID) error {
	// Get active sessions for the user
	activeSessions, err := uc.sessionRepo.FindByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to check existing sessions: %w", err)
	}

	// Count active sessions
	activeCount := 0
	for _, session := range activeSessions {
		if session.Status() == entities.SessionStatusActive || session.Status() == entities.SessionStatusStarting {
			activeCount++
		}
	}

	// Business rule: Maximum 10 active sessions per user
	maxSessions := 10
	if activeCount >= maxSessions {
		return fmt.Errorf("maximum number of active sessions (%d) reached", maxSessions)
	}

	return nil
}

// generateSessionID generates a unique session ID
func (uc *CreateSessionUseCase) generateSessionID() entities.SessionID {
	// In a real implementation, this should generate a proper UUID
	// For now, we'll use a simple timestamp-based approach
	// This should be replaced with a proper UUID library
	return entities.SessionID(fmt.Sprintf("session_%d", getCurrentTimestamp()))
}

// getCurrentTimestamp returns current timestamp in milliseconds
func getCurrentTimestamp() int64 {
	// This is a placeholder - in real implementation use time.Now().UnixNano()
	return 1640995200000 // 2022-01-01 00:00:00 UTC
}
