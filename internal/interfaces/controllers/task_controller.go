package controllers

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// TaskController handles task HTTP requests
type TaskController struct {
	repo portrepos.TaskRepository
}

// NewTaskController creates a new TaskController
func NewTaskController(repo portrepos.TaskRepository) *TaskController {
	return &TaskController{repo: repo}
}

// GetName returns the name of this controller for logging
func (c *TaskController) GetName() string {
	return "TaskController"
}

// --- Request/Response DTOs ---

// TaskLinkRequest is the JSON body for a task link
type TaskLinkRequest struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// TaskLinkResponse is the response for a single task link
type TaskLinkResponse struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// CreateTaskRequest is the JSON body for POST /tasks
type CreateTaskRequest struct {
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	TaskType    string            `json:"task_type"`            // "user" or "agent"
	Scope       string            `json:"scope"`                // "user" or "team"
	TeamID      string            `json:"team_id,omitempty"`    // required when scope=="team"
	GroupID     string            `json:"group_id,omitempty"`   // optional
	SessionID   string            `json:"session_id,omitempty"` // optional, for agent tasks
	Links       []TaskLinkRequest `json:"links,omitempty"`
}

// UpdateTaskRequest is the JSON body for PUT /tasks/:taskId.
// All fields are optional; omitted/null fields are not changed.
type UpdateTaskRequest struct {
	Title       *string            `json:"title,omitempty"`
	Description *string            `json:"description,omitempty"`
	Status      *string            `json:"status,omitempty"`
	GroupID     *string            `json:"group_id,omitempty"`
	SessionID   *string            `json:"session_id,omitempty"`
	Links       *[]TaskLinkRequest `json:"links,omitempty"`
}

// TaskResponse is the response for a single task
type TaskResponse struct {
	ID          string             `json:"id"`
	Title       string             `json:"title"`
	Description string             `json:"description,omitempty"`
	Status      string             `json:"status"`
	TaskType    string             `json:"task_type"`
	Scope       string             `json:"scope"`
	OwnerID     string             `json:"owner_id"`
	TeamID      string             `json:"team_id,omitempty"`
	GroupID     string             `json:"group_id,omitempty"`
	SessionID   string             `json:"session_id,omitempty"`
	Links       []TaskLinkResponse `json:"links"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
}

// TaskListResponse wraps a list of tasks
type TaskListResponse struct {
	Tasks []*TaskResponse `json:"tasks"`
	Total int             `json:"total"`
}

// --- HTTP Handlers ---

// CreateTask handles POST /tasks
func (c *TaskController) CreateTask(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req CreateTaskRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate
	if req.Title == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "title is required")
	}
	if req.TaskType != string(entities.TaskTypeUser) && req.TaskType != string(entities.TaskTypeAgent) {
		return echo.NewHTTPError(http.StatusBadRequest, "task_type must be 'user' or 'agent'")
	}
	if req.Scope != string(entities.ScopeUser) && req.Scope != string(entities.ScopeTeam) {
		return echo.NewHTTPError(http.StatusBadRequest, "scope must be 'user' or 'team'")
	}
	if req.Scope == string(entities.ScopeTeam) && req.TeamID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
	}

	// Access check
	if !c.canCreateTask(user, req.Scope, req.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	id := uuid.New().String()
	scope := entities.ResourceScope(req.Scope)

	links := make([]*entities.TaskLink, 0, len(req.Links))
	for _, l := range req.Links {
		if l.URL == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "link url is required")
		}
		links = append(links, entities.NewTaskLink(uuid.New().String(), l.URL, l.Title))
	}

	task := entities.NewTask(
		id,
		req.Title,
		req.Description,
		entities.TaskStatusTodo,
		entities.TaskType(req.TaskType),
		scope,
		string(user.ID()),
		req.TeamID,
		req.GroupID,
		req.SessionID,
		links,
	)

	if err := c.repo.Create(ctx.Request().Context(), task); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create task")
	}

	return ctx.JSON(http.StatusCreated, c.toResponse(task))
}

// GetTask handles GET /tasks/:taskId
func (c *TaskController) GetTask(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	taskID := ctx.Param("taskId")
	task, err := c.repo.GetByID(ctx.Request().Context(), taskID)
	if err != nil {
		var notFound entities.ErrTaskNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get task")
	}

	if !c.canAccessTask(user, task) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(task))
}

// ListTasks handles GET /tasks
// Query params: scope, team_id, group_id, status, task_type
func (c *TaskController) ListTasks(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	scopeParam := ctx.QueryParam("scope")
	teamIDParam := ctx.QueryParam("team_id")
	groupIDParam := ctx.QueryParam("group_id")
	statusParam := ctx.QueryParam("status")
	taskTypeParam := ctx.QueryParam("task_type")

	// Validate optional filter params
	if statusParam != "" && statusParam != string(entities.TaskStatusTodo) && statusParam != string(entities.TaskStatusDone) {
		return echo.NewHTTPError(http.StatusBadRequest, "status must be 'todo' or 'done'")
	}
	if taskTypeParam != "" && taskTypeParam != string(entities.TaskTypeUser) && taskTypeParam != string(entities.TaskTypeAgent) {
		return echo.NewHTTPError(http.StatusBadRequest, "task_type must be 'user' or 'agent'")
	}

	var tasks []*entities.Task

	switch scopeParam {
	case string(entities.ScopeUser):
		filter := portrepos.TaskFilter{
			Scope:    entities.ScopeUser,
			OwnerID:  string(user.ID()),
			GroupID:  groupIDParam,
			Status:   entities.TaskStatus(statusParam),
			TaskType: entities.TaskType(taskTypeParam),
		}
		result, err := c.repo.List(ctx.Request().Context(), filter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list tasks")
		}
		tasks = result

	case string(entities.ScopeTeam):
		if teamIDParam == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if !c.isMemberOfTeam(user, teamIDParam) {
			return echo.NewHTTPError(http.StatusForbidden, "Access denied: not a member of the specified team")
		}
		filter := portrepos.TaskFilter{
			Scope:    entities.ScopeTeam,
			TeamID:   teamIDParam,
			GroupID:  groupIDParam,
			Status:   entities.TaskStatus(statusParam),
			TaskType: entities.TaskType(taskTypeParam),
		}
		result, err := c.repo.List(ctx.Request().Context(), filter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list tasks")
		}
		tasks = result

	default:
		// No scope filter: return own user-scoped + all team-scoped entries for user's teams
		userFilter := portrepos.TaskFilter{
			Scope:    entities.ScopeUser,
			OwnerID:  string(user.ID()),
			GroupID:  groupIDParam,
			Status:   entities.TaskStatus(statusParam),
			TaskType: entities.TaskType(taskTypeParam),
		}
		userTasks, err := c.repo.List(ctx.Request().Context(), userFilter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list tasks")
		}

		teamIDs := c.userTeamIDs(user)
		var teamTasks []*entities.Task
		if len(teamIDs) > 0 {
			teamFilter := portrepos.TaskFilter{
				Scope:    entities.ScopeTeam,
				TeamIDs:  teamIDs,
				GroupID:  groupIDParam,
				Status:   entities.TaskStatus(statusParam),
				TaskType: entities.TaskType(taskTypeParam),
			}
			teamTasks, err = c.repo.List(ctx.Request().Context(), teamFilter)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list tasks")
			}
		}

		tasks = append(userTasks, teamTasks...)
	}

	if tasks == nil {
		tasks = []*entities.Task{}
	}

	resp := &TaskListResponse{
		Total: len(tasks),
		Tasks: make([]*TaskResponse, 0, len(tasks)),
	}
	for _, t := range tasks {
		resp.Tasks = append(resp.Tasks, c.toResponse(t))
	}

	return ctx.JSON(http.StatusOK, resp)
}

// UpdateTask handles PUT /tasks/:taskId
func (c *TaskController) UpdateTask(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	taskID := ctx.Param("taskId")
	task, err := c.repo.GetByID(ctx.Request().Context(), taskID)
	if err != nil {
		var notFound entities.ErrTaskNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get task")
	}

	if !c.canModifyTask(user, task) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	var req UpdateTaskRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Apply partial updates (only non-nil fields)
	if req.Title != nil {
		if *req.Title == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "title cannot be empty")
		}
		task.SetTitle(*req.Title)
	}
	if req.Description != nil {
		task.SetDescription(*req.Description)
	}
	if req.Status != nil {
		s := entities.TaskStatus(*req.Status)
		if s != entities.TaskStatusTodo && s != entities.TaskStatusDone {
			return echo.NewHTTPError(http.StatusBadRequest, "status must be 'todo' or 'done'")
		}
		task.SetStatus(s)
	}
	if req.GroupID != nil {
		task.SetGroupID(*req.GroupID)
	}
	if req.SessionID != nil {
		task.SetSessionID(*req.SessionID)
	}
	if req.Links != nil {
		links := make([]*entities.TaskLink, 0, len(*req.Links))
		for _, l := range *req.Links {
			if l.URL == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "link url is required")
			}
			links = append(links, entities.NewTaskLink(uuid.New().String(), l.URL, l.Title))
		}
		task.SetLinks(links)
	}

	if err := c.repo.Update(ctx.Request().Context(), task); err != nil {
		var notFound entities.ErrTaskNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update task")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(task))
}

// DeleteTask handles DELETE /tasks/:taskId
func (c *TaskController) DeleteTask(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	taskID := ctx.Param("taskId")
	task, err := c.repo.GetByID(ctx.Request().Context(), taskID)
	if err != nil {
		var notFound entities.ErrTaskNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get task")
	}

	if !c.canModifyTask(user, task) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), taskID); err != nil {
		var notFound entities.ErrTaskNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete task")
	}

	return ctx.JSON(http.StatusOK, map[string]bool{"success": true})
}

// --- Internal Access Control ---

func (c *TaskController) canCreateTask(user *entities.User, scope, teamID string) bool {
	if scope == string(entities.ScopeUser) {
		return true
	}
	return c.isMemberOfTeam(user, teamID)
}

func (c *TaskController) canAccessTask(user *entities.User, task *entities.Task) bool {
	switch task.Scope() {
	case entities.ScopeUser:
		return string(user.ID()) == task.OwnerID()
	case entities.ScopeTeam:
		return c.isMemberOfTeam(user, task.TeamID())
	default:
		return false
	}
}

func (c *TaskController) canModifyTask(user *entities.User, task *entities.Task) bool {
	return c.canAccessTask(user, task)
}

func (c *TaskController) isMemberOfTeam(user *entities.User, teamID string) bool {
	if teamID == "" {
		return false
	}
	if user.UserType() == entities.UserTypeServiceAccount {
		return user.TeamID() == teamID
	}
	return user.IsMemberOfTeam(teamID)
}

func (c *TaskController) userTeamIDs(user *entities.User) []string {
	if user.UserType() == entities.UserTypeServiceAccount {
		if user.TeamID() != "" {
			return []string{user.TeamID()}
		}
		return nil
	}

	githubInfo := user.GitHubInfo()
	if githubInfo == nil {
		return nil
	}

	teams := githubInfo.Teams()
	if len(teams) == 0 {
		return nil
	}

	teamIDs := make([]string, 0, len(teams))
	for _, team := range teams {
		teamID := team.Organization + "/" + team.TeamSlug
		teamIDs = append(teamIDs, teamID)
	}
	return teamIDs
}

// toResponse converts a Task entity to TaskResponse DTO
func (c *TaskController) toResponse(t *entities.Task) *TaskResponse {
	links := make([]TaskLinkResponse, 0, len(t.Links()))
	for _, l := range t.Links() {
		links = append(links, TaskLinkResponse{
			ID:    l.ID(),
			URL:   l.URL(),
			Title: l.Title(),
		})
	}
	return &TaskResponse{
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
		CreatedAt:   t.CreatedAt().Format(time.RFC3339),
		UpdatedAt:   t.UpdatedAt().Format(time.RFC3339),
	}
}
