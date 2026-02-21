package mcp

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// MCPSessionToolsUseCase provides use cases for MCP session tools
type MCPSessionToolsUseCase struct {
	sessionManager repositories.SessionManager
	shareRepo      repositories.ShareRepository
	taskRepo       repositories.TaskRepository
}

// NewMCPSessionToolsUseCase creates a new MCPSessionToolsUseCase
func NewMCPSessionToolsUseCase(
	sessionManager repositories.SessionManager,
	shareRepo repositories.ShareRepository,
	taskRepo repositories.TaskRepository,
) *MCPSessionToolsUseCase {
	return &MCPSessionToolsUseCase{
		sessionManager: sessionManager,
		shareRepo:      shareRepo,
		taskRepo:       taskRepo,
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

const (
	// MaxMessageLength is the maximum allowed length for a message
	MaxMessageLength = 100000 // 100KB
)

// validateSessionID validates that the session ID is a valid UUID
func validateSessionID(sessionID string) error {
	if _, err := uuid.Parse(sessionID); err != nil {
		return fmt.Errorf("invalid session ID format")
	}
	return nil
}

// checkSessionOwnership verifies that the requesting user owns the session
func (uc *MCPSessionToolsUseCase) checkSessionOwnership(sessionID, requestingUserID string) error {
	session := uc.sessionManager.GetSession(sessionID)
	if session == nil {
		return fmt.Errorf("session not found")
	}

	if session.UserID() != requestingUserID {
		return fmt.Errorf("access denied")
	}

	return nil
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
	session, err := uc.sessionManager.CreateSession(ctx, sessionID, runReq, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID(), nil
}

// GetSessionStatus gets the status of a session
func (uc *MCPSessionToolsUseCase) GetSessionStatus(ctx context.Context, sessionID, requestingUserID string) (string, error) {
	// Validate session ID format
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}

	// Check ownership
	if err := uc.checkSessionOwnership(sessionID, requestingUserID); err != nil {
		return "", err
	}

	session := uc.sessionManager.GetSession(sessionID)
	if session == nil {
		return "", fmt.Errorf("session not found")
	}

	return session.Status(), nil
}

// SendMessage sends a message to a session
func (uc *MCPSessionToolsUseCase) SendMessage(ctx context.Context, sessionID, message, msgType, requestingUserID string) (string, error) {
	// Validate session ID format
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}

	// Validate message length
	if len(message) > MaxMessageLength {
		return "", fmt.Errorf("message too long")
	}

	if msgType != "" && msgType != "user" {
		return "", fmt.Errorf("only 'user' message type is supported via SessionManager")
	}

	// Check ownership
	if err := uc.checkSessionOwnership(sessionID, requestingUserID); err != nil {
		return "", err
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
func (uc *MCPSessionToolsUseCase) GetMessages(ctx context.Context, sessionID, requestingUserID string) ([]Message, error) {
	// Validate session ID format
	if err := validateSessionID(sessionID); err != nil {
		return nil, err
	}

	// Check ownership
	if err := uc.checkSessionOwnership(sessionID, requestingUserID); err != nil {
		return nil, err
	}

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
func (uc *MCPSessionToolsUseCase) DeleteSession(ctx context.Context, sessionID, requestingUserID string) error {
	// Validate session ID format
	if err := validateSessionID(sessionID); err != nil {
		return err
	}

	// Check ownership
	if err := uc.checkSessionOwnership(sessionID, requestingUserID); err != nil {
		return err
	}

	// Delete associated share link if exists (ignore errors as share may not exist)
	if uc.shareRepo != nil {
		_ = uc.shareRepo.Delete(sessionID)
	}

	// Delete associated tasks for this session (cascade delete)
	if uc.taskRepo != nil {
		tasks, err := uc.taskRepo.List(ctx, repositories.TaskFilter{SessionID: sessionID})
		if err != nil {
			log.Printf("[MCP] Warning: failed to list tasks for session %s: %v", sessionID, err)
		} else {
			for _, task := range tasks {
				if err := uc.taskRepo.Delete(ctx, task.ID()); err != nil {
					log.Printf("[MCP] Warning: failed to delete task %s for session %s: %v", task.ID(), sessionID, err)
				}
			}
			if len(tasks) > 0 {
				log.Printf("[MCP] Deleted %d tasks associated with session %s", len(tasks), sessionID)
			}
		}
	}

	return uc.sessionManager.DeleteSession(sessionID)
}
