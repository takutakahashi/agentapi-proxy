package controllers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
)

type ProvisionerController struct {
	manager *services.KubernetesSessionManager
}

func NewProvisionerController(manager *services.KubernetesSessionManager) *ProvisionerController {
	return &ProvisionerController{manager: manager}
}

func (pc *ProvisionerController) Connect(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var req services.ProvisionerConnectRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if req.SessionID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "session_id is required"})
	}
	if err := pc.manager.ConnectProvisioner(c.Request().Context(), req); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "connected"})
}

func (pc *ProvisionerController) GetProvisionRequest(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	sessionID := c.Param("sessionId")
	podName := c.QueryParam("pod_name")
	wait := parseWait(c.QueryParam("wait"))
	deadline := time.Now().Add(wait)

	for {
		provisionReq, ok, err := pc.manager.ClaimProvisionRequest(c.Request().Context(), sessionID, podName)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if ok {
			return c.JSON(http.StatusOK, provisionReq)
		}
		if wait == 0 || time.Now().After(deadline) {
			return c.NoContent(http.StatusNoContent)
		}
		select {
		case <-c.Request().Context().Done():
			return c.Request().Context().Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (pc *ProvisionerController) UpdateProvisionRequestStatus(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var req services.ProvisionRequestStatusUpdate
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if req.Status == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "status is required"})
	}
	if err := pc.manager.UpdateProvisionRequestStatus(c.Request().Context(), c.Param("sessionId"), c.Param("requestId"), req); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (pc *ProvisionerController) authorized(c echo.Context) bool {
	if pc.manager == nil {
		return false
	}
	h := c.Request().Header.Get("Authorization")
	token := strings.TrimPrefix(h, "Bearer ")
	return pc.manager.ValidateProvisionerToken(token)
}

func parseWait(raw string) time.Duration {
	if raw == "" {
		return 0
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d > 30*time.Second {
			return 30 * time.Second
		}
		return d
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		d := time.Duration(n) * time.Second
		if d > 30*time.Second {
			return 30 * time.Second
		}
		return d
	}
	return 0
}
