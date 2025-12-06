package session

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// StartSessionUseCase handles starting new agent sessions
type StartSessionUseCase struct {
	sessionRepo    repositories.SessionRepository
	userRepo       repositories.UserRepository
	agentService   services.AgentService
	environmentSvc services.EnvironmentService
	portManager    services.PortManager
	repositorySvc  services.RepositoryService
}

// NewStartSessionUseCase creates a new StartSessionUseCase
func NewStartSessionUseCase(
	sessionRepo repositories.SessionRepository,
	userRepo repositories.UserRepository,
	agentService services.AgentService,
	environmentSvc services.EnvironmentService,
	portManager services.PortManager,
	repositorySvc services.RepositoryService,
) *StartSessionUseCase {
	return &StartSessionUseCase{
		sessionRepo:    sessionRepo,
		userRepo:       userRepo,
		agentService:   agentService,
		environmentSvc: environmentSvc,
		portManager:    portManager,
		repositorySvc:  repositorySvc,
	}
}

// Execute starts a new session based on the request
func (uc *StartSessionUseCase) Execute(ctx context.Context, req *StartSessionRequest) (*StartSessionResponse, error) {
	// Validate request
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	// Verify user exists and is authorized
	user, err := uc.userRepo.FindByID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUserNotFound, err)
	}
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}

	// Check user's session limit (business rule)
	if err := uc.checkSessionLimit(ctx, req.UserID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSessionLimitReached, err)
	}

	// Generate session ID
	sessionID := uc.generateSessionID()

	// Merge environment variables
	mergedEnv, err := uc.mergeEnvironment(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvironmentMerge, err)
	}

	// Get available port
	port, err := uc.portManager.GetAvailablePort(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPortAllocation, err)
	}

	// Extract and initialize repository if needed
	repoInfo := uc.repositorySvc.ExtractRepositoryInfo(sessionID, req.Tags)
	if repoInfo != nil {
		if err := uc.repositorySvc.InitializeRepository(ctx, repoInfo, string(req.UserID)); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrRepositoryInit, err)
		}
	}

	// Create session entity
	session := entities.NewSession(
		entities.SessionID(sessionID),
		req.UserID,
		entities.Port(port),
		entities.Environment(mergedEnv),
		entities.Tags(req.Tags),
		uc.convertRepositoryInfo(repoInfo),
	)

	// Validate session
	if err := session.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	// Save session in starting state
	if err := uc.sessionRepo.Save(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	// Start the agent process
	agentConfig := &services.AgentConfig{
		SessionID:   entities.SessionID(sessionID),
		UserID:      req.UserID,
		Port:        entities.Port(port),
		Environment: entities.Environment(mergedEnv),
		Repository:  uc.convertRepositoryInfo(repoInfo),
		Message:     req.Message,
	}

	processInfo, err := uc.agentService.StartAgent(ctx, agentConfig)
	if err != nil {
		// Mark session as failed and update
		session.MarkFailed(err.Error())
		_ = uc.sessionRepo.Update(ctx, session)
		return nil, fmt.Errorf("%w: %v", ErrSessionStart, err)
	}

	// Mark session as active
	if err := session.Start(processInfo); err != nil {
		// Try to stop the agent if session start failed
		_ = uc.agentService.StopAgent(ctx, processInfo)
		session.MarkFailed(err.Error())
		_ = uc.sessionRepo.Update(ctx, session)
		return nil, fmt.Errorf("%w: %v", ErrSessionStart, err)
	}

	// Update session with process info
	if err := uc.sessionRepo.Update(ctx, session); err != nil {
		// Try to stop the agent if update failed
		_ = uc.agentService.StopAgent(ctx, processInfo)
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	// Update user's last used timestamp
	user.UpdateLastUsed()
	_ = uc.userRepo.Update(ctx, user)

	log.Printf("[SESSION_CREATED] ID: %s, Port: %d, User: %s, Tags: %v",
		sessionID, port, req.UserID, req.Tags)

	return &StartSessionResponse{
		SessionID: sessionID,
	}, nil
}

// mergeEnvironment merges environment variables from multiple sources
func (uc *StartSessionUseCase) mergeEnvironment(ctx context.Context, req *StartSessionRequest) (map[string]string, error) {
	if req.AuthContext == nil {
		req.AuthContext = &AuthContext{}
	}

	config := &services.EnvMergeConfig{
		UserRole:        req.AuthContext.UserRole,
		TeamEnvFile:     uc.environmentSvc.ExtractTeamEnvFile(req.Tags),
		AuthTeamEnvFile: req.AuthContext.AuthTeamEnvFile,
		RequestEnv:      req.Environment,
	}

	return uc.environmentSvc.MergeEnvironmentVariables(ctx, config)
}

// checkSessionLimit checks if the user has exceeded their session limit
func (uc *StartSessionUseCase) checkSessionLimit(ctx context.Context, userID entities.UserID) error {
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
func (uc *StartSessionUseCase) generateSessionID() string {
	return uuid.New().String()
}

// convertRepositoryInfo converts RepositoryInfo from service layer to domain layer
func (uc *StartSessionUseCase) convertRepositoryInfo(repoInfo *services.RepositoryInfo) *entities.Repository {
	if repoInfo == nil {
		return nil
	}

	// Convert service layer RepositoryInfo to domain entities.Repository
	// This would need proper implementation based on entities.Repository structure
	return &entities.Repository{
		// Map fields appropriately
		// FullName: repoInfo.FullName,
		// CloneDir: repoInfo.CloneDir,
	}
}
