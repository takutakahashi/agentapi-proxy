package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
)

// ServerAdapter adapts MCP server to Echo HTTP handler
type ServerAdapter struct {
	mcpController *controllers.MCPController
	tools         map[string]func(*controllers.ToolContext, mcp.CallToolRequest) (*mcp.CallToolResult, error)
}

// NewServerAdapter creates a new ServerAdapter
func NewServerAdapter(mcpController *controllers.MCPController) *ServerAdapter {
	return &ServerAdapter{
		mcpController: mcpController,
		tools:         make(map[string]func(*controllers.ToolContext, mcp.CallToolRequest) (*mcp.CallToolResult, error)),
	}
}

// RegisterTools registers all MCP tools
func (a *ServerAdapter) RegisterTools() {
	a.tools["create_session"] = a.mcpController.HandleCreateSession
	a.tools["list_sessions"] = a.mcpController.HandleListSessions
	a.tools["get_session"] = a.mcpController.HandleGetSession
	a.tools["delete_session"] = a.mcpController.HandleDeleteSession
	a.tools["send_message"] = a.mcpController.HandleSendMessage
	a.tools["get_messages"] = a.mcpController.HandleGetMessages
	a.tools["get_status"] = a.mcpController.HandleGetStatus

	log.Printf("[MCP_ADAPTER] Registered 7 MCP tools")
}

// HandleMCPRequest handles MCP protocol requests via Echo
func (a *ServerAdapter) HandleMCPRequest(c echo.Context) error {
	// Read request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		log.Printf("[MCP_ADAPTER] Failed to read request body: %v", err)
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Failed to read request body",
		})
	}

	// Parse MCP request
	var mcpRequest map[string]interface{}
	if err := json.Unmarshal(body, &mcpRequest); err != nil {
		log.Printf("[MCP_ADAPTER] Failed to parse MCP request: %v", err)
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Invalid MCP request format",
		})
	}

	// Extract method
	method, ok := mcpRequest["method"].(string)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "Missing or invalid 'method' field",
		})
	}

	// Handle different MCP methods
	switch method {
	case "tools/list":
		return a.handleToolsList(c, mcpRequest)
	case "tools/call":
		return a.handleToolsCall(c, mcpRequest)
	case "initialize":
		return a.handleInitialize(c, mcpRequest)
	default:
		log.Printf("[MCP_ADAPTER] Unknown method: %s", method)
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("Unknown method: %s", method),
		})
	}
}

// handleToolsList handles tools/list method
func (a *ServerAdapter) handleToolsList(c echo.Context, request map[string]interface{}) error {
	// Get request ID
	id := request["id"]

	// Define available tools
	tools := []map[string]interface{}{
		{
			"name":        "create_session",
			"description": "Create a new agentapi session",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"environment": map[string]interface{}{"type": "object", "description": "Environment variables"},
					"tags":        map[string]interface{}{"type": "object", "description": "Tags to attach"},
					"params":      map[string]interface{}{"type": "object", "description": "Session parameters"},
					"scope":       map[string]interface{}{"type": "string", "enum": []string{"user", "team"}, "description": "Resource scope"},
					"team_id":     map[string]interface{}{"type": "string", "description": "Team ID"},
				},
			},
		},
		{
			"name":        "list_sessions",
			"description": "List and search sessions",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status":  map[string]interface{}{"type": "string", "description": "Filter by status"},
					"scope":   map[string]interface{}{"type": "string", "enum": []string{"user", "team"}, "description": "Filter by scope"},
					"team_id": map[string]interface{}{"type": "string", "description": "Filter by team ID"},
					"tags":    map[string]interface{}{"type": "object", "description": "Filter by tags"},
				},
			},
		},
		{
			"name":        "get_session",
			"description": "Get details of a specific session",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{"type": "string", "description": "Session ID"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			"name":        "delete_session",
			"description": "Delete a session",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{"type": "string", "description": "Session ID"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			"name":        "send_message",
			"description": "Send a message to a session",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{"type": "string", "description": "Session ID"},
					"message":    map[string]interface{}{"type": "string", "description": "Message content"},
					"type":       map[string]interface{}{"type": "string", "enum": []string{"user", "raw"}, "description": "Message type"},
				},
				"required": []string{"session_id", "message"},
			},
		},
		{
			"name":        "get_messages",
			"description": "Get conversation history from a session",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{"type": "string", "description": "Session ID"},
				},
				"required": []string{"session_id"},
			},
		},
		{
			"name":        "get_status",
			"description": "Get the status of a session",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]interface{}{"type": "string", "description": "Session ID"},
				},
				"required": []string{"session_id"},
			},
		},
	}

	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"tools": tools,
		},
	}

	// Set CORS headers
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")

	return c.JSON(http.StatusOK, response)
}

// handleToolsCall handles tools/call method
func (a *ServerAdapter) handleToolsCall(c echo.Context, request map[string]interface{}) error {
	// Get request ID
	id := request["id"]

	// Extract params
	params, ok := request["params"].(map[string]interface{})
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]interface{}{
				"code":    -32602,
				"message": "Invalid params",
			},
		})
	}

	// Extract tool name
	toolName, ok := params["name"].(string)
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]interface{}{
				"code":    -32602,
				"message": "Missing tool name",
			},
		})
	}

	// Get arguments
	arguments, _ := params["arguments"].(map[string]interface{})

	// Find tool handler
	handler, ok := a.tools[toolName]
	if !ok {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("Tool not found: %s", toolName),
			},
		})
	}

	// Create tool context
	toolContext := &controllers.ToolContext{
		Context:     c.Request().Context(),
		EchoContext: c,
		Controller:  a.mcpController,
	}

	// Create MCP request by marshaling and unmarshaling
	// This is necessary because CallToolRequest has unexported fields
	requestData := map[string]interface{}{
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}
	requestBytes, _ := json.Marshal(requestData)

	var mcpToolRequest mcp.CallToolRequest
	_ = json.Unmarshal(requestBytes, &mcpToolRequest)

	// Call handler
	result, err := handler(toolContext, mcpToolRequest)
	if err != nil {
		log.Printf("[MCP_ADAPTER] Tool handler error for %s: %v", toolName, err)
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]interface{}{
				"code":    -32603,
				"message": fmt.Sprintf("Internal error: %v", err),
			},
		})
	}

	// Format response
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}

	// Set CORS headers
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")

	return c.JSON(http.StatusOK, response)
}

// handleInitialize handles initialize method
func (a *ServerAdapter) handleInitialize(c echo.Context, request map[string]interface{}) error {
	// Get request ID
	id := request["id"]

	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "agentapi-proxy-mcp-integrated",
				"version": "1.0.0",
			},
		},
	}

	// Set CORS headers
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")

	return c.JSON(http.StatusOK, response)
}
