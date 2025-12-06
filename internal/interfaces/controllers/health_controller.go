package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type HealthController struct{}

func NewHealthController() *HealthController {
	return &HealthController{}
}

func (c *HealthController) RegisterRoutes(e *echo.Echo) {
	e.GET("/health", c.HealthCheck)
}

type HealthResponse struct {
	Status string `json:"status"`
}

func (c *HealthController) HealthCheck(ctx echo.Context) error {
	response := HealthResponse{
		Status: "ok",
	}
	return ctx.JSON(http.StatusOK, response)
}
