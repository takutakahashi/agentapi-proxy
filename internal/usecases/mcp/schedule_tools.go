package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
)

// MCPScheduleToolsUseCase provides use cases for MCP schedule tools
type MCPScheduleToolsUseCase struct {
	scheduleManager schedule.Manager
}

// NewMCPScheduleToolsUseCase creates a new MCPScheduleToolsUseCase
func NewMCPScheduleToolsUseCase(mgr schedule.Manager) *MCPScheduleToolsUseCase {
	return &MCPScheduleToolsUseCase{scheduleManager: mgr}
}

// ScheduleSessionConfigInfo represents session configuration for a schedule
type ScheduleSessionConfigInfo struct {
	Environment  map[string]string      `json:"environment,omitempty"`
	Tags         map[string]string      `json:"tags,omitempty"`
	Params       *ScheduleSessionParams `json:"params,omitempty"`
	MemoryKey    map[string]string      `json:"memory_key,omitempty"`
	ReuseSession bool                   `json:"reuse_session,omitempty"`
	ReuseMessage string                 `json:"reuse_message,omitempty"`
}

// ScheduleSessionParams represents session params for a schedule
type ScheduleSessionParams struct {
	Message      string `json:"message,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	Oneshot      bool   `json:"oneshot,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
}

// ScheduleExecutionInfo represents a single execution record
type ScheduleExecutionInfo struct {
	ExecutedAt    time.Time `json:"executed_at"`
	SessionID     string    `json:"session_id,omitempty"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	SessionReused bool      `json:"session_reused,omitempty"`
}

// ScheduleInfo represents schedule information returned by MCP tools
type ScheduleInfo struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	UserID          string                    `json:"user_id"`
	Scope           string                    `json:"scope"`
	TeamID          string                    `json:"team_id,omitempty"`
	Status          string                    `json:"status"`
	ScheduledAt     *time.Time                `json:"scheduled_at,omitempty"`
	CronExpr        string                    `json:"cron_expr,omitempty"`
	Timezone        string                    `json:"timezone,omitempty"`
	SessionConfig   ScheduleSessionConfigInfo `json:"session_config"`
	LastExecution   *ScheduleExecutionInfo    `json:"last_execution,omitempty"`
	NextExecutionAt *time.Time                `json:"next_execution_at,omitempty"`
	ExecutionCount  int                       `json:"execution_count"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

// ListSchedulesInput represents input for listing schedules
type ListSchedulesInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Status string `json:"status,omitempty"`
}

// CreateScheduleInput represents input for creating a schedule
type CreateScheduleInput struct {
	Name          string                    `json:"name"`
	Scope         string                    `json:"scope,omitempty"`
	TeamID        string                    `json:"team_id,omitempty"`
	ScheduledAt   *time.Time                `json:"scheduled_at,omitempty"`
	CronExpr      string                    `json:"cron_expr,omitempty"`
	Timezone      string                    `json:"timezone,omitempty"`
	SessionConfig ScheduleSessionConfigInfo `json:"session_config"`
}

// UpdateScheduleInput represents input for updating a schedule
type UpdateScheduleInput struct {
	Name          *string                    `json:"name,omitempty"`
	Status        *string                    `json:"status,omitempty"`
	ScheduledAt   *time.Time                 `json:"scheduled_at,omitempty"`
	CronExpr      *string                    `json:"cron_expr,omitempty"`
	Timezone      *string                    `json:"timezone,omitempty"`
	SessionConfig *ScheduleSessionConfigInfo `json:"session_config,omitempty"`
}

func toScheduleInfo(s *schedule.Schedule) ScheduleInfo {
	info := ScheduleInfo{
		ID:              s.ID,
		Name:            s.Name,
		UserID:          s.UserID,
		Scope:           string(s.Scope),
		TeamID:          s.TeamID,
		Status:          string(s.Status),
		ScheduledAt:     s.ScheduledAt,
		CronExpr:        s.CronExpr,
		Timezone:        s.Timezone,
		ExecutionCount:  s.ExecutionCount,
		NextExecutionAt: s.NextExecutionAt,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
		SessionConfig:   toScheduleSessionConfigInfo(s.SessionConfig),
	}
	if s.LastExecution != nil {
		exec := toScheduleExecutionInfo(*s.LastExecution)
		info.LastExecution = &exec
	}
	return info
}

func toScheduleSessionConfigInfo(sc schedule.SessionConfig) ScheduleSessionConfigInfo {
	info := ScheduleSessionConfigInfo{
		Environment:  sc.Environment,
		Tags:         sc.Tags,
		MemoryKey:    sc.MemoryKey,
		ReuseSession: sc.ReuseSession,
		ReuseMessage: sc.ReuseMessage,
	}
	if sc.Params != nil {
		info.Params = &ScheduleSessionParams{
			Message:      sc.Params.Message,
			AgentType:    sc.Params.AgentType,
			Oneshot:      sc.Params.Oneshot,
			RepoFullName: sc.Params.RepoFullName,
		}
	}
	return info
}

func toScheduleExecutionInfo(e schedule.ExecutionRecord) ScheduleExecutionInfo {
	return ScheduleExecutionInfo{
		ExecutedAt:    e.ExecutedAt,
		SessionID:     e.SessionID,
		Status:        e.Status,
		Error:         e.Error,
		SessionReused: e.SessionReused,
	}
}

func fromScheduleSessionConfigInfo(info ScheduleSessionConfigInfo) schedule.SessionConfig {
	sc := schedule.SessionConfig{
		Environment:  info.Environment,
		Tags:         info.Tags,
		MemoryKey:    info.MemoryKey,
		ReuseSession: info.ReuseSession,
		ReuseMessage: info.ReuseMessage,
	}
	if info.Params != nil {
		sc.Params = &entities.SessionParams{
			Message:      info.Params.Message,
			AgentType:    info.Params.AgentType,
			Oneshot:      info.Params.Oneshot,
			RepoFullName: info.Params.RepoFullName,
		}
	}
	return sc
}

func canAccessSchedule(s *schedule.Schedule, requestingUserID string, teamIDs []string) bool {
	switch s.Scope {
	case entities.ScopeUser:
		return s.UserID == requestingUserID
	case entities.ScopeTeam:
		return containsTeam(teamIDs, s.TeamID)
	default:
		return false
	}
}

// ListSchedules lists schedules for the requesting user
func (uc *MCPScheduleToolsUseCase) ListSchedules(ctx context.Context, input ListSchedulesInput, requestingUserID string, teamIDs []string) ([]ScheduleInfo, error) {
	if uc.scheduleManager == nil {
		return nil, fmt.Errorf("schedule manager not available")
	}

	var schedules []*schedule.Schedule

	switch entities.ResourceScope(input.Scope) {
	case entities.ScopeUser:
		result, err := uc.scheduleManager.List(ctx, schedule.ScheduleFilter{
			UserID: requestingUserID,
			Scope:  entities.ScopeUser,
			Status: schedule.ScheduleStatus(input.Status),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list schedules: %w", err)
		}
		schedules = result

	case entities.ScopeTeam:
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
		result, err := uc.scheduleManager.List(ctx, schedule.ScheduleFilter{
			Scope:  entities.ScopeTeam,
			TeamID: input.TeamID,
			Status: schedule.ScheduleStatus(input.Status),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list schedules: %w", err)
		}
		schedules = result

	default:
		userResult, err := uc.scheduleManager.List(ctx, schedule.ScheduleFilter{
			UserID: requestingUserID,
			Scope:  entities.ScopeUser,
			Status: schedule.ScheduleStatus(input.Status),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list user schedules: %w", err)
		}
		var teamResult []*schedule.Schedule
		if len(teamIDs) > 0 {
			teamResult, err = uc.scheduleManager.List(ctx, schedule.ScheduleFilter{
				TeamIDs: teamIDs,
				Scope:   entities.ScopeTeam,
				Status:  schedule.ScheduleStatus(input.Status),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to list team schedules: %w", err)
			}
		}
		schedules = append(userResult, teamResult...)
	}

	result := make([]ScheduleInfo, 0, len(schedules))
	for _, s := range schedules {
		result = append(result, toScheduleInfo(s))
	}
	return result, nil
}

// GetSchedule retrieves a schedule by ID
func (uc *MCPScheduleToolsUseCase) GetSchedule(ctx context.Context, scheduleID, requestingUserID string, teamIDs []string) (*ScheduleInfo, error) {
	if uc.scheduleManager == nil {
		return nil, fmt.Errorf("schedule manager not available")
	}

	s, err := uc.scheduleManager.Get(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("schedule not found: %w", err)
	}

	if !canAccessSchedule(s, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	info := toScheduleInfo(s)
	return &info, nil
}

// CreateSchedule creates a new schedule
func (uc *MCPScheduleToolsUseCase) CreateSchedule(ctx context.Context, input CreateScheduleInput, requestingUserID string, teamIDs []string) (*ScheduleInfo, error) {
	if uc.scheduleManager == nil {
		return nil, fmt.Errorf("schedule manager not available")
	}

	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if input.CronExpr == "" && input.ScheduledAt == nil {
		return nil, fmt.Errorf("either cron_expr or scheduled_at is required")
	}

	scope := entities.ResourceScope(input.Scope)
	if scope == "" {
		scope = entities.ScopeUser
	}
	if scope != entities.ScopeUser && scope != entities.ScopeTeam {
		return nil, fmt.Errorf("scope must be 'user' or 'team'")
	}
	if scope == entities.ScopeTeam {
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
	}

	s := &schedule.Schedule{
		ID:            uuid.New().String(),
		Name:          input.Name,
		UserID:        requestingUserID,
		Scope:         scope,
		TeamID:        input.TeamID,
		Status:        schedule.ScheduleStatusActive,
		ScheduledAt:   input.ScheduledAt,
		CronExpr:      input.CronExpr,
		Timezone:      input.Timezone,
		SessionConfig: fromScheduleSessionConfigInfo(input.SessionConfig),
		UserTeams:     teamIDs,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := uc.scheduleManager.Create(ctx, s); err != nil {
		return nil, fmt.Errorf("failed to create schedule: %w", err)
	}

	info := toScheduleInfo(s)
	return &info, nil
}

// UpdateSchedule updates an existing schedule
func (uc *MCPScheduleToolsUseCase) UpdateSchedule(ctx context.Context, scheduleID string, input UpdateScheduleInput, requestingUserID string, teamIDs []string) (*ScheduleInfo, error) {
	if uc.scheduleManager == nil {
		return nil, fmt.Errorf("schedule manager not available")
	}

	s, err := uc.scheduleManager.Get(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("schedule not found: %w", err)
	}

	if !canAccessSchedule(s, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	if input.Name != nil {
		if *input.Name == "" {
			return nil, fmt.Errorf("name cannot be empty")
		}
		s.Name = *input.Name
	}
	if input.Status != nil {
		s.Status = schedule.ScheduleStatus(*input.Status)
	}
	if input.ScheduledAt != nil {
		s.ScheduledAt = input.ScheduledAt
	}
	if input.CronExpr != nil {
		s.CronExpr = *input.CronExpr
	}
	if input.Timezone != nil {
		s.Timezone = *input.Timezone
	}
	if input.SessionConfig != nil {
		s.SessionConfig = fromScheduleSessionConfigInfo(*input.SessionConfig)
	}
	s.UpdatedAt = time.Now()

	if err := uc.scheduleManager.Update(ctx, s); err != nil {
		return nil, fmt.Errorf("failed to update schedule: %w", err)
	}

	info := toScheduleInfo(s)
	return &info, nil
}

// DeleteSchedule deletes a schedule
func (uc *MCPScheduleToolsUseCase) DeleteSchedule(ctx context.Context, scheduleID, requestingUserID string, teamIDs []string) error {
	if uc.scheduleManager == nil {
		return fmt.Errorf("schedule manager not available")
	}

	s, err := uc.scheduleManager.Get(ctx, scheduleID)
	if err != nil {
		return fmt.Errorf("schedule not found: %w", err)
	}

	if !canAccessSchedule(s, requestingUserID, teamIDs) {
		return fmt.Errorf("access denied")
	}

	return uc.scheduleManager.Delete(ctx, scheduleID)
}
