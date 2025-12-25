package schedule

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHandlers_CreateSchedule(t *testing.T) {
	e := echo.New()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	handlers := NewHandlers(manager, nil)

	tests := []struct {
		name       string
		body       CreateScheduleRequest
		wantStatus int
	}{
		{
			name: "valid one-time schedule",
			body: CreateScheduleRequest{
				Name:        "Test Schedule",
				ScheduledAt: timePtr(time.Now().Add(time.Hour)),
				SessionConfig: SessionConfig{
					Tags: map[string]string{"test": "true"},
				},
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "valid recurring schedule",
			body: CreateScheduleRequest{
				Name:     "Daily Schedule",
				CronExpr: "0 9 * * *",
				Timezone: "UTC",
				SessionConfig: SessionConfig{
					Tags: map[string]string{"test": "true"},
				},
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "missing name",
			body: CreateScheduleRequest{
				ScheduledAt: timePtr(time.Now().Add(time.Hour)),
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing schedule",
			body: CreateScheduleRequest{
				Name: "No Schedule",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid cron expression",
			body: CreateScheduleRequest{
				Name:     "Bad Cron",
				CronExpr: "invalid",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid timezone",
			body: CreateScheduleRequest{
				Name:     "Bad Timezone",
				CronExpr: "0 9 * * *",
				Timezone: "Invalid/Zone",
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/schedules", bytes.NewReader(body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := handlers.CreateSchedule(c)
			if err != nil {
				he, ok := err.(*echo.HTTPError)
				if !ok {
					t.Fatalf("unexpected error type: %T", err)
				}
				if he.Code != tt.wantStatus {
					t.Errorf("got status %d, want %d", he.Code, tt.wantStatus)
				}
				return
			}

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandlers_ListSchedules(t *testing.T) {
	e := echo.New()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	handlers := NewHandlers(manager, nil)

	// Create some schedules
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < 3; i++ {
		schedule := &Schedule{
			ID:          "schedule-" + string(rune('a'+i)),
			Name:        "Schedule " + string(rune('A'+i)),
			UserID:      "test-user",
			Status:      ScheduleStatusActive,
			ScheduledAt: &now,
		}
		if err := manager.Create(ctx, schedule); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/schedules", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := handlers.ListSchedules(c)
	if err != nil {
		t.Fatalf("ListSchedules() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	var response map[string][]ScheduleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if len(response["schedules"]) != 3 {
		t.Errorf("got %d schedules, want 3", len(response["schedules"]))
	}
}

func TestHandlers_GetSchedule(t *testing.T) {
	e := echo.New()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	handlers := NewHandlers(manager, nil)

	// Create a schedule
	ctx := context.Background()
	now := time.Now()
	schedule := &Schedule{
		ID:          "test-schedule",
		Name:        "Test Schedule",
		UserID:      "test-user",
		Status:      ScheduleStatusActive,
		ScheduledAt: &now,
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	tests := []struct {
		name       string
		scheduleID string
		wantStatus int
	}{
		{
			name:       "existing schedule",
			scheduleID: "test-schedule",
			wantStatus: http.StatusOK,
		},
		{
			name:       "non-existent schedule",
			scheduleID: "not-found",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/schedules/"+tt.scheduleID, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("id")
			c.SetParamValues(tt.scheduleID)

			err := handlers.GetSchedule(c)
			if err != nil {
				he, ok := err.(*echo.HTTPError)
				if !ok {
					t.Fatalf("unexpected error type: %T", err)
				}
				if he.Code != tt.wantStatus {
					t.Errorf("got status %d, want %d", he.Code, tt.wantStatus)
				}
				return
			}

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandlers_UpdateSchedule(t *testing.T) {
	e := echo.New()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	handlers := NewHandlers(manager, nil)

	// Create a schedule
	ctx := context.Background()
	now := time.Now()
	schedule := &Schedule{
		ID:          "test-schedule",
		Name:        "Test Schedule",
		UserID:      "test-user",
		Status:      ScheduleStatusActive,
		ScheduledAt: &now,
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Update the schedule
	updateReq := UpdateScheduleRequest{
		Name:   strPtr("Updated Schedule"),
		Status: statusPtr(ScheduleStatusPaused),
	}
	body, _ := json.Marshal(updateReq)
	req := httptest.NewRequest(http.MethodPut, "/schedules/test-schedule", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("test-schedule")

	err := handlers.UpdateSchedule(c)
	if err != nil {
		t.Fatalf("UpdateSchedule() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify the update
	updated, _ := manager.Get(ctx, "test-schedule")
	if updated.Name != "Updated Schedule" {
		t.Errorf("got name %q, want %q", updated.Name, "Updated Schedule")
	}
	if updated.Status != ScheduleStatusPaused {
		t.Errorf("got status %v, want %v", updated.Status, ScheduleStatusPaused)
	}
}

func TestHandlers_DeleteSchedule(t *testing.T) {
	e := echo.New()
	client := fake.NewSimpleClientset()
	manager := NewKubernetesManager(client, "default")
	handlers := NewHandlers(manager, nil)

	// Create a schedule
	ctx := context.Background()
	now := time.Now()
	schedule := &Schedule{
		ID:          "test-schedule",
		Name:        "Test Schedule",
		UserID:      "test-user",
		Status:      ScheduleStatusActive,
		ScheduledAt: &now,
	}
	if err := manager.Create(ctx, schedule); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/schedules/test-schedule", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("test-schedule")

	err := handlers.DeleteSchedule(c)
	if err != nil {
		t.Fatalf("DeleteSchedule() error = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify deletion
	_, getErr := manager.Get(ctx, "test-schedule")
	if getErr == nil {
		t.Error("schedule should be deleted")
	}
}

// Helper functions for tests
func timePtr(t time.Time) *time.Time {
	return &t
}

func strPtr(s string) *string {
	return &s
}

func statusPtr(s ScheduleStatus) *ScheduleStatus {
	return &s
}
