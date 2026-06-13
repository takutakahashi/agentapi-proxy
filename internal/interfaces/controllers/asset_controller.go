package controllers

import (
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// AssetController handles static asset uploads.
type AssetController struct {
	store services.AssetStore
}

// NewAssetController creates a new AssetController.
func NewAssetController(store services.AssetStore) *AssetController {
	return &AssetController{store: store}
}

// CreateAssetRequest is the JSON request body for creating an HTML asset.
type CreateAssetRequest struct {
	HTML string `json:"html"`
}

// AssetResponse is returned after uploading an asset.
type AssetResponse struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// CreateAsset handles POST /assets.
func (c *AssetController) CreateAsset(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var html string
	contentType := ctx.Request().Header.Get(echo.HeaderContentType)
	if strings.HasPrefix(contentType, "text/html") || strings.HasPrefix(contentType, "text/plain") {
		body, err := io.ReadAll(ctx.Request().Body)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
		}
		html = string(body)
	} else {
		var req CreateAssetRequest
		if err := ctx.Bind(&req); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
		}
		html = req.HTML
	}

	if html == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "html is required")
	}

	asset, err := c.store.SaveHTML(ctx.Request().Context(), string(user.ID()), html)
	if err != nil {
		log.Printf("[ASSETS] Failed to save asset for user %s: %v", user.ID(), err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save asset")
	}

	return ctx.JSON(http.StatusCreated, AssetResponse{ID: asset.ID, URL: asset.URL})
}
