package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// MCPTaskToolsUseCase provides use cases for MCP task tools
type MCPTaskToolsUseCase struct {
	taskRepo      portrepos.TaskRepository
	taskGroupRepo portrepos.TaskGroupRepository
}

// NewMCPTaskToolsUseCase creates a new MCPTaskToolsUseCase
func NewMCPTaskToolsUseCase(
	taskRepo portrepos.TaskRepository,
	taskGroupRepo portrepos.TaskGroupRepository,
) *MCPTaskToolsUseCase {
	return &MCPTaskToolsUseCase{
		taskRepo:      taskRepo,
		taskGroupRepo: taskGroupRepo,
	}
}

// TaskLinkInfo represents a link associated with a task
type TaskLinkInfo struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// TaskInfo represents task information returned by MCP tools
type TaskInfo struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status"`
	TaskType    string         `json:"task_type"`
	Scope       string         `json:"scope"`
	OwnerID     string         `json:"owner_id"`
	TeamID      string         `json:"team_id,omitempty"`
	GroupID     string         `json:"group_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Links       []TaskLinkInfo `json:"links"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// TaskGroupInfo represents task group information returned by MCP tools
type TaskGroupInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Scope       string    `json:"scope"`
	OwnerID     string    `json:"owner_id"`
	TeamID      string    `json:"team_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateTaskInput represents input for creating a task
type CreateTaskInput struct {
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	TaskType    string         `json:"task_type"` // "user" or "agent"
	Scope       string         `json:"scope"`     // "user" or "team"
	TeamID      string         `json:"team_id,omitempty"`
	GroupID     string         `json:"group_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	Links       []TaskLinkInfo `json:"links,omitempty"`
}

// UpdateTaskInput represents input for updating a task
type UpdateTaskInput struct {
	Title       *string         `json:"title,omitempty"`
	Description *string         `json:"description,omitempty"`
	Status      *string         `json:"status,omitempty"`
	GroupID     *string         `json:"group_id,omitempty"`
	SessionID   *string         `json:"session_id,omitempty"`
	Links       *[]TaskLinkInfo `json:"links,omitempty"`
}

// ListTasksInput represents input for listing tasks
type ListTasksInput struct {
	Scope    string `json:"scope,omitempty"`
	TeamID   string `json:"team_id,omitempty"`
	GroupID  string `json:"group_id,omitempty"`
	Status   string `json:"status,omitempty"`
	TaskType string `json:"task_type,omitempty"`
}

// CreateTaskGroupInput represents input for creating a task group
type CreateTaskGroupInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"` // "user" or "team"
	TeamID      string `json:"team_id,omitempty"`
}

// toTaskInfo converts a Task entity to TaskInfo
func toTaskInfo(t *entities.Task) TaskInfo {
	links := make([]TaskLinkInfo, 0, len(t.Links()))
	for _, l := range t.Links() {
		links = append(links, TaskLinkInfo{
			ID:    l.ID(),
			URL:   l.URL(),
			Title: l.Title(),
		})
	}
	return TaskInfo{
		ID:          t.ID(),
		Title:       t.Title(),
		Description: t.Description(),
		Status:      string(t.Status()),
		TaskType:    string(t.TaskType()),
		Scope:       string(t.Scope()),
		OwnerID:     t.OwnerID(),
		TeamID:      t.TeamID(),
		GroupID:     t.GroupID(),
		SessionID:   t.SessionID(),
		Links:       links,
		CreatedAt:   t.CreatedAt(),
		UpdatedAt:   t.UpdatedAt(),
	}
}

// toTaskGroupInfo converts a TaskGroup entity to TaskGroupInfo
func toTaskGroupInfo(g *entities.TaskGroup) TaskGroupInfo {
	return TaskGroupInfo{
		ID:          g.ID(),
		Name:        g.Name(),
		Description: g.Description(),
		Scope:       string(g.Scope()),
		OwnerID:     g.OwnerID(),
		TeamID:      g.TeamID(),
		CreatedAt:   g.CreatedAt(),
		UpdatedAt:   g.UpdatedAt(),
	}
}

// ListTasks lists tasks with the given filters for the requesting user
func (uc *MCPTaskToolsUseCase) ListTasks(ctx context.Context, input ListTasksInput, requestingUserID string, teamIDs []string) ([]TaskInfo, error) {
	if uc.taskRepo == nil {
		return nil, fmt.Errorf("task repository not available")
	}

	var tasks []*entities.Task

	switch entities.ResourceScope(input.Scope) {
	case entities.ScopeUser:
		filter := portrepos.TaskFilter{
			Scope:    entities.ScopeUser,
			OwnerID:  requestingUserID,
			GroupID:  input.GroupID,
			Status:   entities.TaskStatus(input.Status),
			TaskType: entities.TaskType(input.TaskType),
		}
		result, err := uc.taskRepo.List(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to list tasks: %w", err)
		}
		tasks = result

	case entities.ScopeTeam:
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		// Verify user is a member of the team
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
		filter := portrepos.TaskFilter{
			Scope:    entities.ScopeTeam,
			TeamID:   input.TeamID,
			GroupID:  input.GroupID,
			Status:   entities.TaskStatus(input.Status),
			TaskType: entities.TaskType(input.TaskType),
		}
		result, err := uc.taskRepo.List(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to list tasks: %w", err)
		}
		tasks = result

	default:
		// No scope: return user-scoped tasks + all team-scoped tasks for user's teams
		userFilter := portrepos.TaskFilter{
			Scope:    entities.ScopeUser,
			OwnerID:  requestingUserID,
			GroupID:  input.GroupID,
			Status:   entities.TaskStatus(input.Status),
			TaskType: entities.TaskType(input.TaskType),
		}
		userTasks, err := uc.taskRepo.List(ctx, userFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to list user tasks: %w", err)
		}

		var teamTasks []*entities.Task
		if len(teamIDs) > 0 {
			teamFilter := portrepos.TaskFilter{
				Scope:    entities.ScopeTeam,
				TeamIDs:  teamIDs,
				GroupID:  input.GroupID,
				Status:   entities.TaskStatus(input.Status),
				TaskType: entities.TaskType(input.TaskType),
			}
			teamTasks, err = uc.taskRepo.List(ctx, teamFilter)
			if err != nil {
				return nil, fmt.Errorf("failed to list team tasks: %w", err)
			}
		}

		tasks = append(userTasks, teamTasks...)
	}

	result := make([]TaskInfo, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, toTaskInfo(t))
	}
	return result, nil
}

// GetTask retrieves a task by ID for the requesting user
func (uc *MCPTaskToolsUseCase) GetTask(ctx context.Context, taskID, requestingUserID string, teamIDs []string) (*TaskInfo, error) {
	if uc.taskRepo == nil {
		return nil, fmt.Errorf("task repository not available")
	}

	task, err := uc.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	if !canAccessTask(task, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	info := toTaskInfo(task)
	return &info, nil
}

// CreateTask creates a new task for the requesting user
func (uc *MCPTaskToolsUseCase) CreateTask(ctx context.Context, input CreateTaskInput, requestingUserID string, teamIDs []string) (*TaskInfo, error) {
	if uc.taskRepo == nil {
		return nil, fmt.Errorf("task repository not available")
	}

	// Validate required fields
	if input.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if input.TaskType != string(entities.TaskTypeUser) && input.TaskType != string(entities.TaskTypeAgent) {
		return nil, fmt.Errorf("task_type must be 'user' or 'agent'")
	}
	if input.Scope != string(entities.ScopeUser) && input.Scope != string(entities.ScopeTeam) {
		return nil, fmt.Errorf("scope must be 'user' or 'team'")
	}
	if input.Scope == string(entities.ScopeTeam) {
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
	}

	links := make([]*entities.TaskLink, 0, len(input.Links))
	for _, l := range input.Links {
		if l.URL == "" {
			return nil, fmt.Errorf("link url is required")
		}
		links = append(links, entities.NewTaskLink(uuid.New().String(), l.URL, l.Title))
	}

	task := entities.NewTask(
		uuid.New().String(),
		input.Title,
		input.Description,
		entities.TaskStatusTodo,
		entities.TaskType(input.TaskType),
		entities.ResourceScope(input.Scope),
		requestingUserID,
		input.TeamID,
		input.GroupID,
		input.SessionID,
		links,
	)

	if err := uc.taskRepo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	info := toTaskInfo(task)
	return &info, nil
}

// UpdateTask updates an existing task for the requesting user
func (uc *MCPTaskToolsUseCase) UpdateTask(ctx context.Context, taskID string, input UpdateTaskInput, requestingUserID string, teamIDs []string) (*TaskInfo, error) {
	if uc.taskRepo == nil {
		return nil, fmt.Errorf("task repository not available")
	}

	task, err := uc.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}

	if !canAccessTask(task, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	if input.Title != nil {
		if *input.Title == "" {
			return nil, fmt.Errorf("title cannot be empty")
		}
		task.SetTitle(*input.Title)
	}
	if input.Description != nil {
		task.SetDescription(*input.Description)
	}
	if input.Status != nil {
		s := entities.TaskStatus(*input.Status)
		if s != entities.TaskStatusTodo && s != entities.TaskStatusDone {
			return nil, fmt.Errorf("status must be 'todo' or 'done'")
		}
		task.SetStatus(s)
	}
	if input.GroupID != nil {
		task.SetGroupID(*input.GroupID)
	}
	if input.SessionID != nil {
		task.SetSessionID(*input.SessionID)
	}
	if input.Links != nil {
		links := make([]*entities.TaskLink, 0, len(*input.Links))
		for _, l := range *input.Links {
			if l.URL == "" {
				return nil, fmt.Errorf("link url is required")
			}
			links = append(links, entities.NewTaskLink(uuid.New().String(), l.URL, l.Title))
		}
		task.SetLinks(links)
	}

	if err := uc.taskRepo.Update(ctx, task); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	info := toTaskInfo(task)
	return &info, nil
}

// DeleteTask deletes a task for the requesting user
func (uc *MCPTaskToolsUseCase) DeleteTask(ctx context.Context, taskID, requestingUserID string, teamIDs []string) error {
	if uc.taskRepo == nil {
		return fmt.Errorf("task repository not available")
	}

	task, err := uc.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}

	if !canAccessTask(task, requestingUserID, teamIDs) {
		return fmt.Errorf("access denied")
	}

	return uc.taskRepo.Delete(ctx, taskID)
}

// ListTaskGroups lists task groups for the requesting user
func (uc *MCPTaskToolsUseCase) ListTaskGroups(ctx context.Context, scope, teamID, requestingUserID string, teamIDs []string) ([]TaskGroupInfo, error) {
	if uc.taskGroupRepo == nil {
		return nil, fmt.Errorf("task group repository not available")
	}

	var groups []*entities.TaskGroup

	switch entities.ResourceScope(scope) {
	case entities.ScopeUser:
		filter := portrepos.TaskGroupFilter{
			Scope:   entities.ScopeUser,
			OwnerID: requestingUserID,
		}
		result, err := uc.taskGroupRepo.List(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to list task groups: %w", err)
		}
		groups = result

	case entities.ScopeTeam:
		if teamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, teamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
		filter := portrepos.TaskGroupFilter{
			Scope:  entities.ScopeTeam,
			TeamID: teamID,
		}
		result, err := uc.taskGroupRepo.List(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to list task groups: %w", err)
		}
		groups = result

	default:
		userFilter := portrepos.TaskGroupFilter{
			Scope:   entities.ScopeUser,
			OwnerID: requestingUserID,
		}
		userGroups, err := uc.taskGroupRepo.List(ctx, userFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to list user task groups: %w", err)
		}

		var teamGroups []*entities.TaskGroup
		if len(teamIDs) > 0 {
			teamFilter := portrepos.TaskGroupFilter{
				Scope:   entities.ScopeTeam,
				TeamIDs: teamIDs,
			}
			teamGroups, err = uc.taskGroupRepo.List(ctx, teamFilter)
			if err != nil {
				return nil, fmt.Errorf("failed to list team task groups: %w", err)
			}
		}

		groups = append(userGroups, teamGroups...)
	}

	result := make([]TaskGroupInfo, 0, len(groups))
	for _, g := range groups {
		result = append(result, toTaskGroupInfo(g))
	}
	return result, nil
}

// CreateTaskGroup creates a new task group for the requesting user
func (uc *MCPTaskToolsUseCase) CreateTaskGroup(ctx context.Context, input CreateTaskGroupInput, requestingUserID string, teamIDs []string) (*TaskGroupInfo, error) {
	if uc.taskGroupRepo == nil {
		return nil, fmt.Errorf("task group repository not available")
	}

	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if input.Scope != string(entities.ScopeUser) && input.Scope != string(entities.ScopeTeam) {
		return nil, fmt.Errorf("scope must be 'user' or 'team'")
	}
	if input.Scope == string(entities.ScopeTeam) {
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
	}

	group := entities.NewTaskGroup(
		uuid.New().String(),
		input.Name,
		input.Description,
		entities.ResourceScope(input.Scope),
		requestingUserID,
		input.TeamID,
	)

	if err := uc.taskGroupRepo.Create(ctx, group); err != nil {
		return nil, fmt.Errorf("failed to create task group: %w", err)
	}

	info := toTaskGroupInfo(group)
	return &info, nil
}

// DeleteTaskGroup deletes a task group for the requesting user
func (uc *MCPTaskToolsUseCase) DeleteTaskGroup(ctx context.Context, groupID, requestingUserID string, teamIDs []string) error {
	if uc.taskGroupRepo == nil {
		return fmt.Errorf("task group repository not available")
	}

	group, err := uc.taskGroupRepo.GetByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("task group not found: %w", err)
	}

	if !canAccessTaskGroup(group, requestingUserID, teamIDs) {
		return fmt.Errorf("access denied")
	}

	return uc.taskGroupRepo.Delete(ctx, groupID)
}

// canAccessTask checks whether the requesting user can access the task
func canAccessTask(task *entities.Task, requestingUserID string, teamIDs []string) bool {
	switch task.Scope() {
	case entities.ScopeUser:
		return task.OwnerID() == requestingUserID
	case entities.ScopeTeam:
		return containsTeam(teamIDs, task.TeamID())
	default:
		return false
	}
}

// canAccessTaskGroup checks whether the requesting user can access the task group
func canAccessTaskGroup(group *entities.TaskGroup, requestingUserID string, teamIDs []string) bool {
	switch group.Scope() {
	case entities.ScopeUser:
		return group.OwnerID() == requestingUserID
	case entities.ScopeTeam:
		return containsTeam(teamIDs, group.TeamID())
	default:
		return false
	}
}

// containsTeam checks if the given teamID is in the list of teamIDs
func containsTeam(teamIDs []string, teamID string) bool {
	for _, id := range teamIDs {
		if id == teamID {
			return true
		}
	}
	return false
}
