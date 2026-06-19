package controllers

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

type ProvisionerController struct {
	manager          *services.KubernetesSessionManager
	allocationQueue  sessionallocation.Queue
	settingsRepo     repositories.SettingsRepository
	sessionRouteRepo repositories.SessionRouteRepository
}

func NewProvisionerController(manager *services.KubernetesSessionManager, allocationQueue sessionallocation.Queue, settingsRepo repositories.SettingsRepository, sessionRouteRepo repositories.SessionRouteRepository) *ProvisionerController {
	return &ProvisionerController{manager: manager, allocationQueue: allocationQueue, settingsRepo: settingsRepo, sessionRouteRepo: sessionRouteRepo}
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

func (pc *ProvisionerController) GetNextSessionAllocation(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	wait := parseWait(c.QueryParam("wait"))
	req, ok, err := pc.allocationQueue.NextSessionAllocation(c.Request().Context(), wait)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if !ok {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, req)
}

func (pc *ProvisionerController) CompleteSessionAllocation(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	var result sessionallocation.AllocationResult
	if err := c.Bind(&result); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if result.Status == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "status is required"})
	}
	if err := pc.allocationQueue.CompleteSessionAllocation(c.Request().Context(), c.Param("sessionId"), result); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (pc *ProvisionerController) GetNextExternalSessionAllocation(c echo.Context) error {
	managerID, _, ok := pc.authorizedExternalManager(c)
	if !ok {
		return c.NoContent(http.StatusUnauthorized)
	}
	wait := parseWait(c.QueryParam("wait"))
	req, found, err := pc.allocationQueue.NextExternalSessionAllocation(c.Request().Context(), managerID, wait)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if !found {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, req)
}

func (pc *ProvisionerController) CompleteExternalSessionAllocation(c echo.Context) error {
	_, managerSecret, ok := pc.authorizedExternalManager(c)
	if !ok {
		return c.NoContent(http.StatusUnauthorized)
	}
	var result sessionallocation.AllocationResult
	if err := c.Bind(&result); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if result.Status == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "status is required"})
	}
	if result.Status == sessionallocation.StatusAssigned && result.AllocatedSessionID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "allocated_session_id is required when status is assigned"})
	}
	allocation, err := pc.allocationQueue.CompleteExternalSessionAllocation(c.Request().Context(), c.Param("sessionId"), result)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if result.Status != sessionallocation.StatusAssigned {
		if pc.sessionRouteRepo != nil {
			_ = pc.sessionRouteRepo.Delete(c.Request().Context(), allocation.SessionID)
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
	if pc.sessionRouteRepo != nil {
		route, err := pc.sessionRouteRepo.Get(c.Request().Context(), allocation.SessionID)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if route == nil {
			route = &repositories.SessionRoute{
				SessionID:  allocation.SessionID,
				StartedAt:  time.Now(),
				HMACSecret: managerSecret,
			}
		}
		route.RemoteSessionID = result.AllocatedSessionID
		route.ProxyURL = result.ProxyURL
		if route.HMACSecret == "" {
			route.HMACSecret = managerSecret
		}
		if allocation.Request != nil {
			route.UserID = allocation.Request.UserID
			route.Scope = string(allocation.Request.Scope)
			route.TeamID = allocation.Request.TeamID
			route.Tags = allocation.Request.Tags
			route.InitialMessage = allocation.Request.InitialMessage
		}
		if err := pc.sessionRouteRepo.Save(c.Request().Context(), route); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
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

func (pc *ProvisionerController) authorizedExternalManager(c echo.Context) (string, string, bool) {
	if pc.manager == nil || pc.settingsRepo == nil {
		return "", "", false
	}
	h := c.Request().Header.Get("Authorization")
	token := strings.TrimPrefix(h, "Bearer ")
	if token == "" || token == h {
		return "", "", false
	}
	settingsList, err := pc.settingsRepo.List(c.Request().Context())
	if err != nil {
		return "", "", false
	}
	for _, settings := range settingsList {
		for _, manager := range settings.ExternalSessionManagers() {
			if manager.HMACSecret != "" && subtle.ConstantTimeCompare([]byte(manager.HMACSecret), []byte(token)) == 1 {
				return manager.ID, manager.HMACSecret, true
			}
		}
	}
	return "", "", false
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
