package schedule

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// CronParser handles cron expression parsing and next execution calculation
type CronParser struct {
	parser cron.Parser
}

// NewCronParser creates a new CronParser
func NewCronParser() *CronParser {
	return &CronParser{
		parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// Validate checks if a cron expression is valid
func (p *CronParser) Validate(cronExpr string) error {
	_, err := p.parser.Parse(cronExpr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

// Next calculates the next execution time after the given time
func (p *CronParser) Next(cronExpr string, timezone string, from time.Time) (time.Time, error) {
	schedule, err := p.parser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
	}

	// Handle timezone
	loc := time.UTC
	if timezone != "" {
		loc, err = time.LoadLocation(timezone)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid timezone: %w", err)
		}
	}

	// Convert to timezone for calculation
	fromInTZ := from.In(loc)
	nextInTZ := schedule.Next(fromInTZ)

	// Return in UTC for storage
	return nextInTZ.UTC(), nil
}

// CalculateNextExecution calculates the next execution time for a schedule
func CalculateNextExecution(s *Schedule, from time.Time) (*time.Time, error) {
	parser := NewCronParser()

	// If recurring schedule with cron expression
	if s.IsRecurring() {
		// If scheduled_at is set and is in the future, start from there
		if s.ScheduledAt != nil && from.Before(*s.ScheduledAt) {
			next, err := parser.Next(s.CronExpr, s.Timezone, *s.ScheduledAt)
			if err != nil {
				return nil, err
			}
			// If the scheduled_at itself matches a cron slot, use it
			firstNext, _ := parser.Next(s.CronExpr, s.Timezone, s.ScheduledAt.Add(-time.Second))
			if firstNext.Equal(*s.ScheduledAt) || firstNext.Before(*s.ScheduledAt) {
				return s.ScheduledAt, nil
			}
			return &next, nil
		}

		next, err := parser.Next(s.CronExpr, s.Timezone, from)
		if err != nil {
			return nil, err
		}
		return &next, nil
	}

	// One-time schedule
	if s.ScheduledAt != nil {
		return s.ScheduledAt, nil
	}

	return nil, nil
}
