package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpusecases "github.com/takutakahashi/agentapi-proxy/internal/usecases/mcp"
)

// --- Memory Tool Input/Output types ---

// ListMemoriesToolInput represents input for list_memories tool
type ListMemoriesToolInput struct {
	Scope  string            `json:"scope,omitempty"`
	TeamID string            `json:"team_id,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
	Query  string            `json:"query,omitempty"`
}

// MemoryOutput represents a memory entry in the response
type MemoryOutput struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Tags      map[string]string `json:"tags,omitempty"`
	Scope     string            `json:"scope"`
	OwnerID   string            `json:"owner_id"`
	TeamID    string            `json:"team_id,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// ListMemoriesToolOutput represents output for list_memories tool
type ListMemoriesToolOutput struct {
	Memories []MemoryOutput `json:"memories"`
	Total    int            `json:"total"`
}

// GetMemoryToolInput represents input for get_memory tool
type GetMemoryToolInput struct {
	MemoryID string `json:"memory_id"`
}

// GetMemoryToolOutput represents output for get_memory tool
type GetMemoryToolOutput struct {
	Memory MemoryOutput `json:"memory"`
}

// CreateMemoryToolInput represents input for create_memory tool
type CreateMemoryToolInput struct {
	Title   string            `json:"title"`
	Content string            `json:"content,omitempty"`
	Scope   string            `json:"scope"`
	TeamID  string            `json:"team_id,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
}

// CreateMemoryToolOutput represents output for create_memory tool
type CreateMemoryToolOutput struct {
	Memory MemoryOutput `json:"memory"`
}

// UpdateMemoryToolInput represents input for update_memory tool
type UpdateMemoryToolInput struct {
	MemoryID string             `json:"memory_id"`
	Title    *string            `json:"title,omitempty"`
	Content  *string            `json:"content,omitempty"`
	Tags     *map[string]string `json:"tags,omitempty"`
}

// UpdateMemoryToolOutput represents output for update_memory tool
type UpdateMemoryToolOutput struct {
	Memory MemoryOutput `json:"memory"`
}

// DeleteMemoryToolInput represents input for delete_memory tool
type DeleteMemoryToolInput struct {
	MemoryID string `json:"memory_id"`
}

// DeleteMemoryToolOutput represents output for delete_memory tool
type DeleteMemoryToolOutput struct {
	Message  string `json:"message"`
	MemoryID string `json:"memory_id"`
}

// --- Helper conversion ---

func toMemoryOutput(info mcpusecases.MemoryInfo) MemoryOutput {
	return MemoryOutput{
		ID:        info.ID,
		Title:     info.Title,
		Content:   info.Content,
		Tags:      info.Tags,
		Scope:     info.Scope,
		OwnerID:   info.OwnerID,
		TeamID:    info.TeamID,
		CreatedAt: info.CreatedAt,
		UpdatedAt: info.UpdatedAt,
	}
}

// --- Memory Tool Handlers ---

func (s *MCPServer) handleListMemories(ctx context.Context, req *mcp.CallToolRequest, input ListMemoriesToolInput) (*mcp.CallToolResult, ListMemoriesToolOutput, error) {
	if s.memoryUseCase == nil {
		return nil, ListMemoriesToolOutput{}, fmt.Errorf("memory tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, ListMemoriesToolOutput{}, fmt.Errorf("authentication required")
	}

	listInput := mcpusecases.ListMemoriesInput{
		Scope:  input.Scope,
		TeamID: input.TeamID,
		Tags:   input.Tags,
		Query:  input.Query,
	}

	memories, err := s.memoryUseCase.ListMemories(ctx, listInput, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListMemoriesToolOutput{}, fmt.Errorf("failed to list memories: %w", err)
	}

	output := ListMemoriesToolOutput{
		Memories: make([]MemoryOutput, 0, len(memories)),
		Total:    len(memories),
	}
	for _, m := range memories {
		output.Memories = append(output.Memories, toMemoryOutput(m))
	}

	return nil, output, nil
}

func (s *MCPServer) handleGetMemory(ctx context.Context, req *mcp.CallToolRequest, input GetMemoryToolInput) (*mcp.CallToolResult, GetMemoryToolOutput, error) {
	if s.memoryUseCase == nil {
		return nil, GetMemoryToolOutput{}, fmt.Errorf("memory tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, GetMemoryToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.MemoryID == "" {
		return nil, GetMemoryToolOutput{}, fmt.Errorf("memory_id is required")
	}

	memoryInfo, err := s.memoryUseCase.GetMemory(ctx, input.MemoryID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, GetMemoryToolOutput{}, fmt.Errorf("failed to get memory: %w", err)
	}

	return nil, GetMemoryToolOutput{Memory: toMemoryOutput(*memoryInfo)}, nil
}

func (s *MCPServer) handleCreateMemory(ctx context.Context, req *mcp.CallToolRequest, input CreateMemoryToolInput) (*mcp.CallToolResult, CreateMemoryToolOutput, error) {
	if s.memoryUseCase == nil {
		return nil, CreateMemoryToolOutput{}, fmt.Errorf("memory tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, CreateMemoryToolOutput{}, fmt.Errorf("authentication required")
	}

	createInput := mcpusecases.CreateMemoryInput{
		Title:   input.Title,
		Content: input.Content,
		Scope:   input.Scope,
		TeamID:  input.TeamID,
		Tags:    input.Tags,
	}

	memoryInfo, err := s.memoryUseCase.CreateMemory(ctx, createInput, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, CreateMemoryToolOutput{}, fmt.Errorf("failed to create memory: %w", err)
	}

	return nil, CreateMemoryToolOutput{Memory: toMemoryOutput(*memoryInfo)}, nil
}

func (s *MCPServer) handleUpdateMemory(ctx context.Context, req *mcp.CallToolRequest, input UpdateMemoryToolInput) (*mcp.CallToolResult, UpdateMemoryToolOutput, error) {
	if s.memoryUseCase == nil {
		return nil, UpdateMemoryToolOutput{}, fmt.Errorf("memory tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, UpdateMemoryToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.MemoryID == "" {
		return nil, UpdateMemoryToolOutput{}, fmt.Errorf("memory_id is required")
	}

	updateInput := mcpusecases.UpdateMemoryInput{
		Title:   input.Title,
		Content: input.Content,
		Tags:    input.Tags,
	}

	memoryInfo, err := s.memoryUseCase.UpdateMemory(ctx, input.MemoryID, updateInput, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, UpdateMemoryToolOutput{}, fmt.Errorf("failed to update memory: %w", err)
	}

	return nil, UpdateMemoryToolOutput{Memory: toMemoryOutput(*memoryInfo)}, nil
}

func (s *MCPServer) handleDeleteMemory(ctx context.Context, req *mcp.CallToolRequest, input DeleteMemoryToolInput) (*mcp.CallToolResult, DeleteMemoryToolOutput, error) {
	if s.memoryUseCase == nil {
		return nil, DeleteMemoryToolOutput{}, fmt.Errorf("memory tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, DeleteMemoryToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.MemoryID == "" {
		return nil, DeleteMemoryToolOutput{}, fmt.Errorf("memory_id is required")
	}

	if err := s.memoryUseCase.DeleteMemory(ctx, input.MemoryID, s.authenticatedUserID, s.authenticatedTeams); err != nil {
		return nil, DeleteMemoryToolOutput{}, fmt.Errorf("failed to delete memory: %w", err)
	}

	return nil, DeleteMemoryToolOutput{
		Message:  "Memory deleted successfully",
		MemoryID: input.MemoryID,
	}, nil
}
