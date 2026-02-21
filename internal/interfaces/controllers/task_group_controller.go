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

// TaskGroupController handles task group HTTP requests
type TaskGroupController struct {
	repo portrepos.TaskGroupRepository
}

// NewTaskGroupController creates a new TaskGroupController
func NewTaskGroupController(repo portrepos.TaskGroupRepository) *TaskGroupController {
	return &TaskGroupController{repo: repo}
}

// GetName returns the name of this controller for logging
func (c *TaskGroupController) GetName() string {
	return "TaskGroupController"
}

// --- Request/Response DTOs ---

// CreateTaskGroupRequest is the JSON body for POST /task-groups
type CreateTaskGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`             // "user" or "team"
	TeamID      string `json:"team_id,omitempty"` // required when scope=="team"
}

// UpdateTaskGroupRequest is the JSON body for PUT /task-groups/:groupId
type UpdateTaskGroupRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// TaskGroupResponse is the response for a single task group
type TaskGroupResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
	OwnerID     string `json:"owner_id"`
	TeamID      string `json:"team_id,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// TaskGroupListResponse wraps a list of task groups
type TaskGroupListResponse struct {
	TaskGroups []*TaskGroupResponse `json:"task_groups"`
	Total      int                  `json:"total"`
}

// --- HTTP Handlers ---

// CreateTaskGroup handles POST /task-groups
func (c *TaskGroupController) CreateTaskGroup(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req CreateTaskGroupRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Scope != string(entities.ScopeUser) && req.Scope != string(entities.ScopeTeam) {
		return echo.NewHTTPError(http.StatusBadRequest, "scope must be 'user' or 'team'")
	}
	if req.Scope == string(entities.ScopeTeam) && req.TeamID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
	}

	// Access check
	if !c.canCreateGroup(user, req.Scope, req.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	id := uuid.New().String()
	scope := entities.ResourceScope(req.Scope)

	group := entities.NewTaskGroup(id, req.Name, req.Description, scope, string(user.ID()), req.TeamID)

	if err := c.repo.Create(ctx.Request().Context(), group); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create task group")
	}

	return ctx.JSON(http.StatusCreated, c.toResponse(group))
}

// GetTaskGroup handles GET /task-groups/:groupId
func (c *TaskGroupController) GetTaskGroup(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	groupID := ctx.Param("groupId")
	group, err := c.repo.GetByID(ctx.Request().Context(), groupID)
	if err != nil {
		var notFound entities.ErrTaskGroupNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task group not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get task group")
	}

	if !c.canAccessGroup(user, group) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(group))
}

// ListTaskGroups handles GET /task-groups
// Query params: scope, team_id
func (c *TaskGroupController) ListTaskGroups(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	scopeParam := ctx.QueryParam("scope")
	teamIDParam := ctx.QueryParam("team_id")

	var groups []*entities.TaskGroup

	switch scopeParam {
	case string(entities.ScopeUser):
		filter := portrepos.TaskGroupFilter{
			Scope:   entities.ScopeUser,
			OwnerID: string(user.ID()),
		}
		result, err := c.repo.List(ctx.Request().Context(), filter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list task groups")
		}
		groups = result

	case string(entities.ScopeTeam):
		if teamIDParam == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if !c.isMemberOfTeam(user, teamIDParam) {
			return echo.NewHTTPError(http.StatusForbidden, "Access denied: not a member of the specified team")
		}
		filter := portrepos.TaskGroupFilter{
			Scope:  entities.ScopeTeam,
			TeamID: teamIDParam,
		}
		result, err := c.repo.List(ctx.Request().Context(), filter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list task groups")
		}
		groups = result

	default:
		// No scope filter: return own user-scoped + all team-scoped groups
		userFilter := portrepos.TaskGroupFilter{
			Scope:   entities.ScopeUser,
			OwnerID: string(user.ID()),
		}
		userGroups, err := c.repo.List(ctx.Request().Context(), userFilter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list task groups")
		}

		teamIDs := c.userTeamIDs(user)
		var teamGroups []*entities.TaskGroup
		if len(teamIDs) > 0 {
			teamFilter := portrepos.TaskGroupFilter{
				Scope:   entities.ScopeTeam,
				TeamIDs: teamIDs,
			}
			teamGroups, err = c.repo.List(ctx.Request().Context(), teamFilter)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list task groups")
			}
		}

		groups = append(userGroups, teamGroups...)
	}

	if groups == nil {
		groups = []*entities.TaskGroup{}
	}

	resp := &TaskGroupListResponse{
		Total:      len(groups),
		TaskGroups: make([]*TaskGroupResponse, 0, len(groups)),
	}
	for _, g := range groups {
		resp.TaskGroups = append(resp.TaskGroups, c.toResponse(g))
	}

	return ctx.JSON(http.StatusOK, resp)
}

// UpdateTaskGroup handles PUT /task-groups/:groupId
func (c *TaskGroupController) UpdateTaskGroup(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	groupID := ctx.Param("groupId")
	group, err := c.repo.GetByID(ctx.Request().Context(), groupID)
	if err != nil {
		var notFound entities.ErrTaskGroupNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task group not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get task group")
	}

	if !c.canModifyGroup(user, group) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	var req UpdateTaskGroupRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Apply partial updates
	if req.Name != nil {
		if *req.Name == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "name cannot be empty")
		}
		group.SetName(*req.Name)
	}
	if req.Description != nil {
		group.SetDescription(*req.Description)
	}

	if err := c.repo.Update(ctx.Request().Context(), group); err != nil {
		var notFound entities.ErrTaskGroupNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task group not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update task group")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(group))
}

// DeleteTaskGroup handles DELETE /task-groups/:groupId
func (c *TaskGroupController) DeleteTaskGroup(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	groupID := ctx.Param("groupId")
	group, err := c.repo.GetByID(ctx.Request().Context(), groupID)
	if err != nil {
		var notFound entities.ErrTaskGroupNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task group not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get task group")
	}

	if !c.canModifyGroup(user, group) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), groupID); err != nil {
		var notFound entities.ErrTaskGroupNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Task group not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete task group")
	}

	return ctx.JSON(http.StatusOK, map[string]bool{"success": true})
}

// --- Internal Access Control ---

func (c *TaskGroupController) canCreateGroup(user *entities.User, scope, teamID string) bool {
	if scope == string(entities.ScopeUser) {
		return true
	}
	return c.isMemberOfTeam(user, teamID)
}

func (c *TaskGroupController) canAccessGroup(user *entities.User, group *entities.TaskGroup) bool {
	switch group.Scope() {
	case entities.ScopeUser:
		return string(user.ID()) == group.OwnerID()
	case entities.ScopeTeam:
		return c.isMemberOfTeam(user, group.TeamID())
	default:
		return false
	}
}

func (c *TaskGroupController) canModifyGroup(user *entities.User, group *entities.TaskGroup) bool {
	return c.canAccessGroup(user, group)
}

func (c *TaskGroupController) isMemberOfTeam(user *entities.User, teamID string) bool {
	if teamID == "" {
		return false
	}
	if user.UserType() == entities.UserTypeServiceAccount {
		return user.TeamID() == teamID
	}
	return user.IsMemberOfTeam(teamID)
}

func (c *TaskGroupController) userTeamIDs(user *entities.User) []string {
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

// toResponse converts a TaskGroup entity to TaskGroupResponse DTO
func (c *TaskGroupController) toResponse(g *entities.TaskGroup) *TaskGroupResponse {
	return &TaskGroupResponse{
		ID:          g.ID(),
		Name:        g.Name(),
		Description: g.Description(),
		Scope:       string(g.Scope()),
		OwnerID:     g.OwnerID(),
		TeamID:      g.TeamID(),
		CreatedAt:   g.CreatedAt().Format(time.RFC3339),
		UpdatedAt:   g.UpdatedAt().Format(time.RFC3339),
	}
}
