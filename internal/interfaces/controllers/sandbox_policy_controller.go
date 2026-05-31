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

// SandboxPolicyController handles sandbox policy HTTP requests.
type SandboxPolicyController struct {
	repo portrepos.SandboxPolicyRepository
}

func NewSandboxPolicyController(repo portrepos.SandboxPolicyRepository) *SandboxPolicyController {
	return &SandboxPolicyController{repo: repo}
}

func (c *SandboxPolicyController) GetName() string { return "SandboxPolicyController" }

// --- DTOs ---

type CreateSandboxPolicyRequest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	DeniedDomains  []string `json:"denied_domains,omitempty"`
	Scope          string   `json:"scope"`
	TeamID         string   `json:"team_id,omitempty"`
}

type UpdateSandboxPolicyRequest struct {
	Name           *string   `json:"name,omitempty"`
	Description    *string   `json:"description,omitempty"`
	AllowedDomains *[]string `json:"allowed_domains,omitempty"`
	DeniedDomains  *[]string `json:"denied_domains,omitempty"`
}

type SandboxPolicyResponse struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	AllowedDomains []string `json:"allowed_domains,omitempty"`
	DeniedDomains  []string `json:"denied_domains,omitempty"`
	Scope          string   `json:"scope"`
	OwnerID        string   `json:"owner_id"`
	TeamID         string   `json:"team_id,omitempty"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

type SandboxPolicyListResponse struct {
	SandboxPolicies []*SandboxPolicyResponse `json:"sandbox_policies"`
	Total           int                      `json:"total"`
}

// --- Handlers ---

// CreateSandboxPolicy handles POST /sandbox-policies
func (c *SandboxPolicyController) CreateSandboxPolicy(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req CreateSandboxPolicyRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	req.Scope, req.TeamID = auth.ResolveUserScope(user, req.Scope, req.TeamID)

	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Scope != string(entities.ScopeUser) && req.Scope != string(entities.ScopeTeam) {
		return echo.NewHTTPError(http.StatusBadRequest, "scope must be 'user' or 'team'")
	}
	if req.Scope == string(entities.ScopeTeam) && req.TeamID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
	}
	if !c.canCreate(user, req.Scope, req.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	policy := entities.NewSandboxPolicy(
		uuid.New().String(),
		req.Name,
		req.Description,
		req.AllowedDomains,
		req.DeniedDomains,
		entities.ResourceScope(req.Scope),
		string(user.ID()),
		req.TeamID,
	)

	if err := c.repo.Create(ctx.Request().Context(), policy); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create sandbox policy")
	}

	return ctx.JSON(http.StatusCreated, c.toResponse(policy))
}

// GetSandboxPolicy handles GET /sandbox-policies/:id
func (c *SandboxPolicyController) GetSandboxPolicy(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	policy, err := c.repo.GetByID(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		var notFound entities.ErrSandboxPolicyNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Sandbox policy not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get sandbox policy")
	}

	if !c.canAccess(user, policy) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(policy))
}

// ListSandboxPolicies handles GET /sandbox-policies
func (c *SandboxPolicyController) ListSandboxPolicies(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	scopeParam := ctx.QueryParam("scope")
	teamIDParam := ctx.QueryParam("team_id")
	scopeParam, teamIDParam = auth.ResolveUserScope(user, scopeParam, teamIDParam)

	var policies []*entities.SandboxPolicy

	switch scopeParam {
	case string(entities.ScopeUser):
		result, err := c.repo.List(ctx.Request().Context(), portrepos.SandboxPolicyFilter{
			Scope:   entities.ScopeUser,
			OwnerID: string(user.ID()),
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list sandbox policies")
		}
		policies = result

	case string(entities.ScopeTeam):
		if teamIDParam == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if !c.isMemberOfTeam(user, teamIDParam) {
			return echo.NewHTTPError(http.StatusForbidden, "Access denied: not a member of the specified team")
		}
		result, err := c.repo.List(ctx.Request().Context(), portrepos.SandboxPolicyFilter{
			Scope:  entities.ScopeTeam,
			TeamID: teamIDParam,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list sandbox policies")
		}
		policies = result

	default:
		userResult, err := c.repo.List(ctx.Request().Context(), portrepos.SandboxPolicyFilter{
			Scope:   entities.ScopeUser,
			OwnerID: string(user.ID()),
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list sandbox policies")
		}
		teamIDs := c.userTeamIDs(user)
		var teamResult []*entities.SandboxPolicy
		if len(teamIDs) > 0 {
			teamResult, err = c.repo.List(ctx.Request().Context(), portrepos.SandboxPolicyFilter{
				Scope:   entities.ScopeTeam,
				TeamIDs: teamIDs,
			})
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "Failed to list sandbox policies")
			}
		}
		policies = append(userResult, teamResult...)
	}

	if policies == nil {
		policies = []*entities.SandboxPolicy{}
	}

	resp := &SandboxPolicyListResponse{Total: len(policies)}
	for _, p := range policies {
		resp.SandboxPolicies = append(resp.SandboxPolicies, c.toResponse(p))
	}
	if resp.SandboxPolicies == nil {
		resp.SandboxPolicies = []*SandboxPolicyResponse{}
	}

	return ctx.JSON(http.StatusOK, resp)
}

// UpdateSandboxPolicy handles PUT /sandbox-policies/:id
func (c *SandboxPolicyController) UpdateSandboxPolicy(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	policy, err := c.repo.GetByID(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		var notFound entities.ErrSandboxPolicyNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Sandbox policy not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get sandbox policy")
	}

	if !c.canModify(user, policy) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	var req UpdateSandboxPolicyRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if req.Name != nil {
		policy.SetName(*req.Name)
	}
	if req.Description != nil {
		policy.SetDescription(*req.Description)
	}
	if req.AllowedDomains != nil {
		policy.SetAllowedDomains(*req.AllowedDomains)
	}
	if req.DeniedDomains != nil {
		policy.SetDeniedDomains(*req.DeniedDomains)
	}

	if err := c.repo.Update(ctx.Request().Context(), policy); err != nil {
		var notFound entities.ErrSandboxPolicyNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Sandbox policy not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update sandbox policy")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(policy))
}

// DeleteSandboxPolicy handles DELETE /sandbox-policies/:id
func (c *SandboxPolicyController) DeleteSandboxPolicy(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	policy, err := c.repo.GetByID(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		var notFound entities.ErrSandboxPolicyNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Sandbox policy not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get sandbox policy")
	}

	if !c.canModify(user, policy) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), policy.ID()); err != nil {
		var notFound entities.ErrSandboxPolicyNotFound
		if errors.As(err, &notFound) {
			return echo.NewHTTPError(http.StatusNotFound, "Sandbox policy not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete sandbox policy")
	}

	return ctx.JSON(http.StatusOK, map[string]bool{"success": true})
}

// --- Access control ---

func (c *SandboxPolicyController) canCreate(user *entities.User, scope, teamID string) bool {
	if scope == string(entities.ScopeUser) {
		return true
	}
	return c.isMemberOfTeam(user, teamID)
}

func (c *SandboxPolicyController) canAccess(user *entities.User, policy *entities.SandboxPolicy) bool {
	switch policy.Scope() {
	case entities.ScopeUser:
		return string(user.ID()) == policy.OwnerID()
	case entities.ScopeTeam:
		return c.isMemberOfTeam(user, policy.TeamID())
	default:
		return false
	}
}

func (c *SandboxPolicyController) canModify(user *entities.User, policy *entities.SandboxPolicy) bool {
	return c.canAccess(user, policy)
}

func (c *SandboxPolicyController) isMemberOfTeam(user *entities.User, teamID string) bool {
	if teamID == "" {
		return false
	}
	if user.UserType() == entities.UserTypeServiceAccount {
		return user.TeamID() == teamID
	}
	return user.IsMemberOfTeam(teamID)
}

func (c *SandboxPolicyController) userTeamIDs(user *entities.User) []string {
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
	teamIDs := make([]string, 0, len(teams))
	for _, team := range teams {
		teamIDs = append(teamIDs, team.Organization+"/"+team.TeamSlug)
	}
	return teamIDs
}

func (c *SandboxPolicyController) toResponse(p *entities.SandboxPolicy) *SandboxPolicyResponse {
	return &SandboxPolicyResponse{
		ID:             p.ID(),
		Name:           p.Name(),
		Description:    p.Description(),
		AllowedDomains: p.AllowedDomains(),
		DeniedDomains:  p.DeniedDomains(),
		Scope:          string(p.Scope()),
		OwnerID:        p.OwnerID(),
		TeamID:         p.TeamID(),
		CreatedAt:      p.CreatedAt().Format(time.RFC3339),
		UpdatedAt:      p.UpdatedAt().Format(time.RFC3339),
	}
}
