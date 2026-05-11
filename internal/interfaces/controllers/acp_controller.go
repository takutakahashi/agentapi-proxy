package controllers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
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
}

// NewACPController creates a new ACPController.
func NewACPController(
	sessionManagerProvider SessionManagerProvider,
	sessionCreator SessionCreator,
) *ACPController {
	return &ACPController{
		sessionManagerProvider: sessionManagerProvider,
		sessionCreator:         sessionCreator,
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
	case "session/prompt", "session/cancel":
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

// proxyToBridge forwards session/prompt and session/cancel to the ACP bridge's
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

	body, err := json.Marshal(req)
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to marshal request"))
	}

	rpcURL := "http://" + addr + "/rpc"
	log.Printf("[ACP] %s → POST %s", req.Method, rpcURL)

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "bridge unreachable: "+err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
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

	w := ctx.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, hasFlusher := w.Writer.(http.Flusher)

	// Stream live events from the bridge's /sse endpoint.
	// History is NOT replayed here; clients fetch it via GET /{sessionId}/messages.
	c.streamBridgeSSE(ctx.Request().Context(), addr, w, flusher, hasFlusher)
	return nil
}

// streamBridgeSSE proxies the ACP bridge's /sse stream to the current response writer.
func (c *ACPController) streamBridgeSSE(ctx interface{ Done() <-chan struct{} }, addr string, w io.Writer, flusher http.Flusher, hasFlusher bool) {
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/sse", nil)
	if err != nil {
		log.Printf("[ACP] SSE: failed to create bridge request: %v", err)
		return
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[ACP] SSE: failed to connect to bridge: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := resp.Body.Read(buf)
		if n > 0 {
			data := buf[:n]
			// Pass through SSE lines, re-emitting only "data:" lines as proper SSE events.
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "data: ") {
					jsonData := strings.TrimPrefix(line, "data: ")
					if jsonData != "" {
						_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", jsonData)
						if hasFlusher {
							flusher.Flush()
						}
					}
				}
			}
		}
		if err != nil {
			return
		}
	}
}
