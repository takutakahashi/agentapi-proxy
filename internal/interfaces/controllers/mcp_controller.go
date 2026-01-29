package controllers

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

// MCPController handles MCP tool requests
type MCPController struct {
	sessionManagerProvider SessionManagerProvider
	sessionCreator         SessionCreator
	validateTeamUC         *sessionuc.ValidateTeamAccessUseCase
	client                 *client.Client
}

// NewMCPController creates a new MCPController instance
func NewMCPController(
	sessionManagerProvider SessionManagerProvider,
	sessionCreator SessionCreator,
) *MCPController {
	return &MCPController{
		sessionManagerProvider: sessionManagerProvider,
		sessionCreator:         sessionCreator,
		validateTeamUC:         sessionuc.NewValidateTeamAccessUseCase(),
		client:                 nil, // Will be initialized with session address
	}
}

// ToolContext provides context for MCP tool handlers
type ToolContext struct {
	Context     context.Context
	EchoContext echo.Context
	Controller  *MCPController
}

// HandleCreateSession handles the create_session MCP tool
func (c *MCPController) HandleCreateSession(tc *ToolContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract parameters
	environment := mcp.ParseStringMap(request, "environment", nil)
	tags := mcp.ParseStringMap(request, "tags", nil)
	paramsMap := mcp.ParseStringMap(request, "params", nil)
	scope := mcp.ParseString(request, "scope", string(entities.ScopeUser))
	teamID := mcp.ParseString(request, "team_id", "")

	// Convert maps to proper types
	envMap := make(map[string]string)
	for k, v := range environment {
		if strVal, ok := v.(string); ok {
			envMap[k] = strVal
		}
	}

	tagsMap := make(map[string]string)
	for k, v := range tags {
		if strVal, ok := v.(string); ok {
			tagsMap[k] = strVal
		}
	}

	// Extract params
	var params *entities.SessionParams
	if paramsMap != nil {
		params = &entities.SessionParams{}
		if msg, ok := paramsMap["message"].(string); ok {
			params.Message = msg
		}
		if token, ok := paramsMap["github_token"].(string); ok {
			params.GithubToken = token
		}
		if agentType, ok := paramsMap["agent_type"].(string); ok {
			params.AgentType = agentType
		}
	}

	// Get user from context
	user := auth.GetUserFromContext(tc.EchoContext)
	var userID, userRole string
	var teams []string
	if user != nil {
		userID = string(user.ID())
		if len(user.Roles()) > 0 {
			userRole = string(user.Roles()[0])
		} else {
			userRole = "user"
		}
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				teams = append(teams, teamSlug)
			}
		}
	} else {
		userID = "anonymous"
		userRole = "guest"
	}

	// Validate team scope
	if err := c.validateTeamUC.ValidateTeamScope(
		entities.ResourceScope(scope),
		teamID,
		teams,
		user != nil,
	); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Validation failed: %v", err)), nil
	}

	// Create start request
	startReq := entities.StartRequest{
		Environment: envMap,
		Tags:        tagsMap,
		Params:      params,
		Scope:       entities.ResourceScope(scope),
		TeamID:      teamID,
	}

	// Create session
	sessionID := uuid.New().String()
	session, err := c.sessionCreator.CreateSession(sessionID, startReq, userID, userRole, teams)
	if err != nil {
		log.Printf("[MCP] Failed to create session: %v", err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to create session: %v", err)), nil
	}

	log.Printf("[MCP] Session created: %s by user: %s", session.ID(), userID)

	resultText := fmt.Sprintf("Session created successfully.\nSession ID: %s\nStatus: %s\nScope: %s",
		session.ID(), session.Status(), session.Scope())
	if teamID != "" {
		resultText += fmt.Sprintf("\nTeam ID: %s", teamID)
	}

	return mcp.NewToolResultText(resultText), nil
}

// HandleListSessions handles the list_sessions MCP tool
func (c *MCPController) HandleListSessions(tc *ToolContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract parameters
	status := mcp.ParseString(request, "status", "")
	scope := mcp.ParseString(request, "scope", "")
	teamID := mcp.ParseString(request, "team_id", "")
	tags := mcp.ParseStringMap(request, "tags", nil)

	// Convert tags
	tagsMap := make(map[string]string)
	for k, v := range tags {
		if strVal, ok := v.(string); ok {
			tagsMap[k] = strVal
		}
	}

	// Get user from context
	user := auth.GetUserFromContext(tc.EchoContext)
	var userID string
	var userTeamIDs []string
	if user != nil {
		userID = string(user.ID())
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				userTeamIDs = append(userTeamIDs, teamSlug)
			}
		}
	}

	// Build filter
	filter := entities.SessionFilter{
		Status:  status,
		Tags:    tagsMap,
		Scope:   entities.ResourceScope(scope),
		TeamID:  teamID,
		TeamIDs: userTeamIDs,
	}

	// For non-admin users, set UserID filter for user-scoped resources
	if user != nil && !user.IsAdmin() && scope != string(entities.ScopeTeam) && teamID == "" {
		filter.UserID = userID
	}

	// List sessions
	sessions := c.sessionManagerProvider.GetSessionManager().ListSessions(filter)

	// Check if auth is enabled
	cfg := auth.GetConfigFromContext(tc.EchoContext)
	authEnabled := cfg != nil && cfg.Auth.Enabled

	// Filter by authorization
	var filteredSessions []entities.Session
	for _, session := range sessions {
		// If auth is not enabled, include all sessions
		if !authEnabled {
			filteredSessions = append(filteredSessions, session)
			continue
		}

		// Scope isolation
		sessionScope := session.Scope()
		if scope == string(entities.ScopeTeam) {
			if sessionScope != entities.ScopeTeam {
				continue
			}
		} else {
			if sessionScope == entities.ScopeTeam {
				continue
			}
		}

		// Admin can see all sessions within filtered scope
		if user != nil && user.IsAdmin() {
			filteredSessions = append(filteredSessions, session)
			continue
		}

		// Authorization check
		if user != nil && user.CanAccessResource(
			entities.UserID(session.UserID()),
			string(sessionScope),
			session.TeamID(),
		) {
			filteredSessions = append(filteredSessions, session)
		}
	}

	// Format response
	result := fmt.Sprintf("Found %d sessions:\n\n", len(filteredSessions))
	for _, session := range filteredSessions {
		result += fmt.Sprintf("Session ID: %s\n", session.ID())
		result += fmt.Sprintf("  User: %s\n", session.UserID())
		result += fmt.Sprintf("  Scope: %s\n", session.Scope())
		if session.TeamID() != "" {
			result += fmt.Sprintf("  Team: %s\n", session.TeamID())
		}
		result += fmt.Sprintf("  Status: %s\n", session.Status())
		result += fmt.Sprintf("  Started: %s\n", session.StartedAt().Format(time.RFC3339))
		if session.Description() != "" {
			result += fmt.Sprintf("  Description: %s\n", session.Description())
		}
		result += "\n"
	}

	log.Printf("[MCP] Listed %d sessions for user: %s", len(filteredSessions), userID)

	return mcp.NewToolResultText(result), nil
}

// HandleGetSession handles the get_session MCP tool
func (c *MCPController) HandleGetSession(tc *ToolContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(request, "session_id", "")
	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Session not found: %s", sessionID)), nil
	}

	// Check authorization
	user := auth.GetUserFromContext(tc.EchoContext)
	if !c.userCanAccessSession(tc.EchoContext, session) {
		log.Printf("[MCP] Access denied to session %s for user: %s", sessionID, c.getUserID(user))
		return mcp.NewToolResultError("Permission denied: you don't have access to this session"), nil
	}

	// Format response
	result := "Session Details:\n\n"
	result += fmt.Sprintf("ID: %s\n", session.ID())
	result += fmt.Sprintf("User ID: %s\n", session.UserID())
	result += fmt.Sprintf("Scope: %s\n", session.Scope())
	if session.TeamID() != "" {
		result += fmt.Sprintf("Team ID: %s\n", session.TeamID())
	}
	result += fmt.Sprintf("Status: %s\n", session.Status())
	result += fmt.Sprintf("Address: %s\n", session.Addr())
	result += fmt.Sprintf("Started At: %s\n", session.StartedAt().Format(time.RFC3339))
	result += fmt.Sprintf("Updated At: %s\n", session.UpdatedAt().Format(time.RFC3339))
	if session.Description() != "" {
		result += fmt.Sprintf("Description: %s\n", session.Description())
	}

	// Add tags
	if tags := session.Tags(); len(tags) > 0 {
		result += "\nTags:\n"
		for k, v := range tags {
			result += fmt.Sprintf("  %s: %s\n", k, v)
		}
	}

	log.Printf("[MCP] Retrieved session %s for user: %s", sessionID, c.getUserID(user))

	return mcp.NewToolResultText(result), nil
}

// HandleDeleteSession handles the delete_session MCP tool
func (c *MCPController) HandleDeleteSession(tc *ToolContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(request, "session_id", "")
	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Session not found: %s", sessionID)), nil
	}

	// Check authorization
	user := auth.GetUserFromContext(tc.EchoContext)
	if !c.userCanAccessSession(tc.EchoContext, session) {
		log.Printf("[MCP] Delete denied for session %s by user: %s", sessionID, c.getUserID(user))
		return mcp.NewToolResultError("Permission denied: you don't have access to this session"), nil
	}

	// Delete session
	if err := c.sessionCreator.DeleteSessionByID(sessionID); err != nil {
		log.Printf("[MCP] Failed to delete session %s: %v", sessionID, err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to delete session: %v", err)), nil
	}

	log.Printf("[MCP] Deleted session %s by user: %s", sessionID, c.getUserID(user))

	return mcp.NewToolResultText(fmt.Sprintf("Session %s deleted successfully", sessionID)), nil
}

// HandleSendMessage handles the send_message MCP tool
func (c *MCPController) HandleSendMessage(tc *ToolContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(request, "session_id", "")
	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}

	message := mcp.ParseString(request, "message", "")
	if message == "" {
		return mcp.NewToolResultError("message is required"), nil
	}

	messageType := mcp.ParseString(request, "type", "user")

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Session not found: %s", sessionID)), nil
	}

	// Check authorization
	user := auth.GetUserFromContext(tc.EchoContext)
	if !c.userCanAccessSession(tc.EchoContext, session) {
		log.Printf("[MCP] Send message denied for session %s by user: %s", sessionID, c.getUserID(user))
		return mcp.NewToolResultError("Permission denied: you don't have access to this session"), nil
	}

	// Create client for this session
	sessionClient := client.NewClient(fmt.Sprintf("http://%s", session.Addr()))

	msg := &client.Message{
		Content: message,
		Type:    messageType,
	}

	ctx, cancel := context.WithTimeout(tc.Context, 30*time.Second)
	defer cancel()

	resp, err := sessionClient.SendMessage(ctx, sessionID, msg)
	if err != nil {
		log.Printf("[MCP] Failed to send message to session %s: %v", sessionID, err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to send message: %v", err)), nil
	}

	log.Printf("[MCP] Sent message to session %s by user: %s", sessionID, c.getUserID(user))

	return mcp.NewToolResultText(fmt.Sprintf("Message sent successfully. Message ID: %s", resp.ID)), nil
}

// HandleGetMessages handles the get_messages MCP tool
func (c *MCPController) HandleGetMessages(tc *ToolContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(request, "session_id", "")
	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Session not found: %s", sessionID)), nil
	}

	// Check authorization
	user := auth.GetUserFromContext(tc.EchoContext)
	if !c.userCanAccessSession(tc.EchoContext, session) {
		log.Printf("[MCP] Get messages denied for session %s by user: %s", sessionID, c.getUserID(user))
		return mcp.NewToolResultError("Permission denied: you don't have access to this session"), nil
	}

	// Create client for this session
	sessionClient := client.NewClient(fmt.Sprintf("http://%s", session.Addr()))

	ctx, cancel := context.WithTimeout(tc.Context, 30*time.Second)
	defer cancel()

	resp, err := sessionClient.GetMessages(ctx, sessionID)
	if err != nil {
		log.Printf("[MCP] Failed to get messages from session %s: %v", sessionID, err)
		return mcp.NewToolResultError(fmt.Sprintf("Failed to get messages: %v", err)), nil
	}

	result := fmt.Sprintf("Conversation History (%d messages):\n\n", len(resp.Messages))
	for _, msg := range resp.Messages {
		result += fmt.Sprintf("[%s] %s: %s\n\n",
			msg.Timestamp.Format("15:04:05"), msg.Role, msg.Content)
	}

	log.Printf("[MCP] Retrieved %d messages from session %s for user: %s",
		len(resp.Messages), sessionID, c.getUserID(user))

	return mcp.NewToolResultText(result), nil
}

// HandleGetStatus handles the get_status MCP tool
func (c *MCPController) HandleGetStatus(tc *ToolContext, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sessionID := mcp.ParseString(request, "session_id", "")
	if sessionID == "" {
		return mcp.NewToolResultError("session_id is required"), nil
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return mcp.NewToolResultError(fmt.Sprintf("Session not found: %s", sessionID)), nil
	}

	// Check authorization
	user := auth.GetUserFromContext(tc.EchoContext)
	if !c.userCanAccessSession(tc.EchoContext, session) {
		log.Printf("[MCP] Get status denied for session %s by user: %s", sessionID, c.getUserID(user))
		return mcp.NewToolResultError("Permission denied: you don't have access to this session"), nil
	}

	result := fmt.Sprintf("Session Status: %s\n", session.Status())
	result += fmt.Sprintf("Session ID: %s\n", session.ID())
	result += fmt.Sprintf("Started At: %s\n", session.StartedAt().Format(time.RFC3339))
	result += fmt.Sprintf("Updated At: %s\n", session.UpdatedAt().Format(time.RFC3339))

	log.Printf("[MCP] Retrieved status for session %s by user: %s", sessionID, c.getUserID(user))

	return mcp.NewToolResultText(result), nil
}

// userCanAccessSession checks if the current user can access the session
func (c *MCPController) userCanAccessSession(echoCtx echo.Context, session entities.Session) bool {
	user := auth.GetUserFromContext(echoCtx)
	if user == nil {
		// If no auth is configured, allow access
		cfg := auth.GetConfigFromContext(echoCtx)
		if cfg == nil || !cfg.Auth.Enabled {
			return true
		}
		return false
	}
	return user.CanAccessResource(
		entities.UserID(session.UserID()),
		string(session.Scope()),
		session.TeamID(),
	)
}

// getUserID safely gets user ID from user object
func (c *MCPController) getUserID(user *entities.User) string {
	if user == nil {
		return "anonymous"
	}
	return string(user.ID())
}
