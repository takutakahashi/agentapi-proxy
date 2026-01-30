package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

// MCPSessionToolsUseCase provides use cases for MCP session tools
type MCPSessionToolsUseCase struct {
	client *client.Client
}

// NewMCPSessionToolsUseCase creates a new MCPSessionToolsUseCase
func NewMCPSessionToolsUseCase(proxyURL string) *MCPSessionToolsUseCase {
	return &MCPSessionToolsUseCase{
		client: client.NewClient(proxyURL),
	}
}

// SessionInfo represents session information
type SessionInfo struct {
	SessionID string            `json:"session_id"`
	UserID    string            `json:"user_id"`
	Status    string            `json:"status"`
	StartedAt time.Time         `json:"started_at"`
	Port      int               `json:"port"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// CreateSessionInput represents input for creating a session
type CreateSessionInput struct {
	UserID      string            `json:"user_id"`
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// Message represents a message in the conversation
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ListSessions lists sessions matching the given filters
func (uc *MCPSessionToolsUseCase) ListSessions(ctx context.Context, status string, tags map[string]string) ([]SessionInfo, error) {
	resp, err := uc.client.SearchWithTags(ctx, status, tags)
	if err != nil {
		return nil, fmt.Errorf("failed to search sessions: %w", err)
	}

	sessions := make([]SessionInfo, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		sessions = append(sessions, SessionInfo{
			SessionID: s.SessionID,
			UserID:    s.UserID,
			Status:    s.Status,
			StartedAt: s.StartedAt,
			Port:      s.Port,
			Tags:      s.Tags,
		})
	}

	return sessions, nil
}

// CreateSession creates a new session
func (uc *MCPSessionToolsUseCase) CreateSession(ctx context.Context, req *CreateSessionInput) (string, error) {
	// Add user_id to tags if provided
	tags := req.Tags
	if tags == nil {
		tags = make(map[string]string)
	}
	if req.UserID != "" {
		tags["user_id"] = req.UserID
	}

	startReq := &client.StartRequest{
		Environment: req.Environment,
		Tags:        tags,
	}

	resp, err := uc.client.Start(ctx, startReq)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return resp.SessionID, nil
}

// GetSessionStatus gets the status of a session
func (uc *MCPSessionToolsUseCase) GetSessionStatus(ctx context.Context, sessionID string) (string, error) {
	resp, err := uc.client.GetStatus(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get session status: %w", err)
	}

	return resp.Status, nil
}

// SendMessage sends a message to a session
func (uc *MCPSessionToolsUseCase) SendMessage(ctx context.Context, sessionID, message, msgType string) (string, error) {
	if msgType == "" {
		msgType = "user"
	}

	msg := &client.Message{
		Content: message,
		Type:    msgType,
	}

	resp, err := uc.client.SendMessage(ctx, sessionID, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}

	if !resp.OK {
		return "", fmt.Errorf("message was not sent successfully")
	}

	// Generate a message ID since agentapi doesn't return one
	// Use timestamp-based ID for tracking
	messageID := fmt.Sprintf("msg-%d", time.Now().UnixNano())
	return messageID, nil
}

// GetMessages gets messages from a session
func (uc *MCPSessionToolsUseCase) GetMessages(ctx context.Context, sessionID string) ([]Message, error) {
	resp, err := uc.client.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	messages := make([]Message, 0, len(resp.Messages))
	for _, m := range resp.Messages {
		messages = append(messages, Message{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		})
	}

	return messages, nil
}

// DeleteSession deletes a session
func (uc *MCPSessionToolsUseCase) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := uc.client.DeleteSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}
