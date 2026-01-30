package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// MCPSessionToolsUseCase provides use cases for MCP session tools
type MCPSessionToolsUseCase struct {
	sessionManager repositories.SessionManager
	shareRepo      repositories.ShareRepository
}

// NewMCPSessionToolsUseCase creates a new MCPSessionToolsUseCase
func NewMCPSessionToolsUseCase(
	sessionManager repositories.SessionManager,
	shareRepo repositories.ShareRepository,
) *MCPSessionToolsUseCase {
	return &MCPSessionToolsUseCase{
		sessionManager: sessionManager,
		shareRepo:      shareRepo,
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
	GithubToken string            `json:"github_token,omitempty"`
	Teams       []string          `json:"teams,omitempty"` // GitHub team slugs (e.g., ["org/team-a"])
}

// Message represents a message in the conversation
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ListSessions lists sessions matching the given filters
func (uc *MCPSessionToolsUseCase) ListSessions(ctx context.Context, status string, tags map[string]string) ([]SessionInfo, error) {
	// Build filter
	filter := entities.SessionFilter{
		Status: status,
		Tags:   tags,
	}

	sessions := uc.sessionManager.ListSessions(filter)

	result := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		// Use default port (actual port is in service address, not needed for MCP)
		port := 8080

		result = append(result, SessionInfo{
			SessionID: s.ID(),
			UserID:    s.UserID(),
			Status:    s.Status(),
			StartedAt: s.StartedAt(),
			Port:      port,
			Tags:      s.Tags(),
		})
	}

	return result, nil
}

// CreateSession creates a new session
func (uc *MCPSessionToolsUseCase) CreateSession(ctx context.Context, req *CreateSessionInput) (string, error) {
	// Generate session ID
	sessionID := uuid.New().String()

	// Build RunServerRequest
	tags := req.Tags
	if tags == nil {
		tags = make(map[string]string)
	}
	if req.UserID != "" {
		tags["user_id"] = req.UserID
	}

	runReq := &entities.RunServerRequest{
		UserID:      req.UserID,
		Environment: req.Environment,
		Tags:        tags,
		Scope:       entities.ScopeUser,
		Teams:       req.Teams,
		GithubToken: req.GithubToken,
	}

	// Create session using SessionManager
	session, err := uc.sessionManager.CreateSession(ctx, sessionID, runReq)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID(), nil
}

// GetSessionStatus gets the status of a session
func (uc *MCPSessionToolsUseCase) GetSessionStatus(ctx context.Context, sessionID string) (string, error) {
	session := uc.sessionManager.GetSession(sessionID)
	if session == nil {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	return session.Status(), nil
}

// SendMessage sends a message to a session
func (uc *MCPSessionToolsUseCase) SendMessage(ctx context.Context, sessionID, message, msgType string) (string, error) {
	if msgType != "" && msgType != "user" {
		return "", fmt.Errorf("only 'user' message type is supported via SessionManager")
	}

	// Use SessionManager's SendMessage
	err := uc.sessionManager.SendMessage(ctx, sessionID, message)
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}

	// Generate a message ID since SessionManager doesn't return one
	messageID := fmt.Sprintf("msg-%d", time.Now().UnixNano())
	return messageID, nil
}

// GetMessages gets messages from a session
func (uc *MCPSessionToolsUseCase) GetMessages(ctx context.Context, sessionID string) ([]Message, error) {
	messages, err := uc.sessionManager.GetMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	result := make([]Message, 0, len(messages))
	for _, m := range messages {
		result = append(result, Message{
			Role:      m.Role,
			Content:   m.Content,
			Timestamp: m.Timestamp,
		})
	}

	return result, nil
}

// DeleteSession deletes a session
func (uc *MCPSessionToolsUseCase) DeleteSession(ctx context.Context, sessionID string) error {
	// Delete associated share link if exists (ignore errors as share may not exist)
	if uc.shareRepo != nil {
		_ = uc.shareRepo.Delete(sessionID)
	}

	return uc.sessionManager.DeleteSession(sessionID)
}
