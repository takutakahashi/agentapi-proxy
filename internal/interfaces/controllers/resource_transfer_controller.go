package controllers

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/resource_transfer"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type ResourceTransferController struct {
	uc *resource_transfer.UseCase
}

func NewResourceTransferController(uc *resource_transfer.UseCase) *ResourceTransferController {
	return &ResourceTransferController{uc: uc}
}

func (c *ResourceTransferController) GetName() string { return "ResourceTransferController" }

type TransferResourceRequest struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	TargetScope  string `json:"target_scope"`
	TargetUserID string `json:"target_user_id,omitempty"`
	TargetTeamID string `json:"target_team_id,omitempty"`
	DryRun       bool   `json:"dry_run,omitempty"`
}

func (c *ResourceTransferController) TransferResource(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	var req TransferResourceRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.ResourceType == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "resource_type is required")
	}
	if req.ResourceID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "resource_id is required")
	}

	result, err := c.uc.Transfer(ctx.Request().Context(), resource_transfer.Request{
		ResourceType: resource_transfer.ResourceType(req.ResourceType),
		ResourceID:   req.ResourceID,
		TargetScope:  entities.ResourceScope(req.TargetScope),
		TargetUserID: req.TargetUserID,
		TargetTeamID: req.TargetTeamID,
		DryRun:       req.DryRun,
		Actor:        user,
	})
	if err != nil {
		return transferError(err)
	}
	return ctx.JSON(http.StatusOK, result)
}

func transferError(err error) error {
	msg := err.Error()
	switch {
	case msg == "authentication required":
		return echo.NewHTTPError(http.StatusUnauthorized, msg)
	case strings.Contains(msg, "access denied"), strings.Contains(msg, "cannot transfer"), strings.Contains(msg, "not a member"):
		return echo.NewHTTPError(http.StatusForbidden, msg)
	case strings.Contains(msg, "not found"):
		return echo.NewHTTPError(http.StatusNotFound, msg)
	case strings.Contains(msg, "required"), strings.Contains(msg, "must be"), strings.Contains(msg, "unsupported"):
		return echo.NewHTTPError(http.StatusBadRequest, msg)
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, msg)
	}
}
