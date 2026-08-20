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
	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
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
	profile  interface {
		ExternalRuntimeProfile() *sessionsettings.RuntimeProfile
	}
	now func() time.Time
}

func NewSessionPoolController(store core.Store, routes portrepos.SessionRouteRepository, providers ...interface {
	ExternalRuntimeProfile() *sessionsettings.RuntimeProfile
}) *SessionPoolController {
	c := &SessionPoolController{store: store, resolver: core.NewResolver(store, 90*time.Second), routes: routes, now: func() time.Time { return time.Now().UTC() }}
	if len(providers) > 0 {
		c.profile = providers[0]
	}
	return c
}

func (c *SessionPoolController) GetManagerRuntimeProfile(ctx echo.Context) error {
	if _, err := c.authenticateManager(ctx); err != nil {
		return err
	}
	if c.profile == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "runtime profile is unavailable")
	}
	profile := c.profile.ExternalRuntimeProfile()
	raw, err := json.Marshal(profile)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "marshal runtime profile")
	}
	digest := sha256.Sum256(raw)
	return ctx.JSON(http.StatusOK, &sessionallocation.RuntimeProfileSnapshot{Revision: hex.EncodeToString(digest[:]), Profile: profile})
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

func (c *SessionPoolController) DeleteManager(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	managerID := ctx.Param("id")
	if _, err := c.store.GetManager(requestCtx, managerID); err != nil {
		return sessionRunnerStoreError(err)
	}
	runners, err := c.store.ListRunners(requestCtx, "")
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	allocations, err := c.store.ListAllocations(requestCtx, "")
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, runner := range runners {
		if runner.ManagerID == managerID && runnerIsActive(runner) {
			return echo.NewHTTPError(http.StatusConflict, "manager has active runners")
		}
	}
	for _, allocation := range allocations {
		if allocation.ManagerID == managerID && allocationIsActive(allocation) {
			return echo.NewHTTPError(http.StatusConflict, "manager has active allocations")
		}
	}
	suppliers, err := c.store.ListPoolSuppliers(requestCtx)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, runner := range runners {
		if runner.ManagerID == managerID {
			if err := c.store.DeleteRunner(requestCtx, runner.ID); err != nil {
				return sessionRunnerStoreError(err)
			}
		}
	}
	for _, supplier := range suppliers {
		if supplier.ManagerID == managerID {
			if err := c.store.DeletePoolSupplier(requestCtx, managerID, supplier.Pool); err != nil {
				return sessionRunnerStoreError(err)
			}
		}
	}
	if err := c.store.DeleteManager(requestCtx, managerID); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) CreatePoolSupplier(ctx echo.Context) error {
	manager, err := c.store.GetManager(ctx.Request().Context(), ctx.Param("id"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	var supplier core.PoolSupplier
	supplier.Enabled = true
	if err := ctx.Bind(&supplier); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if supplier.Pool == "" {
		supplier.Pool = ctx.Param("pool")
	}
	if strings.TrimSpace(supplier.Pool) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name is required")
	}
	if problems := validation.IsValidLabelValue(supplier.Pool); len(problems) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name must be a valid Kubernetes label value")
	}
	if _, err := c.store.GetLogicalPool(ctx.Request().Context(), supplier.Pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, supplier.Pool); err != nil {
		return err
	}
	if supplier.MinIdle < 0 || supplier.MaxRunners < 0 || (supplier.MaxRunners > 0 && supplier.MinIdle > supplier.MaxRunners) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid runner limits")
	}
	supplier.ManagerID = manager.ID
	if err := c.store.CreatePoolSupplier(ctx.Request().Context(), &supplier); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, &supplier)
}

func (c *SessionPoolController) CreateLogicalPool(ctx echo.Context) error {
	var input struct {
		Name    string            `json:"name"`
		Labels  map[string]string `json:"labels,omitempty"`
		Enabled *bool             `json:"enabled,omitempty"`
		TeamID  string            `json:"team_id,omitempty"`
	}
	if err := ctx.Bind(&input); err != nil || strings.TrimSpace(input.Name) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name is required")
	}
	if problems := validation.IsValidLabelValue(input.Name); len(problems) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "pool name must be a valid Kubernetes label value")
	}
	user := auth.GetUserFromContext(ctx)
	if input.TeamID != "" && user != nil && !user.IsAdmin() && !user.IsMemberOfTeam(input.TeamID) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	pool := core.LogicalPool{Name: input.Name, Labels: input.Labels, Enabled: enabled}
	if err := c.store.CreateLogicalPool(ctx.Request().Context(), &pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	if user != nil {
		binding := &core.Binding{Pool: pool.Name, SubjectType: core.SubjectUser, SubjectID: string(user.ID()), Role: core.BindingRoleManage, Enabled: true}
		if input.TeamID != "" {
			binding.SubjectType, binding.SubjectID = core.SubjectTeam, input.TeamID
		}
		if err := c.store.CreateBinding(ctx.Request().Context(), binding); err != nil {
			_ = c.store.DeleteLogicalPool(ctx.Request().Context(), pool.Name)
			return sessionRunnerStoreError(err)
		}
	}
	return ctx.JSON(http.StatusCreated, &pool)
}

func (c *SessionPoolController) ListPoolSuppliers(ctx echo.Context) error {
	if poolName := ctx.Param("pool"); poolName != "" {
		if err := c.requirePoolManage(ctx, poolName); err != nil {
			return err
		}
	}
	suppliers, err := c.store.ListPoolSuppliers(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	managerID := ctx.Param("id")
	poolName := ctx.Param("pool")
	result := make([]*core.PoolSupplier, 0)
	for _, supplier := range suppliers {
		if (managerID == "" || supplier.ManagerID == managerID) && (poolName == "" || supplier.Pool == poolName) {
			result = append(result, supplier)
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"pool_suppliers": result})
}

func (c *SessionPoolController) ListPools(ctx echo.Context) error {
	pools, err := c.store.ListLogicalPools(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	user := auth.GetUserFromContext(ctx)
	if user == nil || user.IsAdmin() {
		return ctx.JSON(http.StatusOK, map[string]any{"session_pools": pools})
	}
	result := make([]*core.LogicalPool, 0, len(pools))
	for _, pool := range pools {
		if ok, checkErr := c.canManagePool(ctx.Request().Context(), user, pool.Name); checkErr != nil {
			return sessionRunnerStoreError(checkErr)
		} else if ok {
			result = append(result, pool)
		}
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_pools": result})
}

func (c *SessionPoolController) PatchLogicalPool(ctx echo.Context) error {
	pool, err := c.store.GetLogicalPool(ctx.Request().Context(), ctx.Param("pool"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, pool.Name); err != nil {
		return err
	}
	var patch struct {
		Labels  map[string]string `json:"labels,omitempty"`
		Enabled *bool             `json:"enabled,omitempty"`
	}
	if err := ctx.Bind(&patch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if patch.Labels != nil {
		pool.Labels = patch.Labels
	}
	if patch.Enabled != nil {
		pool.Enabled = *patch.Enabled
	}
	if err := c.store.UpdateLogicalPool(ctx.Request().Context(), pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, pool)
}

func (c *SessionPoolController) DeleteLogicalPool(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	poolName := ctx.Param("pool")
	if _, err := c.store.GetLogicalPool(requestCtx, poolName); err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, poolName); err != nil {
		return err
	}
	runners, err := c.store.ListRunners(requestCtx, poolName)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	allocations, err := c.store.ListAllocations(requestCtx, poolName)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, runner := range runners {
		if runnerIsActive(runner) {
			return echo.NewHTTPError(http.StatusConflict, "pool has active runners")
		}
	}
	for _, allocation := range allocations {
		if allocationIsActive(allocation) {
			return echo.NewHTTPError(http.StatusConflict, "pool has active allocations")
		}
	}
	suppliers, err := c.store.ListPoolSuppliers(requestCtx)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	bindings, err := c.store.ListBindings(requestCtx, poolName)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, allocation := range allocations {
		if err := c.store.DeleteAllocation(requestCtx, allocation.SessionID); err != nil {
			return sessionRunnerStoreError(err)
		}
	}
	for _, runner := range runners {
		if err := c.store.DeleteRunner(requestCtx, runner.ID); err != nil {
			return sessionRunnerStoreError(err)
		}
	}
	for _, supplier := range suppliers {
		if supplier.Pool == poolName {
			if err := c.store.DeletePoolSupplier(requestCtx, supplier.ManagerID, poolName); err != nil {
				return sessionRunnerStoreError(err)
			}
		}
	}
	for _, binding := range bindings {
		if err := c.store.DeleteBinding(requestCtx, binding.ID); err != nil {
			return sessionRunnerStoreError(err)
		}
	}
	if err := c.store.DeleteLogicalPool(requestCtx, poolName); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) PatchPoolSupplier(ctx echo.Context) error {
	pool, err := c.store.GetPoolSupplier(ctx.Request().Context(), ctx.Param("id"), ctx.Param("pool"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, pool.Pool); err != nil {
		return err
	}
	var patch struct {
		Labels     map[string]string `json:"labels,omitempty"`
		MinIdle    *int              `json:"min_idle,omitempty"`
		MaxRunners *int              `json:"max_runners,omitempty"`
		Enabled    *bool             `json:"enabled,omitempty"`
		Draining   *bool             `json:"draining,omitempty"`
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
	if pool.MinIdle < 0 || pool.MaxRunners < 0 || (pool.MaxRunners > 0 && pool.MinIdle > pool.MaxRunners) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid runner limits")
	}
	if err := c.store.UpdatePoolSupplier(ctx.Request().Context(), pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, pool)
}

func (c *SessionPoolController) DeletePoolSupplier(ctx echo.Context) error {
	requestCtx := ctx.Request().Context()
	managerID, poolName := ctx.Param("id"), ctx.Param("pool")
	if _, err := c.store.GetPoolSupplier(requestCtx, managerID, poolName); err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, poolName); err != nil {
		return err
	}
	runners, err := c.store.ListRunners(requestCtx, poolName)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, runner := range runners {
		if runner.ManagerID == managerID && runnerIsActive(runner) {
			return echo.NewHTTPError(http.StatusConflict, "pool supplier has active runners")
		}
	}
	for _, runner := range runners {
		if runner.ManagerID == managerID {
			if err := c.store.DeleteRunner(requestCtx, runner.ID); err != nil {
				return sessionRunnerStoreError(err)
			}
		}
	}
	if err := c.store.DeletePoolSupplier(requestCtx, managerID, poolName); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func runnerIsActive(runner *core.Runner) bool {
	return runner.Status == core.RunnerRunning || runner.Status == core.RunnerClaiming
}

func allocationIsActive(allocation *core.Allocation) bool {
	return allocation.Status == core.AllocationLeased ||
		allocation.Status == core.AllocationClaimed ||
		allocation.Status == core.AllocationRunning
}

func (c *SessionPoolController) CreateBinding(ctx echo.Context) error {
	var binding core.Binding
	binding.Enabled = true
	if err := ctx.Bind(&binding); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	binding.Pool = ctx.Param("pool")
	if binding.Role == "" {
		binding.Role = core.BindingRoleUse
	}
	if err := validatePoolBindingSubject(binding.SubjectType, binding.SubjectID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := validatePoolBindingRole(binding.SubjectType, binding.Role); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if binding.MaxConcurrent < 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "max_concurrent must be non-negative")
	}
	if err := c.ensureLogicalPoolExists(ctx.Request().Context(), binding.Pool); err != nil {
		return sessionRunnerStoreError(err)
	}
	if err := c.requirePoolManage(ctx, binding.Pool); err != nil {
		return err
	}
	if err := c.store.CreateBinding(ctx.Request().Context(), &binding); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusCreated, &binding)
}

func (c *SessionPoolController) DeleteBinding(ctx echo.Context) error {
	pool := ctx.Param("pool")
	if err := c.requirePoolManage(ctx, pool); err != nil {
		return err
	}
	bindings, err := c.store.ListBindings(ctx.Request().Context(), pool)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	found := false
	for _, binding := range bindings {
		found = found || binding.ID == ctx.Param("bindingId")
	}
	if !found {
		return echo.NewHTTPError(http.StatusNotFound, "binding not found")
	}
	if err := c.store.DeleteBinding(ctx.Request().Context(), ctx.Param("bindingId")); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (c *SessionPoolController) ListBindings(ctx echo.Context) error {
	if err := c.requirePoolManage(ctx, ctx.Param("pool")); err != nil {
		return err
	}
	bindings, err := c.store.ListBindings(ctx.Request().Context(), ctx.Param("pool"))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"pool_bindings": bindings})
}

func (c *SessionPoolController) PatchBinding(ctx echo.Context) error {
	pool := ctx.Param("pool")
	if err := c.requirePoolManage(ctx, pool); err != nil {
		return err
	}
	bindings, err := c.store.ListBindings(ctx.Request().Context(), pool)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	var binding *core.Binding
	for _, candidate := range bindings {
		if candidate.ID == ctx.Param("bindingId") {
			binding = candidate
			break
		}
	}
	if binding == nil {
		return echo.NewHTTPError(http.StatusNotFound, "binding not found")
	}
	var patch struct {
		Role          *core.BindingRole `json:"role,omitempty"`
		Enabled       *bool             `json:"enabled,omitempty"`
		Priority      *int              `json:"priority,omitempty"`
		MaxConcurrent *int              `json:"max_concurrent,omitempty"`
	}
	if err := ctx.Bind(&patch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if patch.Role != nil {
		binding.Role = *patch.Role
	}
	if patch.Enabled != nil {
		binding.Enabled = *patch.Enabled
	}
	if patch.Priority != nil {
		binding.Priority = *patch.Priority
	}
	if patch.MaxConcurrent != nil {
		if *patch.MaxConcurrent < 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "max_concurrent must be non-negative")
		}
		binding.MaxConcurrent = *patch.MaxConcurrent
	}
	if err := validatePoolBindingRole(binding.SubjectType, binding.Role); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := c.store.UpdateBinding(ctx.Request().Context(), binding); err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, binding)
}

func (c *SessionPoolController) ListAvailablePools(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	pools, err := c.resolver.AvailablePools(ctx.Request().Context(), poolSubjectForUser(user))
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"session_pools": pools})
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
	pool, err := c.store.GetPoolSupplier(ctx.Request().Context(), manager.ID, input.Pool)
	if err != nil || !pool.Enabled || pool.Draining {
		return echo.NewHTTPError(http.StatusForbidden, "pool is unavailable")
	}
	token, tokenHash, err := newSessionRunnerToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create runner token")
	}
	runner := &core.Runner{ID: input.RunnerID, ManagerID: manager.ID, Pool: pool.Pool, TokenHash: tokenHash, Status: core.RunnerIdle, PodName: input.PodName, Namespace: input.Namespace}
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
	pools, err := c.store.ListPoolSuppliers(ctx.Request().Context())
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	owned := make([]*core.PoolSupplier, 0)
	runners, err := c.store.ListRunners(ctx.Request().Context(), "")
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	for _, pool := range pools {
		if pool.ManagerID == manager.ID {
			copy := *pool
			for _, runner := range runners {
				if runner.ManagerID != manager.ID || runner.Pool != pool.Pool {
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
	_, err := c.store.GetLogicalPool(ctx, name)
	return err
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

func validatePoolBindingSubject(kind core.SubjectType, id string) error {
	if kind == core.SubjectAll {
		if strings.TrimSpace(id) != "" {
			return errors.New("subject_id must be empty when subject_type is all")
		}
		return nil
	}
	return validatePoolSubject(kind, id)
}

func validatePoolBindingRole(kind core.SubjectType, role core.BindingRole) error {
	if role != core.BindingRoleUse && role != core.BindingRoleManage {
		return errors.New("role must be use or manage")
	}
	if kind == core.SubjectAll && role == core.BindingRoleManage {
		return errors.New("all binding cannot have manage role")
	}
	return nil
}

func (c *SessionPoolController) requirePoolManage(ctx echo.Context, pool string) error {
	user := auth.GetUserFromContext(ctx)
	// Admin routes already enforce authentication in middleware. Keeping nil
	// permissive also supports internal controller calls without an HTTP identity.
	if user == nil || user.IsAdmin() {
		return nil
	}
	ok, err := c.canManagePool(ctx.Request().Context(), user, pool)
	if err != nil {
		return sessionRunnerStoreError(err)
	}
	if !ok {
		return echo.NewHTTPError(http.StatusForbidden, "pool manage binding required")
	}
	return nil
}

func (c *SessionPoolController) canManagePool(ctx context.Context, user *entities.User, pool string) (bool, error) {
	if user == nil || user.IsAdmin() {
		return true, nil
	}
	bindings, err := c.store.ListBindings(ctx, pool)
	if err != nil {
		return false, err
	}
	for _, binding := range bindings {
		if !binding.Enabled || binding.Role != core.BindingRoleManage {
			continue
		}
		if binding.SubjectType == core.SubjectUser && binding.SubjectID == string(user.ID()) {
			return true, nil
		}
		if binding.SubjectType == core.SubjectTeam && user.IsMemberOfTeam(binding.SubjectID) {
			return true, nil
		}
	}
	return false, nil
}

func poolSubjectForUser(user *entities.User) core.Subject {
	if user.UserType() == entities.UserTypeServiceAccount && user.TeamID() != "" {
		return core.Subject{Type: core.SubjectTeam, ID: user.TeamID()}
	}
	return core.Subject{Type: core.SubjectUser, ID: string(user.ID())}
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
