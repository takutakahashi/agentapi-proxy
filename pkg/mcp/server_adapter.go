package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	oldmcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

// ServerAdapter adapts MCP server to Echo HTTP handler
type ServerAdapter struct {
	mcpServer     *mcp.Server
	mcpController *controllers.MCPController
	httpHandler   http.Handler
}

// Tool parameter types
type CreateSessionParams struct {
	Environment map[string]string `json:"environment,omitempty" jsonschema:"description=Environment variables"`
	Tags        map[string]string `json:"tags,omitempty" jsonschema:"description=Tags to attach"`
	Params      map[string]any    `json:"params,omitempty" jsonschema:"description=Session parameters"`
	Scope       string            `json:"scope,omitempty" jsonschema:"enum=user,enum=team,description=Resource scope"`
	TeamID      string            `json:"team_id,omitempty" jsonschema:"description=Team ID"`
}

type ListSessionsParams struct {
	Status string            `json:"status,omitempty" jsonschema:"description=Filter by status"`
	Scope  string            `json:"scope,omitempty" jsonschema:"enum=user,enum=team,description=Filter by scope"`
	TeamID string            `json:"team_id,omitempty" jsonschema:"description=Filter by team ID"`
	Tags   map[string]string `json:"tags,omitempty" jsonschema:"description=Filter by tags"`
}

type GetSessionParams struct {
	SessionID string `json:"session_id" jsonschema:"required,description=Session ID"`
}

type DeleteSessionParams struct {
	SessionID string `json:"session_id" jsonschema:"required,description=Session ID"`
}

type SendMessageParams struct {
	SessionID string `json:"session_id" jsonschema:"required,description=Session ID"`
	Message   string `json:"message" jsonschema:"required,description=Message content"`
	Type      string `json:"type,omitempty" jsonschema:"enum=user,enum=raw,description=Message type"`
}

type GetMessagesParams struct {
	SessionID string `json:"session_id" jsonschema:"required,description=Session ID"`
}

type GetStatusParams struct {
	SessionID string `json:"session_id" jsonschema:"required,description=Session ID"`
}

// NewServerAdapter creates a new ServerAdapter
func NewServerAdapter(mcpController *controllers.MCPController) *ServerAdapter {
	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agentapi-proxy-mcp",
		Version: "1.0.0",
	}, nil)

	adapter := &ServerAdapter{
		mcpServer:     server,
		mcpController: mcpController,
	}

	return adapter
}

// RegisterTools registers all MCP tools
func (a *ServerAdapter) RegisterTools() {
	// Register tools using mcp.AddTool
	mcp.AddTool(a.mcpServer, &mcp.Tool{
		Name:        "create_session",
		Description: "Create a new agentapi session",
	}, a.handleCreateSession)

	mcp.AddTool(a.mcpServer, &mcp.Tool{
		Name:        "list_sessions",
		Description: "List and search sessions",
	}, a.handleListSessions)

	mcp.AddTool(a.mcpServer, &mcp.Tool{
		Name:        "get_session",
		Description: "Get details of a specific session",
	}, a.handleGetSession)

	mcp.AddTool(a.mcpServer, &mcp.Tool{
		Name:        "delete_session",
		Description: "Delete a session",
	}, a.handleDeleteSession)

	mcp.AddTool(a.mcpServer, &mcp.Tool{
		Name:        "send_message",
		Description: "Send a message to a session",
	}, a.handleSendMessage)

	mcp.AddTool(a.mcpServer, &mcp.Tool{
		Name:        "get_messages",
		Description: "Get conversation history from a session",
	}, a.handleGetMessages)

	mcp.AddTool(a.mcpServer, &mcp.Tool{
		Name:        "get_status",
		Description: "Get the status of a session",
	}, a.handleGetStatus)

	// Create the Streamable HTTP handler
	a.httpHandler = mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return a.mcpServer
	}, nil)

	log.Printf("[MCP_ADAPTER] Registered 7 MCP tools")
}

// HandleMCPRequest handles MCP protocol requests via Echo
func (a *ServerAdapter) HandleMCPRequest(c echo.Context) error {
	// Store Echo context in request context for tool handlers to access
	ctx := context.WithValue(c.Request().Context(), echoContextKey, c)
	req := c.Request().WithContext(ctx)

	// Delegate to the Streamable HTTP handler
	a.httpHandler.ServeHTTP(c.Response(), req)
	return nil
}

// Context key for storing Echo context
type contextKey string

const echoContextKey contextKey = "echo-context"

// getEchoContext extracts Echo context from request context
func getEchoContext(ctx context.Context) echo.Context {
	if c, ok := ctx.Value(echoContextKey).(echo.Context); ok {
		return c
	}
	return nil
}

// Tool handlers that match mcp.AddTool signature

func (a *ServerAdapter) handleCreateSession(ctx context.Context, req *mcp.CallToolRequest, params *CreateSessionParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	// Create tool context
	toolContext := &controllers.ToolContext{
		Context:     ctx,
		EchoContext: echoCtx,
		Controller:  a.mcpController,
	}

	// Convert params to arguments map
	arguments := make(map[string]interface{})
	if params.Environment != nil {
		arguments["environment"] = params.Environment
	}
	if params.Tags != nil {
		arguments["tags"] = params.Tags
	}
	if params.Params != nil {
		arguments["params"] = params.Params
	}
	if params.Scope != "" {
		arguments["scope"] = params.Scope
	}
	if params.TeamID != "" {
		arguments["team_id"] = params.TeamID
	}

	// Create CallToolRequest for the controller
	oldReq := createOldCallToolRequest("create_session", arguments)

	// Call existing controller handler
	result, err := a.mcpController.HandleCreateSession(toolContext, oldReq)
	if err != nil {
		return nil, nil, err
	}

	// Convert old result to new result
	return convertResult(result), nil, nil
}

func (a *ServerAdapter) handleListSessions(ctx context.Context, req *mcp.CallToolRequest, params *ListSessionsParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	toolContext := &controllers.ToolContext{
		Context:     ctx,
		EchoContext: echoCtx,
		Controller:  a.mcpController,
	}

	arguments := make(map[string]interface{})
	if params.Status != "" {
		arguments["status"] = params.Status
	}
	if params.Scope != "" {
		arguments["scope"] = params.Scope
	}
	if params.TeamID != "" {
		arguments["team_id"] = params.TeamID
	}
	if params.Tags != nil {
		arguments["tags"] = params.Tags
	}

	oldReq := createOldCallToolRequest("list_sessions", arguments)
	result, err := a.mcpController.HandleListSessions(toolContext, oldReq)
	if err != nil {
		return nil, nil, err
	}

	return convertResult(result), nil, nil
}

func (a *ServerAdapter) handleGetSession(ctx context.Context, req *mcp.CallToolRequest, params *GetSessionParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	toolContext := &controllers.ToolContext{
		Context:     ctx,
		EchoContext: echoCtx,
		Controller:  a.mcpController,
	}

	arguments := map[string]interface{}{
		"session_id": params.SessionID,
	}

	oldReq := createOldCallToolRequest("get_session", arguments)
	result, err := a.mcpController.HandleGetSession(toolContext, oldReq)
	if err != nil {
		return nil, nil, err
	}

	return convertResult(result), nil, nil
}

func (a *ServerAdapter) handleDeleteSession(ctx context.Context, req *mcp.CallToolRequest, params *DeleteSessionParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	toolContext := &controllers.ToolContext{
		Context:     ctx,
		EchoContext: echoCtx,
		Controller:  a.mcpController,
	}

	arguments := map[string]interface{}{
		"session_id": params.SessionID,
	}

	oldReq := createOldCallToolRequest("delete_session", arguments)
	result, err := a.mcpController.HandleDeleteSession(toolContext, oldReq)
	if err != nil {
		return nil, nil, err
	}

	return convertResult(result), nil, nil
}

func (a *ServerAdapter) handleSendMessage(ctx context.Context, req *mcp.CallToolRequest, params *SendMessageParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	toolContext := &controllers.ToolContext{
		Context:     ctx,
		EchoContext: echoCtx,
		Controller:  a.mcpController,
	}

	arguments := map[string]interface{}{
		"session_id": params.SessionID,
		"message":    params.Message,
	}
	if params.Type != "" {
		arguments["type"] = params.Type
	}

	oldReq := createOldCallToolRequest("send_message", arguments)
	result, err := a.mcpController.HandleSendMessage(toolContext, oldReq)
	if err != nil {
		return nil, nil, err
	}

	return convertResult(result), nil, nil
}

func (a *ServerAdapter) handleGetMessages(ctx context.Context, req *mcp.CallToolRequest, params *GetMessagesParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	toolContext := &controllers.ToolContext{
		Context:     ctx,
		EchoContext: echoCtx,
		Controller:  a.mcpController,
	}

	arguments := map[string]interface{}{
		"session_id": params.SessionID,
	}

	oldReq := createOldCallToolRequest("get_messages", arguments)
	result, err := a.mcpController.HandleGetMessages(toolContext, oldReq)
	if err != nil {
		return nil, nil, err
	}

	return convertResult(result), nil, nil
}

func (a *ServerAdapter) handleGetStatus(ctx context.Context, req *mcp.CallToolRequest, params *GetStatusParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	toolContext := &controllers.ToolContext{
		Context:     ctx,
		EchoContext: echoCtx,
		Controller:  a.mcpController,
	}

	arguments := map[string]interface{}{
		"session_id": params.SessionID,
	}

	oldReq := createOldCallToolRequest("get_status", arguments)
	result, err := a.mcpController.HandleGetStatus(toolContext, oldReq)
	if err != nil {
		return nil, nil, err
	}

	return convertResult(result), nil, nil
}

// Helper functions to bridge between old and new SDK types

func createOldCallToolRequest(name string, arguments map[string]interface{}) oldmcp.CallToolRequest {
	// Create request via JSON marshaling because CallToolRequest has unexported fields
	requestData := map[string]interface{}{
		"params": map[string]interface{}{
			"name":      name,
			"arguments": arguments,
		},
	}
	requestBytes, _ := json.Marshal(requestData)

	var req oldmcp.CallToolRequest
	_ = json.Unmarshal(requestBytes, &req)
	return req
}

func convertResult(oldResult *oldmcp.CallToolResult) *mcp.CallToolResult {
	if oldResult == nil {
		return &mcp.CallToolResult{}
	}

	// Convert Content from old format to new format
	var newContent []mcp.Content
	for _, c := range oldResult.Content {
		// Assuming old content is *TextContent
		if textContent, ok := c.(*oldmcp.TextContent); ok {
			newContent = append(newContent, &mcp.TextContent{
				Text: textContent.Text,
			})
		}
	}

	return &mcp.CallToolResult{
		Content: newContent,
		IsError: oldResult.IsError,
	}
}
