package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpusecases "github.com/takutakahashi/agentapi-proxy/internal/usecases/mcp"
)

// --- Task Tool Input/Output types ---

// ListTasksToolInput represents input for list_tasks tool
type ListTasksToolInput struct {
	Scope    string `json:"scope,omitempty"`
	TeamID   string `json:"team_id,omitempty"`
	GroupID  string `json:"group_id,omitempty"`
	Status   string `json:"status,omitempty"`
	TaskType string `json:"task_type,omitempty"`
}

// TaskLinkOutput represents a link in a task response
type TaskLinkOutput struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// TaskOutput represents a task in the response
type TaskOutput struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Description string           `json:"description,omitempty"`
	Status      string           `json:"status"`
	TaskType    string           `json:"task_type"`
	Scope       string           `json:"scope"`
	OwnerID     string           `json:"owner_id"`
	TeamID      string           `json:"team_id,omitempty"`
	GroupID     string           `json:"group_id,omitempty"`
	SessionID   string           `json:"session_id,omitempty"`
	Links       []TaskLinkOutput `json:"links"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// ListTasksToolOutput represents output for list_tasks tool
type ListTasksToolOutput struct {
	Tasks []TaskOutput `json:"tasks"`
	Total int          `json:"total"`
}

// GetTaskToolInput represents input for get_task tool
type GetTaskToolInput struct {
	TaskID string `json:"task_id"`
}

// GetTaskToolOutput represents output for get_task tool
type GetTaskToolOutput struct {
	Task TaskOutput `json:"task"`
}

// TaskLinkInput represents a link in a task creation/update request
type TaskLinkInput struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// CreateTaskToolInput represents input for create_task tool
type CreateTaskToolInput struct {
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	TaskType    string          `json:"task_type"`
	Scope       string          `json:"scope"`
	TeamID      string          `json:"team_id,omitempty"`
	GroupID     string          `json:"group_id,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Links       []TaskLinkInput `json:"links,omitempty"`
}

// CreateTaskToolOutput represents output for create_task tool
type CreateTaskToolOutput struct {
	Task TaskOutput `json:"task"`
}

// UpdateTaskToolInput represents input for update_task tool
type UpdateTaskToolInput struct {
	TaskID      string           `json:"task_id"`
	Title       *string          `json:"title,omitempty"`
	Description *string          `json:"description,omitempty"`
	Status      *string          `json:"status,omitempty"`
	GroupID     *string          `json:"group_id,omitempty"`
	SessionID   *string          `json:"session_id,omitempty"`
	Links       *[]TaskLinkInput `json:"links,omitempty"`
}

// UpdateTaskToolOutput represents output for update_task tool
type UpdateTaskToolOutput struct {
	Task TaskOutput `json:"task"`
}

// DeleteTaskToolInput represents input for delete_task tool
type DeleteTaskToolInput struct {
	TaskID string `json:"task_id"`
}

// DeleteTaskToolOutput represents output for delete_task tool
type DeleteTaskToolOutput struct {
	Message string `json:"message"`
	TaskID  string `json:"task_id"`
}

// --- Task Group Tool Input/Output types ---

// ListTaskGroupsToolInput represents input for list_task_groups tool
type ListTaskGroupsToolInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
}

// TaskGroupOutput represents a task group in the response
type TaskGroupOutput struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Scope       string    `json:"scope"`
	OwnerID     string    `json:"owner_id"`
	TeamID      string    `json:"team_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListTaskGroupsToolOutput represents output for list_task_groups tool
type ListTaskGroupsToolOutput struct {
	TaskGroups []TaskGroupOutput `json:"task_groups"`
	Total      int               `json:"total"`
}

// CreateTaskGroupToolInput represents input for create_task_group tool
type CreateTaskGroupToolInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
	TeamID      string `json:"team_id,omitempty"`
}

// CreateTaskGroupToolOutput represents output for create_task_group tool
type CreateTaskGroupToolOutput struct {
	TaskGroup TaskGroupOutput `json:"task_group"`
}

// DeleteTaskGroupToolInput represents input for delete_task_group tool
type DeleteTaskGroupToolInput struct {
	GroupID string `json:"group_id"`
}

// DeleteTaskGroupToolOutput represents output for delete_task_group tool
type DeleteTaskGroupToolOutput struct {
	Message string `json:"message"`
	GroupID string `json:"group_id"`
}

// --- Helper conversion functions ---

func toTaskOutput(info mcpusecases.TaskInfo) TaskOutput {
	links := make([]TaskLinkOutput, 0, len(info.Links))
	for _, l := range info.Links {
		links = append(links, TaskLinkOutput{
			ID:    l.ID,
			URL:   l.URL,
			Title: l.Title,
		})
	}
	return TaskOutput{
		ID:          info.ID,
		Title:       info.Title,
		Description: info.Description,
		Status:      info.Status,
		TaskType:    info.TaskType,
		Scope:       info.Scope,
		OwnerID:     info.OwnerID,
		TeamID:      info.TeamID,
		GroupID:     info.GroupID,
		SessionID:   info.SessionID,
		Links:       links,
		CreatedAt:   info.CreatedAt,
		UpdatedAt:   info.UpdatedAt,
	}
}

func toTaskGroupOutput(info mcpusecases.TaskGroupInfo) TaskGroupOutput {
	return TaskGroupOutput{
		ID:          info.ID,
		Name:        info.Name,
		Description: info.Description,
		Scope:       info.Scope,
		OwnerID:     info.OwnerID,
		TeamID:      info.TeamID,
		CreatedAt:   info.CreatedAt,
		UpdatedAt:   info.UpdatedAt,
	}
}

// --- Task Tool Handlers ---

func (s *MCPServer) handleListTasks(ctx context.Context, req *mcp.CallToolRequest, input ListTasksToolInput) (*mcp.CallToolResult, ListTasksToolOutput, error) {
	if s.taskUseCase == nil {
		return nil, ListTasksToolOutput{}, fmt.Errorf("task tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, ListTasksToolOutput{}, fmt.Errorf("authentication required")
	}

	listInput := mcpusecases.ListTasksInput{
		Scope:    input.Scope,
		TeamID:   input.TeamID,
		GroupID:  input.GroupID,
		Status:   input.Status,
		TaskType: input.TaskType,
	}

	tasks, err := s.taskUseCase.ListTasks(ctx, listInput, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListTasksToolOutput{}, fmt.Errorf("failed to list tasks: %w", err)
	}

	output := ListTasksToolOutput{
		Tasks: make([]TaskOutput, 0, len(tasks)),
		Total: len(tasks),
	}
	for _, t := range tasks {
		output.Tasks = append(output.Tasks, toTaskOutput(t))
	}

	return nil, output, nil
}

func (s *MCPServer) handleGetTask(ctx context.Context, req *mcp.CallToolRequest, input GetTaskToolInput) (*mcp.CallToolResult, GetTaskToolOutput, error) {
	if s.taskUseCase == nil {
		return nil, GetTaskToolOutput{}, fmt.Errorf("task tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, GetTaskToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.TaskID == "" {
		return nil, GetTaskToolOutput{}, fmt.Errorf("task_id is required")
	}

	taskInfo, err := s.taskUseCase.GetTask(ctx, input.TaskID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, GetTaskToolOutput{}, fmt.Errorf("failed to get task: %w", err)
	}

	return nil, GetTaskToolOutput{Task: toTaskOutput(*taskInfo)}, nil
}

func (s *MCPServer) handleCreateTask(ctx context.Context, req *mcp.CallToolRequest, input CreateTaskToolInput) (*mcp.CallToolResult, CreateTaskToolOutput, error) {
	if s.taskUseCase == nil {
		return nil, CreateTaskToolOutput{}, fmt.Errorf("task tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, CreateTaskToolOutput{}, fmt.Errorf("authentication required")
	}

	links := make([]mcpusecases.TaskLinkInfo, 0, len(input.Links))
	for _, l := range input.Links {
		links = append(links, mcpusecases.TaskLinkInfo{
			URL:   l.URL,
			Title: l.Title,
		})
	}

	createInput := mcpusecases.CreateTaskInput{
		Title:       input.Title,
		Description: input.Description,
		TaskType:    input.TaskType,
		Scope:       input.Scope,
		TeamID:      input.TeamID,
		GroupID:     input.GroupID,
		SessionID:   input.SessionID,
		Links:       links,
	}

	taskInfo, err := s.taskUseCase.CreateTask(ctx, createInput, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, CreateTaskToolOutput{}, fmt.Errorf("failed to create task: %w", err)
	}

	return nil, CreateTaskToolOutput{Task: toTaskOutput(*taskInfo)}, nil
}

func (s *MCPServer) handleUpdateTask(ctx context.Context, req *mcp.CallToolRequest, input UpdateTaskToolInput) (*mcp.CallToolResult, UpdateTaskToolOutput, error) {
	if s.taskUseCase == nil {
		return nil, UpdateTaskToolOutput{}, fmt.Errorf("task tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, UpdateTaskToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.TaskID == "" {
		return nil, UpdateTaskToolOutput{}, fmt.Errorf("task_id is required")
	}

	var links *[]mcpusecases.TaskLinkInfo
	if input.Links != nil {
		converted := make([]mcpusecases.TaskLinkInfo, 0, len(*input.Links))
		for _, l := range *input.Links {
			converted = append(converted, mcpusecases.TaskLinkInfo{
				URL:   l.URL,
				Title: l.Title,
			})
		}
		links = &converted
	}

	updateInput := mcpusecases.UpdateTaskInput{
		Title:       input.Title,
		Description: input.Description,
		Status:      input.Status,
		GroupID:     input.GroupID,
		SessionID:   input.SessionID,
		Links:       links,
	}

	taskInfo, err := s.taskUseCase.UpdateTask(ctx, input.TaskID, updateInput, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, UpdateTaskToolOutput{}, fmt.Errorf("failed to update task: %w", err)
	}

	return nil, UpdateTaskToolOutput{Task: toTaskOutput(*taskInfo)}, nil
}

func (s *MCPServer) handleDeleteTask(ctx context.Context, req *mcp.CallToolRequest, input DeleteTaskToolInput) (*mcp.CallToolResult, DeleteTaskToolOutput, error) {
	if s.taskUseCase == nil {
		return nil, DeleteTaskToolOutput{}, fmt.Errorf("task tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, DeleteTaskToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.TaskID == "" {
		return nil, DeleteTaskToolOutput{}, fmt.Errorf("task_id is required")
	}

	if err := s.taskUseCase.DeleteTask(ctx, input.TaskID, s.authenticatedUserID, s.authenticatedTeams); err != nil {
		return nil, DeleteTaskToolOutput{}, fmt.Errorf("failed to delete task: %w", err)
	}

	return nil, DeleteTaskToolOutput{
		Message: "Task deleted successfully",
		TaskID:  input.TaskID,
	}, nil
}

// --- Task Group Tool Handlers ---

func (s *MCPServer) handleListTaskGroups(ctx context.Context, req *mcp.CallToolRequest, input ListTaskGroupsToolInput) (*mcp.CallToolResult, ListTaskGroupsToolOutput, error) {
	if s.taskUseCase == nil {
		return nil, ListTaskGroupsToolOutput{}, fmt.Errorf("task tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, ListTaskGroupsToolOutput{}, fmt.Errorf("authentication required")
	}

	groups, err := s.taskUseCase.ListTaskGroups(ctx, input.Scope, input.TeamID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListTaskGroupsToolOutput{}, fmt.Errorf("failed to list task groups: %w", err)
	}

	output := ListTaskGroupsToolOutput{
		TaskGroups: make([]TaskGroupOutput, 0, len(groups)),
		Total:      len(groups),
	}
	for _, g := range groups {
		output.TaskGroups = append(output.TaskGroups, toTaskGroupOutput(g))
	}

	return nil, output, nil
}

func (s *MCPServer) handleCreateTaskGroup(ctx context.Context, req *mcp.CallToolRequest, input CreateTaskGroupToolInput) (*mcp.CallToolResult, CreateTaskGroupToolOutput, error) {
	if s.taskUseCase == nil {
		return nil, CreateTaskGroupToolOutput{}, fmt.Errorf("task tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, CreateTaskGroupToolOutput{}, fmt.Errorf("authentication required")
	}

	createInput := mcpusecases.CreateTaskGroupInput{
		Name:        input.Name,
		Description: input.Description,
		Scope:       input.Scope,
		TeamID:      input.TeamID,
	}

	groupInfo, err := s.taskUseCase.CreateTaskGroup(ctx, createInput, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, CreateTaskGroupToolOutput{}, fmt.Errorf("failed to create task group: %w", err)
	}

	return nil, CreateTaskGroupToolOutput{TaskGroup: toTaskGroupOutput(*groupInfo)}, nil
}

func (s *MCPServer) handleDeleteTaskGroup(ctx context.Context, req *mcp.CallToolRequest, input DeleteTaskGroupToolInput) (*mcp.CallToolResult, DeleteTaskGroupToolOutput, error) {
	if s.taskUseCase == nil {
		return nil, DeleteTaskGroupToolOutput{}, fmt.Errorf("task tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, DeleteTaskGroupToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.GroupID == "" {
		return nil, DeleteTaskGroupToolOutput{}, fmt.Errorf("group_id is required")
	}

	if err := s.taskUseCase.DeleteTaskGroup(ctx, input.GroupID, s.authenticatedUserID, s.authenticatedTeams); err != nil {
		return nil, DeleteTaskGroupToolOutput{}, fmt.Errorf("failed to delete task group: %w", err)
	}

	return nil, DeleteTaskGroupToolOutput{
		Message: "Task group deleted successfully",
		GroupID: input.GroupID,
	}, nil
}
