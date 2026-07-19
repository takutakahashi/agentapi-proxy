package controllers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
)

const (
	acpProtocolVersion  = "2025-01-15"
	acpSessionListLimit = 20
)

// ACPController handles ACP (Agent Client Protocol) JSON-RPC 2.0 endpoints.
// POST /acp  – JSON-RPC request/response
// GET  /acp/sse – SSE stream of session/update notifications
type ACPController struct {
	sessionManagerProvider SessionManagerProvider
	sessionCreator         SessionCreator
	sessionRouteRepo       portrepos.SessionRouteRepository
	// bridgeSessionCache maps proxy session ID → bridge-internal session ID.
	// Populated on first use to avoid fetching GET /session on every RPC call.
	bridgeSessionCache sync.Map
}

// NewACPController creates a new ACPController.
func NewACPController(
	sessionManagerProvider SessionManagerProvider,
	sessionCreator SessionCreator,
	sessionRouteRepos ...portrepos.SessionRouteRepository,
) *ACPController {
	var sessionRouteRepo portrepos.SessionRouteRepository
	if len(sessionRouteRepos) > 0 {
		sessionRouteRepo = sessionRouteRepos[0]
	}
	return &ACPController{
		sessionManagerProvider: sessionManagerProvider,
		sessionCreator:         sessionCreator,
		sessionRouteRepo:       sessionRouteRepo,
	}
}

// ----------------------------------------------------------------------------
// Wire types
// ----------------------------------------------------------------------------

type acpRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type acpResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  interface{}      `json:"result,omitempty"`
	Error   *acpRPCError     `json:"error,omitempty"`
}

type acpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func acpSuccessResp(id *json.RawMessage, result interface{}) acpResponse {
	return acpResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func acpErrResp(id *json.RawMessage, code int, message string) acpResponse {
	return acpResponse{JSONRPC: "2.0", ID: id, Error: &acpRPCError{Code: code, Message: message}}
}

// ----------------------------------------------------------------------------
// HandleRPC – POST /acp
// ----------------------------------------------------------------------------

// HandleRPC handles POST /acp (JSON-RPC 2.0).
func (c *ACPController) HandleRPC(ctx echo.Context) error {
	var req acpRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(nil, -32700, "Parse error"))
	}
	if req.JSONRPC != "2.0" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32600, "Invalid Request: jsonrpc must be \"2.0\""))
	}

	// JSON-RPC result (no method field) — route to session bridge via Acp-Session-Id header.
	if req.Method == "" {
		return c.proxyResultToBridge(ctx, req)
	}

	switch req.Method {
	case "initialize":
		return c.handleInitialize(ctx, req)
	case "session/new":
		return c.handleSessionNew(ctx, req)
	case "session/list":
		return c.handleSessionList(ctx, req)
	case "session/close":
		return c.handleSessionClose(ctx, req)
	case "session/resume":
		return c.handleSessionResume(ctx, req)
	case "session/load":
		return c.handleSessionLoad(ctx, req)
	case "session/prompt", "session/cancel", "session/set_config_option":
		return c.proxyToBridge(ctx, req)
	default:
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32601, "Method not found: "+req.Method))
	}
}

// ----------------------------------------------------------------------------
// initialize
// ----------------------------------------------------------------------------

func (c *ACPController) handleInitialize(ctx echo.Context, req acpRequest) error {
	result := map[string]interface{}{
		"protocolVersion": acpProtocolVersion,
		"capabilities": map[string]interface{}{
			"sessionCapabilities": map[string]bool{
				"list":        true,
				"close":       true,
				"resume":      true,
				"loadSession": true,
			},
		},
		"serverInfo": map[string]string{
			"name": "agentapi-proxy",
		},
	}
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, result))
}

// ----------------------------------------------------------------------------
// session/new
// ----------------------------------------------------------------------------

type sessionNewParams struct {
	Cwd        string      `json:"cwd"`
	McpServers interface{} `json:"mcpServers"`
	Meta       struct {
		Tags   map[string]string `json:"tags"`
		Params struct {
			Message   string `json:"message"`
			AgentType string `json:"agentType"`
		} `json:"params"`
	} `json:"_meta"`
}

func (c *ACPController) handleSessionNew(ctx echo.Context, req acpRequest) error {
	var params sessionNewParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	if params.Cwd == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "cwd is required"))
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	userID := authzCtx.PersonalScope.UserID
	userRole := "user"
	if authzCtx.User != nil && len(authzCtx.User.Roles()) > 0 {
		userRole = string(authzCtx.User.Roles()[0])
	}
	teams := authzCtx.TeamScope.Teams

	tags := make(map[string]string)
	for k, v := range params.Meta.Tags {
		tags[k] = v
	}
	tags["cwd"] = params.Cwd

	startReq := entities.StartRequest{
		Scope: entities.ScopeUser,
		Tags:  tags,
		Params: &entities.SessionParams{
			Message:   params.Meta.Params.Message,
			AgentType: params.Meta.Params.AgentType,
		},
	}

	resolvedScope, resolvedTeamID := authzCtx.ResolveScope(string(startReq.Scope), startReq.TeamID)
	startReq.Scope = entities.ResourceScope(resolvedScope)
	startReq.TeamID = resolvedTeamID

	sessionID := uuid.New().String()
	session, err := c.sessionCreator.CreateSession(sessionID, startReq, userID, userRole, teams)
	if err != nil {
		log.Printf("[ACP] session/new failed: %v", err)
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to create session: "+err.Error()))
	}

	// Use the actual session ID from the returned entity: a stock K8s session may have been
	// adopted with a different ID than the one we generated.
	actualID := session.ID()
	log.Printf("[ACP] session/new created sessionId=%s for user=%s", actualID, userID)
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, map[string]string{
		"sessionId": actualID,
	}))
}

// ----------------------------------------------------------------------------
// session/list
// ----------------------------------------------------------------------------

type sessionListParams struct {
	Cwd    string `json:"cwd"`
	Cursor string `json:"cursor"`
}

func (c *ACPController) handleSessionList(ctx echo.Context, req acpRequest) error {
	var params sessionListParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &params)
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	userID := authzCtx.PersonalScope.UserID

	filter := entities.SessionFilter{
		UserID: userID,
		Scope:  entities.ScopeUser,
	}
	if params.Cwd != "" {
		filter.Tags = map[string]string{"cwd": params.Cwd}
	}

	sessions := c.sessionManagerProvider.GetSessionManager().ListSessions(filter)

	// Decode cursor to determine start index.
	startIdx := 0
	if params.Cursor != "" {
		if decoded, err := base64.StdEncoding.DecodeString(params.Cursor); err == nil {
			cursorID := string(decoded)
			for i, s := range sessions {
				if s.ID() == cursorID {
					startIdx = i + 1
					break
				}
			}
		}
	}

	endIdx := startIdx + acpSessionListLimit
	var nextCursor string
	if endIdx < len(sessions) {
		nextCursor = base64.StdEncoding.EncodeToString([]byte(sessions[endIdx-1].ID()))
	} else {
		endIdx = len(sessions)
	}

	result := make([]map[string]interface{}, 0, endIdx-startIdx)
	for _, s := range sessions[startIdx:endIdx] {
		result = append(result, map[string]interface{}{
			"sessionId": s.ID(),
			"cwd":       s.Tags()["cwd"],
			"title":     s.Description(),
			"updatedAt": s.UpdatedAt().Format(time.RFC3339),
			"_meta": map[string]interface{}{
				"status": s.Status(),
				"scope":  string(s.Scope()),
				"teamId": s.TeamID(),
				"tags":   s.Tags(),
			},
		})
	}

	resp := map[string]interface{}{
		"sessions": result,
	}
	if nextCursor != "" {
		resp["nextCursor"] = nextCursor
	}
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, resp))
}

// ----------------------------------------------------------------------------
// session/close
// ----------------------------------------------------------------------------

func (c *ACPController) handleSessionClose(ctx echo.Context, req acpRequest) error {
	var params struct {
		SessionId string `json:"sessionId"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	if params.SessionId == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "sessionId is required"))
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(params.SessionId)
	if session == nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "session not found"))
	}
	if !authzCtx.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "permission denied"))
	}

	if err := c.sessionCreator.DeleteSessionByID(params.SessionId); err != nil {
		log.Printf("[ACP] session/close failed: %v", err)
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to close session: "+err.Error()))
	}

	log.Printf("[ACP] session/close sessionId=%s", params.SessionId)
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
}

// ----------------------------------------------------------------------------
// session/resume
// ----------------------------------------------------------------------------

func (c *ACPController) handleSessionResume(ctx echo.Context, req acpRequest) error {
	var params struct {
		SessionId string `json:"sessionId"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	if params.SessionId == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "sessionId is required"))
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(params.SessionId)
	if session == nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "session not found"))
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "permission denied"))
	}

	// "active" is the proxy-level status for a running K8s session.
	// "running"/"stable" are agentapi-level statuses used in other contexts.
	status := session.Status()
	if status != "active" && status != "running" && status != "stable" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "session is not active (status: "+status+")"))
	}

	log.Printf("[ACP] session/resume sessionId=%s status=%s", params.SessionId, status)
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
}

// ----------------------------------------------------------------------------
// session/load
// ----------------------------------------------------------------------------

// handleSessionLoad confirms the session exists and returns {}.
// History replay is handled by the SSE endpoint (GET /acp/sse) which automatically
// replays stored bridge messages when a client connects.
func (c *ACPController) handleSessionLoad(ctx echo.Context, req acpRequest) error {
	var params struct {
		SessionId string `json:"sessionId"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	if params.SessionId == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "sessionId is required"))
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(params.SessionId)
	if session == nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "session not found"))
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "permission denied"))
	}

	log.Printf("[ACP] session/load sessionId=%s (history replay via GET /acp/sse)", params.SessionId)
	return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
}

// ----------------------------------------------------------------------------
// session/prompt and session/cancel → ACP bridge /rpc
// ----------------------------------------------------------------------------

// getBridgeSessionID returns the bridge-internal session ID for the given proxy session ID
// and bridge address. Results are cached so subsequent calls are free.
func (c *ACPController) getBridgeSessionID(addr, proxySessionID string) (string, error) {
	if cached, ok := c.bridgeSessionCache.Load(proxySessionID); ok {
		return cached.(string), nil
	}

	resp, err := http.Get("http://" + addr + "/session") //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("GET /session failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var bridgeSession struct {
		SessionId string `json:"sessionId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bridgeSession); err != nil {
		return "", fmt.Errorf("decode /session response: %w", err)
	}
	if bridgeSession.SessionId != "" {
		c.bridgeSessionCache.Store(proxySessionID, bridgeSession.SessionId)
	}
	return bridgeSession.SessionId, nil
}

// proxyToBridge forwards session/prompt, session/cancel, and session/set_config_option to the ACP bridge's
// /rpc endpoint. Requires the session to run with an ACP-native agent (e.g. claude-acp).
func (c *ACPController) proxyToBridge(ctx echo.Context, req acpRequest) error {
	var baseParams struct {
		SessionId string `json:"sessionId"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &baseParams); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	if baseParams.SessionId == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "sessionId is required"))
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(baseParams.SessionId)
	if session == nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "session not found"))
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "permission denied"))
	}

	addr := session.Addr()
	if addr == "" {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "session has no address"))
	}

	// The bridge uses its own internal session ID, not the proxy session ID.
	// Fetch the bridge's session ID (cached after the first call) and substitute
	// it into params.sessionId before forwarding.
	bridgeSessionID, err := c.getBridgeSessionID(addr, baseParams.SessionId)
	if err != nil || bridgeSessionID == "" {
		log.Printf("[ACP] proxyToBridge: failed to get bridge session ID for %s: %v", baseParams.SessionId, err)
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to resolve bridge session ID"))
	}

	var rawParams map[string]json.RawMessage
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &rawParams); err == nil {
			bridgeIDJSON, _ := json.Marshal(bridgeSessionID)
			rawParams["sessionId"] = bridgeIDJSON
			if newParams, err := json.Marshal(rawParams); err == nil {
				req.Params = newParams
			}
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to marshal request"))
	}

	rpcURL := "http://" + addr + "/rpc"
	log.Printf("[ACP] %s → POST %s (proxySessionId=%s bridgeSessionId=%s)", req.Method, rpcURL, baseParams.SessionId, bridgeSessionID)

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "bridge unreachable: "+err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if req.Method == "session/set_config_option" {
			var rpcResp acpResponse
			if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
				return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to decode bridge response: "+err.Error()))
			}
			return ctx.JSON(http.StatusOK, rpcResp)
		}
		return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
	}

	var rpcResp interface{}
	_ = json.NewDecoder(resp.Body).Decode(&rpcResp)
	return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, fmt.Sprintf("bridge error (HTTP %d)", resp.StatusCode)))
}

// proxyResultToBridge forwards a JSON-RPC result (no method field) to the per-session
// bridge /rpc endpoint. The target session is identified by the Acp-Session-Id header.
func (c *ACPController) proxyResultToBridge(ctx echo.Context, req acpRequest) error {
	sessionId := ctx.Request().Header.Get("Acp-Session-Id")
	if sessionId == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"message": "Acp-Session-Id header is required"})
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionId)
	if session == nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{"message": "session not found"})
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return ctx.JSON(http.StatusForbidden, map[string]string{"message": "permission denied"})
	}

	addr := session.Addr()
	if addr == "" {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"message": "session has no address"})
	}

	body, err := json.Marshal(req)
	if err != nil {
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"message": "failed to marshal request"})
	}

	rpcURL := "http://" + addr + "/rpc"
	log.Printf("[ACP] result → POST %s (session=%s)", rpcURL, sessionId)

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return ctx.JSON(http.StatusBadGateway, map[string]string{"message": "bridge unreachable: " + err.Error()})
	}
	defer func() { _ = resp.Body.Close() }()

	return ctx.JSON(http.StatusOK, map[string]interface{}{})
}

// ----------------------------------------------------------------------------
// HandleSessionSSE – GET /acp
// ----------------------------------------------------------------------------

// HandleSessionSSE handles GET /acp with Acp-Session-Id header.
// Streams live session/update notifications from the per-session bridge until
// the client disconnects. History is NOT replayed here; clients fetch it separately.
func (c *ACPController) HandleSessionSSE(ctx echo.Context) error {
	log.Printf("[ACP] HandleSessionSSE: called (method=%s, origin=%s, headers=%v)",
		ctx.Request().Method,
		ctx.Request().Header.Get("Origin"),
		ctx.Request().Header,
	)
	sessionId := ctx.Request().Header.Get("Acp-Session-Id")
	if sessionId == "" {
		log.Printf("[ACP] HandleSessionSSE: missing Acp-Session-Id header")
		return ctx.JSON(http.StatusBadRequest, map[string]string{"message": "Acp-Session-Id header is required"})
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionId)
	if session == nil {
		if c.sessionRouteRepo != nil {
			route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionId)
			if err != nil {
				log.Printf("[ACP] HandleSessionSSE: route lookup failed (sessionId=%s): %v", sessionId, err)
				return ctx.JSON(http.StatusInternalServerError, map[string]string{"message": "session route lookup failed"})
			}
			if route != nil && route.ProxyURL != "" && route.RemoteSessionID != "" {
				if !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
					return ctx.JSON(http.StatusForbidden, map[string]string{"message": "permission denied"})
				}
				return c.handleRemoteSessionSSE(ctx, route)
			}
		}
		log.Printf("[ACP] HandleSessionSSE: session not found (sessionId=%s)", sessionId)
		return ctx.JSON(http.StatusNotFound, map[string]string{"message": "session not found"})
	}
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		log.Printf("[ACP] HandleSessionSSE: permission denied (sessionId=%s)", sessionId)
		return ctx.JSON(http.StatusForbidden, map[string]string{"message": "permission denied"})
	}

	addr := session.Addr()
	if addr == "" {
		log.Printf("[ACP] HandleSessionSSE: session has no address (sessionId=%s)", sessionId)
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{"message": "session has no address"})
	}

	log.Printf("[ACP] HandleSessionSSE: connecting to bridge SSE (sessionId=%s, addr=%s)", sessionId, addr)

	// Forward Last-Event-ID so the bridge replays only missed messages.
	lastEventID := ctx.Request().Header.Get("Last-Event-ID")

	w := ctx.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.Writer.(http.Flusher)

	// Flush response headers immediately so the client receives the 200 OK
	// before we block waiting for the bridge's first SSE event.
	if hasFlusher {
		flusher.Flush()
	}

	// Stream events from the bridge's /sse endpoint, forwarding Last-Event-ID so
	// the bridge replays history from the correct position on reconnect.
	c.streamBridgeSSE(ctx.Request().Context(), addr, w, flusher, hasFlusher, lastEventID)
	return nil
}

func (c *ACPController) handleRemoteSessionSSE(ctx echo.Context, route *portrepos.SessionRoute) error {
	w := ctx.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.Writer.(http.Flusher)
	if hasFlusher {
		flusher.Flush()
	}
	c.streamRemoteBridgeSSE(ctx.Request().Context(), route, w, flusher, hasFlusher, ctx.Request().Header.Get("Last-Event-ID"))
	return nil
}

// sseEvent holds the fields of a parsed SSE event.
type sseEvent struct {
	id   string
	data string
	err  error
}

// streamBridgeSSE proxies the ACP bridge's /sse stream to the current response writer.
//
// When the bridge-side SSE connection drops (e.g. transient network hiccup or bridge
// restart), this function immediately reconnects to the bridge and resumes from the
// last forwarded event ID — keeping the client-side SSE stream alive the whole time.
// History replay is handled by the bridge's Last-Event-ID support, so no messages are
// lost during the reconnection window.
//
// A keepalive comment is sent every 15 seconds so that NGINX proxy_read_timeout does not
// drop the client connection while the agent is idle between messages.
func (c *ACPController) streamBridgeSSE(
	ctx context.Context,
	addr string,
	w io.Writer,
	flusher http.Flusher,
	hasFlusher bool,
	lastEventID string,
) {
	c.streamSSE(ctx, w, flusher, hasFlusher, lastEventID, func(ctx context.Context, lastEventID string) (<-chan sseEvent, func()) {
		return c.dialBridgeSSE(ctx, addr, lastEventID)
	})
}

func (c *ACPController) streamRemoteBridgeSSE(
	ctx context.Context,
	route *portrepos.SessionRoute,
	w io.Writer,
	flusher http.Flusher,
	hasFlusher bool,
	lastEventID string,
) {
	c.streamSSE(ctx, w, flusher, hasFlusher, lastEventID, func(ctx context.Context, lastEventID string) (<-chan sseEvent, func()) {
		return c.dialRemoteBridgeSSE(ctx, route, lastEventID)
	})
}

func (c *ACPController) streamSSE(
	ctx context.Context,
	w io.Writer,
	flusher http.Flusher,
	hasFlusher bool,
	lastEventID string,
	dial func(context.Context, string) (<-chan sseEvent, func()),
) {
	keepaliveTicker := time.NewTicker(15 * time.Second)
	defer keepaliveTicker.Stop()

	const (
		backoffMin = time.Second
		backoffMax = 30 * time.Second
	)
	backoff := backoffMin

	for {
		// Check client disconnection before each (re)connection attempt.
		select {
		case <-ctx.Done():
			return
		default:
		}

		connStart := time.Now()
		eventCh, cleanup := dial(ctx, lastEventID)

		gotEvent := false

		// Drain events from this bridge connection until it drops or the client leaves.
	drainLoop:
		for {
			select {
			case <-ctx.Done():
				cleanup()
				return
			case ev, ok := <-eventCh:
				if !ok {
					cleanup()
					log.Printf("[ACP] SSE: bridge SSE dropped (lastEventID=%s), reconnecting...", lastEventID)
					break drainLoop
				}
				if ev.err != nil {
					cleanup()
					log.Printf("[ACP] SSE: bridge read error (lastEventID=%s): %v", lastEventID, ev.err)
					break drainLoop
				}
				gotEvent = true
				if ev.id != "" {
					lastEventID = ev.id
					_, _ = fmt.Fprintf(w, "id: %s\nevent: message\ndata: %s\n\n", ev.id, ev.data)
				} else {
					_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", ev.data)
				}
				if hasFlusher {
					flusher.Flush()
				}
			case <-keepaliveTicker.C:
				_, _ = fmt.Fprintf(w, ": keepalive\n\n")
				if hasFlusher {
					flusher.Flush()
				}
			}
		}

		// Reset backoff when the connection was healthy (delivered at least one event
		// or stayed alive for over 5 seconds). Otherwise use exponential backoff so
		// permanently unreachable bridges (deleted sessions) don't spin-log.
		elapsed := time.Since(connStart)
		if gotEvent || elapsed >= 5*time.Second {
			backoff = backoffMin
		}

		wait := backoff - elapsed
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}

		// Double backoff for next attempt, capped at backoffMax.
		backoff *= 2
		if backoff > backoffMax {
			backoff = backoffMax
		}
	}
}

// dialBridgeSSE opens a single SSE connection to the bridge and returns a channel of
// parsed events plus a cleanup function. The goroutine exits when the response body
// closes (bridge dropped or ctx cancelled via cleanup).
func (c *ACPController) dialBridgeSSE(
	ctx context.Context,
	addr, lastEventID string,
) (<-chan sseEvent, func()) {
	eventCh := make(chan sseEvent, 8)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/sse", nil)
	if err != nil {
		log.Printf("[ACP] SSE: failed to create bridge request: %v", err)
		close(eventCh)
		return eventCh, func() {}
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[ACP] SSE: failed to connect to bridge: %v", err)
		close(eventCh)
		return eventCh, func() {}
	}

	// cleanup closes the response body, which unblocks the scanner goroutine.
	cleanup := func() { _ = resp.Body.Close() }

	go func() {
		defer close(eventCh)
		scanner := bufio.NewScanner(resp.Body)
		var cur sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			case line == "": // blank line = event boundary
				if cur.data != "" {
					select {
					case eventCh <- cur:
					case <-ctx.Done():
						return
					}
				}
				cur = sseEvent{}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			select {
			case eventCh <- sseEvent{err: scanErr}:
			case <-ctx.Done():
			}
		}
	}()

	return eventCh, cleanup
}

func (c *ACPController) dialRemoteBridgeSSE(
	ctx context.Context,
	route *portrepos.SessionRoute,
	lastEventID string,
) (<-chan sseEvent, func()) {
	eventCh := make(chan sseEvent, 8)
	targetURL := strings.TrimRight(route.ProxyURL, "/") + "/" + route.RemoteSessionID + "/sse"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		log.Printf("[ACP] SSE: failed to create remote bridge request: %v", err)
		close(eventCh)
		return eventCh, func() {}
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	ts := hmacutil.NowTimestamp()
	msg := hmacutil.BuildMessage(http.MethodGet, req.URL.RequestURI(), ts, nil)
	req.Header.Set("X-Hub-Signature-256", hmacutil.Sign([]byte(route.HMACSecret), msg))
	req.Header.Set(hmacutil.TimestampHeader, ts)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[ACP] SSE: failed to connect to remote bridge: %v", err)
		close(eventCh)
		return eventCh, func() {}
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		log.Printf("[ACP] SSE: remote bridge returned HTTP %d", resp.StatusCode)
		close(eventCh)
		return eventCh, func() {}
	}

	cleanup := func() { _ = resp.Body.Close() }
	go scanSSE(ctx, resp.Body, eventCh)
	return eventCh, cleanup
}

func scanSSE(ctx context.Context, body io.Reader, eventCh chan<- sseEvent) {
	defer close(eventCh)
	scanner := bufio.NewScanner(body)
	var cur sseEvent
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "id: "):
			cur.id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.data != "" {
				select {
				case eventCh <- cur:
				case <-ctx.Done():
					return
				}
			}
			cur = sseEvent{}
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case eventCh <- sseEvent{err: err}:
		case <-ctx.Done():
		}
	}
}
