package schedule

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// WorkerConfig contains configuration for the schedule worker
type WorkerConfig struct {
	// CheckInterval is how often to check for due schedules
	CheckInterval time.Duration
	// Enabled indicates whether the worker should run
	Enabled bool
}

// DefaultWorkerConfig returns the default worker configuration
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		CheckInterval: 30 * time.Second,
		Enabled:       true,
	}
}

// Worker processes scheduled sessions
type Worker struct {
	manager        Manager
	sessionManager portrepos.SessionManager
	config         WorkerConfig
	logger         *log.Logger

	// Internal state
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
}

// NewWorker creates a new schedule worker
func NewWorker(manager Manager, sessionManager portrepos.SessionManager, config WorkerConfig) *Worker {
	return &Worker{
		manager:        manager,
		sessionManager: sessionManager,
		config:         config,
		logger:         log.Default(),
		stopCh:         make(chan struct{}),
	}
}

// Start begins the worker loop
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()

	w.wg.Add(1)
	go w.run(ctx)

	log.Printf("[SCHEDULE_WORKER] Started with check interval %v", w.config.CheckInterval)
	return nil
}

// Stop gracefully stops the worker
func (w *Worker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	close(w.stopCh)
	w.wg.Wait()
	log.Printf("[SCHEDULE_WORKER] Stopped")
}

// run is the main worker loop
func (w *Worker) run(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.CheckInterval)
	defer ticker.Stop()

	// Run immediately on start
	w.processSchedules(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[SCHEDULE_WORKER] Context cancelled, stopping")
			return
		case <-w.stopCh:
			log.Printf("[SCHEDULE_WORKER] Stop signal received")
			return
		case <-ticker.C:
			w.processSchedules(ctx)
		}
	}
}

// processSchedules checks and executes due schedules
func (w *Worker) processSchedules(ctx context.Context) {
	now := time.Now()

	schedules, err := w.manager.GetDueSchedules(ctx, now)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Failed to get due schedules: %v", err)
		return
	}

	if len(schedules) == 0 {
		return
	}

	log.Printf("[SCHEDULE_WORKER] Found %d due schedules", len(schedules))

	for _, schedule := range schedules {
		w.executeSchedule(ctx, schedule)
	}
}

// executeSchedule executes a single schedule
func (w *Worker) executeSchedule(ctx context.Context, schedule *Schedule) {
	log.Printf("[SCHEDULE_WORKER] Executing schedule %s (%s)", schedule.ID, schedule.Name)

	// Check if previous session is still active
	if schedule.LastExecution != nil && schedule.LastExecution.SessionID != "" {
		if w.isSessionActive(schedule.LastExecution.SessionID) {
			log.Printf("[SCHEDULE_WORKER] Skipping schedule %s: previous session %s still active",
				schedule.ID, schedule.LastExecution.SessionID)
			w.recordExecution(ctx, schedule, ExecutionRecord{
				ExecutedAt: time.Now(),
				Status:     "skipped",
				Error:      "previous session still active",
			})
			// Update next execution time even when skipped
			w.updateNextExecution(ctx, schedule)
			return
		}
		// Delete previous session if it's no longer active
		w.deletePreviousSession(schedule.LastExecution.SessionID)
	}

	// Create session
	sessionID := uuid.New().String()
	req := w.buildRunServerRequest(schedule, sessionID)

	session, err := w.sessionManager.CreateSession(ctx, sessionID, req, nil)
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Failed to create session for schedule %s: %v",
			schedule.ID, err)
		w.recordExecution(ctx, schedule, ExecutionRecord{
			ExecutedAt: time.Now(),
			Status:     "failed",
			Error:      err.Error(),
		})
		return
	}

	log.Printf("[SCHEDULE_WORKER] Successfully created session %s for schedule %s",
		session.ID(), schedule.ID)

	// Update schedule status
	if schedule.IsRecurring() {
		// Record execution first, then update next execution time
		w.recordExecution(ctx, schedule, ExecutionRecord{
			ExecutedAt: time.Now(),
			SessionID:  session.ID(),
			Status:     "success",
		})
		w.updateNextExecution(ctx, schedule)
	} else {
		// For one-time schedule: record execution and mark as completed together
		// First record the execution
		record := ExecutionRecord{
			ExecutedAt: time.Now(),
			SessionID:  session.ID(),
			Status:     "success",
		}
		if err := w.manager.RecordExecution(ctx, schedule.ID, record); err != nil {
			log.Printf("[SCHEDULE_WORKER] Failed to record execution for schedule %s: %v",
				schedule.ID, err)
		}

		// Then get the updated schedule and mark as completed
		updated, err := w.manager.Get(ctx, schedule.ID)
		if err != nil {
			log.Printf("[SCHEDULE_WORKER] Failed to get schedule %s for completion: %v",
				schedule.ID, err)
			return
		}
		updated.Status = ScheduleStatusCompleted
		if err := w.manager.Update(ctx, updated); err != nil {
			log.Printf("[SCHEDULE_WORKER] Failed to mark schedule %s as completed: %v",
				schedule.ID, err)
		}
	}
}

// buildRunServerRequest builds a RunServerRequest from a schedule
func (w *Worker) buildRunServerRequest(schedule *Schedule, sessionID string) *entities.RunServerRequest {
	scheduleScope := schedule.GetScope() // Use GetScope() to handle default value
	req := &entities.RunServerRequest{
		UserID:      schedule.UserID,
		Environment: schedule.SessionConfig.Environment,
		Tags:        schedule.SessionConfig.Tags,
		Scope:       scheduleScope,
		TeamID:      schedule.TeamID,
	}

	// For team-scoped schedules, only include the team's credentials
	// (not all teams the creating user belongs to)
	if scheduleScope == entities.ScopeTeam && schedule.TeamID != "" {
		req.Teams = []string{schedule.TeamID}
	}

	// Add schedule metadata to tags
	if req.Tags == nil {
		req.Tags = make(map[string]string)
	}
	req.Tags["schedule_id"] = schedule.ID
	req.Tags["schedule_name"] = schedule.Name

	if schedule.SessionConfig.Params != nil {
		req.InitialMessage = schedule.SessionConfig.Params.Message
		// For team-scoped schedules, do not use the creator's github_token
		if scheduleScope != entities.ScopeTeam {
			req.GithubToken = schedule.SessionConfig.Params.GithubToken
		}
		req.AgentType = schedule.SessionConfig.Params.AgentType
		req.SlackParams = schedule.SessionConfig.Params.Slack
	}

	// Extract repository information from tags
	req.RepoInfo = app.ExtractRepositoryInfo(req.Tags, sessionID)

	return req
}

// isSessionActive checks if a session is still active
func (w *Worker) isSessionActive(sessionID string) bool {
	session := w.sessionManager.GetSession(sessionID)
	if session == nil {
		return false
	}
	status := session.Status()
	return status == "active" || status == "starting" || status == "creating"
}

// deletePreviousSession deletes the previous session if it exists
func (w *Worker) deletePreviousSession(sessionID string) {
	if err := w.sessionManager.DeleteSession(sessionID); err != nil {
		log.Printf("[SCHEDULE_WORKER] Failed to delete previous session %s: %v", sessionID, err)
	} else {
		log.Printf("[SCHEDULE_WORKER] Deleted previous session %s", sessionID)
	}
}

// recordExecution records an execution attempt
func (w *Worker) recordExecution(ctx context.Context, schedule *Schedule, record ExecutionRecord) {
	if err := w.manager.RecordExecution(ctx, schedule.ID, record); err != nil {
		log.Printf("[SCHEDULE_WORKER] Failed to record execution for schedule %s: %v",
			schedule.ID, err)
	}
}

// updateNextExecution calculates and updates the next execution time
func (w *Worker) updateNextExecution(ctx context.Context, schedule *Schedule) {
	nextAt, err := CalculateNextExecution(schedule, time.Now())
	if err != nil {
		log.Printf("[SCHEDULE_WORKER] Failed to calculate next execution for schedule %s: %v",
			schedule.ID, err)
		return
	}

	if nextAt != nil {
		if err := w.manager.UpdateNextExecution(ctx, schedule.ID, *nextAt); err != nil {
			log.Printf("[SCHEDULE_WORKER] Failed to update next execution for schedule %s: %v",
				schedule.ID, err)
		} else {
			log.Printf("[SCHEDULE_WORKER] Next execution for schedule %s: %v",
				schedule.ID, nextAt)
		}
	}
}
