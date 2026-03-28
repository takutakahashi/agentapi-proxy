package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// ExternalSessionManagerController handles external session manager CRUD
type ExternalSessionManagerController struct {
	repo repositories.ExternalSessionManagerRepository
}

// NewExternalSessionManagerController creates a new controller
func NewExternalSessionManagerController(repo repositories.ExternalSessionManagerRepository) *ExternalSessionManagerController {
	return &ExternalSessionManagerController{repo: repo}
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// CreateExternalSessionManagerRequest is the request body for registration
type CreateExternalSessionManagerRequest struct {
	Name   string                 `json:"name"`
	URL    string                 `json:"url"`
	Scope  entities.ResourceScope `json:"scope"`
	TeamID string                 `json:"team_id,omitempty"`
}

// UpdateExternalSessionManagerRequest is the request body for updates
type UpdateExternalSessionManagerRequest struct {
	Name   string                 `json:"name,omitempty"`
	URL    string                 `json:"url,omitempty"`
	Scope  entities.ResourceScope `json:"scope,omitempty"`
	TeamID string                 `json:"team_id,omitempty"`
}

// esmResponse is the standard response (HMAC secret masked)
type esmResponse struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	URL            string                 `json:"url"`
	UserID         string                 `json:"user_id"`
	Scope          entities.ResourceScope `json:"scope"`
	TeamID         string                 `json:"team_id,omitempty"`
	HMACSecretHint string                 `json:"hmac_secret_hint"`
	CreatedAt      time.Time              `json:"created_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// esmCreateResponse is the create/regenerate response (full HMAC secret exposed once)
type esmCreateResponse struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	URL        string                 `json:"url"`
	UserID     string                 `json:"user_id"`
	Scope      entities.ResourceScope `json:"scope"`
	TeamID     string                 `json:"team_id,omitempty"`
	HMACSecret string                 `json:"hmac_secret"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

func toESMResponse(esm *entities.ExternalSessionManager) esmResponse {
	return esmResponse{
		ID:             esm.ID(),
		Name:           esm.Name(),
		URL:            esm.URL(),
		UserID:         esm.UserID(),
		Scope:          esm.Scope(),
		TeamID:         esm.TeamID(),
		HMACSecretHint: esm.MaskedSecret(),
		CreatedAt:      esm.CreatedAt(),
		UpdatedAt:      esm.UpdatedAt(),
	}
}

func toESMCreateResponse(esm *entities.ExternalSessionManager) esmCreateResponse {
	return esmCreateResponse{
		ID:         esm.ID(),
		Name:       esm.Name(),
		URL:        esm.URL(),
		UserID:     esm.UserID(),
		Scope:      esm.Scope(),
		TeamID:     esm.TeamID(),
		HMACSecret: esm.HMACSecret(),
		CreatedAt:  esm.CreatedAt(),
		UpdatedAt:  esm.UpdatedAt(),
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// Create handles POST /external-session-managers
func (c *ExternalSessionManagerController) Create(ctx echo.Context) error {
	authzCtx := auth.GetAuthorizationContext(ctx)
	user := auth.GetUserFromContext(ctx)
	if authzCtx == nil || user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	var req CreateExternalSessionManagerRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
	}

	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.URL == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "url is required")
	}
	if req.Scope == "" {
		req.Scope = entities.ScopeUser
	}

	// Access control: check caller can create in the requested scope/team
	if !authzCtx.CanCreateResource(string(req.Scope), req.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "insufficient permissions for requested scope/team")
	}

	id := uuid.New().String()
	esm := entities.NewExternalSessionManager(id, req.Name, req.URL, string(user.ID()))
	esm.SetScope(req.Scope)
	if req.Scope == entities.ScopeTeam {
		esm.SetTeamID(req.TeamID)
	}

	if err := c.repo.Create(ctx.Request().Context(), esm); err != nil {
		log.Printf("[ESM] Failed to create external session manager: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create external session manager")
	}

	log.Printf("[ESM] Created external session manager %s (%s) for user %s", esm.ID(), esm.Name(), esm.UserID())
	return ctx.JSON(http.StatusCreated, toESMCreateResponse(esm))
}

// List handles GET /external-session-managers
func (c *ExternalSessionManagerController) List(ctx echo.Context) error {
	authzCtx := auth.GetAuthorizationContext(ctx)
	user := auth.GetUserFromContext(ctx)
	if authzCtx == nil || user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	filter := repositories.ExternalSessionManagerFilter{
		UserID:  string(user.ID()),
		TeamIDs: authzCtx.TeamScope.Teams,
	}
	// Admins can see all without user filter
	if authzCtx.TeamScope.IsAdmin {
		filter.UserID = ""
	}

	esms, err := c.repo.List(ctx.Request().Context(), filter)
	if err != nil {
		log.Printf("[ESM] Failed to list external session managers: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list external session managers")
	}

	resp := make([]esmResponse, 0, len(esms))
	for _, e := range esms {
		resp = append(resp, toESMResponse(e))
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"managers": resp})
}

// Get handles GET /external-session-managers/:id
func (c *ExternalSessionManagerController) Get(ctx echo.Context) error {
	authzCtx := auth.GetAuthorizationContext(ctx)
	user := auth.GetUserFromContext(ctx)
	if authzCtx == nil || user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	esm, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "external session manager not found")
	}

	if !authzCtx.CanAccessResource(esm.UserID(), string(esm.Scope()), esm.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	return ctx.JSON(http.StatusOK, toESMResponse(esm))
}

// Update handles PUT /external-session-managers/:id
func (c *ExternalSessionManagerController) Update(ctx echo.Context) error {
	authzCtx := auth.GetAuthorizationContext(ctx)
	user := auth.GetUserFromContext(ctx)
	if authzCtx == nil || user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	esm, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "external session manager not found")
	}

	if !authzCtx.CanModifyResource(esm.UserID(), string(esm.Scope()), esm.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	var req UpdateExternalSessionManagerRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request: %v", err))
	}

	if req.Name != "" {
		esm.SetName(req.Name)
	}
	if req.URL != "" {
		esm.SetURL(req.URL)
	}
	if req.Scope != "" {
		esm.SetScope(req.Scope)
		if req.Scope == entities.ScopeTeam {
			if req.TeamID == "" && esm.TeamID() == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
			}
		}
	}
	if req.TeamID != "" {
		esm.SetTeamID(req.TeamID)
	}

	if err := c.repo.Update(ctx.Request().Context(), esm); err != nil {
		log.Printf("[ESM] Failed to update external session manager %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update external session manager")
	}

	return ctx.JSON(http.StatusOK, toESMResponse(esm))
}

// Delete handles DELETE /external-session-managers/:id
func (c *ExternalSessionManagerController) Delete(ctx echo.Context) error {
	authzCtx := auth.GetAuthorizationContext(ctx)
	user := auth.GetUserFromContext(ctx)
	if authzCtx == nil || user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	esm, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "external session manager not found")
	}

	if !authzCtx.CanModifyResource(esm.UserID(), string(esm.Scope()), esm.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), id); err != nil {
		log.Printf("[ESM] Failed to delete external session manager %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete external session manager")
	}

	log.Printf("[ESM] Deleted external session manager %s", id)
	return ctx.NoContent(http.StatusNoContent)
}

// RegenerateSecret handles POST /external-session-managers/:id/regenerate-secret
func (c *ExternalSessionManagerController) RegenerateSecret(ctx echo.Context) error {
	authzCtx := auth.GetAuthorizationContext(ctx)
	user := auth.GetUserFromContext(ctx)
	if authzCtx == nil || user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	esm, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "external session manager not found")
	}

	if !authzCtx.CanModifyResource(esm.UserID(), string(esm.Scope()), esm.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	// Generate new HMAC secret
	newSecret, err := generateControllerESMSecret(32)
	if err != nil {
		log.Printf("[ESM] Failed to generate new secret for %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate new secret")
	}
	esm.SetHMACSecret(newSecret)

	if err := c.repo.Update(ctx.Request().Context(), esm); err != nil {
		log.Printf("[ESM] Failed to update secret for %s: %v", id, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update secret")
	}

	log.Printf("[ESM] Regenerated secret for external session manager %s", id)
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"id":          esm.ID(),
		"hmac_secret": esm.HMACSecret(),
	})
}

// generateControllerESMSecret generates a random hex secret
func generateControllerESMSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
