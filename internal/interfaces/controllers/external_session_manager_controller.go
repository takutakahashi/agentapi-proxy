package controllers

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

type ESMRegistrationRequest struct {
	ManagerID   string            `json:"manager_id,omitempty"`
	InstanceID  string            `json:"instance_id"`
	Name        string            `json:"name"`
	Scope       string            `json:"scope,omitempty"`
	TeamID      string            `json:"team_id,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Default     bool              `json:"default,omitempty"`
	PublicURL   string            `json:"public_url,omitempty"`
	Version     string            `json:"version,omitempty"`
	RotateToken bool              `json:"rotate_token,omitempty"`
}

type ESMHeartbeatRequest struct {
	PublicURL      string `json:"public_url,omitempty"`
	Version        string `json:"version,omitempty"`
	ActiveSessions int    `json:"active_sessions,omitempty"`
}

type esmRegistrationResponse struct {
	ExternalSessionManagerResponse
	Created bool `json:"created"`
}

func (c *SettingsController) RegisterExternalSessionManager(ctx echo.Context) error {
	var req ESMRegistrationRequest
	if err := ctx.Bind(&req); err != nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.Name) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "instance_id and name are required")
	}
	name, err := c.esmSettingsName(ctx, req.Scope, req.TeamID, true)
	if err != nil {
		return err
	}

	c.esmMu.Lock()
	defer c.esmMu.Unlock()
	settings, err := c.findOrCreateSettings(ctx, name)
	if err != nil {
		return err
	}
	managers := append([]entities.ExternalSessionManagerEntry(nil), settings.ExternalSessionManagers()...)
	idx := -1
	for i := range managers {
		if managers[i].InstanceID == req.InstanceID || (req.ManagerID != "" && managers[i].ID == req.ManagerID) {
			idx = i
			break
		}
	}
	created := idx < 0
	connectionToken := ""
	if created || req.RotateToken {
		connectionToken, err = generateSettingsESMSecret(32)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate connection token")
		}
		if created {
			managerID := req.ManagerID
			if managerID == "" {
				managerID = uuid.NewString()
			}
			managers = append(managers, entities.ExternalSessionManagerEntry{ID: managerID, InstanceID: req.InstanceID, HMACSecret: connectionToken})
			idx = len(managers) - 1
		} else {
			managers[idx].HMACSecret = connectionToken
		}
	}
	if req.Default {
		for i := range managers {
			managers[i].Default = false
		}
	}
	manager := &managers[idx]
	manager.InstanceID = req.InstanceID
	manager.Name = req.Name
	manager.Labels = req.Labels
	manager.Default = req.Default
	manager.PublicURL = req.PublicURL
	manager.Version = req.Version
	settings.SetExternalSessionManagers(managers)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save external session manager")
	}
	return ctx.JSON(http.StatusOK, esmRegistrationResponse{ExternalSessionManagerResponse: esmResponse(*manager, connectionToken), Created: created})
}

func (c *SettingsController) ListExternalSessionManagers(ctx echo.Context) error {
	name, err := c.esmSettingsName(ctx, ctx.QueryParam("scope"), ctx.QueryParam("team_id"), false)
	if err != nil {
		return err
	}
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ctx.JSON(http.StatusOK, map[string]interface{}{"external_session_managers": []interface{}{}})
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list external session managers")
	}
	responses := make([]ExternalSessionManagerResponse, 0, len(settings.ExternalSessionManagers()))
	for _, manager := range settings.ExternalSessionManagers() {
		responses = append(responses, esmResponse(manager, ""))
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{"external_session_managers": responses})
}

func (c *SettingsController) GetExternalSessionManager(ctx echo.Context) error {
	manager, _, err := c.findAuthorizedESM(ctx, false)
	if err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, esmResponse(*manager, ""))
}

func (c *SettingsController) PatchExternalSessionManager(ctx echo.Context) error {
	var req ESMRegistrationRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	c.esmMu.Lock()
	defer c.esmMu.Unlock()
	manager, settings, err := c.findAuthorizedESM(ctx, true)
	if err != nil {
		return err
	}
	managers := settings.ExternalSessionManagers()
	for i := range managers {
		if managers[i].ID != manager.ID {
			if req.Default {
				managers[i].Default = false
			}
			continue
		}
		if req.Name != "" {
			managers[i].Name = req.Name
		}
		if req.InstanceID != "" {
			managers[i].InstanceID = req.InstanceID
		}
		if req.Labels != nil {
			managers[i].Labels = req.Labels
		}
		if req.PublicURL != "" {
			managers[i].PublicURL = req.PublicURL
		}
		if req.Version != "" {
			managers[i].Version = req.Version
		}
		managers[i].Default = req.Default
		manager = &managers[i]
	}
	settings.SetExternalSessionManagers(managers)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update external session manager")
	}
	return ctx.JSON(http.StatusOK, esmResponse(*manager, ""))
}

func (c *SettingsController) DeleteExternalSessionManager(ctx echo.Context) error {
	c.esmMu.Lock()
	defer c.esmMu.Unlock()
	manager, settings, err := c.findAuthorizedESM(ctx, true)
	if err != nil {
		return err
	}
	managers := settings.ExternalSessionManagers()
	updated := make([]entities.ExternalSessionManagerEntry, 0, len(managers)-1)
	for _, item := range managers {
		if item.ID != manager.ID {
			updated = append(updated, item)
		}
	}
	settings.SetExternalSessionManagers(updated)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete external session manager")
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SettingsController) RotateExternalSessionManagerToken(ctx echo.Context) error {
	c.esmMu.Lock()
	defer c.esmMu.Unlock()
	manager, settings, err := c.findAuthorizedESM(ctx, true)
	if err != nil {
		return err
	}
	token, err := generateSettingsESMSecret(32)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to rotate connection token")
	}
	managers := settings.ExternalSessionManagers()
	for i := range managers {
		if managers[i].ID == manager.ID {
			managers[i].HMACSecret = token
			manager = &managers[i]
		}
	}
	settings.SetExternalSessionManagers(managers)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save rotated token")
	}
	return ctx.JSON(http.StatusOK, esmResponse(*manager, token))
}

func (c *SettingsController) HeartbeatExternalSessionManager(ctx echo.Context) error {
	token := strings.TrimPrefix(ctx.Request().Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = ctx.Request().Header.Get("X-Session-Manager-Token")
	}
	var req ESMHeartbeatRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid heartbeat")
	}
	c.esmMu.Lock()
	defer c.esmMu.Unlock()
	settingsList, err := c.repo.List(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to validate manager")
	}
	for _, settings := range settingsList {
		managers := settings.ExternalSessionManagers()
		for i := range managers {
			if managers[i].ID == ctx.Param("id") && subtle.ConstantTimeCompare([]byte(managers[i].HMACSecret), []byte(token)) == 1 {
				managers[i].LastHeartbeatAt = time.Now().UTC()
				if req.PublicURL != "" {
					managers[i].PublicURL = req.PublicURL
				}
				if req.Version != "" {
					managers[i].Version = req.Version
				}
				managers[i].ActiveSessions = req.ActiveSessions
				if managers[i].PublicURL != "" {
					probeCtx, cancel := context.WithTimeout(ctx.Request().Context(), 3*time.Second)
					probeReq, _ := http.NewRequestWithContext(probeCtx, http.MethodGet, strings.TrimRight(managers[i].PublicURL, "/")+"/healthz", nil)
					probeResp, probeErr := http.DefaultClient.Do(probeReq)
					cancel()
					if probeErr != nil || probeResp.StatusCode != http.StatusOK {
						if probeResp != nil {
							_ = probeResp.Body.Close()
						}
						return echo.NewHTTPError(http.StatusFailedDependency, "public_url is not reachable from parent proxy")
					}
					_ = probeResp.Body.Close()
				}
				settings.SetExternalSessionManagers(managers)
				if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to save heartbeat")
				}
				return ctx.JSON(http.StatusOK, map[string]interface{}{"status": "ok", "manager_id": managers[i].ID, "server_time": managers[i].LastHeartbeatAt})
			}
		}
	}
	return echo.NewHTTPError(http.StatusUnauthorized, "invalid connection token")
}

func (c *SettingsController) esmSettingsName(ctx echo.Context, scope, teamID string, modify bool) (string, error) {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return "", echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	if scope == "" {
		scope = "user"
	}
	if scope != "user" && scope != "team" {
		return "", echo.NewHTTPError(http.StatusBadRequest, "scope must be user or team")
	}
	name := string(user.ID())
	if scope == "team" {
		if teamID == "" {
			return "", echo.NewHTTPError(http.StatusBadRequest, "team_id is required")
		}
		name = teamID
	}
	allowed := c.canAccess(user, name)
	if modify {
		allowed = c.canModify(user, name)
	}
	if !allowed {
		return "", echo.NewHTTPError(http.StatusForbidden, "access denied")
	}
	return name, nil
}

func (c *SettingsController) findOrCreateSettings(ctx echo.Context, name string) (*entities.Settings, error) {
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err == nil {
		return settings, nil
	}
	if strings.Contains(err.Error(), "not found") {
		return entities.NewSettings(name), nil
	}
	return nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to load settings")
}

func (c *SettingsController) findAuthorizedESM(ctx echo.Context, modify bool) (*entities.ExternalSessionManagerEntry, *entities.Settings, error) {
	name, err := c.esmSettingsName(ctx, ctx.QueryParam("scope"), ctx.QueryParam("team_id"), modify)
	if err != nil {
		return nil, nil, err
	}
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		return nil, nil, echo.NewHTTPError(http.StatusNotFound, "external session manager not found")
	}
	for _, manager := range settings.ExternalSessionManagers() {
		if manager.ID == ctx.Param("id") {
			copyManager := manager
			return &copyManager, settings, nil
		}
	}
	return nil, nil, echo.NewHTTPError(http.StatusNotFound, "external session manager not found")
}

func esmResponse(manager entities.ExternalSessionManagerEntry, token string) ExternalSessionManagerResponse {
	return ExternalSessionManagerResponse{ID: manager.ID, InstanceID: manager.InstanceID, Name: manager.Name,
		HasConnectionToken: manager.HMACSecret != "", ConnectionToken: token, Default: manager.Default,
		Labels: manager.Labels, PublicURL: manager.PublicURL, Version: manager.Version,
		ActiveSessions:  manager.ActiveSessions,
		LastHeartbeatAt: timePtrUnlessZero(manager.LastHeartbeatAt)}
}

func timePtrUnlessZero(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
