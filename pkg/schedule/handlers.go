package schedule

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxy"
)

// Handlers handles schedule management endpoints
type Handlers struct {
	manager         Manager
	sessionManager  proxy.SessionManager
	defaultTimezone string
}

// NewHandlers creates a new Handlers instance
func NewHandlers(manager Manager, sessionManager proxy.SessionManager) *Handlers {
	return &Handlers{
		manager:         manager,
		sessionManager:  sessionManager,
		defaultTimezone: "Asia/Tokyo",
	}
}

// NewHandlersWithTimezone creates a new Handlers instance with a custom default timezone
func NewHandlersWithTimezone(manager Manager, sessionManager proxy.SessionManager, defaultTimezone string) *Handlers {
	return &Handlers{
		manager:         manager,
		sessionManager:  sessionManager,
		defaultTimezone: defaultTimezone,
	}
}

// GetName returns the name of this handler for logging
func (h *Handlers) GetName() string {
	return "ScheduleHandlers"
}

// RegisterRoutes registers schedule management routes
// Implements the proxy.CustomHandler interface
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *proxy.Proxy) error {
	g := e.Group("/schedules")

	g.POST("", h.CreateSchedule)
	g.GET("", h.ListSchedules)
	g.GET("/:id", h.GetSchedule)
	g.PUT("/:id", h.UpdateSchedule)
	g.DELETE("/:id", h.DeleteSchedule)
	g.POST("/:id/trigger", h.TriggerSchedule)

	log.Printf("Registered schedule management routes")
	return nil
}

// CreateScheduleRequest represents the request body for creating a schedule
type CreateScheduleRequest struct {
	Name          string        `json:"name"`
	ScheduledAt   *time.Time    `json:"scheduled_at,omitempty"`
	CronExpr      string        `json:"cron_expr,omitempty"`
	Timezone      string        `json:"timezone,omitempty"`
	SessionConfig SessionConfig `json:"session_config"`
}

// UpdateScheduleRequest represents the request body for updating a schedule
type UpdateScheduleRequest struct {
	Name          *string         `json:"name,omitempty"`
	Status        *ScheduleStatus `json:"status,omitempty"`
	ScheduledAt   *time.Time      `json:"scheduled_at,omitempty"`
	CronExpr      *string         `json:"cron_expr,omitempty"`
	Timezone      *string         `json:"timezone,omitempty"`
	SessionConfig *SessionConfig  `json:"session_config,omitempty"`
}

// ScheduleResponse represents the response for a schedule
type ScheduleResponse struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	UserID          string           `json:"user_id"`
	Status          ScheduleStatus   `json:"status"`
	ScheduledAt     *time.Time       `json:"scheduled_at,omitempty"`
	CronExpr        string           `json:"cron_expr,omitempty"`
	Timezone        string           `json:"timezone,omitempty"`
	SessionConfig   SessionConfig    `json:"session_config"`
	NextExecutionAt *time.Time       `json:"next_execution_at,omitempty"`
	ExecutionCount  int              `json:"execution_count"`
	LastExecution   *ExecutionRecord `json:"last_execution,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// CreateSchedule handles POST /schedules
func (h *Handlers) CreateSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	var req CreateScheduleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate request
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.ScheduledAt == nil && req.CronExpr == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "either scheduled_at or cron_expr must be set")
	}

	// Validate cron expression if provided
	if req.CronExpr != "" {
		parser := NewCronParser()
		if err := parser.Validate(req.CronExpr); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid cron expression: "+err.Error())
		}
	}

	// Use default timezone if not provided
	timezone := req.Timezone
	if timezone == "" {
		timezone = h.defaultTimezone
	}

	// Validate timezone
	if _, err := time.LoadLocation(timezone); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid timezone: "+timezone)
	}

	// Get user from context
	user := auth.GetUserFromContext(c)
	var userID string
	if user != nil {
		userID = string(user.ID())
	} else {
		userID = "anonymous"
	}

	// Create schedule
	schedule := &Schedule{
		ID:            uuid.New().String(),
		Name:          req.Name,
		UserID:        userID,
		Status:        ScheduleStatusActive,
		ScheduledAt:   req.ScheduledAt,
		CronExpr:      req.CronExpr,
		Timezone:      timezone,
		SessionConfig: req.SessionConfig,
	}

	// Calculate next execution time
	nextAt, err := CalculateNextExecution(schedule, time.Now())
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to calculate next execution: "+err.Error())
	}
	schedule.NextExecutionAt = nextAt

	if err := h.manager.Create(c.Request().Context(), schedule); err != nil {
		log.Printf("Failed to create schedule: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create schedule")
	}

	log.Printf("Created schedule %s (%s) for user %s", schedule.ID, schedule.Name, userID)

	return c.JSON(http.StatusCreated, h.toResponse(schedule))
}

// ListSchedules handles GET /schedules
func (h *Handlers) ListSchedules(c echo.Context) error {
	h.setCORSHeaders(c)

	user := auth.GetUserFromContext(c)
	var userID string
	if user != nil && !user.IsAdmin() {
		userID = string(user.ID())
	}

	filter := ScheduleFilter{
		UserID: userID,
	}

	// Filter by status if provided
	if status := c.QueryParam("status"); status != "" {
		filter.Status = ScheduleStatus(status)
	}

	schedules, err := h.manager.List(c.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list schedules: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list schedules")
	}

	responses := make([]ScheduleResponse, 0, len(schedules))
	for _, s := range schedules {
		responses = append(responses, h.toResponse(s))
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"schedules": responses,
	})
}

// GetSchedule handles GET /schedules/:id
func (h *Handlers) GetSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule ID is required")
	}

	schedule, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrScheduleNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		log.Printf("Failed to get schedule %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get schedule")
	}

	// Check authorization
	if !h.userOwnsSchedule(c, schedule.UserID) {
		return echo.NewHTTPError(http.StatusForbidden, "You can only access your own schedules")
	}

	return c.JSON(http.StatusOK, h.toResponse(schedule))
}

// UpdateSchedule handles PUT /schedules/:id
func (h *Handlers) UpdateSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule ID is required")
	}

	schedule, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrScheduleNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get schedule")
	}

	// Check authorization
	if !h.userOwnsSchedule(c, schedule.UserID) {
		return echo.NewHTTPError(http.StatusForbidden, "You can only update your own schedules")
	}

	var req UpdateScheduleRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Apply updates
	if req.Name != nil {
		schedule.Name = *req.Name
	}
	if req.Status != nil {
		schedule.Status = *req.Status
	}
	if req.ScheduledAt != nil {
		schedule.ScheduledAt = req.ScheduledAt
	}
	if req.CronExpr != nil {
		// Validate new cron expression
		if *req.CronExpr != "" {
			parser := NewCronParser()
			if err := parser.Validate(*req.CronExpr); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid cron expression: "+err.Error())
			}
		}
		schedule.CronExpr = *req.CronExpr
	}
	if req.Timezone != nil {
		if *req.Timezone != "" {
			if _, err := time.LoadLocation(*req.Timezone); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "invalid timezone: "+*req.Timezone)
			}
		}
		schedule.Timezone = *req.Timezone
	}
	if req.SessionConfig != nil {
		schedule.SessionConfig = *req.SessionConfig
	}

	// Recalculate next execution if schedule changed
	if req.ScheduledAt != nil || req.CronExpr != nil || req.Status != nil {
		nextAt, err := CalculateNextExecution(schedule, time.Now())
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "failed to calculate next execution: "+err.Error())
		}
		schedule.NextExecutionAt = nextAt
	}

	if err := h.manager.Update(c.Request().Context(), schedule); err != nil {
		log.Printf("Failed to update schedule %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update schedule")
	}

	log.Printf("Updated schedule %s", id)

	return c.JSON(http.StatusOK, h.toResponse(schedule))
}

// DeleteSchedule handles DELETE /schedules/:id
func (h *Handlers) DeleteSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule ID is required")
	}

	schedule, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrScheduleNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get schedule")
	}

	// Check authorization
	if !h.userOwnsSchedule(c, schedule.UserID) {
		return echo.NewHTTPError(http.StatusForbidden, "You can only delete your own schedules")
	}

	if err := h.manager.Delete(c.Request().Context(), id); err != nil {
		log.Printf("Failed to delete schedule %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete schedule")
	}

	log.Printf("Deleted schedule %s", id)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message": "Schedule deleted successfully",
		"id":      id,
	})
}

// TriggerSchedule handles POST /schedules/:id/trigger
func (h *Handlers) TriggerSchedule(c echo.Context) error {
	h.setCORSHeaders(c)

	id := c.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Schedule ID is required")
	}

	schedule, err := h.manager.Get(c.Request().Context(), id)
	if err != nil {
		if _, ok := err.(ErrScheduleNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "Schedule not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get schedule")
	}

	// Check authorization
	if !h.userOwnsSchedule(c, schedule.UserID) {
		return echo.NewHTTPError(http.StatusForbidden, "You can only trigger your own schedules")
	}

	// Create session
	sessionID := uuid.New().String()
	req := &proxy.RunServerRequest{
		UserID:      schedule.UserID,
		Environment: schedule.SessionConfig.Environment,
		Tags:        schedule.SessionConfig.Tags,
	}
	if schedule.SessionConfig.Params != nil {
		req.InitialMessage = schedule.SessionConfig.Params.Message
		req.GithubToken = schedule.SessionConfig.Params.GithubToken
	}

	session, err := h.sessionManager.CreateSession(c.Request().Context(), sessionID, req)
	if err != nil {
		log.Printf("Failed to trigger schedule %s: %v", id, err)

		// Record failed execution
		record := ExecutionRecord{
			ExecutedAt: time.Now(),
			Status:     "failed",
			Error:      err.Error(),
		}
		_ = h.manager.RecordExecution(c.Request().Context(), id, record)

		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create session")
	}

	// Record successful execution
	record := ExecutionRecord{
		ExecutedAt: time.Now(),
		SessionID:  session.ID(),
		Status:     "success",
	}
	if err := h.manager.RecordExecution(c.Request().Context(), id, record); err != nil {
		log.Printf("Failed to record execution for schedule %s: %v", id, err)
	}

	log.Printf("Manually triggered schedule %s, created session %s", id, session.ID())

	return c.JSON(http.StatusOK, map[string]interface{}{
		"session_id":   session.ID(),
		"triggered_at": record.ExecutedAt,
	})
}

// toResponse converts a Schedule to ScheduleResponse
func (h *Handlers) toResponse(s *Schedule) ScheduleResponse {
	return ScheduleResponse{
		ID:              s.ID,
		Name:            s.Name,
		UserID:          s.UserID,
		Status:          s.Status,
		ScheduledAt:     s.ScheduledAt,
		CronExpr:        s.CronExpr,
		Timezone:        s.Timezone,
		SessionConfig:   s.SessionConfig,
		NextExecutionAt: s.NextExecutionAt,
		ExecutionCount:  s.ExecutionCount,
		LastExecution:   s.LastExecution,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

// userOwnsSchedule checks if the current user owns the schedule
func (h *Handlers) userOwnsSchedule(c echo.Context, scheduleUserID string) bool {
	user := auth.GetUserFromContext(c)
	if user == nil {
		// If no auth is configured, allow access
		cfg := auth.GetConfigFromContext(c)
		if cfg == nil || !cfg.Auth.Enabled {
			return true
		}
		return false
	}
	if user.IsAdmin() {
		return true
	}
	return string(user.ID()) == scheduleUserID
}

// setCORSHeaders sets CORS headers for all schedule endpoints
func (h *Handlers) setCORSHeaders(c echo.Context) {
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	c.Response().Header().Set("Access-Control-Max-Age", "86400")
}
