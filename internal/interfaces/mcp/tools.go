package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpusecases "github.com/takutakahashi/agentapi-proxy/internal/usecases/mcp"
)

// Tool Input/Output types

// ListSessionsInput represents input for list_sessions tool
type ListSessionsInput struct {
	Status string            `json:"status,omitempty" jsonschema:"Filter by session status"`
	Tags   map[string]string `json:"tags,omitempty" jsonschema:"Filter by tags"`
}

// ListSessionsOutput represents output for list_sessions tool
type ListSessionsOutput struct {
	Sessions []SessionOutput `json:"sessions" jsonschema:"List of sessions"`
}

// SessionOutput represents a session in the output
type SessionOutput struct {
	SessionID string            `json:"session_id" jsonschema:"Session ID"`
	UserID    string            `json:"user_id" jsonschema:"User ID"`
	Status    string            `json:"status" jsonschema:"Session status"`
	StartedAt time.Time         `json:"started_at" jsonschema:"When the session was started"`
	Port      int               `json:"port" jsonschema:"Port number"`
	Tags      map[string]string `json:"tags,omitempty" jsonschema:"Session tags"`
}

// CreateSessionInput represents input for create_session tool
type CreateSessionInput struct {
	Environment map[string]string `json:"environment,omitempty" jsonschema:"Environment variables for the session"`
	Tags        map[string]string `json:"tags,omitempty" jsonschema:"Tags for the session"`
	Repository  string            `json:"repository,omitempty" jsonschema:"Repository to clone (e.g., 'owner/repo' or 'https://github.com/owner/repo')"`
}

// CreateSessionOutput represents output for create_session tool
type CreateSessionOutput struct {
	SessionID string `json:"session_id" jsonschema:"Created session ID"`
}

// GetStatusInput represents input for get_session_status tool
type GetStatusInput struct {
	SessionID string `json:"session_id" jsonschema:"Session ID to get status for"`
}

// GetStatusOutput represents output for get_session_status tool
type GetStatusOutput struct {
	Status string `json:"status" jsonschema:"Session status"`
}

// SendMessageInput represents input for send_message tool
type SendMessageInput struct {
	SessionID string `json:"session_id" jsonschema:"Session ID to send message to"`
	Message   string `json:"message" jsonschema:"Message content to send"`
	Type      string `json:"type,omitempty" jsonschema:"Message type (user or raw)"`
}

// SendMessageOutput represents output for send_message tool
type SendMessageOutput struct {
	MessageID string `json:"message_id" jsonschema:"Sent message ID"`
}

// GetMessagesInput represents input for get_messages tool
type GetMessagesInput struct {
	SessionID string `json:"session_id" jsonschema:"Session ID to get messages from"`
}

// GetMessagesOutput represents output for get_messages tool
type GetMessagesOutput struct {
	Messages []MessageOutput `json:"messages" jsonschema:"List of messages"`
}

// MessageOutput represents a message in the output
type MessageOutput struct {
	Role      string    `json:"role" jsonschema:"Message role"`
	Content   string    `json:"content" jsonschema:"Message content"`
	Timestamp time.Time `json:"timestamp" jsonschema:"Message timestamp"`
}

// DeleteSessionInput represents input for delete_session tool
type DeleteSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"Session ID to delete"`
}

// DeleteSessionOutput represents output for delete_session tool
type DeleteSessionOutput struct {
	Message   string `json:"message" jsonschema:"Success message"`
	SessionID string `json:"session_id" jsonschema:"Deleted session ID"`
}

// Tool Handlers

func (s *MCPServer) handleListSessions(ctx context.Context, req *mcp.CallToolRequest, input ListSessionsInput) (*mcp.CallToolResult, ListSessionsOutput, error) {
	// Build tags filter with authenticated user
	tags := input.Tags
	if tags == nil {
		tags = make(map[string]string)
	}
	if s.authenticatedUserID != "" {
		tags["user_id"] = s.authenticatedUserID
	}

	sessions, err := s.useCase.ListSessions(ctx, input.Status, tags)
	if err != nil {
		return nil, ListSessionsOutput{}, fmt.Errorf("failed to list sessions: %w", err)
	}

	// Convert to output format
	output := ListSessionsOutput{
		Sessions: make([]SessionOutput, 0, len(sessions)),
	}
	for _, s := range sessions {
		output.Sessions = append(output.Sessions, SessionOutput{
			SessionID: s.SessionID,
			UserID:    s.UserID,
			Status:    s.Status,
			StartedAt: s.StartedAt,
			Port:      s.Port,
			Tags:      s.Tags,
		})
	}

	return nil, output, nil
}

func (s *MCPServer) handleCreateSession(ctx context.Context, req *mcp.CallToolRequest, input CreateSessionInput) (*mcp.CallToolResult, CreateSessionOutput, error) {
	// Use authenticated user_id
	if s.authenticatedUserID == "" {
		return nil, CreateSessionOutput{}, fmt.Errorf("authentication required")
	}

	// Always use github_token from Authorization header
	githubToken := s.authenticatedGithubToken

	// Merge repository into tags if provided
	tags := input.Tags
	if tags == nil {
		tags = make(map[string]string)
	}
	if input.Repository != "" {
		tags["repository"] = input.Repository
	}

	createReq := &mcpusecases.CreateSessionInput{
		UserID:      s.authenticatedUserID,
		Environment: input.Environment,
		Tags:        tags,
		GithubToken: githubToken,
		Teams:       s.authenticatedTeams,
	}

	sessionID, err := s.useCase.CreateSession(ctx, createReq)
	if err != nil {
		return nil, CreateSessionOutput{}, fmt.Errorf("failed to create session: %w", err)
	}

	return nil, CreateSessionOutput{SessionID: sessionID}, nil
}

func (s *MCPServer) handleGetStatus(ctx context.Context, req *mcp.CallToolRequest, input GetStatusInput) (*mcp.CallToolResult, GetStatusOutput, error) {
	if input.SessionID == "" {
		return nil, GetStatusOutput{}, fmt.Errorf("session_id is required")
	}

	status, err := s.useCase.GetSessionStatus(ctx, input.SessionID, s.authenticatedUserID)
	if err != nil {
		return nil, GetStatusOutput{}, fmt.Errorf("failed to get session status: %w", err)
	}

	return nil, GetStatusOutput{Status: status}, nil
}

func (s *MCPServer) handleSendMessage(ctx context.Context, req *mcp.CallToolRequest, input SendMessageInput) (*mcp.CallToolResult, SendMessageOutput, error) {
	if input.SessionID == "" {
		return nil, SendMessageOutput{}, fmt.Errorf("session_id is required")
	}
	if input.Message == "" {
		return nil, SendMessageOutput{}, fmt.Errorf("message is required")
	}

	msgType := input.Type
	if msgType == "" {
		msgType = "user"
	}

	messageID, err := s.useCase.SendMessage(ctx, input.SessionID, input.Message, msgType, s.authenticatedUserID)
	if err != nil {
		return nil, SendMessageOutput{}, fmt.Errorf("failed to send message: %w", err)
	}

	return nil, SendMessageOutput{MessageID: messageID}, nil
}

func (s *MCPServer) handleGetMessages(ctx context.Context, req *mcp.CallToolRequest, input GetMessagesInput) (*mcp.CallToolResult, GetMessagesOutput, error) {
	if input.SessionID == "" {
		return nil, GetMessagesOutput{}, fmt.Errorf("session_id is required")
	}

	messages, err := s.useCase.GetMessages(ctx, input.SessionID, s.authenticatedUserID)
	if err != nil {
		return nil, GetMessagesOutput{}, fmt.Errorf("failed to get messages: %w", err)
	}

	// Convert to output format
	output := GetMessagesOutput{
		Messages: make([]MessageOutput, 0, len(messages)),
	}
	for _, m := range messages {
		output.Messages = append(output.Messages, MessageOutput{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		})
	}

	return nil, output, nil
}

func (s *MCPServer) handleDeleteSession(ctx context.Context, req *mcp.CallToolRequest, input DeleteSessionInput) (*mcp.CallToolResult, DeleteSessionOutput, error) {
	if input.SessionID == "" {
		return nil, DeleteSessionOutput{}, fmt.Errorf("session_id is required")
	}

	err := s.useCase.DeleteSession(ctx, input.SessionID, s.authenticatedUserID)
	if err != nil {
		return nil, DeleteSessionOutput{}, fmt.Errorf("failed to delete session: %w", err)
	}

	return nil, DeleteSessionOutput{
		Message:   "Session deleted successfully",
		SessionID: input.SessionID,
	}, nil
}
