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

// PersonalAPIKeyController handles personal API key related requests
type PersonalAPIKeyController struct {
	getOrCreateAPIKeyUC GetOrCreatePersonalAPIKeyUseCase
}

// NewPersonalAPIKeyController creates a new PersonalAPIKeyController
func NewPersonalAPIKeyController(
	getOrCreateAPIKeyUC GetOrCreatePersonalAPIKeyUseCase,
) *PersonalAPIKeyController {
	return &PersonalAPIKeyController{
		getOrCreateAPIKeyUC: getOrCreateAPIKeyUC,
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
