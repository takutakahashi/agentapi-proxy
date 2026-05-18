package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpusecases "github.com/takutakahashi/agentapi-proxy/internal/usecases/mcp"
)

// --- Schedule Tool Input/Output types ---

type ListSchedulesToolInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Status string `json:"status,omitempty"`
}

type ScheduleSessionParamsOutput struct {
	Message      string `json:"message,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	Oneshot      bool   `json:"oneshot,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
}

type ScheduleSessionConfigOutput struct {
	Environment  map[string]string            `json:"environment,omitempty"`
	Tags         map[string]string            `json:"tags,omitempty"`
	Params       *ScheduleSessionParamsOutput `json:"params,omitempty"`
	MemoryKey    map[string]string            `json:"memory_key,omitempty"`
	ReuseSession bool                         `json:"reuse_session,omitempty"`
	ReuseMessage string                       `json:"reuse_message,omitempty"`
}

type ScheduleExecutionOutput struct {
	ExecutedAt    time.Time `json:"executed_at"`
	SessionID     string    `json:"session_id,omitempty"`
	Status        string    `json:"status"`
	Error         string    `json:"error,omitempty"`
	SessionReused bool      `json:"session_reused,omitempty"`
}

type ScheduleOutput struct {
	ID              string                      `json:"id"`
	Name            string                      `json:"name"`
	UserID          string                      `json:"user_id"`
	Scope           string                      `json:"scope"`
	TeamID          string                      `json:"team_id,omitempty"`
	Status          string                      `json:"status"`
	ScheduledAt     *time.Time                  `json:"scheduled_at,omitempty"`
	CronExpr        string                      `json:"cron_expr,omitempty"`
	Timezone        string                      `json:"timezone,omitempty"`
	SessionConfig   ScheduleSessionConfigOutput `json:"session_config"`
	LastExecution   *ScheduleExecutionOutput    `json:"last_execution,omitempty"`
	NextExecutionAt *time.Time                  `json:"next_execution_at,omitempty"`
	ExecutionCount  int                         `json:"execution_count"`
	CreatedAt       time.Time                   `json:"created_at"`
	UpdatedAt       time.Time                   `json:"updated_at"`
}

type ListSchedulesToolOutput struct {
	Schedules []ScheduleOutput `json:"schedules"`
	Total     int              `json:"total"`
}

type GetScheduleToolInput struct {
	ScheduleID string `json:"schedule_id"`
}

type GetScheduleToolOutput struct {
	Schedule ScheduleOutput `json:"schedule"`
}

type CreateScheduleToolInput struct {
	Name          string                      `json:"name"`
	Scope         string                      `json:"scope,omitempty"`
	TeamID        string                      `json:"team_id,omitempty"`
	ScheduledAt   *time.Time                  `json:"scheduled_at,omitempty"`
	CronExpr      string                      `json:"cron_expr,omitempty"`
	Timezone      string                      `json:"timezone,omitempty"`
	SessionConfig ScheduleSessionConfigOutput `json:"session_config"`
}

type CreateScheduleToolOutput struct {
	Schedule ScheduleOutput `json:"schedule"`
}

type UpdateScheduleToolInput struct {
	ScheduleID    string                       `json:"schedule_id"`
	Name          *string                      `json:"name,omitempty"`
	Status        *string                      `json:"status,omitempty"`
	ScheduledAt   *time.Time                   `json:"scheduled_at,omitempty"`
	CronExpr      *string                      `json:"cron_expr,omitempty"`
	Timezone      *string                      `json:"timezone,omitempty"`
	SessionConfig *ScheduleSessionConfigOutput `json:"session_config,omitempty"`
}

type UpdateScheduleToolOutput struct {
	Schedule ScheduleOutput `json:"schedule"`
}

type DeleteScheduleToolInput struct {
	ScheduleID string `json:"schedule_id"`
}

type DeleteScheduleToolOutput struct {
	Message    string `json:"message"`
	ScheduleID string `json:"schedule_id"`
}

// --- Conversion helpers ---

func toScheduleOutput(info mcpusecases.ScheduleInfo) ScheduleOutput {
	out := ScheduleOutput{
		ID:              info.ID,
		Name:            info.Name,
		UserID:          info.UserID,
		Scope:           info.Scope,
		TeamID:          info.TeamID,
		Status:          info.Status,
		ScheduledAt:     info.ScheduledAt,
		CronExpr:        info.CronExpr,
		Timezone:        info.Timezone,
		ExecutionCount:  info.ExecutionCount,
		NextExecutionAt: info.NextExecutionAt,
		CreatedAt:       info.CreatedAt,
		UpdatedAt:       info.UpdatedAt,
		SessionConfig:   toScheduleSessionConfigOutput(info.SessionConfig),
	}
	if info.LastExecution != nil {
		exec := ScheduleExecutionOutput{
			ExecutedAt:    info.LastExecution.ExecutedAt,
			SessionID:     info.LastExecution.SessionID,
			Status:        info.LastExecution.Status,
			Error:         info.LastExecution.Error,
			SessionReused: info.LastExecution.SessionReused,
		}
		out.LastExecution = &exec
	}
	return out
}

func toScheduleSessionConfigOutput(info mcpusecases.ScheduleSessionConfigInfo) ScheduleSessionConfigOutput {
	out := ScheduleSessionConfigOutput{
		Environment:  info.Environment,
		Tags:         info.Tags,
		MemoryKey:    info.MemoryKey,
		ReuseSession: info.ReuseSession,
		ReuseMessage: info.ReuseMessage,
	}
	if info.Params != nil {
		out.Params = &ScheduleSessionParamsOutput{
			Message:      info.Params.Message,
			AgentType:    info.Params.AgentType,
			Oneshot:      info.Params.Oneshot,
			RepoFullName: info.Params.RepoFullName,
		}
	}
	return out
}

func fromScheduleSessionConfigOutput(out ScheduleSessionConfigOutput) mcpusecases.ScheduleSessionConfigInfo {
	info := mcpusecases.ScheduleSessionConfigInfo{
		Environment:  out.Environment,
		Tags:         out.Tags,
		MemoryKey:    out.MemoryKey,
		ReuseSession: out.ReuseSession,
		ReuseMessage: out.ReuseMessage,
	}
	if out.Params != nil {
		info.Params = &mcpusecases.ScheduleSessionParams{
			Message:      out.Params.Message,
			AgentType:    out.Params.AgentType,
			Oneshot:      out.Params.Oneshot,
			RepoFullName: out.Params.RepoFullName,
		}
	}
	return info
}

// --- Schedule Tool Handlers ---

func (s *MCPServer) handleListSchedules(ctx context.Context, req *mcp.CallToolRequest, input ListSchedulesToolInput) (*mcp.CallToolResult, ListSchedulesToolOutput, error) {
	if s.scheduleUseCase == nil {
		return nil, ListSchedulesToolOutput{}, fmt.Errorf("schedule tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, ListSchedulesToolOutput{}, fmt.Errorf("authentication required")
	}

	schedules, err := s.scheduleUseCase.ListSchedules(ctx, mcpusecases.ListSchedulesInput{
		Scope:  input.Scope,
		TeamID: input.TeamID,
		Status: input.Status,
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListSchedulesToolOutput{}, fmt.Errorf("failed to list schedules: %w", err)
	}

	out := ListSchedulesToolOutput{
		Schedules: make([]ScheduleOutput, 0, len(schedules)),
		Total:     len(schedules),
	}
	for _, sc := range schedules {
		out.Schedules = append(out.Schedules, toScheduleOutput(sc))
	}
	return nil, out, nil
}

func (s *MCPServer) handleGetSchedule(ctx context.Context, req *mcp.CallToolRequest, input GetScheduleToolInput) (*mcp.CallToolResult, GetScheduleToolOutput, error) {
	if s.scheduleUseCase == nil {
		return nil, GetScheduleToolOutput{}, fmt.Errorf("schedule tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, GetScheduleToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.ScheduleID == "" {
		return nil, GetScheduleToolOutput{}, fmt.Errorf("schedule_id is required")
	}

	info, err := s.scheduleUseCase.GetSchedule(ctx, input.ScheduleID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, GetScheduleToolOutput{}, fmt.Errorf("failed to get schedule: %w", err)
	}

	return nil, GetScheduleToolOutput{Schedule: toScheduleOutput(*info)}, nil
}

func (s *MCPServer) handleCreateSchedule(ctx context.Context, req *mcp.CallToolRequest, input CreateScheduleToolInput) (*mcp.CallToolResult, CreateScheduleToolOutput, error) {
	if s.scheduleUseCase == nil {
		return nil, CreateScheduleToolOutput{}, fmt.Errorf("schedule tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, CreateScheduleToolOutput{}, fmt.Errorf("authentication required")
	}

	info, err := s.scheduleUseCase.CreateSchedule(ctx, mcpusecases.CreateScheduleInput{
		Name:          input.Name,
		Scope:         input.Scope,
		TeamID:        input.TeamID,
		ScheduledAt:   input.ScheduledAt,
		CronExpr:      input.CronExpr,
		Timezone:      input.Timezone,
		SessionConfig: fromScheduleSessionConfigOutput(input.SessionConfig),
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, CreateScheduleToolOutput{}, fmt.Errorf("failed to create schedule: %w", err)
	}

	return nil, CreateScheduleToolOutput{Schedule: toScheduleOutput(*info)}, nil
}

func (s *MCPServer) handleUpdateSchedule(ctx context.Context, req *mcp.CallToolRequest, input UpdateScheduleToolInput) (*mcp.CallToolResult, UpdateScheduleToolOutput, error) {
	if s.scheduleUseCase == nil {
		return nil, UpdateScheduleToolOutput{}, fmt.Errorf("schedule tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, UpdateScheduleToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.ScheduleID == "" {
		return nil, UpdateScheduleToolOutput{}, fmt.Errorf("schedule_id is required")
	}

	var sessionConfig *mcpusecases.ScheduleSessionConfigInfo
	if input.SessionConfig != nil {
		cfg := fromScheduleSessionConfigOutput(*input.SessionConfig)
		sessionConfig = &cfg
	}

	info, err := s.scheduleUseCase.UpdateSchedule(ctx, input.ScheduleID, mcpusecases.UpdateScheduleInput{
		Name:          input.Name,
		Status:        input.Status,
		ScheduledAt:   input.ScheduledAt,
		CronExpr:      input.CronExpr,
		Timezone:      input.Timezone,
		SessionConfig: sessionConfig,
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, UpdateScheduleToolOutput{}, fmt.Errorf("failed to update schedule: %w", err)
	}

	return nil, UpdateScheduleToolOutput{Schedule: toScheduleOutput(*info)}, nil
}

func (s *MCPServer) handleDeleteSchedule(ctx context.Context, req *mcp.CallToolRequest, input DeleteScheduleToolInput) (*mcp.CallToolResult, DeleteScheduleToolOutput, error) {
	if s.scheduleUseCase == nil {
		return nil, DeleteScheduleToolOutput{}, fmt.Errorf("schedule tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, DeleteScheduleToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.ScheduleID == "" {
		return nil, DeleteScheduleToolOutput{}, fmt.Errorf("schedule_id is required")
	}

	if err := s.scheduleUseCase.DeleteSchedule(ctx, input.ScheduleID, s.authenticatedUserID, s.authenticatedTeams); err != nil {
		return nil, DeleteScheduleToolOutput{}, fmt.Errorf("failed to delete schedule: %w", err)
	}

	return nil, DeleteScheduleToolOutput{
		Message:    "Schedule deleted successfully",
		ScheduleID: input.ScheduleID,
	}, nil
}
