package schedule

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sync"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
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
	launcher       *sessionuc.LaunchUseCase
	config         WorkerConfig
	logger         *log.Logger

	// Internal state
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
}

// NewWorker creates a new schedule worker
func NewWorker(manager Manager, sessionManager portrepos.SessionManager, memoryRepo portrepos.MemoryRepository, config WorkerConfig) *Worker {
	return &Worker{
		manager:        manager,
		sessionManager: sessionManager,
		launcher:       sessionuc.NewLaunchUseCase(sessionManager).WithMemoryRepository(memoryRepo),
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
	launchReq := w.buildLaunchRequest(schedule, sessionID)

	result, err := w.launcher.Launch(ctx, sessionID, launchReq)
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
		result.SessionID, schedule.ID)

	// Update schedule status
	if schedule.IsRecurring() {
		// Record execution first, then update next execution time
		w.recordExecution(ctx, schedule, ExecutionRecord{
			ExecutedAt: time.Now(),
			SessionID:  result.SessionID,
			Status:     "success",
		})
		w.updateNextExecution(ctx, schedule)
	} else {
		// For one-time schedule: record execution and mark as completed together
		// First record the execution
		record := ExecutionRecord{
			ExecutedAt: time.Now(),
			SessionID:  result.SessionID,
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

// buildLaunchRequest builds a LaunchRequest from a schedule.
// It uses ResolveTeams to ensure team-level settings are always injected correctly.
func (w *Worker) buildLaunchRequest(schedule *Schedule, sessionID string) sessionuc.LaunchRequest {
	scheduleScope := schedule.GetScope() // Use GetScope() to handle default value

	// Collect tags and add schedule metadata
	tags := schedule.SessionConfig.Tags
	if tags == nil {
		tags = make(map[string]string)
	}
	tags["schedule_id"] = schedule.ID
	tags["schedule_name"] = schedule.Name

	var initialMessage, githubToken, agentType string
	var slackParams *entities.SlackParams
	var oneshot bool
	if schedule.SessionConfig.Params != nil {
		initialMessage = schedule.SessionConfig.Params.Message
		// For team-scoped schedules, do not use the creator's github_token.
		if scheduleScope != entities.ScopeTeam {
			githubToken = schedule.SessionConfig.Params.GithubToken
		}
		agentType = schedule.SessionConfig.Params.AgentType
		slackParams = schedule.SessionConfig.Params.Slack
		oneshot = schedule.SessionConfig.Params.Oneshot
	}

	// Render memory_key values as Go templates with schedule context.
	// This allows values like {{ .schedule_id }} to be resolved at runtime.
	memoryKey := schedule.SessionConfig.MemoryKey
	if len(memoryKey) > 0 {
		schedulePayload := map[string]interface{}{
			"schedule_id":   schedule.ID,
			"schedule_name": schedule.Name,
			"timezone":      schedule.Timezone,
		}
		rendered, err := renderScheduleTemplateMap(memoryKey, schedulePayload)
		if err != nil {
			log.Printf("[SCHEDULE_WORKER] Failed to render memory_key templates for schedule %s: %v", schedule.ID, err)
		} else {
			memoryKey = rendered
		}
	}

	return sessionuc.LaunchRequest{
		UserID: schedule.UserID,
		Scope:  scheduleScope,
		TeamID: schedule.TeamID,
		// ResolveTeams centralises the scope-based teams logic so that it cannot
		// accidentally diverge between the worker and the manual-trigger handler.
		Teams:          sessionuc.ResolveTeams(scheduleScope, schedule.TeamID, schedule.UserTeams),
		Environment:    schedule.SessionConfig.Environment,
		Tags:           tags,
		InitialMessage: initialMessage,
		GithubToken:    githubToken,
		AgentType:      agentType,
		SlackParams:    slackParams,
		Oneshot:        oneshot,
		MemoryKey:      memoryKey,
		RepoInfo:       app.ExtractRepositoryInfo(tags, sessionID),
	}
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

// renderScheduleTemplateMap renders all template values in a map using schedule context data.
// This allows memory_key values to use Go template expressions such as {{ .schedule_id }}.
func renderScheduleTemplateMap(templates map[string]string, payload map[string]interface{}) (map[string]string, error) {
	result := make(map[string]string, len(templates))
	for key, tmplStr := range templates {
		tmpl, err := template.New("schedule_memory_key").Parse(tmplStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse template for key '%s': %w", key, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, payload); err != nil {
			return nil, fmt.Errorf("failed to execute template for key '%s': %w", key, err)
		}
		result[key] = buf.String()
	}
	return result, nil
}
