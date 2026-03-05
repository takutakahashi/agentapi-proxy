package schedule

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// Handlers handles schedule management endpoints
type Handlers struct {
	manager         Manager
	sessionManager  portrepos.SessionManager
	launcher        *sessionuc.LaunchUseCase
	defaultTimezone string
}

// NewHandlers creates a new Handlers instance
func NewHandlers(manager Manager, sessionManager portrepos.SessionManager) *Handlers {
	return &Handlers{
		manager:         manager,
		sessionManager:  sessionManager,
		launcher:        sessionuc.NewLaunchUseCase(sessionManager),
		defaultTimezone: "Asia/Tokyo",
	}
}

// NewHandlersWithTimezone creates a new Handlers instance with a custom default timezone
func NewHandlersWithTimezone(manager Manager, sessionManager portrepos.SessionManager, defaultTimezone string) *Handlers {
	return &Handlers{
		manager:         manager,
		sessionManager:  sessionManager,
		launcher:        sessionuc.NewLaunchUseCase(sessionManager),
		defaultTimezone: defaultTimezone,
	}
}

// GetName returns the name of this handler for logging
func (h *Handlers) GetName() string {
	return "ScheduleHandlers"
}

// RegisterRoutes registers schedule management routes
// Implements the app.CustomHandler interface
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
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
	Name          string                 `json:"name"`
	Scope         entities.ResourceScope `json:"scope,omitempty"`
	TeamID        string                 `json:"team_id,omitempty"`
	ScheduledAt   *time.Time             `json:"scheduled_at,omitempty"`
	CronExpr      string                 `json:"cron_expr,omitempty"`
	Timezone      string                 `json:"timezone,omitempty"`
	SessionConfig SessionConfig          `json:"session_config"`
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
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	UserID          string                 `json:"user_id"`
	Scope           entities.ResourceScope `json:"scope,omitempty"`
	TeamID          string                 `json:"team_id,omitempty"`
	Status          ScheduleStatus         `json:"status"`
	ScheduledAt     *time.Time             `json:"scheduled_at,omitempty"`
	CronExpr        string                 `json:"cron_expr,omitempty"`
	Timezone        string                 `json:"timezone,omitempty"`
	SessionConfig   SessionConfig          `json:"session_config"`
	NextExecutionAt *time.Time             `json:"next_execution_at,omitempty"`
	ExecutionCount  int                    `json:"execution_count"`
	LastExecution   *ExecutionRecord       `json:"last_execution,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
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

	// Validate team scope: user must be a member of the team
	if req.Scope == entities.ScopeTeam {
		if req.TeamID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if user == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required for team-scoped schedules")
		}
		if !user.IsMemberOfTeam(req.TeamID) {
			log.Printf("User %s is not a member of team %s", userID, req.TeamID)
			return echo.NewHTTPError(http.StatusForbidden, "You are not a member of this team")
		}
	}

	// For user-scoped schedules, auto-populate github_token from auth header if not provided
	sessionConfig := req.SessionConfig
	if req.Scope != entities.ScopeTeam {
		if sessionConfig.Params == nil {
			sessionConfig.Params = &entities.SessionParams{}
		}
		if sessionConfig.Params.GithubToken == "" {
			if cfg := auth.GetConfigFromContext(c); cfg != nil && cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
				tokenHeader := c.Request().Header.Get(cfg.Auth.GitHub.TokenHeader)
				if token := auth.ExtractTokenFromHeader(tokenHeader); token != "" {
					sessionConfig.Params.GithubToken = token
				}
			}
		}
	}

	// For user-scoped schedules, capture the creator's team memberships so that
	// the background worker can inject team-level settings (Bedrock, MCP, etc.)
	// when executing the schedule without an HTTP auth context.
	var userTeams []string
	if req.Scope != entities.ScopeTeam {
		if azCtx := auth.GetAuthorizationContext(c); azCtx != nil {
			userTeams = azCtx.TeamScope.Teams
		}
	}

	// Create schedule
	schedule := &Schedule{
		ID:            uuid.New().String(),
		Name:          req.Name,
		UserID:        userID,
		Scope:         req.Scope,
		TeamID:        req.TeamID,
		UserTeams:     userTeams,
		Status:        ScheduleStatusActive,
		ScheduledAt:   req.ScheduledAt,
		CronExpr:      req.CronExpr,
		Timezone:      timezone,
		SessionConfig: sessionConfig,
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
	scopeFilter := c.QueryParam("scope")
	teamIDFilter := c.QueryParam("team_id")

	var userID string
	var userTeamIDs []string
	if user != nil {
		userID = string(user.ID())
		// Extract user's team IDs for filtering team-scoped schedules
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				userTeamIDs = append(userTeamIDs, teamSlug)
			}
		}
	}

	// Build filter (without UserID to get all schedules, we'll filter by authorization below)
	filter := ScheduleFilter{
		Scope:   entities.ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}

	// Filter by status if provided
	if status := c.QueryParam("status"); status != "" {
		filter.Status = ScheduleStatus(status)
	}

	// For non-team scope, always filter by user ID - even admins should not see
	// other users' personal schedules. Admin privileges apply to team-scoped resources only.
	if user != nil && scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	schedules, err := h.manager.List(c.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list schedules: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list schedules")
	}

	// Filter by user authorization (supports both user-scoped and team-scoped)
	// IMPORTANT: Resources are isolated by scope - team scope resources are only visible
	// when explicitly filtering by scope=team, and user scope resources are only visible
	// when filtering by scope=user or no scope filter (default to user scope)
	responses := make([]ScheduleResponse, 0, len(schedules))
	for _, s := range schedules {

		// Scope isolation: resources are only visible within their respective scope
		// - scope=team filter: only show team-scoped resources
		// - scope=user filter or no filter: only show user-scoped resources
		scheduleScope := s.GetScope() // Use GetScope() to handle default value
		if scopeFilter == string(entities.ScopeTeam) {
			// Only show team-scoped schedules
			if scheduleScope != entities.ScopeTeam {
				continue
			}
		} else {
			// Default to user scope: only show user-scoped schedules
			if scheduleScope == entities.ScopeTeam {
				continue
			}
		}

		// Admin can access all team-scoped schedules; user-scoped requires ownership.
		// Admin privileges do not extend to other users' personal resources.
		if scheduleScope == entities.ScopeTeam {
			// Team-scoped: admin or team member can see
			if user != nil && (user.IsAdmin() || user.IsMemberOfTeam(s.TeamID)) {
				responses = append(responses, h.toResponse(s))
			}
		} else {
			// User-scoped: only owner can see, regardless of admin status
			if user != nil && s.UserID == string(user.ID()) {
				responses = append(responses, h.toResponse(s))
			}
		}
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
	if !h.userCanAccessSchedule(c, schedule) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this schedule")
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
	if !h.userCanAccessSchedule(c, schedule) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to update this schedule")
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

	// Refresh UserTeams from the current auth context so that team membership
	// changes since schedule creation are picked up.
	if schedule.GetScope() != entities.ScopeTeam {
		if azCtx := auth.GetAuthorizationContext(c); azCtx != nil {
			schedule.UserTeams = azCtx.TeamScope.Teams
		}
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
	if !h.userCanAccessSchedule(c, schedule) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this schedule")
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
	if !h.userCanAccessSchedule(c, schedule) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to trigger this schedule")
	}

	// Build the launch request for this manual trigger.
	// For the Teams field we prefer the live auth context (most up-to-date team
	// memberships), falling back to the memberships captured at schedule creation time.
	scheduleScope := schedule.GetScope() // Use GetScope() to handle default value
	sessionID := uuid.New().String()

	// Resolve user teams: prefer live auth context, fall back to saved UserTeams
	var userTeams []string
	authzCtx := auth.GetAuthorizationContext(c)
	if authzCtx != nil {
		userTeams = authzCtx.TeamScope.Teams
	} else {
		userTeams = schedule.UserTeams
	}
	teams := sessionuc.ResolveTeams(scheduleScope, schedule.TeamID, userTeams)

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
		// For team-scoped schedules, do not use the creator's github_token
		if scheduleScope != entities.ScopeTeam {
			githubToken = schedule.SessionConfig.Params.GithubToken
		}
		agentType = schedule.SessionConfig.Params.AgentType
		slackParams = schedule.SessionConfig.Params.Slack
		oneshot = schedule.SessionConfig.Params.Oneshot
	}

	result, err := h.launcher.Launch(c.Request().Context(), sessionID, sessionuc.LaunchRequest{
		UserID:         schedule.UserID,
		Scope:          scheduleScope,
		TeamID:         schedule.TeamID,
		Teams:          teams,
		Environment:    schedule.SessionConfig.Environment,
		Tags:           tags,
		InitialMessage: initialMessage,
		GithubToken:    githubToken,
		AgentType:      agentType,
		SlackParams:    slackParams,
		Oneshot:        oneshot,
		RepoInfo:       app.ExtractRepositoryInfo(tags, sessionID),
	})
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
		SessionID:  result.SessionID,
		Status:     "success",
	}
	if err := h.manager.RecordExecution(c.Request().Context(), id, record); err != nil {
		log.Printf("Failed to record execution for schedule %s: %v", id, err)
	}

	log.Printf("Manually triggered schedule %s, created session %s", id, result.SessionID)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"session_id":   result.SessionID,
		"triggered_at": record.ExecutedAt,
	})
}

// toResponse converts a Schedule to ScheduleResponse
func (h *Handlers) toResponse(s *Schedule) ScheduleResponse {
	return ScheduleResponse{
		ID:              s.ID,
		Name:            s.Name,
		UserID:          s.UserID,
		Scope:           s.GetScope(), // Use GetScope() to handle default value
		TeamID:          s.TeamID,
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

// userCanAccessSchedule checks if the current user can access the schedule
// For team-scoped schedules, all team members have access
// For user-scoped schedules, only the owner has access
func (h *Handlers) userCanAccessSchedule(c echo.Context, schedule *Schedule) bool {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return false
	}
	return user.CanAccessResource(
		entities.UserID(schedule.UserID),
		string(schedule.GetScope()), // Use GetScope() to handle default value
		schedule.TeamID,
	)
}

// setCORSHeaders sets CORS headers for all schedule endpoints
func (h *Handlers) setCORSHeaders(c echo.Context) {
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	c.Response().Header().Set("Access-Control-Max-Age", "86400")
}
