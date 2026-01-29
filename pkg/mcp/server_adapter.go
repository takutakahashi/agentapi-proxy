package mcp

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
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
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Params      map[string]any    `json:"params,omitempty"`
	Scope       string            `json:"scope,omitempty"`
	TeamID      string            `json:"team_id,omitempty"`
}

type ListSessionsParams struct {
	Status string            `json:"status,omitempty"`
	Scope  string            `json:"scope,omitempty"`
	TeamID string            `json:"team_id,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
}

type GetSessionParams struct {
	SessionID string `json:"session_id"`
}

type DeleteSessionParams struct {
	SessionID string `json:"session_id"`
}

type SendMessageParams struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	Type      string `json:"type,omitempty"`
}

type GetMessagesParams struct {
	SessionID string `json:"session_id"`
}

type GetStatusParams struct {
	SessionID string `json:"session_id"`
}

// NewServerAdapter creates a new ServerAdapter
func NewServerAdapter(mcpController *controllers.MCPController) *ServerAdapter {
	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agentapi-proxy-mcp",
		Version: "1.0.0",
	}, &mcp.ServerOptions{
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{},
		},
	})

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
	log.Printf("[MCP_ADAPTER] Handling %s request to %s", c.Request().Method, c.Request().URL.Path)

	// Store Echo context in request context for tool handlers to access
	ctx := context.WithValue(c.Request().Context(), echoContextKey, c)
	req := c.Request().WithContext(ctx)

	// Delegate to the Streamable HTTP handler
	// Use the underlying ResponseWriter to avoid Echo's buffering issues
	log.Printf("[MCP_ADAPTER] Delegating to Streamable HTTP handler")
	a.httpHandler.ServeHTTP(c.Response().Writer, req)

	log.Printf("[MCP_ADAPTER] Response status: %d", c.Response().Status)

	// Mark response as committed so Echo doesn't try to write again
	c.Response().Committed = true
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

	// Convert params to controller params
	controllerParams := controllers.CreateSessionParams{
		Environment: params.Environment,
		Tags:        params.Tags,
		Params:      params.Params,
		Scope:       params.Scope,
		TeamID:      params.TeamID,
	}

	// Call controller handler
	resultText, err := a.mcpController.HandleCreateSession(ctx, echoCtx, controllerParams)
	if err != nil {
		return nil, nil, err
	}

	// Wrap result in MCP TextContent
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: resultText},
		},
	}, nil, nil
}

func (a *ServerAdapter) handleListSessions(ctx context.Context, req *mcp.CallToolRequest, params *ListSessionsParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	controllerParams := controllers.ListSessionsParams{
		Status: params.Status,
		Scope:  params.Scope,
		TeamID: params.TeamID,
		Tags:   params.Tags,
	}

	resultText, err := a.mcpController.HandleListSessions(ctx, echoCtx, controllerParams)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: resultText},
		},
	}, nil, nil
}

func (a *ServerAdapter) handleGetSession(ctx context.Context, req *mcp.CallToolRequest, params *GetSessionParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	controllerParams := controllers.SessionIDParams{
		SessionID: params.SessionID,
	}

	resultText, err := a.mcpController.HandleGetSession(ctx, echoCtx, controllerParams)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: resultText},
		},
	}, nil, nil
}

func (a *ServerAdapter) handleDeleteSession(ctx context.Context, req *mcp.CallToolRequest, params *DeleteSessionParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	controllerParams := controllers.SessionIDParams{
		SessionID: params.SessionID,
	}

	resultText, err := a.mcpController.HandleDeleteSession(ctx, echoCtx, controllerParams)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: resultText},
		},
	}, nil, nil
}

func (a *ServerAdapter) handleSendMessage(ctx context.Context, req *mcp.CallToolRequest, params *SendMessageParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	controllerParams := controllers.SendMessageParams{
		SessionID: params.SessionID,
		Message:   params.Message,
		Type:      params.Type,
	}

	resultText, err := a.mcpController.HandleSendMessage(ctx, echoCtx, controllerParams)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: resultText},
		},
	}, nil, nil
}

func (a *ServerAdapter) handleGetMessages(ctx context.Context, req *mcp.CallToolRequest, params *GetMessagesParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	controllerParams := controllers.SessionIDParams{
		SessionID: params.SessionID,
	}

	resultText, err := a.mcpController.HandleGetMessages(ctx, echoCtx, controllerParams)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: resultText},
		},
	}, nil, nil
}

func (a *ServerAdapter) handleGetStatus(ctx context.Context, req *mcp.CallToolRequest, params *GetStatusParams) (*mcp.CallToolResult, any, error) {
	echoCtx := getEchoContext(ctx)
	if echoCtx == nil {
		return nil, nil, fmt.Errorf("echo context not found")
	}

	controllerParams := controllers.SessionIDParams{
		SessionID: params.SessionID,
	}

	resultText, err := a.mcpController.HandleGetStatus(ctx, echoCtx, controllerParams)
	if err != nil {
		return nil, nil, err
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: resultText},
		},
	}, nil, nil
}
