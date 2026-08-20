package controllers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

type ProvisionerController struct {
	manager          ProvisionerManager
	allocationQueue  sessionallocation.Queue
	settingsRepo     repositories.SettingsRepository
	sessionRouteRepo repositories.SessionRouteRepository
	stateStore       services.SessionStateStore
}

type ProvisionerManager interface {
	ValidateProvisionerToken(token string) bool
	ValidateSessionControlToken(sessionID, token string) bool
	ConnectProvisioner(ctx context.Context, req services.ProvisionerConnectRequest) error
	ClaimProvisionRequest(ctx context.Context, sessionID, podName string) (*services.ProvisionRequest, bool, error)
	UpdateProvisionRequestStatus(ctx context.Context, sessionID, requestID string, req services.ProvisionRequestStatusUpdate) error
}

type sessionSuspendScheduler interface {
	ScheduleSessionSuspend(ctx context.Context, sessionID string) error
}

type externalRuntimeProfileProvider interface {
	ExternalRuntimeProfile() *sessionsettings.RuntimeProfile
}

func (pc *ProvisionerController) externalRuntimeProfileSnapshot() (*sessionallocation.RuntimeProfileSnapshot, error) {
	provider, ok := pc.allocationQueue.(externalRuntimeProfileProvider)
	if !ok {
		return nil, errors.New("runtime profile is unavailable")
	}
	profile := provider.ExternalRuntimeProfile()
	data, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return &sessionallocation.RuntimeProfileSnapshot{Revision: hex.EncodeToString(sum[:]), Profile: profile}, nil
}

func NewProvisionerController(manager ProvisionerManager, allocationQueue sessionallocation.Queue, settingsRepo repositories.SettingsRepository, sessionRouteRepo repositories.SessionRouteRepository, stateStore ...services.SessionStateStore) *ProvisionerController {
	pc := &ProvisionerController{manager: manager, allocationQueue: allocationQueue, settingsRepo: settingsRepo, sessionRouteRepo: sessionRouteRepo}
	if len(stateStore) > 0 {
		pc.stateStore = stateStore[0]
	}
	return pc
}

const maxSessionStateBytes = 1 << 30

type multipartStartResponse struct {
	UploadID string `json:"upload_id"`
	PartSize int64  `json:"part_size"`
}

func (pc *ProvisionerController) multipartStore(c echo.Context) (services.MultipartSessionStateStore, error) {
	if !pc.authorized(c) {
		return nil, echo.NewHTTPError(http.StatusUnauthorized)
	}
	store, ok := pc.stateStore.(services.MultipartSessionStateStore)
	if !ok {
		return nil, echo.NewHTTPError(http.StatusNotImplemented, "direct transfer is unavailable")
	}
	return store, nil
}

func (pc *ProvisionerController) BeginSessionStateUpload(c echo.Context) error {
	store, err := pc.multipartStore(c)
	if err != nil {
		return err
	}
	uploadID, partSize, err := store.BeginMultipart(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence backend is unavailable"})
	}
	return c.JSON(http.StatusOK, multipartStartResponse{UploadID: uploadID, PartSize: partSize})
}

func (pc *ProvisionerController) PresignSessionStatePart(c echo.Context) error {
	store, err := pc.multipartStore(c)
	if err != nil {
		return err
	}
	number, err := strconv.ParseInt(c.Param("partNumber"), 10, 32)
	if err != nil || number < 1 || number > maxSessionStateBytes/(8<<20) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid part number"})
	}
	url, err := store.PresignPart(c.Request().Context(), c.Param("sessionId"), c.Param("uploadId"), int32(number))
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence backend is unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]string{"url": url})
}

func (pc *ProvisionerController) CompleteSessionStateUpload(c echo.Context) error {
	store, err := pc.multipartStore(c)
	if err != nil {
		return err
	}
	var req struct {
		Parts []services.MultipartPart `json:"parts"`
	}
	if err := c.Bind(&req); err != nil || len(req.Parts) == 0 || len(req.Parts) > maxSessionStateBytes/(8<<20) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "parts are required"})
	}
	if err := store.CompleteMultipart(c.Request().Context(), c.Param("sessionId"), c.Param("uploadId"), req.Parts); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence backend is unavailable"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (pc *ProvisionerController) AbortSessionStateUpload(c echo.Context) error {
	store, err := pc.multipartStore(c)
	if err != nil {
		return err
	}
	if err := store.AbortMultipart(c.Request().Context(), c.Param("sessionId"), c.Param("uploadId")); err != nil {
		return c.NoContent(http.StatusServiceUnavailable)
	}
	return c.NoContent(http.StatusNoContent)
}

func (pc *ProvisionerController) PresignSessionStateDownload(c echo.Context) error {
	store, err := pc.multipartStore(c)
	if err != nil {
		return err
	}
	url, err := store.PresignDownload(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence backend is unavailable"})
	}
	return c.JSON(http.StatusOK, map[string]string{"url": url})
}

func (pc *ProvisionerController) SaveSessionState(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if pc.stateStore == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence is disabled"})
	}
	body := http.MaxBytesReader(c.Response(), c.Request().Body, maxSessionStateBytes)
	if err := pc.stateStore.Save(c.Request().Context(), c.Param("sessionId"), body); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return c.JSON(http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
		}
		log.Printf("[SESSION_STATE] Backup skipped because persistence backend is unavailable: %v", err)
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence backend is unavailable"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (pc *ProvisionerController) ScheduleSessionSuspend(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	scheduler, ok := pc.manager.(sessionSuspendScheduler)
	if !ok || pc.stateStore == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence suspend is unavailable"})
	}
	if err := scheduler.ScheduleSessionSuspend(c.Request().Context(), c.Param("sessionId")); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}

func (pc *ProvisionerController) LoadSessionState(c echo.Context) error {
	if !pc.authorized(c) {
		return c.NoContent(http.StatusUnauthorized)
	}
	if pc.stateStore == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence is disabled"})
	}
	body, err := pc.stateStore.Load(c.Request().Context(), c.Param("sessionId"))
	if os.IsNotExist(err) {
		return c.NoContent(http.StatusNotFound)
	}
	if err != nil {
		log.Printf("[SESSION_STATE] Restore skipped because persistence backend is unavailable: %v", err)
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "session persistence backend is unavailable"})
	}
	defer func() { _ = body.Close() }()
	return c.Stream(http.StatusOK, "application/zstd", body)
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
	allocation, err := pc.allocationQueue.CompleteSessionAllocation(c.Request().Context(), c.Param("sessionId"), result)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if result.Status == sessionallocation.StatusAssigned && result.AllocatedSessionID != "" && result.AllocatedSessionID != allocation.SessionID && pc.sessionRouteRepo != nil {
		route := &repositories.SessionRoute{
			SessionID:       allocation.SessionID,
			RemoteSessionID: result.AllocatedSessionID,
			StartedAt:       time.Now(),
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

func (pc *ProvisionerController) GetNextExternalSessionAllocation(c echo.Context) error {
	managerID, _, ok := pc.authorizedExternalManager(c)
	if !ok {
		return c.NoContent(http.StatusUnauthorized)
	}
	if snapshot, snapshotErr := pc.externalRuntimeProfileSnapshot(); snapshotErr == nil {
		c.Response().Header().Set("X-AgentAPI-Runtime-Profile-Revision", snapshot.Revision)
		if _, present := c.QueryParams()["profile_revision"]; present && c.QueryParam("profile_revision") != snapshot.Revision {
			return c.NoContent(http.StatusNoContent)
		}
	}
	wait := parseWait(c.QueryParam("wait"))
	req, found, err := pc.allocationQueue.NextExternalSessionAllocation(c.Request().Context(), managerID, wait)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if !found {
		return c.NoContent(http.StatusNoContent)
	}
	if c.QueryParam("metadata_only") == "true" {
		req = allocationMetadata(req)
	}
	return c.JSON(http.StatusOK, req)
}

func (pc *ProvisionerController) GetExternalSessionManagerRuntimeProfile(c echo.Context) error {
	if _, _, ok := pc.authorizedExternalManager(c); !ok {
		return c.NoContent(http.StatusUnauthorized)
	}
	snapshot, err := pc.externalRuntimeProfileSnapshot()
	if err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}
	return c.JSON(http.StatusOK, snapshot)
}

func allocationMetadata(req *sessionallocation.AllocationRequest) *sessionallocation.AllocationRequest {
	if req == nil {
		return nil
	}
	copyReq := *req
	copyReq.ProvisionSettings = nil
	if req.Request != nil {
		copyReq.Request = &entities.RunServerRequest{
			UserID:    req.Request.UserID,
			Tags:      req.Request.Tags,
			Teams:     req.Request.Teams,
			Scope:     req.Request.Scope,
			TeamID:    req.Request.TeamID,
			AgentType: req.Request.AgentType,
			Oneshot:   req.Request.Oneshot,
		}
	}
	return &copyReq
}

func (pc *ProvisionerController) CompleteExternalSessionAllocation(c echo.Context) error {
	managerID, managerSecret, ok := pc.authorizedExternalManager(c)
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
				ManagerID:  managerID,
			}
		}
		route.ManagerID = managerID
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
	if pc.settingsRepo == nil {
		return "", "", false
	}
	token := c.Request().Header.Get("X-Session-Manager-Token")
	if token == "" {
		h := c.Request().Header.Get("Authorization")
		token = strings.TrimPrefix(h, "Bearer ")
		if token == "" || token == h {
			return "", "", false
		}
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
