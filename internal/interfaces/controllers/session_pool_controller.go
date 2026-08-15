package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionrunner"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	validation "k8s.io/apimachinery/pkg/util/validation"
)

type SessionPoolController struct {
	store    core.Store
	resolver *core.Resolver
	routes   portrepos.SessionRouteRepository
	now      func() time.Time
}

func NewSessionPoolController(store core.Store, routes portrepos.SessionRouteRepository) *SessionPoolController {
	return &SessionPoolController{store: store, resolver: core.NewResolver(store, 90*time.Second), routes: routes, now: func() time.Time { return time.Now().UTC() }}
}

type managerCreateRequest struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Labels       map[string]string `json:"labels,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
}

func (c *SessionPoolController) CreateManager(ctx echo.Context) error {
	var input managerCreateRequest
	if err := ctx.Bind(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create connection token")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	manager := &core.Manager{ID: input.ID, Name: input.Name, Labels: input.Labels, Capabilities: input.Capabilities, Enabled: enabled, ConnectionTokenHash: tokenHash}
	if err := c.store.CreateManager(ctx.Request().Context(), manager); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, map[string]any{"manager": redactManager(manager), "connection_token": token})
}

func (c *SessionPoolController) ListManagers(ctx echo.Context) error {
	managers, err := c.store.ListManagers(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	result := make([]*core.Manager, 0, len(managers))
	for _, manager := range managers {
		result = append(result, redactManager(manager))
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_managers": result})
}

func (c *SessionPoolController) PatchManager(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	var patch struct {
		Name         *string           `json:"name,omitempty"`
		Labels       map[string]string `json:"labels,omitempty"`
		Capabilities []string          `json:"capabilities,omitempty"`
		Enabled      *bool             `json:"enabled,omitempty"`
		Draining     *bool             `json:"draining,omitempty"`
	}
	if err := ctx.Bind(&patch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if patch.Name != nil {
		manager.Name = *patch.Name
	}
	if patch.Labels != nil {
		manager.Labels = patch.Labels
	}
	if patch.Capabilities != nil {
		manager.Capabilities = patch.Capabilities
	}
	if patch.Enabled != nil {
		manager.Enabled = *patch.Enabled
	}
	if patch.Draining != nil {
		manager.Draining = *patch.Draining
	}
	if err := c.store.UpdateManager(ctx.Request().Context(), manager); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, redactManager(manager))
}

func (c *SessionPoolController) CreatePool(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	var pool core.Pool
	pool.Enabled = true
	if err := ctx.Bind(&pool); err != nil || strings.TrimSpace(pool.Name) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name is required")
	}
	if problems := validation.IsValidLabelValue(pool.Name); len(problems) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name must be a valid Kubernetes label value")
	}
	if pool.MinIdle < 0 || pool.MaxRunners < 0 || (pool.MaxRunners > 0 && pool.MinIdle > pool.MaxRunners) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid runner limits")
	}
	pool.ManagerID = manager.ID
	if err := c.store.CreatePool(ctx.Request().Context(), &pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, &pool)
}

func (c *SessionPoolController) ListPools(ctx echo.Context) error {
	pools, err := c.store.ListPools(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_pools": pools})
}

func (c *SessionPoolController) PatchPool(ctx echo.Context) error {
	pool, err := c.store.GetPool(ctx.Request().Context(), ctx.Param("id"), ctx.Param("pool"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	var patch struct {
		Labels     map[string]string `json:"labels,omitempty"`
		MinIdle    *int              `json:"min_idle,omitempty"`
		MaxRunners *int              `json:"max_runners,omitempty"`
		Enabled    *bool             `json:"enabled,omitempty"`
		Draining   *bool             `json:"draining,omitempty"`
		IsDefault  *bool             `json:"default,omitempty"`
	}
	if err := ctx.Bind(&patch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if patch.Labels != nil {
		pool.Labels = patch.Labels
	}
	if patch.MinIdle != nil {
		pool.MinIdle = *patch.MinIdle
	}
	if patch.MaxRunners != nil {
		pool.MaxRunners = *patch.MaxRunners
	}
	if patch.Enabled != nil {
		pool.Enabled = *patch.Enabled
	}
	if patch.Draining != nil {
		pool.Draining = *patch.Draining
	}
	if patch.IsDefault != nil {
		pool.IsDefault = *patch.IsDefault
	}
	if pool.MinIdle < 0 || pool.MaxRunners < 0 || (pool.MaxRunners > 0 && pool.MinIdle > pool.MaxRunners) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid runner limits")
	}
	if err := c.store.UpdatePool(ctx.Request().Context(), pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, pool)
}

func (c *SessionPoolController) CreateBinding(ctx echo.Context) error {
	var binding core.Binding
	binding.Enabled = true
	if err := ctx.Bind(&binding); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	binding.Pool = ctx.Param("pool")
	if err := validatePoolSubject(binding.SubjectType, binding.SubjectID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := c.ensureLogicalPoolExists(ctx.Request().Context(), binding.Pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.store.CreateBinding(ctx.Request().Context(), &binding); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, &binding)
}

func (c *SessionPoolController) DeleteBinding(ctx echo.Context) error {
	if err := c.store.DeleteBinding(ctx.Request().Context(), ctx.Param("bindingId")); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) ListBindings(ctx echo.Context) error {
	bindings, err := c.store.ListBindings(ctx.Request().Context(), ctx.Param("pool"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"pool_bindings": bindings})
}

func (c *SessionPoolController) ListAvailablePools(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	pools, err := c.resolver.AvailablePools(ctx.Request().Context(), string(user.ID()), userTeamIDsForPools(user))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_pools": pools})
}

func (c *SessionPoolController) PutPreference(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	var preference core.Preference
	preference.Enabled = true
	if err := ctx.Bind(&preference); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if preference.SubjectType == "" {
		preference.SubjectType = core.SubjectUser
	}
	if preference.SubjectID == "" {
		preference.SubjectID = string(user.ID())
	}
	if preference.SubjectType == core.SubjectUser && preference.SubjectID != string(user.ID()) && !user.IsAdmin() {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}
	if preference.SubjectType == core.SubjectTeam && !user.IsAdmin() && !user.IsMemberOfTeam(preference.SubjectID) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}
	if err := validatePoolSubject(preference.SubjectType, preference.SubjectID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if preference.DefaultPool != "" {
		available, err := c.resolver.AvailablePools(ctx.Request().Context(), string(user.ID()), userTeamIDsForPools(user))
		if err != nil {
			return sessionRunnerStoreError(err)
		}
		found := false
		for _, pool := range available {
			if pool.Name == preference.DefaultPool {
				found = true
				break
			}
		}
		if !found {
			return echo.NewHTTPError(http.StatusForbidden, "pool is not available to this subject")
		}
	}
	if err := c.store.PutPreference(ctx.Request().Context(), &preference); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, &preference)
}

type runnerRegisterRequest struct {
	RunnerID  string `json:"runner_id"`
	Pool      string `json:"pool"`
	PodName   string `json:"pod_name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

func (c *SessionPoolController) RegisterRunner(ctx echo.Context) error {
	managerID := ctx.Request().Header.Get("X-Session-Manager-ID")
	manager, err := c.store.GetManager(ctx.Request().Context(), managerID)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid manager")
	}
	if !verifySessionRunnerToken(manager.ConnectionTokenHash, bearerToken(ctx.Request())) {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid manager token")
	}
	var input runnerRegisterRequest
	if err := ctx.Bind(&input); err != nil || input.Pool == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pool is required")
	}
	pool, err := c.store.GetPool(ctx.Request().Context(), manager.ID, input.Pool)
	if err != nil || !pool.Enabled || pool.Draining {
		return echo.NewHTTPError(http.StatusForbidden, "pool is unavailable")
	}
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create runner token")
	}
	runner := &core.Runner{ID: input.RunnerID, ManagerID: manager.ID, Pool: pool.Name, TokenHash: tokenHash, Status: core.RunnerIdle, PodName: input.PodName, Namespace: input.Namespace}
	if err := c.store.CreateRunner(ctx.Request().Context(), runner); err != nil {
		return sessionRunnerStoreError(err)
	}
	copy := *runner
	copy.TokenHash = ""
	return ctx.JSON(http.StatusCreated, map[string]any{"runner": &copy, "runner_token": token})
}

func (c *SessionPoolController) ClaimRunnerAllocation(ctx echo.Context) error {
	runner, err := c.authenticateRunner(ctx)
	if err != nil {
		return err
	}
	wait := parseRunnerWait(ctx.QueryParam("wait"))
	deadline := c.now().Add(wait)
	for {
		allocation, found, claimErr := c.store.ClaimNext(ctx.Request().Context(), runner.Pool, runner.ID, 45*time.Second)
		if claimErr != nil {
			return sessionRunnerStoreError(claimErr)
		}
		if found {
			runner.Status, runner.LastSeen = core.RunnerClaiming, c.now()
			_ = c.store.UpdateRunner(ctx.Request().Context(), runner)
			if err := c.prepareClaimRoute(ctx.Request().Context(), allocation, runner); err != nil {
				return sessionRunnerStoreError(err)
			}
			return ctx.JSON(http.StatusOK, runnerClaimResponse(allocation, runner))
		}
		if wait <= 0 || c.now().After(deadline) {
			return ctx.NoContent(http.StatusNoContent)
		}
		select {
		case <-ctx.Request().Context().Done():
			return ctx.Request().Context().Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (c *SessionPoolController) AckRunnerAllocation(ctx echo.Context) error {
	runner, err := c.authenticateRunner(ctx)
	if err != nil {
		return err
	}
	var input struct {
		LeaseID string `json:"lease_id"`
	}
	if err := ctx.Bind(&input); err != nil || input.LeaseID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "lease_id is required")
	}
	allocation, err := c.store.Acknowledge(ctx.Request().Context(), ctx.Param("sessionId"), runner.ID, input.LeaseID)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	runner.Status, runner.LastSeen = core.RunnerRunning, c.now()
	_ = c.store.UpdateRunner(ctx.Request().Context(), runner)
	return ctx.JSON(http.StatusOK, allocation)
}

func (c *SessionPoolController) FailRunnerAllocation(ctx echo.Context) error {
	runner, err := c.authenticateRunner(ctx)
	if err != nil {
		return err
	}
	var input struct {
		LeaseID string `json:"lease_id"`
	}
	if err := ctx.Bind(&input); err != nil || input.LeaseID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "lease_id is required")
	}
	allocation, err := c.store.Fail(ctx.Request().Context(), ctx.Param("sessionId"), runner.ID, input.LeaseID)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	runner.Status, runner.LastSeen = core.RunnerIdle, c.now()
	_ = c.store.UpdateRunner(ctx.Request().Context(), runner)
	return ctx.JSON(http.StatusOK, allocation)
}

func (c *SessionPoolController) HeartbeatManager(ctx echo.Context) error {
	manager, err := c.authenticateManager(ctx)
	if err != nil {
		return err
	}
	manager.LastHeartbeatAt = c.now()
	if err := c.store.UpdateManager(ctx.Request().Context(), manager); err != nil {
		return sessionRunnerStoreError(err)
	}
	pools, err := c.store.ListPools(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	owned := make([]*core.Pool, 0)
	runners, err := c.store.ListRunners(ctx.Request().Context(), "")
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, pool := range pools {
		if pool.ManagerID == manager.ID {
			copy := *pool
			for _, runner := range runners {
				if runner.ManagerID != manager.ID || runner.Pool != pool.Name {
					continue
				}
				copy.TotalRunners++
				if runner.Status == core.RunnerIdle {
					copy.IdleRunners++
				}
			}
			owned = append(owned, &copy)
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"ok": true, "at": manager.LastHeartbeatAt, "pools": owned})
}

func (c *SessionPoolController) authenticateManager(ctx echo.Context) (*core.Manager, error) {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil || !verifySessionRunnerToken(manager.ConnectionTokenHash, bearerToken(ctx.Request())) {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "invalid manager token")
	}
	return manager, nil
}

func (c *SessionPoolController) authenticateRunner(ctx echo.Context) (*core.Runner, error) {
	runnerID := ctx.Request().Header.Get("X-Session-Runner-ID")
	runner, err := c.store.GetRunner(ctx.Request().Context(), runnerID)
	if err != nil || !verifySessionRunnerToken(runner.TokenHash, bearerToken(ctx.Request())) {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "invalid runner token")
	}
	runner.LastSeen = c.now()
	return runner, nil
}

func (c *SessionPoolController) ensureLogicalPoolExists(ctx context.Context, name string) error {
	pools, err := c.store.ListPools(ctx)
	if err != nil {
		return err
	}
	for _, pool := range pools {
		if pool.Name == name {
			return nil
		}
	}
	return core.ErrNotFound
}

func (c *SessionPoolController) prepareClaimRoute(ctx context.Context, allocation *core.Allocation, runner *core.Runner) error {
	if c.routes == nil {
		return nil
	}
	route, err := c.routes.Get(ctx, allocation.SessionID)
	if err != nil || route == nil {
		return err
	}
	route.ManagerID = runner.ManagerID
	route.Generation = allocation.Generation
	return c.routes.Save(ctx, route)
}

func runnerClaimResponse(allocation *core.Allocation, runner *core.Runner) map[string]any {
	var settings *sessionsettings.SessionSettings
	if len(allocation.ProvisionSettings) > 0 {
		_ = json.Unmarshal(allocation.ProvisionSettings, &settings)
	}
	if settings != nil {
		settings.ParentRuntime = &sessionsettings.ParentRuntimeConfig{Enabled: true, SessionID: allocation.SessionID, ManagerID: runner.ManagerID, Token: allocation.RuntimeToken, Generation: allocation.Generation}
	}
	copy := *allocation
	copy.RuntimeToken, copy.RuntimeTokenHash, copy.ProvisionSettings = "", "", nil
	return map[string]any{"allocation": &copy, "lease_id": allocation.LeaseID, "runtime_token": allocation.RuntimeToken, "settings": settings}
}

func redactManager(manager *core.Manager) *core.Manager {
	copy := *manager
	copy.ConnectionTokenHash = ""
	return &copy
}

func newSessionRunnerToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(hash[:]), nil
}

func verifySessionRunnerToken(expectedHash, token string) bool {
	decoded, err := hex.DecodeString(expectedHash)
	if err != nil || len(decoded) != sha256.Size || token == "" {
		return false
	}
	candidate := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(decoded, candidate[:]) == 1
}

func bearerToken(req *http.Request) string {
	const prefix = "Bearer "
	header := req.Header.Get(echo.HeaderAuthorization)
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimPrefix(header, prefix)
}

func validatePoolSubject(kind core.SubjectType, id string) error {
	if (kind != core.SubjectUser && kind != core.SubjectTeam) || strings.TrimSpace(id) == "" {
		return errors.New("subject_type must be user or team and subject_id is required")
	}
	return nil
}

func userTeamIDsForPools(user *entities.User) []string {
	if user == nil {
		return nil
	}
	if user.UserType() == entities.UserTypeServiceAccount && user.TeamID() != "" {
		return []string{user.TeamID()}
	}
	if user.GitHubInfo() == nil {
		return nil
	}
	teams := user.GitHubInfo().Teams()
	result := make([]string, 0, len(teams))
	for _, team := range teams {
		result = append(result, team.Organization+"/"+team.TeamSlug)
	}
	return result
}

func parseRunnerWait(raw string) time.Duration {
	if raw == "" {
		return 30 * time.Second
	}
	duration, err := time.ParseDuration(raw)
	if err != nil {
		if seconds, parseErr := strconv.Atoi(raw); parseErr == nil {
			duration = time.Duration(seconds) * time.Second
		}
	}
	if duration < 0 {
		return 0
	}
	if duration > 30*time.Second {
		return 30 * time.Second
	}
	return duration
}

func sessionRunnerStoreError(err error) error {
	switch {
	case errors.Is(err, core.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	case errors.Is(err, core.ErrConflict):
		return echo.NewHTTPError(http.StatusConflict, err.Error())
	case errors.Is(err, core.ErrUnauthorized):
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
}
