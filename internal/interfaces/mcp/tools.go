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
	Status string            `json:"status,omitempty" jsonschema:"description=Filter by session status"`
	UserID string            `json:"user_id,omitempty" jsonschema:"description=Filter by user ID"`
	Tags   map[string]string `json:"tags,omitempty" jsonschema:"description=Filter by tags"`
}

// ListSessionsOutput represents output for list_sessions tool
type ListSessionsOutput struct {
	Sessions []SessionOutput `json:"sessions" jsonschema:"description=List of sessions"`
}

// SessionOutput represents a session in the output
type SessionOutput struct {
	SessionID string            `json:"session_id" jsonschema:"description=Session ID"`
	UserID    string            `json:"user_id" jsonschema:"description=User ID"`
	Status    string            `json:"status" jsonschema:"description=Session status"`
	StartedAt time.Time         `json:"started_at" jsonschema:"description=When the session was started"`
	Port      int               `json:"port" jsonschema:"description=Port number"`
	Tags      map[string]string `json:"tags,omitempty" jsonschema:"description=Session tags"`
}

// CreateSessionInput represents input for create_session tool
type CreateSessionInput struct {
	UserID      string            `json:"user_id" jsonschema:"description=User ID for the session,required"`
	Environment map[string]string `json:"environment,omitempty" jsonschema:"description=Environment variables for the session"`
	Tags        map[string]string `json:"tags,omitempty" jsonschema:"description=Tags for the session"`
}

// CreateSessionOutput represents output for create_session tool
type CreateSessionOutput struct {
	SessionID string `json:"session_id" jsonschema:"description=Created session ID"`
}

// GetStatusInput represents input for get_session_status tool
type GetStatusInput struct {
	SessionID string `json:"session_id" jsonschema:"description=Session ID to get status for,required"`
}

// GetStatusOutput represents output for get_session_status tool
type GetStatusOutput struct {
	Status string `json:"status" jsonschema:"description=Session status"`
}

// SendMessageInput represents input for send_message tool
type SendMessageInput struct {
	SessionID string `json:"session_id" jsonschema:"description=Session ID to send message to,required"`
	Message   string `json:"message" jsonschema:"description=Message content to send,required"`
	Type      string `json:"type,omitempty" jsonschema:"description=Message type (user or raw),enum=user|raw"`
}

// SendMessageOutput represents output for send_message tool
type SendMessageOutput struct {
	MessageID string `json:"message_id" jsonschema:"description=Sent message ID"`
}

// GetMessagesInput represents input for get_messages tool
type GetMessagesInput struct {
	SessionID string `json:"session_id" jsonschema:"description=Session ID to get messages from,required"`
}

// GetMessagesOutput represents output for get_messages tool
type GetMessagesOutput struct {
	Messages []MessageOutput `json:"messages" jsonschema:"description=List of messages"`
}

// MessageOutput represents a message in the output
type MessageOutput struct {
	Role      string    `json:"role" jsonschema:"description=Message role"`
	Content   string    `json:"content" jsonschema:"description=Message content"`
	Timestamp time.Time `json:"timestamp" jsonschema:"description=Message timestamp"`
}

// DeleteSessionInput represents input for delete_session tool
type DeleteSessionInput struct {
	SessionID string `json:"session_id" jsonschema:"description=Session ID to delete,required"`
}

// DeleteSessionOutput represents output for delete_session tool
type DeleteSessionOutput struct {
	Message   string `json:"message" jsonschema:"description=Success message"`
	SessionID string `json:"session_id" jsonschema:"description=Deleted session ID"`
}

// Tool Handlers

func (s *MCPServer) handleListSessions(ctx context.Context, req *mcp.CallToolRequest, input ListSessionsInput) (*mcp.CallToolResult, ListSessionsOutput, error) {
	// Build tags filter
	tags := input.Tags
	if tags == nil {
		tags = make(map[string]string)
	}
	if input.UserID != "" {
		tags["user_id"] = input.UserID
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
	if input.UserID == "" {
		return nil, CreateSessionOutput{}, fmt.Errorf("user_id is required")
	}

	createReq := &mcpusecases.CreateSessionInput{
		UserID:      input.UserID,
		Environment: input.Environment,
		Tags:        input.Tags,
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

	status, err := s.useCase.GetSessionStatus(ctx, input.SessionID)
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

	messageID, err := s.useCase.SendMessage(ctx, input.SessionID, input.Message, msgType)
	if err != nil {
		return nil, SendMessageOutput{}, fmt.Errorf("failed to send message: %w", err)
	}

	return nil, SendMessageOutput{MessageID: messageID}, nil
}

func (s *MCPServer) handleGetMessages(ctx context.Context, req *mcp.CallToolRequest, input GetMessagesInput) (*mcp.CallToolResult, GetMessagesOutput, error) {
	if input.SessionID == "" {
		return nil, GetMessagesOutput{}, fmt.Errorf("session_id is required")
	}

	messages, err := s.useCase.GetMessages(ctx, input.SessionID)
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

	err := s.useCase.DeleteSession(ctx, input.SessionID)
	if err != nil {
		return nil, DeleteSessionOutput{}, fmt.Errorf("failed to delete session: %w", err)
	}

	return nil, DeleteSessionOutput{
		Message:   "Session deleted successfully",
		SessionID: input.SessionID,
	}, nil
}
