package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// MCPMemoryToolsUseCase provides use cases for MCP memory tools
type MCPMemoryToolsUseCase struct {
	memoryRepo portrepos.MemoryRepository
}

// NewMCPMemoryToolsUseCase creates a new MCPMemoryToolsUseCase
func NewMCPMemoryToolsUseCase(memoryRepo portrepos.MemoryRepository) *MCPMemoryToolsUseCase {
	return &MCPMemoryToolsUseCase{
		memoryRepo: memoryRepo,
	}
}

// MemoryInfo represents memory information returned by MCP tools
type MemoryInfo struct {
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

// ListMemoriesInput represents input for listing memories
type ListMemoriesInput struct {
	Scope  string            `json:"scope,omitempty"`
	TeamID string            `json:"team_id,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
	Query  string            `json:"query,omitempty"`
}

// CreateMemoryInput represents input for creating a memory
type CreateMemoryInput struct {
	Title   string            `json:"title"`
	Content string            `json:"content,omitempty"`
	Scope   string            `json:"scope"`
	TeamID  string            `json:"team_id,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
}

// UpdateMemoryInput represents input for updating a memory
type UpdateMemoryInput struct {
	Title   *string            `json:"title,omitempty"`
	Content *string            `json:"content,omitempty"`
	Tags    *map[string]string `json:"tags,omitempty"`
}

// toMemoryInfo converts a Memory entity to MemoryInfo
func toMemoryInfo(m *entities.Memory) MemoryInfo {
	return MemoryInfo{
		ID:        m.ID(),
		Title:     m.Title(),
		Content:   m.Content(),
		Tags:      m.Tags(),
		Scope:     string(m.Scope()),
		OwnerID:   m.OwnerID(),
		TeamID:    m.TeamID(),
		CreatedAt: m.CreatedAt(),
		UpdatedAt: m.UpdatedAt(),
	}
}

// canAccessMemory checks whether the requesting user can access the memory
func canAccessMemory(memory *entities.Memory, requestingUserID string, teamIDs []string) bool {
	switch memory.Scope() {
	case entities.ScopeUser:
		return memory.OwnerID() == requestingUserID
	case entities.ScopeTeam:
		return containsTeam(teamIDs, memory.TeamID())
	default:
		return false
	}
}

// ListMemories lists memories with the given filters for the requesting user
func (uc *MCPMemoryToolsUseCase) ListMemories(ctx context.Context, input ListMemoriesInput, requestingUserID string, teamIDs []string) ([]MemoryInfo, error) {
	if uc.memoryRepo == nil {
		return nil, fmt.Errorf("memory repository not available")
	}

	var memories []*entities.Memory

	switch entities.ResourceScope(input.Scope) {
	case entities.ScopeUser:
		filter := portrepos.MemoryFilter{
			Scope:   entities.ScopeUser,
			OwnerID: requestingUserID,
			Tags:    input.Tags,
			Query:   input.Query,
		}
		result, err := uc.memoryRepo.List(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to list memories: %w", err)
		}
		memories = result

	case entities.ScopeTeam:
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
		filter := portrepos.MemoryFilter{
			Scope:  entities.ScopeTeam,
			TeamID: input.TeamID,
			Tags:   input.Tags,
			Query:  input.Query,
		}
		result, err := uc.memoryRepo.List(ctx, filter)
		if err != nil {
			return nil, fmt.Errorf("failed to list memories: %w", err)
		}
		memories = result

	default:
		// No scope: return user-scoped + all team-scoped memories for user's teams
		userFilter := portrepos.MemoryFilter{
			Scope:   entities.ScopeUser,
			OwnerID: requestingUserID,
			Tags:    input.Tags,
			Query:   input.Query,
		}
		userMemories, err := uc.memoryRepo.List(ctx, userFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to list user memories: %w", err)
		}

		var teamMemories []*entities.Memory
		if len(teamIDs) > 0 {
			teamFilter := portrepos.MemoryFilter{
				Scope:   entities.ScopeTeam,
				TeamIDs: teamIDs,
				Tags:    input.Tags,
				Query:   input.Query,
			}
			teamMemories, err = uc.memoryRepo.List(ctx, teamFilter)
			if err != nil {
				return nil, fmt.Errorf("failed to list team memories: %w", err)
			}
		}

		memories = append(userMemories, teamMemories...)
	}

	result := make([]MemoryInfo, 0, len(memories))
	for _, m := range memories {
		result = append(result, toMemoryInfo(m))
	}
	return result, nil
}

// GetMemory retrieves a memory by ID for the requesting user
func (uc *MCPMemoryToolsUseCase) GetMemory(ctx context.Context, memoryID, requestingUserID string, teamIDs []string) (*MemoryInfo, error) {
	if uc.memoryRepo == nil {
		return nil, fmt.Errorf("memory repository not available")
	}

	memory, err := uc.memoryRepo.GetByID(ctx, memoryID)
	if err != nil {
		return nil, fmt.Errorf("memory not found: %w", err)
	}

	if !canAccessMemory(memory, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	info := toMemoryInfo(memory)
	return &info, nil
}

// CreateMemory creates a new memory for the requesting user
func (uc *MCPMemoryToolsUseCase) CreateMemory(ctx context.Context, input CreateMemoryInput, requestingUserID string, teamIDs []string) (*MemoryInfo, error) {
	if uc.memoryRepo == nil {
		return nil, fmt.Errorf("memory repository not available")
	}

	if input.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if input.Scope != string(entities.ScopeUser) && input.Scope != string(entities.ScopeTeam) {
		return nil, fmt.Errorf("scope must be 'user' or 'team'")
	}
	if input.Scope == string(entities.ScopeTeam) {
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
	}

	memory := entities.NewMemoryWithTags(
		uuid.New().String(),
		input.Title,
		input.Content,
		entities.ResourceScope(input.Scope),
		requestingUserID,
		input.TeamID,
		input.Tags,
	)

	if err := uc.memoryRepo.Create(ctx, memory); err != nil {
		return nil, fmt.Errorf("failed to create memory: %w", err)
	}

	info := toMemoryInfo(memory)
	return &info, nil
}

// UpdateMemory updates an existing memory for the requesting user
func (uc *MCPMemoryToolsUseCase) UpdateMemory(ctx context.Context, memoryID string, input UpdateMemoryInput, requestingUserID string, teamIDs []string) (*MemoryInfo, error) {
	if uc.memoryRepo == nil {
		return nil, fmt.Errorf("memory repository not available")
	}

	memory, err := uc.memoryRepo.GetByID(ctx, memoryID)
	if err != nil {
		return nil, fmt.Errorf("memory not found: %w", err)
	}

	if !canAccessMemory(memory, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	if input.Title != nil {
		if *input.Title == "" {
			return nil, fmt.Errorf("title cannot be empty")
		}
		memory.SetTitle(*input.Title)
	}
	if input.Content != nil {
		memory.SetContent(*input.Content)
	}
	if input.Tags != nil {
		memory.SetTags(*input.Tags)
	}

	if err := uc.memoryRepo.Update(ctx, memory); err != nil {
		return nil, fmt.Errorf("failed to update memory: %w", err)
	}

	info := toMemoryInfo(memory)
	return &info, nil
}

// DeleteMemory deletes a memory for the requesting user
func (uc *MCPMemoryToolsUseCase) DeleteMemory(ctx context.Context, memoryID, requestingUserID string, teamIDs []string) error {
	if uc.memoryRepo == nil {
		return fmt.Errorf("memory repository not available")
	}

	memory, err := uc.memoryRepo.GetByID(ctx, memoryID)
	if err != nil {
		return fmt.Errorf("memory not found: %w", err)
	}

	if !canAccessMemory(memory, requestingUserID, teamIDs) {
		return fmt.Errorf("access denied")
	}

	return uc.memoryRepo.Delete(ctx, memoryID)
}
