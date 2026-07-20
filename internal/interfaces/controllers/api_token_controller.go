package controllers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	apitokenuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/api_token"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// CreateAPITokenUC is the use case interface for creating tokens.
type CreateAPITokenUC interface {
	Execute(ctx context.Context, in *apitokenuc.CreateAPITokenInput) (*apitokenuc.CreateAPITokenOutput, error)
}

// ListAPITokenUC is the use case interface for listing tokens.
type ListAPITokenUC interface {
	Execute(ctx context.Context, in *apitokenuc.ListAPITokenInput) (*apitokenuc.ListAPITokenOutput, error)
}

// GetAPITokenUC is the use case interface for getting a token.
type GetAPITokenUC interface {
	Execute(ctx context.Context, in *apitokenuc.GetAPITokenInput) (*apitokenuc.GetAPITokenOutput, error)
}

// DeleteAPITokenUC is the use case interface for deleting a token.
type DeleteAPITokenUC interface {
	Execute(ctx context.Context, in *apitokenuc.DeleteAPITokenInput) error
}

// APITokenController handles the unified /api-tokens endpoints.
type APITokenController struct {
	createUC CreateAPITokenUC
	listUC   ListAPITokenUC
	getUC    GetAPITokenUC
	deleteUC DeleteAPITokenUC
}

// NewAPITokenController constructs an APITokenController.
func NewAPITokenController(createUC CreateAPITokenUC, listUC ListAPITokenUC, getUC GetAPITokenUC, deleteUC DeleteAPITokenUC) *APITokenController {
	return &APITokenController{
		createUC: createUC,
		listUC:   listUC,
		getUC:    getUC,
		deleteUC: deleteUC,
	}
}

// CreateAPITokenRequest is the JSON body for POST /api-tokens.
type CreateAPITokenRequest struct {
	Name        string   `json:"name"`
	Scope       string   `json:"scope"`             // "user" (default) or "team"
	TeamID      string   `json:"team_id,omitempty"` // required when scope=="team"
	Permissions []string `json:"permissions,omitempty"`
	ExpiresAt   *string  `json:"expires_at,omitempty"` // RFC3339
}

// APITokenMetadata is the metadata-only representation of a token (no secret).
type APITokenMetadata struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Scope         string   `json:"scope"`
	UserID        string   `json:"user_id"`
	TeamID        string   `json:"team_id,omitempty"`
	Permissions   []string `json:"permissions"`
	DisplayPrefix string   `json:"display_prefix"`
	ExpiresAt     *string  `json:"expires_at,omitempty"`
	CreatedBy     string   `json:"created_by"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

// APITokenWithSecret is the create response: metadata plus the plaintext
// token, returned exactly once. The JSON field is named "plaintext_token" to
// match the agentapi-ui create-token contract.
type APITokenWithSecret struct {
	APITokenMetadata
	Secret string `json:"plaintext_token"`
}

// APITokenListResponse wraps a list of tokens: {items:[...]}.
type APITokenListResponse struct {
	Items []*APITokenMetadata `json:"items"`
}

// Create handles POST /api-tokens.
func (c *APITokenController) Create(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	var req CreateAPITokenRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	scope := req.Scope
	if scope == "" {
		scope = string(entities.APITokenScopeUser)
	}

	perms := make([]entities.Permission, 0, len(req.Permissions))
	for _, p := range req.Permissions {
		perms = append(perms, entities.Permission(p))
	}
	if len(perms) == 0 {
		// Default to the caller's session permissions so a freshly created
		// token is at least useful. Still bounded by the caller.
		perms = []entities.Permission{
			entities.PermissionSessionCreate,
			entities.PermissionSessionRead,
			entities.PermissionSessionUpdate,
			entities.PermissionSessionDelete,
		}
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "expires_at must be RFC3339")
		}
		expiresAt = &t
	}

	out, err := c.createUC.Execute(ctx.Request().Context(), &apitokenuc.CreateAPITokenInput{
		Caller:      user,
		Name:        req.Name,
		Scope:       scope,
		TeamID:      req.TeamID,
		Permissions: perms,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return c.mapCreateError(err)
	}

	// The plaintext token is returned exactly once. Cache-Control: no-store
	// prevents any intermediary from caching the response.
	ctx.Response().Header().Set("Cache-Control", "no-store")
	return ctx.JSON(http.StatusCreated, &APITokenWithSecret{
		APITokenMetadata: toMetadata(*out.Token),
		Secret:           out.Token.Secret(),
	})
}

// List handles GET /api-tokens.
func (c *APITokenController) List(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	scope := ctx.QueryParam("scope")
	teamID := ctx.QueryParam("team_id")

	out, err := c.listUC.Execute(ctx.Request().Context(), &apitokenuc.ListAPITokenInput{
		Caller: user,
		Scope:  scope,
		TeamID: teamID,
	})
	if err != nil {
		return c.mapListError(err)
	}

	items := make([]*APITokenMetadata, 0, len(out.Tokens))
	for _, t := range out.Tokens {
		items = append(items, toMetadataPtr(t))
	}
	return ctx.JSON(http.StatusOK, &APITokenListResponse{Items: items})
}

// Get handles GET /api-tokens/:tokenId.
func (c *APITokenController) Get(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	tokenID := ctx.Param("tokenId")
	if tokenID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "token_id is required")
	}

	out, err := c.getUC.Execute(ctx.Request().Context(), &apitokenuc.GetAPITokenInput{
		Caller:  user,
		TokenID: tokenID,
	})
	if err != nil {
		if errors.Is(err, entities.ErrAPITokenNotFound) {
			// Uniform 404: never reveal whether the token exists in another scope.
			return echo.NewHTTPError(http.StatusNotFound, "api token not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get api token")
	}
	return ctx.JSON(http.StatusOK, toMetadataPtr(out.Token))
}

// Delete handles DELETE /api-tokens/:tokenId.
//
// It is idempotent for authorized and nonexistent (owned) token IDs: the
// response is 204 No Content in all non-error cases, including when the token
// does not exist or belongs to another scope. This avoids leaking existence
// across scopes.
func (c *APITokenController) Delete(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	tokenID := ctx.Param("tokenId")
	if tokenID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "token_id is required")
	}

	if err := c.deleteUC.Execute(ctx.Request().Context(), &apitokenuc.DeleteAPITokenInput{
		Caller:  user,
		TokenID: tokenID,
	}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete api token")
	}
	return ctx.NoContent(http.StatusNoContent)
}

// mapCreateError translates use-case authorization/validation errors into HTTP
// statuses without leaking internal details.
func (c *APITokenController) mapCreateError(err error) error {
	msg := err.Error()
	switch {
	case contains(msg, "not a member"):
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	case contains(msg, "not granted to the caller"):
		return echo.NewHTTPError(http.StatusForbidden, "requested permissions exceed caller permissions")
	case contains(msg, "service accounts cannot create"):
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	case contains(msg, "team_id is required"):
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required for team-scoped tokens")
	case contains(msg, "invalid scope"):
		return echo.NewHTTPError(http.StatusBadRequest, "scope must be 'user' or 'team'")
	case contains(msg, "permissions cannot be empty"):
		return echo.NewHTTPError(http.StatusBadRequest, "permissions cannot be empty")
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create api token")
	}
}

// mapListError translates list use-case errors into HTTP statuses.
func (c *APITokenController) mapListError(err error) error {
	msg := err.Error()
	switch {
	case contains(msg, "not a member"):
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	case contains(msg, "team_id is required"):
		return echo.NewHTTPError(http.StatusBadRequest, "team_id is required for team-scoped tokens")
	case contains(msg, "invalid scope"):
		return echo.NewHTTPError(http.StatusBadRequest, "scope must be 'user' or 'team'")
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list api tokens")
	}
}

// contains is a tiny helper to keep error-message matching readable.
func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// toMetadata converts a token entity into its metadata-only DTO (no secret).
func toMetadata(t entities.APIToken) APITokenMetadata {
	return *toMetadataPtr(&t)
}

// toMetadataPtr converts a token entity pointer into its metadata-only DTO.
func toMetadataPtr(t *entities.APIToken) *APITokenMetadata {
	perms := make([]string, 0, len(t.Permissions()))
	for _, p := range t.Permissions() {
		perms = append(perms, string(p))
	}
	var expiresAt *string
	if exp := t.ExpiresAt(); exp != nil {
		s := exp.Format(time.RFC3339Nano)
		expiresAt = &s
	}
	m := &APITokenMetadata{
		ID:            t.ID(),
		Name:          t.Name(),
		Scope:         string(t.Scope()),
		UserID:        string(t.UserID()),
		Permissions:   perms,
		DisplayPrefix: t.DisplayPrefix(),
		ExpiresAt:     expiresAt,
		CreatedBy:     string(t.CreatedBy()),
		CreatedAt:     t.CreatedAt().Format(time.RFC3339Nano),
		UpdatedAt:     t.UpdatedAt().Format(time.RFC3339Nano),
	}
	if t.TeamID() != "" {
		m.TeamID = t.TeamID()
	}
	return m
}
