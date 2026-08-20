package controllers

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sessionrunnercore "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	validation "k8s.io/apimachinery/pkg/util/validation"
)

type ESMUpdateRequest struct {
	InstanceID                 string            `json:"instance_id"`
	Name                       string            `json:"name"`
	Pool                       string            `json:"pool"`
	Labels                     map[string]string `json:"labels,omitempty"`
	PoolEnabled                *bool             `json:"pool_enabled,omitempty"`
	AutomaticAssignmentEnabled *bool             `json:"automatic_assignment_enabled,omitempty"`
	PublicURL                  string            `json:"public_url,omitempty"`
	Version                    string            `json:"version,omitempty"`
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

type ESMEnrollmentTokenRequest struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
}

type esmEnrollmentTokenResponse struct {
	ManagerID         string    `json:"manager_id"`
	Pool              string    `json:"pool"`
	RegistrationToken string    `json:"registration_token"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type ESMEnrollmentRequest struct {
	RegistrationToken          string            `json:"registration_token"`
	InstanceID                 string            `json:"instance_id"`
	Name                       string            `json:"name"`
	Labels                     map[string]string `json:"labels,omitempty"`
	PoolEnabled                *bool             `json:"pool_enabled,omitempty"`
	AutomaticAssignmentEnabled bool              `json:"automatic_assignment_enabled,omitempty"`
	PublicURL                  string            `json:"public_url,omitempty"`
	Version                    string            `json:"version,omitempty"`
}

func (c *SettingsController) IssueExternalSessionManagerEnrollmentToken(ctx echo.Context) error {
	var req ESMEnrollmentTokenRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	user := auth.GetUserFromContext(ctx)
	if req.Scope == "" && user != nil && user.UserType() == entities.UserTypeServiceAccount && user.TeamID() != "" {
		req.Scope = "team"
		req.TeamID = user.TeamID()
	}
	if req.Scope == "" {
		req.Scope = "user"
	}
	name, err := c.esmSettingsName(ctx, req.Scope, req.TeamID, true)
	if err != nil {
		return err
	}
	token, err := generateSettingsESMSecret(32)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate registration token")
	}
	managerID := uuid.NewString()
	pool := ""
	expiresAt := time.Now().UTC().Add(15 * time.Minute)
	hash := sha256.Sum256([]byte(token))

	c.esmMu.Lock()
	defer c.esmMu.Unlock()
	settings, err := c.findOrCreateSettings(ctx, name)
	if err != nil {
		return err
	}
	managers := make([]entities.ExternalSessionManagerEntry, 0, len(settings.ExternalSessionManagers())+1)
	for _, manager := range settings.ExternalSessionManagers() {
		if manager.HMACSecret == "" && !manager.EnrollmentExpiresAt.IsZero() && time.Now().UTC().After(manager.EnrollmentExpiresAt) {
			continue
		}
		managers = append(managers, manager)
	}
	managers = append(managers, entities.ExternalSessionManagerEntry{
		ID: managerID, Name: "pending registration", EnrollmentTokenHash: hex.EncodeToString(hash[:]), EnrollmentExpiresAt: expiresAt,
		Pool: pool, BindingSubjectType: req.Scope, BindingSubjectID: name,
	})
	settings.SetExternalSessionManagers(managers)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save registration token")
	}
	return ctx.JSON(http.StatusCreated, esmEnrollmentTokenResponse{
		ManagerID: managerID, Pool: pool, RegistrationToken: token, ExpiresAt: expiresAt,
	})
}

func (c *SettingsController) EnrollExternalSessionManager(ctx echo.Context) error {
	var req ESMEnrollmentRequest
	if err := ctx.Bind(&req); err != nil || strings.TrimSpace(req.RegistrationToken) == "" ||
		strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.Name) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "registration_token, instance_id and name are required")
	}
	tokenHash := sha256.Sum256([]byte(req.RegistrationToken))
	hashString := hex.EncodeToString(tokenHash[:])

	c.esmMu.Lock()
	defer c.esmMu.Unlock()
	settingsList, err := c.repo.List(ctx.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to validate registration token")
	}
	now := time.Now().UTC()
	for _, settings := range settingsList {
		managers := settings.ExternalSessionManagers()
		for i := range managers {
			if subtle.ConstantTimeCompare([]byte(managers[i].EnrollmentTokenHash), []byte(hashString)) != 1 {
				continue
			}
			if managers[i].EnrollmentExpiresAt.IsZero() || now.After(managers[i].EnrollmentExpiresAt) {
				return echo.NewHTTPError(http.StatusUnauthorized, "registration token has expired")
			}
			connectionToken, genErr := generateSettingsESMSecret(32)
			if genErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate connection token")
			}
			manager := &managers[i]
			manager.InstanceID = req.InstanceID
			manager.Name = req.Name
			manager.Pool = nativePoolName(req.Name, manager.ID)
			manager.HMACSecret = connectionToken
			manager.Labels = req.Labels
			manager.PoolEnabled = req.PoolEnabled
			manager.AutomaticAssignmentEnabled = req.AutomaticAssignmentEnabled
			manager.LegacySchedulable = false
			manager.LegacyDefault = false
			manager.PublicURL = req.PublicURL
			manager.Version = req.Version
			manager.EnrollmentTokenHash = ""
			manager.EnrollmentExpiresAt = time.Time{}
			if err := c.provisionExternalManagerPool(ctx.Request().Context(), manager, connectionToken); err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to provision manager pool").SetInternal(err)
			}
			settings.SetExternalSessionManagers(managers)
			if saveErr := c.repo.Save(ctx.Request().Context(), settings); saveErr != nil {
				_ = c.deleteExternalManagerPool(ctx.Request().Context(), *manager)
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to enroll external session manager")
			}
			return ctx.JSON(http.StatusOK, esmRegistrationResponse{
				ExternalSessionManagerResponse: esmResponse(*manager, connectionToken), Created: true,
			})
		}
	}
	return echo.NewHTTPError(http.StatusUnauthorized, "invalid or already used registration token")
}

var invalidPoolCharacters = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func nativePoolName(deviceName, managerID string) string {
	pool := strings.Trim(invalidPoolCharacters.ReplaceAllString(strings.TrimSpace(deviceName), "-"), "-_.")
	if len(pool) > 63 {
		pool = strings.Trim(pool[:63], "-_.")
	}
	if pool == "" {
		return "esm-" + managerID
	}
	return pool
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
		if manager.HMACSecret == "" {
			continue
		}
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
	var req ESMUpdateRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	c.esmMu.Lock()
	defer c.esmMu.Unlock()
	manager, settings, err := c.findAuthorizedESM(ctx, true)
	if err != nil {
		return err
	}
	previousPool := manager.Pool
	requestedPool := strings.TrimSpace(req.Pool)
	if requestedPool != "" {
		if problems := validation.IsValidLabelValue(requestedPool); len(problems) > 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "pool name must be a valid Kubernetes label value")
		}
		if requestedPool != previousPool && manager.ActiveSessions > 0 {
			return echo.NewHTTPError(http.StatusConflict, "pool name cannot be changed while sessions are active")
		}
		if requestedPool != previousPool && c.sessionRunnerStore != nil {
			if _, getErr := c.sessionRunnerStore.GetLogicalPool(ctx.Request().Context(), requestedPool); getErr == nil {
				return echo.NewHTTPError(http.StatusConflict, "pool name is already in use")
			} else if !errors.Is(getErr, sessionrunnercore.ErrNotFound) {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to validate pool name").SetInternal(getErr)
			}
		}
	}
	managers := settings.ExternalSessionManagers()
	for i := range managers {
		if managers[i].ID != manager.ID {
			continue
		}
		if req.Name != "" {
			managers[i].Name = req.Name
		}
		if requestedPool != "" {
			managers[i].Pool = requestedPool
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
		if req.AutomaticAssignmentEnabled != nil {
			managers[i].AutomaticAssignmentEnabled = *req.AutomaticAssignmentEnabled
			managers[i].LegacySchedulable = false
			managers[i].LegacyDefault = false
		}
		if req.PoolEnabled != nil {
			managers[i].PoolEnabled = req.PoolEnabled
		}
		manager = &managers[i]
	}
	settings.SetExternalSessionManagers(managers)
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update external session manager")
	}
	if err := c.syncExternalManagerPool(ctx.Request().Context(), *manager, previousPool); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update manager pool").SetInternal(err)
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
	if err := c.deleteExternalManagerPool(ctx.Request().Context(), *manager); err != nil {
		return echo.NewHTTPError(http.StatusConflict, "failed to delete manager pool resources").SetInternal(err)
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

func (c *SettingsController) provisionExternalManagerPool(ctx context.Context, manager *entities.ExternalSessionManagerEntry, token string) error {
	if c.sessionRunnerStore == nil {
		return nil
	}
	if manager.Pool == "" {
		manager.Pool = "esm-" + manager.ID
	}
	hash := sha256.Sum256([]byte(token))
	registryManager := &sessionrunnercore.Manager{ID: manager.ID, Name: manager.Name, Labels: manager.Labels,
		Capabilities: []string{sessionrunnercore.CapabilityRunnerClaimV1, sessionrunnercore.CapabilityDirectRuntimeV1},
		Enabled:      true, ConnectionTokenHash: hex.EncodeToString(hash[:])}
	if err := c.sessionRunnerStore.CreateManager(ctx, registryManager); err != nil {
		return err
	}
	pool := &sessionrunnercore.LogicalPool{Name: manager.Pool, Labels: manager.Labels, Enabled: true}
	if err := c.sessionRunnerStore.CreateLogicalPool(ctx, pool); err != nil {
		_ = c.sessionRunnerStore.DeleteManager(ctx, manager.ID)
		return err
	}
	supplier := &sessionrunnercore.PoolSupplier{Pool: manager.Pool, ManagerID: manager.ID, MinIdle: 1, MaxRunners: 10, Enabled: true}
	if err := c.sessionRunnerStore.CreatePoolSupplier(ctx, supplier); err != nil {
		_ = c.sessionRunnerStore.DeleteLogicalPool(ctx, manager.Pool)
		_ = c.sessionRunnerStore.DeleteManager(ctx, manager.ID)
		return err
	}
	subjectType := sessionrunnercore.SubjectType(manager.BindingSubjectType)
	binding := &sessionrunnercore.Binding{Pool: manager.Pool, SubjectType: subjectType, SubjectID: manager.BindingSubjectID,
		Role: sessionrunnercore.BindingRoleManage, Enabled: manager.IsPoolEnabled(), ExplicitOnly: !manager.IsAutomaticAssignmentEnabled()}
	if err := c.sessionRunnerStore.CreateBinding(ctx, binding); err != nil {
		_ = c.sessionRunnerStore.DeletePoolSupplier(ctx, manager.ID, manager.Pool)
		_ = c.sessionRunnerStore.DeleteLogicalPool(ctx, manager.Pool)
		_ = c.sessionRunnerStore.DeleteManager(ctx, manager.ID)
		return err
	}
	return nil
}

func (c *SettingsController) syncExternalManagerPool(ctx context.Context, manager entities.ExternalSessionManagerEntry, previousPools ...string) error {
	if c.sessionRunnerStore == nil || manager.Pool == "" {
		return nil
	}
	previousPool := manager.Pool
	if len(previousPools) > 0 && previousPools[0] != "" {
		previousPool = previousPools[0]
	}
	registered, err := c.sessionRunnerStore.GetManager(ctx, manager.ID)
	if err != nil {
		return err
	}
	registered.Name, registered.Labels, registered.Enabled = manager.Name, manager.Labels, true
	if err := c.sessionRunnerStore.UpdateManager(ctx, registered); err != nil {
		return err
	}
	if previousPool != manager.Pool {
		if err := c.renameExternalManagerPool(ctx, manager, previousPool); err != nil {
			return err
		}
	}
	pool, err := c.sessionRunnerStore.GetLogicalPool(ctx, manager.Pool)
	if err != nil {
		return err
	}
	pool.Labels, pool.Enabled = manager.Labels, true
	if err := c.sessionRunnerStore.UpdateLogicalPool(ctx, pool); err != nil {
		return err
	}
	supplier, err := c.sessionRunnerStore.GetPoolSupplier(ctx, manager.ID, manager.Pool)
	if err != nil {
		return err
	}
	supplier.Enabled = true
	if err := c.sessionRunnerStore.UpdatePoolSupplier(ctx, supplier); err != nil {
		return err
	}
	bindings, err := c.sessionRunnerStore.ListBindings(ctx, manager.Pool)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		binding.Enabled = manager.IsPoolEnabled()
		binding.ExplicitOnly = !manager.IsAutomaticAssignmentEnabled()
		if err := c.sessionRunnerStore.UpdateBinding(ctx, binding); err != nil {
			return err
		}
	}
	return nil
}

func (c *SettingsController) renameExternalManagerPool(ctx context.Context, manager entities.ExternalSessionManagerEntry, previousPool string) error {
	if _, err := c.sessionRunnerStore.GetLogicalPool(ctx, manager.Pool); err == nil {
		return sessionrunnercore.ErrConflict
	} else if !errors.Is(err, sessionrunnercore.ErrNotFound) {
		return err
	}
	if err := c.sessionRunnerStore.CreateLogicalPool(ctx, &sessionrunnercore.LogicalPool{Name: manager.Pool, Labels: manager.Labels, Enabled: true}); err != nil {
		return err
	}
	if err := c.sessionRunnerStore.CreatePoolSupplier(ctx, &sessionrunnercore.PoolSupplier{Pool: manager.Pool, ManagerID: manager.ID, MinIdle: 1, MaxRunners: 10, Enabled: true}); err != nil {
		_ = c.sessionRunnerStore.DeleteLogicalPool(ctx, manager.Pool)
		return err
	}
	if err := c.sessionRunnerStore.CreateBinding(ctx, &sessionrunnercore.Binding{
		Pool: manager.Pool, SubjectType: sessionrunnercore.SubjectType(manager.BindingSubjectType), SubjectID: manager.BindingSubjectID,
		Role: sessionrunnercore.BindingRoleManage, Enabled: manager.IsPoolEnabled(), ExplicitOnly: !manager.IsAutomaticAssignmentEnabled(),
	}); err != nil {
		_ = c.sessionRunnerStore.DeletePoolSupplier(ctx, manager.ID, manager.Pool)
		_ = c.sessionRunnerStore.DeleteLogicalPool(ctx, manager.Pool)
		return err
	}
	bindings, err := c.sessionRunnerStore.ListBindings(ctx, previousPool)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if err := c.sessionRunnerStore.DeleteBinding(ctx, binding.ID); err != nil {
			return err
		}
	}
	if err := c.sessionRunnerStore.DeletePoolSupplier(ctx, manager.ID, previousPool); err != nil {
		return err
	}
	if err := c.sessionRunnerStore.DeleteLogicalPool(ctx, previousPool); err != nil {
		return err
	}
	return nil
}

func (c *SettingsController) deleteExternalManagerPool(ctx context.Context, manager entities.ExternalSessionManagerEntry) error {
	if c.sessionRunnerStore == nil || manager.Pool == "" {
		return nil
	}
	bindings, err := c.sessionRunnerStore.ListBindings(ctx, manager.Pool)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if err := c.sessionRunnerStore.DeleteBinding(ctx, binding.ID); err != nil {
			return err
		}
	}
	if err := c.sessionRunnerStore.DeletePoolSupplier(ctx, manager.ID, manager.Pool); err != nil && !errors.Is(err, sessionrunnercore.ErrNotFound) {
		return err
	}
	if err := c.sessionRunnerStore.DeleteLogicalPool(ctx, manager.Pool); err != nil && !errors.Is(err, sessionrunnercore.ErrNotFound) {
		return err
	}
	if err := c.sessionRunnerStore.DeleteManager(ctx, manager.ID); err != nil && !errors.Is(err, sessionrunnercore.ErrNotFound) {
		return err
	}
	return nil
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
	if c.sessionRunnerStore != nil {
		registered, getErr := c.sessionRunnerStore.GetManager(ctx.Request().Context(), manager.ID)
		if getErr == nil {
			hash := sha256.Sum256([]byte(token))
			registered.ConnectionTokenHash = hex.EncodeToString(hash[:])
			if updateErr := c.sessionRunnerStore.UpdateManager(ctx.Request().Context(), registered); updateErr != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "failed to rotate manager pool token").SetInternal(updateErr)
			}
		}
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
				outboundConnected := c.esmControlTunnel != nil && c.esmControlTunnel.IsConnected(ctx.Request().Context(), managers[i].ID)
				if managers[i].PublicURL != "" && !outboundConnected {
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
				if c.sessionRunnerStore != nil {
					if registered, getErr := c.sessionRunnerStore.GetManager(ctx.Request().Context(), managers[i].ID); getErr == nil {
						registered.LastHeartbeatAt = managers[i].LastHeartbeatAt
						registered.Labels = managers[i].Labels
						_ = c.sessionRunnerStore.UpdateManager(ctx.Request().Context(), registered)
					}
				}
				transport := "public_url"
				if outboundConnected {
					transport = "outbound_control"
				}
				return ctx.JSON(http.StatusOK, map[string]interface{}{"status": "ok", "manager_id": managers[i].ID, "server_time": managers[i].LastHeartbeatAt, "transport": transport})
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
		if !user.IsAdmin() && !user.IsMemberOfTeam(teamID) {
			return "", echo.NewHTTPError(http.StatusForbidden, "access denied")
		}
		return name, nil
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
		HasConnectionToken: manager.HMACSecret != "", ConnectionToken: token,
		PoolEnabled:                manager.IsPoolEnabled(),
		AutomaticAssignmentEnabled: manager.IsAutomaticAssignmentEnabled(),
		Labels:                     manager.Labels, PublicURL: manager.PublicURL, Version: manager.Version,
		ActiveSessions:  manager.ActiveSessions,
		LastHeartbeatAt: timePtrUnlessZero(manager.LastHeartbeatAt), Pool: manager.Pool}
}

func timePtrUnlessZero(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
