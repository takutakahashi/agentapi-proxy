package controllers

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// SessionProfileController handles session profile CRUD endpoints
type SessionProfileController struct {
	repo repositories.SessionProfileRepository
}

// NewSessionProfileController creates a new SessionProfileController
func NewSessionProfileController(repo repositories.SessionProfileRepository) *SessionProfileController {
	return &SessionProfileController{repo: repo}
}

// GetName returns the name of this controller for logging
func (c *SessionProfileController) GetName() string {
	return "SessionProfileController"
}

// --- Request / Response types ---

// SessionProfileConfigRequest represents session profile config in requests
type SessionProfileConfigRequest struct {
	Environment            map[string]string       `json:"environment,omitempty"`
	Tags                   map[string]string       `json:"tags,omitempty"`
	InitialMessageTemplate string                  `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                  `json:"reuse_message_template,omitempty"`
	Params                 *entities.SessionParams `json:"params,omitempty"`
	ReuseSession           bool                    `json:"reuse_session,omitempty"`
	MemoryKey              map[string]string       `json:"memory_key,omitempty"`
	SandboxPolicyID        string                  `json:"sandbox_policy_id,omitempty"`
	SessionTTL             string                  `json:"session_ttl,omitempty"`
}

// CreateSessionProfileRequest is the request body for creating a session profile
type CreateSessionProfileRequest struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description,omitempty"`
	Scope       entities.ResourceScope      `json:"scope,omitempty"`
	TeamID      string                      `json:"team_id,omitempty"`
	IsDefault   bool                        `json:"is_default,omitempty"`
	Config      SessionProfileConfigRequest `json:"config"`
}

// UpdateSessionProfileRequest is the request body for updating a session profile
type UpdateSessionProfileRequest struct {
	Name        *string                      `json:"name,omitempty"`
	Description *string                      `json:"description,omitempty"`
	IsDefault   *bool                        `json:"is_default,omitempty"`
	Config      *SessionProfileConfigRequest `json:"config,omitempty"`
}

// SessionProfileResponse is the response for a session profile
type SessionProfileResponse struct {
	ID          string                       `json:"id"`
	Name        string                       `json:"name"`
	Description string                       `json:"description,omitempty"`
	UserID      string                       `json:"user_id"`
	Scope       entities.ResourceScope       `json:"scope,omitempty"`
	TeamID      string                       `json:"team_id,omitempty"`
	IsDefault   bool                         `json:"is_default,omitempty"`
	Config      SessionProfileConfigResponse `json:"config"`
	CreatedAt   string                       `json:"created_at"`
	UpdatedAt   string                       `json:"updated_at"`
}

// SessionProfileConfigResponse represents session profile config in responses
type SessionProfileConfigResponse struct {
	Environment            map[string]string       `json:"environment,omitempty"`
	Tags                   map[string]string       `json:"tags,omitempty"`
	InitialMessageTemplate string                  `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                  `json:"reuse_message_template,omitempty"`
	Params                 *entities.SessionParams `json:"params,omitempty"`
	ReuseSession           bool                    `json:"reuse_session,omitempty"`
	MemoryKey              map[string]string       `json:"memory_key,omitempty"`
	SandboxPolicyID        string                  `json:"sandbox_policy_id,omitempty"`
	SessionTTL             string                  `json:"session_ttl,omitempty"`
}

// --- Handlers ---

// CreateSessionProfile handles POST /session-profiles
func (c *SessionProfileController) CreateSessionProfile(ctx echo.Context) error {
	var req CreateSessionProfileRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	userID := string(user.ID())

	// Resolve scope
	resolvedScope, resolvedTeamID := auth.ResolveUserScope(user, string(req.Scope), req.TeamID)
	req.Scope = entities.ResourceScope(resolvedScope)
	req.TeamID = resolvedTeamID

	if req.Scope == entities.ScopeTeam && req.TeamID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
	}

	if req.Scope == entities.ScopeTeam {
		if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx == nil || !authzCtx.CanCreateInTeam(req.TeamID) {
			return echo.NewHTTPError(http.StatusForbidden, "you are not a member of this team")
		}
	}

	profile := entities.NewSessionProfile(uuid.New().String(), req.Name, userID)
	profile.SetDescription(req.Description)
	profile.SetScope(req.Scope)
	profile.SetTeamID(req.TeamID)
	profile.SetIsDefault(req.IsDefault)
	profile.SetConfig(c.requestToConfig(req.Config))

	if err := c.repo.Create(ctx.Request().Context(), profile); err != nil {
		log.Printf("Failed to create session profile: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create session profile")
	}

	return ctx.JSON(http.StatusCreated, c.toResponse(profile))
}

// ListSessionProfiles handles GET /session-profiles
func (c *SessionProfileController) ListSessionProfiles(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	userID := string(user.ID())

	scopeFilter := ctx.QueryParam("scope")
	teamIDFilter := ctx.QueryParam("team_id")
	scopeFilter, teamIDFilter = auth.ResolveUserScope(user, scopeFilter, teamIDFilter)

	var userTeamIDs []string
	if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx != nil {
		userTeamIDs = authzCtx.TeamScope.Teams
	}

	filter := repositories.SessionProfileFilter{
		Scope:   entities.ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}
	if scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	profiles, err := c.repo.List(ctx.Request().Context(), filter)
	if err != nil {
		log.Printf("Failed to list session profiles: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list session profiles")
	}

	responses := make([]SessionProfileResponse, 0, len(profiles))
	for _, p := range profiles {
		responses = append(responses, c.toResponse(p))
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"session_profiles": responses,
	})
}

// GetSessionProfile handles GET /session-profiles/:id
func (c *SessionProfileController) GetSessionProfile(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	profile, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSessionProfileNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "session profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session profile")
	}

	if !c.canAccess(ctx, user, profile) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(profile))
}

// UpdateSessionProfile handles PUT /session-profiles/:id
func (c *SessionProfileController) UpdateSessionProfile(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	profile, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSessionProfileNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "session profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session profile")
	}

	if !c.canModify(ctx, user, profile) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	var req UpdateSessionProfileRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if req.Name != nil {
		profile.SetName(*req.Name)
	}
	if req.Description != nil {
		profile.SetDescription(*req.Description)
	}
	if req.IsDefault != nil {
		profile.SetIsDefault(*req.IsDefault)
	}
	if req.Config != nil {
		profile.SetConfig(c.requestToConfig(*req.Config))
	}

	if err := c.repo.Update(ctx.Request().Context(), profile); err != nil {
		log.Printf("Failed to update session profile %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update session profile")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(profile))
}

// DeleteSessionProfile handles DELETE /session-profiles/:id
func (c *SessionProfileController) DeleteSessionProfile(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	profile, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSessionProfileNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "session profile not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get session profile")
	}

	if !c.canModify(ctx, user, profile) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), id); err != nil {
		log.Printf("Failed to delete session profile %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete session profile")
	}

	return ctx.NoContent(http.StatusNoContent)
}

// --- Helpers ---

func (c *SessionProfileController) canAccess(ctx echo.Context, user *entities.User, profile *entities.SessionProfile) bool {
	if user.IsAdmin() {
		return true
	}
	if profile.Scope() == entities.ScopeTeam {
		return user.IsMemberOfTeam(profile.TeamID())
	}
	return profile.UserID() == string(user.ID())
}

func (c *SessionProfileController) canModify(ctx echo.Context, user *entities.User, profile *entities.SessionProfile) bool {
	return c.canAccess(ctx, user, profile)
}

func (c *SessionProfileController) requestToConfig(req SessionProfileConfigRequest) entities.SessionProfileConfig {
	cfg := entities.NewSessionProfileConfig()
	if req.Environment != nil {
		cfg.SetEnvironment(req.Environment)
	}
	if req.Tags != nil {
		cfg.SetTags(req.Tags)
	}
	cfg.SetInitialMessageTemplate(req.InitialMessageTemplate)
	cfg.SetReuseMessageTemplate(req.ReuseMessageTemplate)
	cfg.SetReuseSession(req.ReuseSession)
	if req.MemoryKey != nil {
		cfg.SetMemoryKey(req.MemoryKey)
	}
	if req.Params != nil {
		cfg.SetParams(req.Params)
	}
	cfg.SetSandboxPolicyID(req.SandboxPolicyID)
	cfg.SetSessionTTL(req.SessionTTL)
	return cfg
}

func (c *SessionProfileController) toResponse(p *entities.SessionProfile) SessionProfileResponse {
	cfg := p.Config()
	return SessionProfileResponse{
		ID:          p.ID(),
		Name:        p.Name(),
		Description: p.Description(),
		UserID:      p.UserID(),
		Scope:       p.Scope(),
		TeamID:      p.TeamID(),
		IsDefault:   p.IsDefault(),
		Config: SessionProfileConfigResponse{
			Environment:            cfg.Environment(),
			Tags:                   cfg.Tags(),
			InitialMessageTemplate: cfg.InitialMessageTemplate(),
			ReuseMessageTemplate:   cfg.ReuseMessageTemplate(),
			Params:                 cfg.Params(),
			ReuseSession:           cfg.ReuseSession(),
			MemoryKey:              cfg.MemoryKey(),
			SandboxPolicyID:        cfg.SandboxPolicyID(),
			SessionTTL:             cfg.SessionTTL(),
		},
		CreatedAt: p.CreatedAt().Format(time.RFC3339),
		UpdatedAt: p.UpdatedAt().Format(time.RFC3339),
	}
}
