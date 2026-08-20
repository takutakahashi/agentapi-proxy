package schedule

import (
	"testing"
	"time"
)

func TestCronParser_Validate(t *testing.T) {
	parser := NewCronParser()

	tests := []struct {
		name     string
		cronExpr string
		wantErr  bool
	}{
		{
			name:     "valid cron expression - weekdays at 9am",
			cronExpr: "0 9 * * 1-5",
			wantErr:  false,
		},
		{
			name:     "valid cron expression - every minute",
			cronExpr: "* * * * *",
			wantErr:  false,
		},
		{
			name:     "valid cron expression - first day of month",
			cronExpr: "0 0 1 * *",
			wantErr:  false,
		},
		{
			name:     "invalid cron expression - wrong format",
			cronExpr: "invalid",
			wantErr:  true,
		},
		{
			name:     "invalid cron expression - too few fields",
			cronExpr: "0 9 *",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parser.Validate(tt.cronExpr)
			if (err != nil) != tt.wantErr {
				t.Errorf("CronParser.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCronParser_Next(t *testing.T) {
	parser := NewCronParser()

	// Fixed reference time: Monday, Jan 1, 2024, 08:00:00 UTC
	baseTime := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		cronExpr string
		timezone string
		from     time.Time
		wantErr  bool
		check    func(t *testing.T, got time.Time)
	}{
		{
			name:     "next execution at 9am UTC",
			cronExpr: "0 9 * * *",
			timezone: "",
			from:     baseTime,
			wantErr:  false,
			check: func(t *testing.T, got time.Time) {
				expected := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
				if !got.Equal(expected) {
					t.Errorf("expected %v, got %v", expected, got)
				}
			},
		},
		{
			name:     "next execution at 9am in Asia/Tokyo",
			cronExpr: "0 9 * * *",
			timezone: "Asia/Tokyo",
			from:     baseTime, // 08:00 UTC = 17:00 JST
			wantErr:  false,
			check: func(t *testing.T, got time.Time) {
				// Next 9am JST from 17:00 JST Jan 1 is 9:00 JST Jan 2
				// 9:00 JST = 00:00 UTC
				expected := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
				if !got.Equal(expected) {
					t.Errorf("expected %v, got %v", expected, got)
				}
			},
		},
		{
			name:     "invalid timezone",
			cronExpr: "0 9 * * *",
			timezone: "Invalid/Timezone",
			from:     baseTime,
			wantErr:  true,
			check:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parser.Next(tt.cronExpr, tt.timezone, tt.from)
			if (err != nil) != tt.wantErr {
				t.Errorf("CronParser.Next() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestCalculateNextExecution(t *testing.T) {
	// Fixed reference time
	baseTime := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC)
	scheduledTime := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		schedule *Schedule
		from     time.Time
		wantNil  bool
		check    func(t *testing.T, got *time.Time)
	}{
		{
			name: "one-time schedule returns scheduled_at",
			schedule: &Schedule{
				ScheduledAt: &scheduledTime,
			},
			from:    baseTime,
			wantNil: false,
			check: func(t *testing.T, got *time.Time) {
				if !got.Equal(scheduledTime) {
					t.Errorf("expected %v, got %v", scheduledTime, *got)
				}
			},
		},
		{
			name: "recurring schedule calculates next from cron",
			schedule: &Schedule{
				CronExpr: "0 9 * * *",
			},
			from:    baseTime,
			wantNil: false,
			check: func(t *testing.T, got *time.Time) {
				expected := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
				if !got.Equal(expected) {
					t.Errorf("expected %v, got %v", expected, *got)
				}
			},
		},
		{
			name: "recurring schedule with future scheduled_at",
			schedule: &Schedule{
				ScheduledAt: &scheduledTime,
				CronExpr:    "0 9 * * *",
			},
			from:    baseTime,
			wantNil: false,
			check: func(t *testing.T, got *time.Time) {
				// Should return the scheduled_at since it matches 9am
				if !got.Equal(scheduledTime) {
					t.Errorf("expected %v, got %v", scheduledTime, *got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateNextExecution(tt.schedule, tt.from)
			if err != nil {
				t.Errorf("CalculateNextExecution() error = %v", err)
				return
			}
			if (got == nil) != tt.wantNil {
				t.Errorf("CalculateNextExecution() = %v, wantNil %v", got, tt.wantNil)
				return
			}
			if tt.check != nil && got != nil {
				tt.check(t, got)
			}
		})
	}
}
