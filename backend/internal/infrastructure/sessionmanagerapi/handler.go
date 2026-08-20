package sessionmanagerapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const maxRequestBodyBytes = 16 << 20

// SessionAnnotationUpdater is an optional session-manager capability.
type SessionAnnotationUpdater interface {
	UpdateSessionAnnotations(context.Context, string, entities.UpdateSessionAnnotationsRequest) (entities.SessionAnnotations, error)
}

// StockManager is the execution-plane capability used by the inventory worker.
type StockManager interface {
	CreateStockSession(context.Context, bool) error
	CountStockSessions(context.Context, bool) (int, error)
	PurgeStaleStockSessions(context.Context) error
}

// PoolStockManager is implemented by managers that maintain independent idle
// runner inventories for named logical pools.
type PoolStockManager interface {
	CreateStockSessionForPool(context.Context, string, bool) error
	CountStockSessionsForPool(context.Context, string, bool) (int, error)
}

// PendingAllocationDeleter removes a local allocation that has not been claimed.
type PendingAllocationDeleter interface {
	DeletePendingSessionAllocation(context.Context, string) (bool, error)
}

// ProvisionRequestDeleter removes a session provision request.
type ProvisionRequestDeleter interface {
	DeleteProvisionRequest(context.Context, string) error
}

// Handler exposes the execution-plane session manager through a small private
// HTTP surface. Every route, including health, requires the process-specific
// bearer token.
type Handler struct {
	manager   portrepos.SessionManager
	tokenHash [sha256.Size]byte
}

func NewHandler(manager portrepos.SessionManager, bearerToken string) (*Handler, error) {
	if manager == nil {
		return nil, errors.New("session manager is required")
	}
	if bearerToken == "" {
		return nil, errors.New("session-manager API bearer token is required")
	}
	return &Handler{manager: manager, tokenHash: sha256.Sum256([]byte(bearerToken))}, nil
}

// RegisterRoutes registers only the private session-manager API. The caller is
// responsible for supplying an Echo instance that is not the public API router.
func (h *Handler) RegisterRoutes(e *echo.Echo) {
	g := e.Group(RoutePrefix)
	g.Use(h.authenticate)

	g.GET("/health", h.health)
	g.GET("/runtime-profile", h.runtimeProfile)

	g.POST("/sessions/:sessionId", h.createSession)
	g.GET("/sessions", h.listSessions)
	g.POST("/sessions/:sessionId/messages", h.sendMessage)
	g.GET("/sessions/:sessionId/messages", h.getMessages)
	g.POST("/sessions/:sessionId/stop", h.stopAgent)
	g.POST("/sessions/:sessionId/ensure", h.ensureWorkload)
	g.POST("/sessions/:sessionId/provision-settings", h.provisionSettings)
	g.POST("/sessions/:sessionId/touch", h.touchSession)
	g.GET("/sessions/:sessionId/sandbox-domains", h.sandboxDomains)
	g.PATCH("/sessions/:sessionId/annotations", h.updateAnnotations)
	g.DELETE("/sessions/:sessionId/pending-allocation", h.deletePendingAllocation)
	g.DELETE("/sessions/:sessionId/provision-request", h.deleteProvisionRequest)
	g.GET("/sessions/:sessionId", h.getSession)
	g.DELETE("/sessions/:sessionId", h.deleteSession)

	g.GET("/stock", h.stock)
	g.POST("/stock", h.stock)
	g.DELETE("/stock", h.stock)

	g.GET("/allocations/next", h.nextAllocation)
	g.POST("/allocations/:sessionId/result", h.completeAllocation)
	g.GET("/allocations/external/next", h.nextExternalAllocation)
	g.POST("/allocations/external/:sessionId/result", h.completeExternalAllocation)
	g.POST("/allocations/external/:sessionId", h.submitExternalAllocation)
}

func (h *Handler) authenticate(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		token, formatOK := parseBearer(c.Request().Header.Get(echo.HeaderAuthorization))
		candidate := sha256.Sum256([]byte(token))
		matches := subtle.ConstantTimeCompare(candidate[:], h.tokenHash[:]) == 1
		if !formatOK || !matches {
			return c.JSON(http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
		}
		return next(c)
	}
}

func parseBearer(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(header, prefix)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", false
	}
	return token, true
}

func (h *Handler) health(c echo.Context) error {
	return c.JSON(http.StatusOK, healthResponse{Status: "ok"})
}

func (h *Handler) runtimeProfile(c echo.Context) error {
	provider, ok := h.manager.(interface {
		ExternalRuntimeProfile() *sessionsettings.RuntimeProfile
	})
	if !ok {
		return unsupported(c, "runtime profile is not supported")
	}
	return c.JSON(http.StatusOK, provider.ExternalRuntimeProfile())
}

func (h *Handler) createSession(c echo.Context) error {
	var input createSessionRequest
	if err := decodeJSON(c, &input); err != nil {
		return badRequest(c, err)
	}
	if input.Request == nil {
		return badRequest(c, errors.New("request is required"))
	}
	session, err := h.manager.CreateSession(c.Request().Context(), c.Param("sessionId"), input.Request, input.WebhookPayload)
	if err != nil {
		return internalError(c, err)
	}
	if session == nil {
		return internalError(c, errors.New("session manager returned a nil session"))
	}
	return c.JSON(http.StatusCreated, newSessionDTO(session))
}

func (h *Handler) getSession(c echo.Context) error {
	session := h.manager.GetSession(c.Param("sessionId"))
	if session == nil {
		return c.JSON(http.StatusNotFound, errorResponse{Error: "session not found"})
	}
	return c.JSON(http.StatusOK, newSessionDTO(session))
}

func (h *Handler) listSessions(c echo.Context) error {
	filter := entities.SessionFilter{
		UserID:  c.QueryParam("user_id"),
		Status:  c.QueryParam("status"),
		Scope:   entities.ResourceScope(c.QueryParam("scope")),
		TeamID:  c.QueryParam("team_id"),
		Tags:    make(map[string]string),
		TeamIDs: splitNonEmpty(c.QueryParam("team_ids")),
	}
	for name, values := range c.QueryParams() {
		if !strings.HasPrefix(name, "tag.") || len(values) == 0 {
			continue
		}
		filter.Tags[strings.TrimPrefix(name, "tag.")] = values[0]
	}
	sessions := h.manager.ListSessions(filter)
	result := make([]SessionDTO, 0, len(sessions))
	for _, session := range sessions {
		if session != nil {
			result = append(result, newSessionDTO(session))
		}
	}
	return c.JSON(http.StatusOK, sessionsResponse{Sessions: result})
}

func (h *Handler) deleteSession(c echo.Context) error {
	if err := h.manager.DeleteSession(c.Param("sessionId")); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) sendMessage(c echo.Context) error {
	var input messageRequest
	if err := decodeJSON(c, &input); err != nil {
		return badRequest(c, err)
	}
	if input.Message == "" {
		return badRequest(c, errors.New("message is required"))
	}
	if err := h.manager.SendMessage(c.Request().Context(), c.Param("sessionId"), input.Message); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) stopAgent(c echo.Context) error {
	if err := h.manager.StopAgent(c.Request().Context(), c.Param("sessionId")); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) getMessages(c echo.Context) error {
	messages, err := h.manager.GetMessages(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return internalError(c, err)
	}
	if messages == nil {
		messages = []portrepos.Message{}
	}
	return c.JSON(http.StatusOK, messagesResponse{Messages: messages})
}

func (h *Handler) ensureWorkload(c echo.Context) error {
	ensurer, ok := h.manager.(portrepos.SessionWorkloadEnsurer)
	if !ok {
		return unsupported(c, "session workload ensure is not supported")
	}
	session, restoring, err := ensurer.EnsureSessionWorkload(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return internalError(c, err)
	}
	response := ensureWorkloadResponse{Restoring: restoring}
	if session != nil {
		dto := newSessionDTO(session)
		response.Session = &dto
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handler) provisionSettings(c echo.Context) error {
	builder, ok := h.manager.(portrepos.RemoteProvisionSettingsBuilder)
	if !ok {
		return unsupported(c, "provision settings are not supported")
	}
	var input provisionSettingsRequest
	if err := decodeJSON(c, &input); err != nil {
		return badRequest(c, err)
	}
	if input.Request == nil {
		return badRequest(c, errors.New("request is required"))
	}
	settings, err := builder.BuildRemoteProvisionSettings(c.Request().Context(), c.Param("sessionId"), input.Request)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, settings)
}

func (h *Handler) touchSession(c echo.Context) error {
	toucher, ok := h.manager.(portrepos.SessionToucher)
	if !ok {
		return unsupported(c, "session touch is not supported")
	}
	var input touchRequest
	if err := decodeJSON(c, &input); err != nil {
		return badRequest(c, err)
	}
	if input.At.IsZero() {
		input.At = time.Now()
	}
	if err := toucher.TouchSession(c.Request().Context(), c.Param("sessionId"), input.At); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) sandboxDomains(c echo.Context) error {
	reader, ok := h.manager.(portrepos.SessionSandboxDomainReader)
	if !ok {
		return unsupported(c, "sandbox domains are not supported")
	}
	domains, err := reader.GetSessionSandboxDomains(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, domains)
}

func (h *Handler) updateAnnotations(c echo.Context) error {
	updater, ok := h.manager.(SessionAnnotationUpdater)
	if !ok {
		return unsupported(c, "session annotations are not supported")
	}
	var patch entities.UpdateSessionAnnotationsRequest
	if err := decodeJSON(c, &patch); err != nil {
		return badRequest(c, err)
	}
	annotations, err := updater.UpdateSessionAnnotations(c.Request().Context(), c.Param("sessionId"), patch)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, annotationsResponse{Annotations: annotations})
}

func (h *Handler) stock(c echo.Context) error {
	manager, ok := h.manager.(StockManager)
	if !ok {
		return unsupported(c, "stock sessions are not supported")
	}
	dind, _ := strconv.ParseBool(c.QueryParam("dind"))
	pool := strings.TrimSpace(c.QueryParam("pool"))
	switch c.Request().Method {
	case http.MethodGet:
		var count int
		var err error
		if pool != "" {
			named, supported := h.manager.(PoolStockManager)
			if !supported {
				return unsupported(c, "named stock pools are not supported")
			}
			count, err = named.CountStockSessionsForPool(c.Request().Context(), pool, dind)
		} else {
			count, err = manager.CountStockSessions(c.Request().Context(), dind)
		}
		if err != nil {
			return internalError(c, err)
		}
		return c.JSON(http.StatusOK, stockCountResponse{Count: count})
	case http.MethodPost:
		var err error
		if pool != "" {
			named, supported := h.manager.(PoolStockManager)
			if !supported {
				return unsupported(c, "named stock pools are not supported")
			}
			err = named.CreateStockSessionForPool(c.Request().Context(), pool, dind)
		} else {
			err = manager.CreateStockSession(c.Request().Context(), dind)
		}
		if err != nil {
			return internalError(c, err)
		}
		return c.NoContent(http.StatusCreated)
	case http.MethodDelete:
		if err := manager.PurgeStaleStockSessions(c.Request().Context()); err != nil {
			return internalError(c, err)
		}
		return c.NoContent(http.StatusNoContent)
	default:
		return c.NoContent(http.StatusMethodNotAllowed)
	}
}

func (h *Handler) deletePendingAllocation(c echo.Context) error {
	deleter, ok := h.manager.(PendingAllocationDeleter)
	if !ok {
		return unsupported(c, "pending allocation deletion is not supported")
	}
	deleted, err := deleter.DeletePendingSessionAllocation(c.Request().Context(), c.Param("sessionId"))
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, pendingAllocationDeleteResponse{Deleted: deleted})
}

func (h *Handler) deleteProvisionRequest(c echo.Context) error {
	deleter, ok := h.manager.(ProvisionRequestDeleter)
	if !ok {
		return unsupported(c, "provision request deletion is not supported")
	}
	if err := deleter.DeleteProvisionRequest(c.Request().Context(), c.Param("sessionId")); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) submitExternalAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	var input submitExternalAllocationRequest
	if err := decodeJSON(c, &input); err != nil {
		return badRequest(c, err)
	}
	if input.ManagerID == "" || input.Request == nil {
		return badRequest(c, errors.New("manager_id and request are required"))
	}
	if err := queue.SubmitExternalSessionAllocation(c.Request().Context(), input.ManagerID, c.Param("sessionId"), input.ProvisionSettings, input.Request, input.Runtime); err != nil {
		return internalError(c, err)
	}
	return c.NoContent(http.StatusAccepted)
}

func (h *Handler) nextAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	allocation, found, err := queue.NextSessionAllocation(c.Request().Context(), parseWait(c.QueryParam("wait")))
	if err != nil {
		return internalError(c, err)
	}
	if !found {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, allocation)
}

func (h *Handler) completeAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	var result coreallocation.AllocationResult
	if err := decodeJSON(c, &result); err != nil {
		return badRequest(c, err)
	}
	allocation, err := queue.CompleteSessionAllocation(c.Request().Context(), c.Param("sessionId"), result)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, allocation)
}

func (h *Handler) nextExternalAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	managerID := c.QueryParam("manager_id")
	if managerID == "" {
		return badRequest(c, errors.New("manager_id is required"))
	}
	allocation, found, err := queue.NextExternalSessionAllocation(c.Request().Context(), managerID, parseWait(c.QueryParam("wait")))
	if err != nil {
		return internalError(c, err)
	}
	if !found {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, allocation)
}

func (h *Handler) completeExternalAllocation(c echo.Context) error {
	queue, ok := h.manager.(coreallocation.Queue)
	if !ok {
		return unsupported(c, "session allocation queue is not supported")
	}
	var result coreallocation.AllocationResult
	if err := decodeJSON(c, &result); err != nil {
		return badRequest(c, err)
	}
	allocation, err := queue.CompleteExternalSessionAllocation(c.Request().Context(), c.Param("sessionId"), result)
	if err != nil {
		return internalError(c, err)
	}
	return c.JSON(http.StatusOK, allocation)
}

func decodeJSON(c echo.Context, target any) error {
	body := http.MaxBytesReader(c.Response(), c.Request().Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

func parseWait(value string) time.Duration {
	if value == "" {
		return 0
	}
	wait, err := time.ParseDuration(value)
	if err != nil {
		seconds, intErr := strconv.Atoi(value)
		if intErr != nil || seconds <= 0 {
			return 0
		}
		wait = time.Duration(seconds) * time.Second
	}
	if wait < 0 {
		return 0
	}
	if wait > 30*time.Second {
		return 30 * time.Second
	}
	return wait
}

func splitNonEmpty(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func badRequest(c echo.Context, err error) error {
	return c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
}

func internalError(c echo.Context, err error) error {
	return c.JSON(http.StatusInternalServerError, errorResponse{Error: err.Error()})
}

func unsupported(c echo.Context, message string) error {
	return c.JSON(http.StatusNotImplemented, errorResponse{Error: message})
}
