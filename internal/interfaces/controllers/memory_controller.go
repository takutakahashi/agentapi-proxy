package controllers

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// MemoryController handles memory entry HTTP requests
type MemoryController struct {
	repo portrepos.MemoryRepository
}

// NewMemoryController creates a new MemoryController
func NewMemoryController(repo portrepos.MemoryRepository) *MemoryController {
	return &MemoryController{repo: repo}
}

// GetName returns the name of this controller for logging
func (c *MemoryController) GetName() string {
	return "MemoryController"
}

// --- Request/Response DTOs ---

// CreateMemoryRequest is the JSON body for POST /memories
type CreateMemoryRequest struct {
	Title   string            `json:"title"`
	Content string            `json:"content"`
	Scope   string            `json:"scope"`             // "user" or "team"
	TeamID  string            `json:"team_id,omitempty"` // required when scope=="team"
	Tags    map[string]string `json:"tags,omitempty"`
}

// UpdateMemoryRequest is the JSON body for PUT /memories/:memoryId.
// All fields are optional; omitted/null fields are not changed.
type UpdateMemoryRequest struct {
	Title   *string `json:"title,omitempty"`
	Content *string `json:"content,omitempty"`
	// Tags, when present (even as {}), replaces ALL existing tags.
	// When the field is absent from the JSON body, existing tags are preserved.
	Tags *map[string]string `json:"tags,omitempty"`
}

// MemoryResponse is the response for a single memory entry
type MemoryResponse struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Scope     string            `json:"scope"`
	OwnerID   string            `json:"owner_id"`
	TeamID    string            `json:"team_id,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

// MemoryListResponse wraps a list of memory entries
type MemoryListResponse struct {
	Memories []*MemoryResponse `json:"memories"`
	Total    int               `json:"total"`
}

// --- HTTP Handlers ---

// CreateMemory handles POST /memories
func (c *MemoryController) CreateMemory(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req CreateMemoryRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate
	if req.Title == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "title is required")
	}
	if req.Scope != string(entities.ScopeUser) && req.Scope != string(entities.ScopeTeam) {
		return echo.NewHTTPError(http.StatusBadRequest, "scope must be 'user' or 'team'")
	}
	if req.Scope == string(entities.ScopeTeam) && req.TeamID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
	}

	// Access check
	if !c.canCreateMemory(user, req.Scope, req.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	id := uuid.New().String()
	scope := entities.ResourceScope(req.Scope)

	var memory *entities.Memory
	if len(req.Tags) > 0 {
		memory = entities.NewMemoryWithTags(id, req.Title, req.Content, scope, string(user.ID()), req.TeamID, req.Tags)
	} else {
		memory = entities.NewMemory(id, req.Title, req.Content, scope, string(user.ID()), req.TeamID)
	}

	if err := c.repo.Create(ctx.Request().Context(), memory); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create memory entry")
	}

	return ctx.JSON(http.StatusCreated, c.toResponse(memory))
}

// GetMemory handles GET /memories/:memoryId
func (c *MemoryController) GetMemory(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	memoryID := ctx.Param("memoryId")
	memory, err := c.repo.GetByID(ctx.Request().Context(), memoryID)
	if err != nil {
		var notFound entities.ErrMemoryNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Memory entry not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get memory entry")
	}

	if !c.canAccessMemory(user, memory) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(memory))
}

// ListMemories handles GET /memories
// Query params: scope, team_id, tag.*, q
func (c *MemoryController) ListMemories(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	scopeParam := ctx.QueryParam("scope")
	teamIDParam := ctx.QueryParam("team_id")
	query := ctx.QueryParam("q")
	tagFilters := c.parseTagFilters(ctx)

	var memories []*entities.Memory

	switch scopeParam {
	case string(entities.ScopeUser):
		// User-scoped: always restrict to own entries
		filter := portrepos.MemoryFilter{
			Scope:   entities.ScopeUser,
			OwnerID: string(user.ID()),
			Tags:    tagFilters,
			Query:   query,
		}
		result, err := c.repo.List(ctx.Request().Context(), filter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list memory entries")
		}
		memories = result

	case string(entities.ScopeTeam):
		// Team-scoped: check membership then filter
		if teamIDParam == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if !c.isMemberOfTeam(user, teamIDParam) {
			return echo.NewHTTPError(http.StatusForbidden, "Access denied: not a member of the specified team")
		}
		filter := portrepos.MemoryFilter{
			Scope:  entities.ScopeTeam,
			TeamID: teamIDParam,
			Tags:   tagFilters,
			Query:  query,
		}
		result, err := c.repo.List(ctx.Request().Context(), filter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list memory entries")
		}
		memories = result

	default:
		// No scope filter: return own user-scoped + all team-scoped entries for user's teams
		// Two separate calls are needed (OR logic across different label keys is not expressible as a single K8s label selector)
		userFilter := portrepos.MemoryFilter{
			Scope:   entities.ScopeUser,
			OwnerID: string(user.ID()),
			Tags:    tagFilters,
			Query:   query,
		}
		userEntries, err := c.repo.List(ctx.Request().Context(), userFilter)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list memory entries")
		}

		teamIDs := c.userTeamIDs(user)
		var teamEntries []*entities.Memory
		if len(teamIDs) > 0 {
			teamFilter := portrepos.MemoryFilter{
				Scope:   entities.ScopeTeam,
				TeamIDs: teamIDs,
				Tags:    tagFilters,
				Query:   query,
			}
			teamEntries, err = c.repo.List(ctx.Request().Context(), teamFilter)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list memory entries")
			}
		}

		memories = append(userEntries, teamEntries...)
	}

	if memories == nil {
		memories = []*entities.Memory{}
	}

	resp := &MemoryListResponse{
		Total: len(memories),
	}
	for _, m := range memories {
		resp.Memories = append(resp.Memories, c.toResponse(m))
	}
	if resp.Memories == nil {
		resp.Memories = []*MemoryResponse{}
	}

	return ctx.JSON(http.StatusOK, resp)
}

// UpdateMemory handles PUT /memories/:memoryId
func (c *MemoryController) UpdateMemory(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	memoryID := ctx.Param("memoryId")
	memory, err := c.repo.GetByID(ctx.Request().Context(), memoryID)
	if err != nil {
		var notFound entities.ErrMemoryNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Memory entry not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get memory entry")
	}

	if !c.canModifyMemory(user, memory) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	var req UpdateMemoryRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Apply partial updates (only non-nil fields)
	if req.Title != nil {
		memory.SetTitle(*req.Title)
	}
	if req.Content != nil {
		memory.SetContent(*req.Content)
	}
	if req.Tags != nil {
		memory.SetTags(*req.Tags)
	}

	if err := c.repo.Update(ctx.Request().Context(), memory); err != nil {
		var notFound entities.ErrMemoryNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Memory entry not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update memory entry")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(memory))
}

// DeleteMemory handles DELETE /memories/:memoryId
func (c *MemoryController) DeleteMemory(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	memoryID := ctx.Param("memoryId")
	memory, err := c.repo.GetByID(ctx.Request().Context(), memoryID)
	if err != nil {
		var notFound entities.ErrMemoryNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Memory entry not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get memory entry")
	}

	if !c.canModifyMemory(user, memory) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), memoryID); err != nil {
		var notFound entities.ErrMemoryNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Memory entry not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete memory entry")
	}

	return ctx.JSON(http.StatusOK, map[string]bool{"success": true})
}

// --- Internal Access Control ---

// canCreateMemory checks if the user may create a memory entry with the given scope/team.
// Admins follow the same rules — they can create their own user-scoped memories
// and team-scoped memories only if they are team members.
func (c *MemoryController) canCreateMemory(user *entities.User, scope, teamID string) bool {
	if scope == string(entities.ScopeUser) {
		return true // any authenticated user can create their own user-scoped memories
	}
	// team scope: must be a member of the team
	return c.isMemberOfTeam(user, teamID)
}

// canAccessMemory checks if the user may read a specific memory entry.
// IMPORTANT: Admin privilege does NOT bypass these checks.
// - user-scoped: ONLY the exact owner may access.
// - team-scoped: ONLY team members may access.
func (c *MemoryController) canAccessMemory(user *entities.User, memory *entities.Memory) bool {
	switch memory.Scope() {
	case entities.ScopeUser:
		// Strictly owner-only. Admin has NO special access.
		return string(user.ID()) == memory.OwnerID()
	case entities.ScopeTeam:
		// Any team member. Admin also has no special bypass.
		return c.isMemberOfTeam(user, memory.TeamID())
	default:
		return false
	}
}

// canModifyMemory checks if the user may update or delete a specific memory entry.
// Applies the same rules as canAccessMemory.
func (c *MemoryController) canModifyMemory(user *entities.User, memory *entities.Memory) bool {
	return c.canAccessMemory(user, memory)
}

// isMemberOfTeam checks if the user belongs to the given team.
// Handles both GitHub users (via GitHubInfo.Teams) and service accounts (via TeamID).
func (c *MemoryController) isMemberOfTeam(user *entities.User, teamID string) bool {
	if teamID == "" {
		return false
	}
	// Service accounts use TeamID directly
	if user.UserType() == entities.UserTypeServiceAccount {
		return user.TeamID() == teamID
	}
	return user.IsMemberOfTeam(teamID)
}

// parseTagFilters extracts tag.* query parameters from the request.
// For example: "?tag.category=meeting&tag.project=alpha"
// returns map[string]string{"category": "meeting", "project": "alpha"}
func (c *MemoryController) parseTagFilters(ctx echo.Context) map[string]string {
	tags := make(map[string]string)
	for key, values := range ctx.QueryParams() {
		if strings.HasPrefix(key, "tag.") && len(values) > 0 {
			tagKey := strings.TrimPrefix(key, "tag.")
			if tagKey != "" {
				tags[tagKey] = values[0]
			}
		}
	}
	return tags
}

// userTeamIDs returns all team IDs the user belongs to.
func (c *MemoryController) userTeamIDs(user *entities.User) []string {
	// Service accounts belong to exactly one team
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

// toResponse converts a Memory entity to MemoryResponse DTO
func (c *MemoryController) toResponse(m *entities.Memory) *MemoryResponse {
	return &MemoryResponse{
		ID:        m.ID(),
		Title:     m.Title(),
		Content:   m.Content(),
		Scope:     string(m.Scope()),
		OwnerID:   m.OwnerID(),
		TeamID:    m.TeamID(),
		Tags:      m.Tags(),
		CreatedAt: m.CreatedAt().Format(time.RFC3339),
		UpdatedAt: m.UpdatedAt().Format(time.RFC3339),
	}
}
