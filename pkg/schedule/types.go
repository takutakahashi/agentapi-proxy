package schedule

import (
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// ScheduleStatus defines the current status of a schedule
type ScheduleStatus string

const (
	// ScheduleStatusActive indicates the schedule is active and will execute
	ScheduleStatusActive ScheduleStatus = "active"
	// ScheduleStatusPaused indicates the schedule is paused
	ScheduleStatusPaused ScheduleStatus = "paused"
	// ScheduleStatusCompleted indicates a one-time schedule has completed
	ScheduleStatusCompleted ScheduleStatus = "completed"
)

// Schedule represents a scheduled session configuration
type Schedule struct {
	// ID is the unique identifier for the schedule
	ID string `json:"id"`
	// Name is a human-readable name for the schedule
	Name string `json:"name"`
	// UserID is the ID of the user who created the schedule
	UserID string `json:"user_id"`
	// Scope defines the ownership scope ("user" or "team"). Defaults to "user".
	Scope entities.ResourceScope `json:"scope,omitempty"`
	// TeamID is the team identifier (e.g., "org/team-slug") when Scope is "team"
	TeamID string `json:"team_id,omitempty"`
	// Status is the current status of the schedule
	Status ScheduleStatus `json:"status"`

	// ScheduledAt is the time when the schedule should first execute (optional)
	// If set without CronExpr, this is a one-time schedule
	// If set with CronExpr, execution starts from this time
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`

	// CronExpr is the cron expression for recurring execution (optional)
	// Standard cron format: minute hour day-of-month month day-of-week
	// Example: "0 9 * * 1-5" (weekdays at 9:00 AM)
	CronExpr string `json:"cron_expr,omitempty"`

	// Timezone is the IANA timezone for schedule evaluation (default: UTC)
	Timezone string `json:"timezone,omitempty"`

	// SessionConfig contains the configuration for creating sessions
	SessionConfig SessionConfig `json:"session_config"`

	// LastExecution contains the most recent execution record
	LastExecution *ExecutionRecord `json:"last_execution,omitempty"`

	// NextExecutionAt is the calculated next execution time
	NextExecutionAt *time.Time `json:"next_execution_at,omitempty"`

	// ExecutionCount is the total number of executions
	ExecutionCount int `json:"execution_count"`

	// CreatedAt is when the schedule was created
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the schedule was last updated
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionConfig contains session creation parameters
type SessionConfig struct {
	// Environment variables to set for the session
	Environment map[string]string `json:"environment,omitempty"`
	// Tags for the session (including repository info)
	Tags map[string]string `json:"tags,omitempty"`
	// Params contains session parameters like initial message
	Params *entities.SessionParams `json:"params,omitempty"`
}

// ExecutionRecord represents a single execution attempt
type ExecutionRecord struct {
	// ExecutedAt is when the execution was attempted
	ExecutedAt time.Time `json:"executed_at"`
	// SessionID is the ID of the created session (if successful)
	SessionID string `json:"session_id,omitempty"`
	// Status is the result of the execution: "success", "failed", or "skipped"
	Status string `json:"status"`
	// Error contains the error message if execution failed
	Error string `json:"error,omitempty"`
}

// GetScope returns the resource scope, defaulting to "user" if not set
func (s *Schedule) GetScope() entities.ResourceScope {
	if s.Scope == "" {
		return entities.ScopeUser
	}
	return s.Scope
}

// IsOneTime returns true if this is a one-time schedule (no recurring)
func (s *Schedule) IsOneTime() bool {
	return s.ScheduledAt != nil && s.CronExpr == ""
}

// IsRecurring returns true if this is a recurring schedule
func (s *Schedule) IsRecurring() bool {
	return s.CronExpr != ""
}

// IsDue checks if the schedule is due for execution at the given time
func (s *Schedule) IsDue(now time.Time) bool {
	if s.Status != ScheduleStatusActive {
		return false
	}

	if s.NextExecutionAt == nil {
		return false
	}

	return !now.Before(*s.NextExecutionAt)
}

// Validate checks if the schedule is valid
func (s *Schedule) Validate() error {
	if s.ID == "" {
		return ErrInvalidSchedule{Field: "id", Message: "id is required"}
	}
	if s.Name == "" {
		return ErrInvalidSchedule{Field: "name", Message: "name is required"}
	}
	if s.UserID == "" {
		return ErrInvalidSchedule{Field: "user_id", Message: "user_id is required"}
	}
	if s.ScheduledAt == nil && s.CronExpr == "" {
		return ErrInvalidSchedule{Field: "schedule", Message: "either scheduled_at or cron_expr must be set"}
	}
	return nil
}

// ErrInvalidSchedule represents a validation error
type ErrInvalidSchedule struct {
	Field   string
	Message string
}

func (e ErrInvalidSchedule) Error() string {
	return "invalid schedule: " + e.Field + ": " + e.Message
}

// ErrScheduleNotFound is returned when a schedule is not found
type ErrScheduleNotFound struct {
	ID string
}

func (e ErrScheduleNotFound) Error() string {
	return "schedule not found: " + e.ID
}
