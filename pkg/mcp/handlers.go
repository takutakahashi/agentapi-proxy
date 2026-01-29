package mcp

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// Handlers handles MCP endpoints
type Handlers struct {
	mcpController *controllers.MCPController
	adapter       *ServerAdapter
}

// NewHandlers creates a new Handlers instance
func NewHandlers(sessionManager portrepos.SessionManager, sessionCreator controllers.SessionCreator) *Handlers {
	// Create MCP controller
	mcpController := controllers.NewMCPController(
		&sessionManagerProviderImpl{manager: sessionManager},
		sessionCreator,
	)

	// Create adapter
	adapter := NewServerAdapter(mcpController)

	// Register tools
	adapter.RegisterTools()

	return &Handlers{
		mcpController: mcpController,
		adapter:       adapter,
	}
}

// GetName returns the name of this handler for logging
func (h *Handlers) GetName() string {
	return "MCPHandlers"
}

// RegisterRoutes registers MCP routes
// Implements the app.CustomHandler interface
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
	// Register POST and GET /mcp endpoints for MCP protocol
	// POST: Send messages to server
	// GET: Open SSE stream for server-initiated messages
	e.POST("/mcp", h.adapter.HandleMCPRequest)
	e.GET("/mcp", h.adapter.HandleMCPRequest)
	e.DELETE("/mcp", h.adapter.HandleMCPRequest)

	log.Printf("[MCP_HANDLERS] Registered MCP endpoint at POST/GET/DELETE /mcp")
	log.Printf("[MCP_HANDLERS] Available tools: create_session, list_sessions, get_session, delete_session, send_message, get_messages, get_status")
	log.Printf("[MCP_HANDLERS] NOTE: Standalone 'agentapi-proxy mcp' command is deprecated in favor of /mcp endpoint")

	return nil
}

// sessionManagerProviderImpl implements SessionManagerProvider
type sessionManagerProviderImpl struct {
	manager portrepos.SessionManager
}

func (p *sessionManagerProviderImpl) GetSessionManager() portrepos.SessionManager {
	return p.manager
}
