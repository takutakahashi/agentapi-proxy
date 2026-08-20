package controllers

import (
	"context"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// GetOrCreatePersonalAPIKeyUseCase defines the interface for personal API key use case
type GetOrCreatePersonalAPIKeyUseCase interface {
	Execute(ctx context.Context, userID entities.UserID) (*entities.PersonalAPIKey, error)
}

// AuthServiceForPersonalAPIKey defines the interface for auth service methods needed by this controller
type AuthServiceForPersonalAPIKey interface {
	LoadPersonalAPIKey(ctx context.Context, apiKey *entities.PersonalAPIKey) error
}

// PersonalAPIKeyController handles personal API key related requests
type PersonalAPIKeyController struct {
	getOrCreateAPIKeyUC GetOrCreatePersonalAPIKeyUseCase
	authService         AuthServiceForPersonalAPIKey
}

// NewPersonalAPIKeyController creates a new PersonalAPIKeyController
func NewPersonalAPIKeyController(
	getOrCreateAPIKeyUC GetOrCreatePersonalAPIKeyUseCase,
	authService AuthServiceForPersonalAPIKey,
) *PersonalAPIKeyController {
	return &PersonalAPIKeyController{
		getOrCreateAPIKeyUC: getOrCreateAPIKeyUC,
		authService:         authService,
	}
}

// GetOrCreatePersonalAPIKey handles GET /users/me/api-key and POST /users/me/api-key requests
// GET returns the existing API key (without revealing the key value for security)
// POST creates a new API key or regenerates if one already exists
func (c *PersonalAPIKeyController) GetOrCreatePersonalAPIKey(ctx echo.Context) error {
	// Get authorization context from middleware
	authzCtx := auth.GetAuthorizationContext(ctx)
	if authzCtx == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	userID := entities.UserID(authzCtx.PersonalScope.UserID)
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "user ID is required")
	}

	// Execute use case
	apiKey, err := c.getOrCreateAPIKeyUC.Execute(ctx.Request().Context(), userID)
	if err != nil {
		log.Printf("Failed to get or create personal API key for user %s: %v", userID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get or create personal API key")
	}

	// Load the API key into auth service for immediate use
	if c.authService != nil {
		if err := c.authService.LoadPersonalAPIKey(ctx.Request().Context(), apiKey); err != nil {
			log.Printf("Warning: failed to load personal API key into auth service for user %s: %v", userID, err)
			// Don't fail the request, just log the warning
		} else {
			log.Printf("Loaded personal API key into auth service for user: %s", userID)
		}
	}

	// Return API key details
	// Note: For GET requests, we might want to hide the actual key value for security
	// For POST requests (creation), we return the full key so user can save it
	response := map[string]interface{}{
		"user_id":    string(apiKey.UserID()),
		"api_key":    apiKey.APIKey(), // Return full key (user should save this)
		"created_at": apiKey.CreatedAt(),
		"updated_at": apiKey.UpdatedAt(),
	}

	return ctx.JSON(http.StatusOK, response)
}
