package proxy

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// HealthHandlers handles health check endpoints
type HealthHandlers struct{}

// NewHealthHandlers creates a new HealthHandlers instance
func NewHealthHandlers() *HealthHandlers {
	return &HealthHandlers{}
}

// HealthCheck handles GET /health requests to check server health
func (h *HealthHandlers) HealthCheck(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status": "ok",
	})
}
