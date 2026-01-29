package controllers

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
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

// CreateSessionParams represents parameters for creating a session
type CreateSessionParams struct {
	Environment map[string]string
	Tags        map[string]string
	Params      map[string]interface{}
	Scope       string
	TeamID      string
}

// ListSessionsParams represents parameters for listing sessions
type ListSessionsParams struct {
	Status string
	Scope  string
	TeamID string
	Tags   map[string]string
}

// SessionIDParams represents parameters with session_id
type SessionIDParams struct {
	SessionID string
}

// SendMessageParams represents parameters for sending a message
type SendMessageParams struct {
	SessionID string
	Message   string
	Type      string
}

// HandleCreateSession handles the create_session tool
func (c *MCPController) HandleCreateSession(ctx context.Context, echoCtx echo.Context, params CreateSessionParams) (string, error) {
	environment := params.Environment
	tags := params.Tags
	paramsMap := params.Params
	scope := params.Scope
	if scope == "" {
		scope = string(entities.ScopeUser)
	}
	teamID := params.TeamID

	// Extract session params
	var sessionParams *entities.SessionParams
	if paramsMap != nil {
		sessionParams = &entities.SessionParams{}
		if msg, ok := paramsMap["message"].(string); ok {
			sessionParams.Message = msg
		}
		if token, ok := paramsMap["github_token"].(string); ok {
			sessionParams.GithubToken = token
		}
		if agentType, ok := paramsMap["agent_type"].(string); ok {
			sessionParams.AgentType = agentType
		}
	}

	// Get user from context
	user := auth.GetUserFromContext(echoCtx)
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
		return "", fmt.Errorf("validation failed: %v", err)
	}

	// Create start request
	startReq := entities.StartRequest{
		Environment: environment,
		Tags:        tags,
		Params:      sessionParams,
		Scope:       entities.ResourceScope(scope),
		TeamID:      teamID,
	}

	// Create session
	sessionID := uuid.New().String()
	session, err := c.sessionCreator.CreateSession(sessionID, startReq, userID, userRole, teams)
	if err != nil {
		log.Printf("[MCP] Failed to create session: %v", err)
		return "", fmt.Errorf("failed to create session: %v", err)
	}

	log.Printf("[MCP] Session created: %s by user: %s", session.ID(), userID)

	resultText := fmt.Sprintf("Session created successfully.\nSession ID: %s\nStatus: %s\nScope: %s",
		session.ID(), session.Status(), session.Scope())
	if teamID != "" {
		resultText += fmt.Sprintf("\nTeam ID: %s", teamID)
	}

	return resultText, nil
}

// HandleListSessions handles the list_sessions tool
func (c *MCPController) HandleListSessions(ctx context.Context, echoCtx echo.Context, params ListSessionsParams) (string, error) {
	status := params.Status
	scope := params.Scope
	teamID := params.TeamID
	tags := params.Tags

	// Get user from context
	user := auth.GetUserFromContext(echoCtx)
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
		Tags:    tags,
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
	cfg := auth.GetConfigFromContext(echoCtx)
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

	return result, nil
}

// HandleGetSession handles the get_session tool
func (c *MCPController) HandleGetSession(ctx context.Context, echoCtx echo.Context, params SessionIDParams) (string, error) {
	sessionID := params.SessionID
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	// Check authorization
	user := auth.GetUserFromContext(echoCtx)
	if !c.userCanAccessSession(echoCtx, session) {
		log.Printf("[MCP] Access denied to session %s for user: %s", sessionID, c.getUserID(user))
		return "", fmt.Errorf("permission denied: you don't have access to this session")
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

	return result, nil
}

// HandleDeleteSession handles the delete_session tool
func (c *MCPController) HandleDeleteSession(ctx context.Context, echoCtx echo.Context, params SessionIDParams) (string, error) {
	sessionID := params.SessionID
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	// Check authorization
	user := auth.GetUserFromContext(echoCtx)
	if !c.userCanAccessSession(echoCtx, session) {
		log.Printf("[MCP] Delete denied for session %s by user: %s", sessionID, c.getUserID(user))
		return "", fmt.Errorf("permission denied: you don't have access to this session")
	}

	// Delete session
	if err := c.sessionCreator.DeleteSessionByID(sessionID); err != nil {
		log.Printf("[MCP] Failed to delete session %s: %v", sessionID, err)
		return "", fmt.Errorf("failed to delete session: %v", err)
	}

	log.Printf("[MCP] Deleted session %s by user: %s", sessionID, c.getUserID(user))

	return fmt.Sprintf("Session %s deleted successfully", sessionID), nil
}

// HandleSendMessage handles the send_message tool
func (c *MCPController) HandleSendMessage(ctx context.Context, echoCtx echo.Context, params SendMessageParams) (string, error) {
	sessionID := params.SessionID
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	message := params.Message
	if message == "" {
		return "", fmt.Errorf("message is required")
	}

	messageType := params.Type
	if messageType == "" {
		messageType = "user"
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	// Check authorization
	user := auth.GetUserFromContext(echoCtx)
	if !c.userCanAccessSession(echoCtx, session) {
		log.Printf("[MCP] Send message denied for session %s by user: %s", sessionID, c.getUserID(user))
		return "", fmt.Errorf("permission denied: you don't have access to this session")
	}

	// Create client for this session
	sessionClient := client.NewClient(fmt.Sprintf("http://%s", session.Addr()))

	msg := &client.Message{
		Content: message,
		Type:    messageType,
	}

	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := sessionClient.SendMessage(sendCtx, sessionID, msg)
	if err != nil {
		log.Printf("[MCP] Failed to send message to session %s: %v", sessionID, err)
		return "", fmt.Errorf("failed to send message: %v", err)
	}

	log.Printf("[MCP] Sent message to session %s by user: %s", sessionID, c.getUserID(user))

	return fmt.Sprintf("Message sent successfully. Message ID: %s", resp.ID), nil
}

// HandleGetMessages handles the get_messages tool
func (c *MCPController) HandleGetMessages(ctx context.Context, echoCtx echo.Context, params SessionIDParams) (string, error) {
	sessionID := params.SessionID
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	// Check authorization
	user := auth.GetUserFromContext(echoCtx)
	if !c.userCanAccessSession(echoCtx, session) {
		log.Printf("[MCP] Get messages denied for session %s by user: %s", sessionID, c.getUserID(user))
		return "", fmt.Errorf("permission denied: you don't have access to this session")
	}

	// Create client for this session
	sessionClient := client.NewClient(fmt.Sprintf("http://%s", session.Addr()))

	getCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := sessionClient.GetMessages(getCtx, sessionID)
	if err != nil {
		log.Printf("[MCP] Failed to get messages from session %s: %v", sessionID, err)
		return "", fmt.Errorf("failed to get messages: %v", err)
	}

	result := fmt.Sprintf("Conversation History (%d messages):\n\n", len(resp.Messages))
	for _, msg := range resp.Messages {
		result += fmt.Sprintf("[%s] %s: %s\n\n",
			msg.Timestamp.Format("15:04:05"), msg.Role, msg.Content)
	}

	log.Printf("[MCP] Retrieved %d messages from session %s for user: %s",
		len(resp.Messages), sessionID, c.getUserID(user))

	return result, nil
}

// HandleGetStatus handles the get_status tool
func (c *MCPController) HandleGetStatus(ctx context.Context, echoCtx echo.Context, params SessionIDParams) (string, error) {
	sessionID := params.SessionID
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	// Get session
	session := c.sessionManagerProvider.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	// Check authorization
	user := auth.GetUserFromContext(echoCtx)
	if !c.userCanAccessSession(echoCtx, session) {
		log.Printf("[MCP] Get status denied for session %s by user: %s", sessionID, c.getUserID(user))
		return "", fmt.Errorf("permission denied: you don't have access to this session")
	}

	result := fmt.Sprintf("Session Status: %s\n", session.Status())
	result += fmt.Sprintf("Session ID: %s\n", session.ID())
	result += fmt.Sprintf("Started At: %s\n", session.StartedAt().Format(time.RFC3339))
	result += fmt.Sprintf("Updated At: %s\n", session.UpdatedAt().Format(time.RFC3339))

	log.Printf("[MCP] Retrieved status for session %s by user: %s", sessionID, c.getUserID(user))

	return result, nil
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
