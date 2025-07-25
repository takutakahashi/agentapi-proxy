package session

import (
	"context"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"time"
)

// MonitorSessionUseCase handles session health monitoring and status updates
type MonitorSessionUseCase struct {
	sessionRepo  repositories.SessionRepository
	userRepo     repositories.UserRepository
	agentService services.AgentService
	proxyService services.ProxyService
}

// NewMonitorSessionUseCase creates a new MonitorSessionUseCase
func NewMonitorSessionUseCase(
	sessionRepo repositories.SessionRepository,
	userRepo repositories.UserRepository,
	agentService services.AgentService,
	proxyService services.ProxyService,
) *MonitorSessionUseCase {
	return &MonitorSessionUseCase{
		sessionRepo:  sessionRepo,
		userRepo:     userRepo,
		agentService: agentService,
		proxyService: proxyService,
	}
}

// MonitorSessionRequest represents the input for monitoring a session
type MonitorSessionRequest struct {
	SessionID entities.SessionID
	UserID    entities.UserID // For authorization
}

// MonitorSessionResponse represents the output of monitoring a session
type MonitorSessionResponse struct {
	Session     *entities.Session
	HealthCheck *HealthCheckResult
	Updated     bool // Whether the session status was updated
}

// HealthCheckResult represents the result of a health check
type HealthCheckResult struct {
	ProcessStatus services.ProcessStatus
	IsReachable   bool
	ResponseTime  *time.Duration
	Error         error
	CheckedAt     time.Time
}

// Execute monitors a session and updates its status if necessary
func (uc *MonitorSessionUseCase) Execute(ctx context.Context, req *MonitorSessionRequest) (*MonitorSessionResponse, error) {
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

	// Perform health check
	healthCheck := uc.performHealthCheck(ctx, session)

	// Update session status based on health check results
	updated, err := uc.updateSessionStatus(ctx, session, healthCheck)
	if err != nil {
		return nil, fmt.Errorf("failed to update session status: %w", err)
	}

	return &MonitorSessionResponse{
		Session:     session,
		HealthCheck: healthCheck,
		Updated:     updated,
	}, nil
}

// performHealthCheck performs comprehensive health checks on a session
func (uc *MonitorSessionUseCase) performHealthCheck(ctx context.Context, session *entities.Session) *HealthCheckResult {
	result := &HealthCheckResult{
		CheckedAt: time.Now(),
	}

	// Check process status if process info is available
	if session.ProcessInfo() != nil {
		processStatus, err := uc.agentService.GetAgentStatus(ctx, session.ProcessInfo())
		if err != nil {
			result.Error = fmt.Errorf("failed to check process status: %w", err)
			return result
		}
		result.ProcessStatus = processStatus

		// If process is not running, mark as unreachable
		if processStatus != services.ProcessStatusRunning {
			result.IsReachable = false
			return result
		}
	}

	// Check if session is reachable via HTTP
	if session.IsActive() {
		startTime := time.Now()
		isReachable, err := uc.proxyService.IsSessionReachable(ctx, session.ID(), session.Port())
		responseTime := time.Since(startTime)
		result.ResponseTime = &responseTime

		if err != nil {
			result.Error = fmt.Errorf("failed to check reachability: %w", err)
			result.IsReachable = false
		} else {
			result.IsReachable = isReachable
		}
	}

	return result
}

// updateSessionStatus updates the session status based on health check results
func (uc *MonitorSessionUseCase) updateSessionStatus(ctx context.Context, session *entities.Session, healthCheck *HealthCheckResult) (bool, error) {
	originalStatus := session.Status()
	updated := false

	// Update session status based on health check results
	switch {
	case healthCheck.Error != nil:
		// Health check failed - mark as failed if it was active
		if session.IsActive() {
			session.MarkFailed(healthCheck.Error.Error())
			updated = true
		}

	case healthCheck.ProcessStatus == services.ProcessStatusStopped ||
		healthCheck.ProcessStatus == services.ProcessStatusNotFound:
		// Process is stopped - mark session as stopped
		if !session.IsStopped() && !session.IsFailed() {
			if err := session.MarkStopped(); err != nil {
				return false, fmt.Errorf("failed to mark session as stopped: %w", err)
			}
			updated = true
		}

	case healthCheck.ProcessStatus == services.ProcessStatusZombie:
		// Process is zombie - mark as failed
		if !session.IsFailed() {
			session.MarkFailed("process is in zombie state")
			updated = true
		}

	case session.IsActive() && !healthCheck.IsReachable:
		// Session is active but not reachable - mark as failed
		session.MarkFailed("session is not reachable via HTTP")
		updated = true

	case session.Status() == entities.SessionStatusStarting &&
		healthCheck.ProcessStatus == services.ProcessStatusRunning &&
		healthCheck.IsReachable:
		// Session was starting and is now ready - mark as active
		if err := session.Start(session.ProcessInfo()); err != nil {
			return false, fmt.Errorf("failed to mark session as active: %w", err)
		}
		updated = true
	}

	// Save session if status was updated
	if updated {
		if err := uc.sessionRepo.Update(ctx, session); err != nil {
			return false, fmt.Errorf("failed to save session: %w", err)
		}
	}

	// Log status change if it occurred
	if updated && originalStatus != session.Status() {
		// In a real implementation, this would use proper logging
		fmt.Printf("Session %s status changed from %s to %s\n",
			session.ID(), originalStatus, session.Status())
	}

	return updated, nil
}

// validateRequest validates the monitor session request
func (uc *MonitorSessionUseCase) validateRequest(req *MonitorSessionRequest) error {
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

// checkAuthorization checks if the user is authorized to monitor the session
func (uc *MonitorSessionUseCase) checkAuthorization(ctx context.Context, session *entities.Session, userID entities.UserID) error {
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
		return errors.New("user does not have permission to monitor this session")
	}

	return nil
}

// MonitorAllSessionsUseCase handles monitoring all sessions for health
type MonitorAllSessionsUseCase struct {
	sessionRepo  repositories.SessionRepository
	agentService services.AgentService
	proxyService services.ProxyService
}

// NewMonitorAllSessionsUseCase creates a new MonitorAllSessionsUseCase
func NewMonitorAllSessionsUseCase(
	sessionRepo repositories.SessionRepository,
	agentService services.AgentService,
	proxyService services.ProxyService,
) *MonitorAllSessionsUseCase {
	return &MonitorAllSessionsUseCase{
		sessionRepo:  sessionRepo,
		agentService: agentService,
		proxyService: proxyService,
	}
}

// MonitorAllSessionsResponse represents the output of monitoring all sessions
type MonitorAllSessionsResponse struct {
	TotalSessions   int
	UpdatedSessions int
	FailedSessions  int
	HealthResults   []*SessionHealthResult
}

// SessionHealthResult represents the health result for a single session
type SessionHealthResult struct {
	SessionID   entities.SessionID
	OldStatus   entities.SessionStatus
	NewStatus   entities.SessionStatus
	HealthCheck *HealthCheckResult
	Error       error
}

// Execute monitors all active sessions
func (uc *MonitorAllSessionsUseCase) Execute(ctx context.Context) (*MonitorAllSessionsResponse, error) {
	// Get all active sessions
	filter := &repositories.SessionFilter{
		Status: &[]entities.SessionStatus{
			entities.SessionStatusActive,
			entities.SessionStatusStarting,
		}[0], // Only monitor active/starting sessions
	}

	sessions, _, err := uc.sessionRepo.FindWithFilter(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve sessions: %w", err)
	}

	response := &MonitorAllSessionsResponse{
		TotalSessions: len(sessions),
		HealthResults: make([]*SessionHealthResult, 0, len(sessions)),
	}

	// Monitor each session
	for _, session := range sessions {
		result := &SessionHealthResult{
			SessionID: session.ID(),
			OldStatus: session.Status(),
		}

		// Create monitor use case for individual session
		monitorUC := NewMonitorSessionUseCase(uc.sessionRepo, nil, uc.agentService, uc.proxyService)

		// Perform health check without authorization (system operation)
		healthCheck := monitorUC.performHealthCheck(ctx, session)
		result.HealthCheck = healthCheck

		// Update session status
		updated, err := monitorUC.updateSessionStatus(ctx, session, healthCheck)
		if err != nil {
			result.Error = err
			response.FailedSessions++
		} else {
			result.NewStatus = session.Status()
			if updated {
				response.UpdatedSessions++
			}
		}

		response.HealthResults = append(response.HealthResults, result)
	}

	return response, nil
}
