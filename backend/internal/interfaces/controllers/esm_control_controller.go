package controllers

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
)

type ESMControlController struct {
	store       core.Store
	provisioner *ProvisionerController
}

func NewESMControlController(store core.Store, provisioner *ProvisionerController) *ESMControlController {
	return &ESMControlController{store: store, provisioner: provisioner}
}

func (c *ESMControlController) authorize(ctx echo.Context) (string, bool) {
	if c == nil || c.provisioner == nil {
		return "", false
	}
	managerID, _, ok := c.provisioner.authorizedExternalManager(ctx)
	pathManagerID := ctx.Param("managerId")
	return managerID, ok && (pathManagerID == "" || managerID == pathManagerID)
}

func (c *ESMControlController) WaitCommands(ctx echo.Context) error {
	managerID, ok := c.authorize(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	if err := c.store.TouchManager(ctx.Request().Context(), managerID, ctx.QueryParam("instance_id")); err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	commands, err := c.store.ReadCommands(ctx.Request().Context(), managerID, ctx.QueryParam("after"), parseWait(ctx.QueryParam("wait")), parseControlCount(ctx.QueryParam("count")))
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if len(commands) == 0 {
		return ctx.NoContent(http.StatusNoContent)
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"commands": commands, "next_cursor": commands[len(commands)-1].StreamID})
}

func (c *ESMControlController) AppendFrames(ctx echo.Context) error {
	managerID, ok := c.authorize(ctx)
	if !ok {
		return ctx.NoContent(http.StatusUnauthorized)
	}
	if err := c.store.TouchManager(ctx.Request().Context(), managerID, ctx.QueryParam("instance_id")); err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	var request struct {
		Frames []core.ResponseFrame `json:"frames"`
	}
	if err := ctx.Bind(&request); err != nil || len(request.Frames) == 0 || len(request.Frames) > 100 {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "frames must contain between 1 and 100 entries"})
	}
	requestID := request.Frames[0].RequestID
	belongs, err := c.store.RequestBelongsToManager(ctx.Request().Context(), requestID, managerID)
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	if !belongs {
		return ctx.NoContent(http.StatusForbidden)
	}
	for i := range request.Frames {
		if request.Frames[i].RequestID != requestID {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "all frames must have the same request_id"})
		}
		if request.Frames[i].ID == "" {
			return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "frame id is required"})
		}
		if request.Frames[i].CreatedAt.IsZero() {
			request.Frames[i].CreatedAt = time.Now().UTC()
		}
	}
	last, err := c.store.AppendFrames(ctx.Request().Context(), requestID, request.Frames)
	if err != nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	for _, frame := range request.Frames {
		if frame.CommandStreamID != "" {
			if err := c.store.AckCommand(ctx.Request().Context(), managerID, frame.CommandStreamID); err != nil {
				return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			}
		}
	}
	return ctx.JSON(http.StatusAccepted, map[string]string{"accepted_through": last})
}
