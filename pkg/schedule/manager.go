package schedule

import (
	"context"
	"time"
)

// ScheduleFilter defines filter criteria for listing schedules
type ScheduleFilter struct {
	// UserID filters by user ID
	UserID string
	// Status filters by schedule status
	Status ScheduleStatus
}

// Manager defines the interface for schedule management
type Manager interface {
	// Create creates a new schedule
	Create(ctx context.Context, schedule *Schedule) error

	// Get retrieves a schedule by ID
	Get(ctx context.Context, id string) (*Schedule, error)

	// List retrieves schedules matching the filter
	List(ctx context.Context, filter ScheduleFilter) ([]*Schedule, error)

	// Update updates an existing schedule
	Update(ctx context.Context, schedule *Schedule) error

	// Delete removes a schedule by ID
	Delete(ctx context.Context, id string) error

	// GetDueSchedules returns schedules that are due for execution
	GetDueSchedules(ctx context.Context, now time.Time) ([]*Schedule, error)

	// RecordExecution records an execution attempt
	RecordExecution(ctx context.Context, id string, record ExecutionRecord) error

	// UpdateNextExecution updates the next execution time for a schedule
	UpdateNextExecution(ctx context.Context, id string, nextAt time.Time) error
}
