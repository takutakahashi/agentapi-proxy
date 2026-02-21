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
	taskUseCase              *mcpusecases.MCPTaskToolsUseCase
	authenticatedUserID      string
	authenticatedTeams       []string // GitHub team slugs (e.g., ["org/team-a"])
	authenticatedGithubToken string   // GitHub token from Authorization header
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(sessionManager repositories.SessionManager, shareRepo repositories.ShareRepository, taskRepo repositories.TaskRepository, taskGroupRepo repositories.TaskGroupRepository, authenticatedUserID string, authenticatedTeams []string, authenticatedGithubToken string, opts *mcp.ServerOptions) *MCPServer {
	// Create session use case with actual dependencies
	useCase := mcpusecases.NewMCPSessionToolsUseCase(sessionManager, shareRepo)

	// Create task use case (may be nil if repos are not configured)
	var taskUseCase *mcpusecases.MCPTaskToolsUseCase
	if taskRepo != nil || taskGroupRepo != nil {
		taskUseCase = mcpusecases.NewMCPTaskToolsUseCase(taskRepo, taskGroupRepo)
	}

	// Create MCP server
	impl := &mcp.Implementation{
		Name:    "agentapi-proxy-mcp",
		Version: "1.0.0",
	}

	server := mcp.NewServer(impl, opts)

	return &MCPServer{
		server:                   server,
		useCase:                  useCase,
		taskUseCase:              taskUseCase,
		authenticatedUserID:      authenticatedUserID,
		authenticatedTeams:       authenticatedTeams,
		authenticatedGithubToken: authenticatedGithubToken,
	}
}

// RegisterTools registers all MCP tools
func (s *MCPServer) RegisterTools() {
	// Register session tools
	s.registerSessionTools()

	// Register task tools if available
	if s.taskUseCase != nil {
		s.registerTaskTools()
	}
}

// registerSessionTools registers session management MCP tools
func (s *MCPServer) registerSessionTools() {
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

	slog.Info("[MCP] Registered 6 session tools: list_sessions, create_session, get_session_status, send_message, get_messages, delete_session")
}

// registerTaskTools registers task management MCP tools
func (s *MCPServer) registerTaskTools() {
	// Register list_tasks tool
	listTasksTool := &mcp.Tool{
		Name:        "list_tasks",
		Description: "List tasks with optional filters (scope, team_id, group_id, status, task_type)",
	}
	mcp.AddTool(s.server, listTasksTool, s.handleListTasks)

	// Register get_task tool
	getTaskTool := &mcp.Tool{
		Name:        "get_task",
		Description: "Get details of a specific task by ID",
	}
	mcp.AddTool(s.server, getTaskTool, s.handleGetTask)

	// Register create_task tool
	createTaskTool := &mcp.Tool{
		Name:        "create_task",
		Description: "Create a new task. task_type must be 'user' or 'agent'. scope must be 'user' or 'team'.",
	}
	mcp.AddTool(s.server, createTaskTool, s.handleCreateTask)

	// Register update_task tool
	updateTaskTool := &mcp.Tool{
		Name:        "update_task",
		Description: "Update an existing task. Only provided fields are updated.",
	}
	mcp.AddTool(s.server, updateTaskTool, s.handleUpdateTask)

	// Register delete_task tool
	deleteTaskTool := &mcp.Tool{
		Name:        "delete_task",
		Description: "Delete a task by ID",
	}
	mcp.AddTool(s.server, deleteTaskTool, s.handleDeleteTask)

	// Register list_task_groups tool
	listTaskGroupsTool := &mcp.Tool{
		Name:        "list_task_groups",
		Description: "List task groups with optional filters (scope, team_id)",
	}
	mcp.AddTool(s.server, listTaskGroupsTool, s.handleListTaskGroups)

	// Register create_task_group tool
	createTaskGroupTool := &mcp.Tool{
		Name:        "create_task_group",
		Description: "Create a new task group. scope must be 'user' or 'team'.",
	}
	mcp.AddTool(s.server, createTaskGroupTool, s.handleCreateTaskGroup)

	// Register delete_task_group tool
	deleteTaskGroupTool := &mcp.Tool{
		Name:        "delete_task_group",
		Description: "Delete a task group by ID",
	}
	mcp.AddTool(s.server, deleteTaskGroupTool, s.handleDeleteTaskGroup)

	slog.Info("[MCP] Registered 8 task tools: list_tasks, get_task, create_task, update_task, delete_task, list_task_groups, create_task_group, delete_task_group")
}

// GetServer returns the underlying MCP server
func (s *MCPServer) GetServer() *mcp.Server {
	return s.server
}
