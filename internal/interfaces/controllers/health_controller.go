package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// HealthController handles health check endpoints
type HealthController struct{}

// NewHealthController creates a new HealthController instance
func NewHealthController() *HealthController {
	return &HealthController{}
}

// GetName returns the name of this controller for logging
func (c *HealthController) GetName() string {
	return "HealthController"
}

// HealthCheck handles GET /health requests to check server health
func (c *HealthController) HealthCheck(ctx echo.Context) error {
	return ctx.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}
