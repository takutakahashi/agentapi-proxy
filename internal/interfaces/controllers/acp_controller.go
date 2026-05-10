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

	status := session.Status()
	if status != "running" && status != "stable" {
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
// session/prompt and session/cancel
// ----------------------------------------------------------------------------

type sessionPromptParams struct {
	SessionId string `json:"sessionId"`
	Prompt    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"prompt"`
}

// proxyToBridge handles session/prompt and session/cancel.
// It tries the ACP /rpc endpoint first (used by claude-acp agent type), and falls
// back to the agentapi REST /message endpoint for standard agentapi sessions.
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

	// Try ACP /rpc endpoint (claude-acp agent type runs an ACP bridge that exposes /rpc).
	if handled, err := c.tryRPCProxy(ctx, req, addr); handled {
		return err
	}

	// Fall back to agentapi REST API (standard agentapi sessions, no /rpc endpoint).
	if req.Method == "session/cancel" {
		return c.sendAgentapiMessage(ctx, req, addr, "raw", "\x03")
	}

	var promptParams sessionPromptParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &promptParams); err != nil {
			return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "invalid params: "+err.Error()))
		}
	}
	var textParts []string
	for _, block := range promptParams.Prompt {
		if block.Type == "text" && block.Text != "" {
			textParts = append(textParts, block.Text)
		}
	}
	if len(textParts) == 0 {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32602, "no text content in prompt"))
	}
	return c.sendAgentapiMessage(ctx, req, addr, "user", strings.Join(textParts, "\n"))
}

// tryRPCProxy POSTs the JSON-RPC request to the backend /rpc endpoint.
// Returns (true, result) if the backend has /rpc; returns (false, nil) on 404 so the
// caller can fall back to the agentapi REST API.
func (c *ACPController) tryRPCProxy(ctx echo.Context, req acpRequest, addr string) (bool, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return true, ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to marshal request"))
	}

	rpcURL := "http://" + addr + "/rpc"
	log.Printf("[ACP] %s → POST %s (trying ACP bridge)", req.Method, rpcURL)

	resp, err := http.Post(rpcURL, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return true, ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "bridge unreachable: "+err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[ACP] %s: /rpc not found, falling back to agentapi REST", req.Method)
		return false, nil
	}
	if resp.StatusCode == http.StatusAccepted {
		return true, ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
	}

	var rpcResp interface{}
	_ = json.NewDecoder(resp.Body).Decode(&rpcResp)
	return true, ctx.JSON(resp.StatusCode, rpcResp)
}

// sendAgentapiMessage POSTs to the agentapi REST /message endpoint.
func (c *ACPController) sendAgentapiMessage(ctx echo.Context, req acpRequest, addr, msgType, content string) error {
	body, err := json.Marshal(map[string]string{"type": msgType, "content": content})
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "failed to marshal request"))
	}

	msgURL := "http://" + addr + "/message"
	log.Printf("[ACP] %s → POST %s (type=%s)", req.Method, msgURL, msgType)

	resp, err := http.Post(msgURL, "application/json", bytes.NewReader(body)) //nolint:gosec
	if err != nil {
		return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, "agentapi unreachable: "+err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ctx.JSON(http.StatusOK, acpSuccessResp(req.ID, struct{}{}))
	}
	return ctx.JSON(http.StatusOK, acpErrResp(req.ID, -32603, fmt.Sprintf("agentapi error (HTTP %d)", resp.StatusCode)))
}

// ----------------------------------------------------------------------------
// HandleSSE – GET /acp/sse
// ----------------------------------------------------------------------------

// HandleSSE handles GET /acp/sse?session_id=<id>.
// On connect it replays the stored message history from the bridge, then streams
// live session/update notifications until the client disconnects.
func (c *ACPController) HandleSSE(ctx echo.Context) error {
	sessionId := ctx.QueryParam("session_id")
	if sessionId == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"message": "session_id is required"})
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

	// Replay history from the bridge's /messages endpoint.
	if err := c.replayHistory(addr, w, flusher, hasFlusher); err != nil {
		log.Printf("[ACP] SSE history replay error (session=%s): %v", sessionId, err)
	}

	// Stream live events from the bridge's /sse endpoint.
	c.streamBridgeSSE(ctx.Request().Context(), addr, w, flusher, hasFlusher)
	return nil
}

// replayHistory fetches and writes all stored bridge messages as SSE events.
func (c *ACPController) replayHistory(addr string, w io.Writer, flusher http.Flusher, hasFlusher bool) error {
	resp, err := http.Get("http://" + addr + "/messages") //nolint:gosec
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var histResp struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&histResp); err != nil {
		return err
	}

	for _, raw := range histResp.Messages {
		if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", raw); err != nil {
			return err
		}
		if hasFlusher {
			flusher.Flush()
		}
	}
	return nil
}

// streamBridgeSSE proxies the backend SSE stream to the current response writer.
// It tries /sse first (ACP bridge, used by claude-acp) and falls back to /events
// (standard agentapi) if /sse returns 404.
func (c *ACPController) streamBridgeSSE(ctx interface{ Done() <-chan struct{} }, addr string, w io.Writer, flusher http.Flusher, hasFlusher bool) {
	sseURL := "http://" + addr + "/sse"
	probeReq, _ := http.NewRequest(http.MethodGet, sseURL, nil)
	if probeResp, err := http.DefaultClient.Do(probeReq); err == nil && probeResp.StatusCode == http.StatusNotFound {
		_ = probeResp.Body.Close()
		sseURL = "http://" + addr + "/events"
		log.Printf("[ACP] SSE: /sse not found, falling back to /events")
	} else if probeResp != nil {
		_ = probeResp.Body.Close()
	}

	req, err := http.NewRequest(http.MethodGet, sseURL, nil)
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
