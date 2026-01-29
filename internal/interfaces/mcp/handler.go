package mcp

import (
	"log"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
)

// MCPHandler implements the CustomHandler interface for MCP endpoints
type MCPHandler struct {
	mcpServer   *MCPServer
	httpHandler http.Handler
}

// NewMCPHandler creates a new MCP handler for the /mcp endpoint
func NewMCPHandler(proxyURL string) *MCPHandler {
	// Create MCP server with options
	opts := &mcp.ServerOptions{
		Logger: slog.Default(),
	}

	mcpServer := NewMCPServer(proxyURL, opts)

	// Register all tools
	mcpServer.RegisterTools()

	// Create HTTP handler using go-sdk's streamable HTTP handler
	httpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return mcpServer.GetServer()
	}, nil)

	return &MCPHandler{
		mcpServer:   mcpServer,
		httpHandler: httpHandler,
	}
}

// GetName returns the name of this handler for logging
func (h *MCPHandler) GetName() string {
	return "MCPHandler"
}

// RegisterRoutes registers the /mcp endpoint with Echo
func (h *MCPHandler) RegisterRoutes(e *echo.Echo, server *app.Server) error {
	// Register /mcp endpoint - handles all HTTP methods (POST, GET, etc.)
	e.Any("/mcp", func(c echo.Context) error {
		// Delegate to the go-sdk HTTP handler
		h.httpHandler.ServeHTTP(c.Response(), c.Request())
		return nil
	})

	log.Printf("[MCP] Registered /mcp endpoint successfully")
	return nil
}
