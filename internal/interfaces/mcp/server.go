package mcp

import (
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpusecases "github.com/takutakahashi/agentapi-proxy/internal/usecases/mcp"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// MCPServer wraps the MCP server and use cases
type MCPServer struct {
	server                   *mcp.Server
	useCase                  *mcpusecases.MCPSessionToolsUseCase
	authenticatedUserID      string
	authenticatedTeams       []string // GitHub team slugs (e.g., ["org/team-a"])
	authenticatedGithubToken string   // GitHub token from Authorization header
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(sessionManager repositories.SessionManager, shareRepo repositories.ShareRepository, authenticatedUserID string, authenticatedTeams []string, authenticatedGithubToken string, opts *mcp.ServerOptions) *MCPServer {
	// Create use case with actual dependencies
	useCase := mcpusecases.NewMCPSessionToolsUseCase(sessionManager, shareRepo)

	// Create MCP server
	impl := &mcp.Implementation{
		Name:    "agentapi-proxy-mcp",
		Version: "1.0.0",
	}

	server := mcp.NewServer(impl, opts)

	return &MCPServer{
		server:                   server,
		useCase:                  useCase,
		authenticatedUserID:      authenticatedUserID,
		authenticatedTeams:       authenticatedTeams,
		authenticatedGithubToken: authenticatedGithubToken,
	}
}

// RegisterTools registers all MCP tools
func (s *MCPServer) RegisterTools() {
	// Register list_sessions tool
	listSessionsTool := &mcp.Tool{
		Name:        "list_sessions",
		Description: "List and filter active sessions",
	}
	mcp.AddTool(s.server, listSessionsTool, s.handleListSessions)

	// Register create_session tool
	createSessionTool := &mcp.Tool{
		Name:        "create_session",
		Description: "Create a new agent session",
	}
	mcp.AddTool(s.server, createSessionTool, s.handleCreateSession)

	// Register get_session_status tool
	getStatusTool := &mcp.Tool{
		Name:        "get_session_status",
		Description: "Get the status of an agent session",
	}
	mcp.AddTool(s.server, getStatusTool, s.handleGetStatus)

	// Register send_message tool
	sendMessageTool := &mcp.Tool{
		Name:        "send_message",
		Description: "Send a message to an agent session",
	}
	mcp.AddTool(s.server, sendMessageTool, s.handleSendMessage)

	// Register get_messages tool
	getMessagesTool := &mcp.Tool{
		Name:        "get_messages",
		Description: "Get conversation history from a session",
	}
	mcp.AddTool(s.server, getMessagesTool, s.handleGetMessages)

	// Register delete_session tool
	deleteSessionTool := &mcp.Tool{
		Name:        "delete_session",
		Description: "Delete an agent session",
	}
	mcp.AddTool(s.server, deleteSessionTool, s.handleDeleteSession)

	slog.Info("[MCP] Registered 6 tools: list_sessions, create_session, get_session_status, send_message, get_messages, delete_session")
}

// GetServer returns the underlying MCP server
func (s *MCPServer) GetServer() *mcp.Server {
	return s.server
}
