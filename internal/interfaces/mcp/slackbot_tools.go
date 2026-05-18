package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpusecases "github.com/takutakahashi/agentapi-proxy/internal/usecases/mcp"
)

// --- SlackBot Tool Input/Output types ---

type ListSlackBotsToolInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Status string `json:"status,omitempty"`
}

type SlackBotSessionParamsOutput struct {
	Message      string `json:"message,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	Oneshot      bool   `json:"oneshot,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
}

type SlackBotSessionConfigOutput struct {
	Environment            map[string]string            `json:"environment,omitempty"`
	Tags                   map[string]string            `json:"tags,omitempty"`
	InitialMessageTemplate string                       `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                       `json:"reuse_message_template,omitempty"`
	Params                 *SlackBotSessionParamsOutput `json:"params,omitempty"`
	MemoryKey              map[string]string            `json:"memory_key,omitempty"`
	ReuseSession           bool                         `json:"reuse_session,omitempty"`
	MountPayload           bool                         `json:"mount_payload,omitempty"`
}

type SlackBotOutput struct {
	ID                     string                       `json:"id"`
	Name                   string                       `json:"name"`
	UserID                 string                       `json:"user_id"`
	Scope                  string                       `json:"scope,omitempty"`
	TeamID                 string                       `json:"team_id,omitempty"`
	Teams                  []string                     `json:"teams,omitempty"`
	Status                 string                       `json:"status"`
	BotTokenSecretName     string                       `json:"bot_token_secret_name,omitempty"`
	AllowedEventTypes      []string                     `json:"allowed_event_types,omitempty"`
	AllowedChannelNames    []string                     `json:"allowed_channel_names,omitempty"`
	AllowedUserIDs         []string                     `json:"allowed_user_ids,omitempty"`
	SessionConfig          *SlackBotSessionConfigOutput `json:"session_config,omitempty"`
	MaxSessions            int                          `json:"max_sessions"`
	NotifyOnSessionCreated bool                         `json:"notify_on_session_created"`
	AllowBotMessages       bool                         `json:"allow_bot_messages"`
	CreatedAt              time.Time                    `json:"created_at"`
	UpdatedAt              time.Time                    `json:"updated_at"`
}

type ListSlackBotsToolOutput struct {
	SlackBots []SlackBotOutput `json:"slackbots"`
	Total     int              `json:"total"`
}

type GetSlackBotToolInput struct {
	SlackBotID string `json:"slackbot_id"`
}

type GetSlackBotToolOutput struct {
	SlackBot SlackBotOutput `json:"slackbot"`
}

type CreateSlackBotToolInput struct {
	Name                   string                       `json:"name"`
	Scope                  string                       `json:"scope,omitempty"`
	TeamID                 string                       `json:"team_id,omitempty"`
	BotTokenSecretName     string                       `json:"bot_token_secret_name,omitempty"`
	AllowedEventTypes      []string                     `json:"allowed_event_types,omitempty"`
	AllowedChannelNames    []string                     `json:"allowed_channel_names,omitempty"`
	AllowedUserIDs         []string                     `json:"allowed_user_ids,omitempty"`
	SessionConfig          *SlackBotSessionConfigOutput `json:"session_config,omitempty"`
	MaxSessions            int                          `json:"max_sessions,omitempty"`
	NotifyOnSessionCreated *bool                        `json:"notify_on_session_created,omitempty"`
	AllowBotMessages       *bool                        `json:"allow_bot_messages,omitempty"`
}

type CreateSlackBotToolOutput struct {
	SlackBot SlackBotOutput `json:"slackbot"`
}

type UpdateSlackBotToolInput struct {
	SlackBotID             string                       `json:"slackbot_id"`
	Name                   *string                      `json:"name,omitempty"`
	Status                 *string                      `json:"status,omitempty"`
	BotTokenSecretName     *string                      `json:"bot_token_secret_name,omitempty"`
	AllowedEventTypes      []string                     `json:"allowed_event_types,omitempty"`
	AllowedChannelNames    []string                     `json:"allowed_channel_names,omitempty"`
	AllowedUserIDs         []string                     `json:"allowed_user_ids,omitempty"`
	SessionConfig          *SlackBotSessionConfigOutput `json:"session_config,omitempty"`
	MaxSessions            *int                         `json:"max_sessions,omitempty"`
	NotifyOnSessionCreated *bool                        `json:"notify_on_session_created,omitempty"`
	AllowBotMessages       *bool                        `json:"allow_bot_messages,omitempty"`
}

type UpdateSlackBotToolOutput struct {
	SlackBot SlackBotOutput `json:"slackbot"`
}

type DeleteSlackBotToolInput struct {
	SlackBotID string `json:"slackbot_id"`
}

type DeleteSlackBotToolOutput struct {
	Message    string `json:"message"`
	SlackBotID string `json:"slackbot_id"`
}

// --- Conversion helpers ---

func toSlackBotOutput(info mcpusecases.SlackBotInfo) SlackBotOutput {
	out := SlackBotOutput{
		ID:                     info.ID,
		Name:                   info.Name,
		UserID:                 info.UserID,
		Scope:                  info.Scope,
		TeamID:                 info.TeamID,
		Teams:                  info.Teams,
		Status:                 info.Status,
		BotTokenSecretName:     info.BotTokenSecretName,
		AllowedEventTypes:      info.AllowedEventTypes,
		AllowedChannelNames:    info.AllowedChannelNames,
		AllowedUserIDs:         info.AllowedUserIDs,
		MaxSessions:            info.MaxSessions,
		NotifyOnSessionCreated: info.NotifyOnSessionCreated,
		AllowBotMessages:       info.AllowBotMessages,
		CreatedAt:              info.CreatedAt,
		UpdatedAt:              info.UpdatedAt,
	}
	if info.SessionConfig != nil {
		cfg := SlackBotSessionConfigOutput{
			Environment:            info.SessionConfig.Environment,
			Tags:                   info.SessionConfig.Tags,
			InitialMessageTemplate: info.SessionConfig.InitialMessageTemplate,
			ReuseMessageTemplate:   info.SessionConfig.ReuseMessageTemplate,
			MemoryKey:              info.SessionConfig.MemoryKey,
			ReuseSession:           info.SessionConfig.ReuseSession,
			MountPayload:           info.SessionConfig.MountPayload,
		}
		if info.SessionConfig.Params != nil {
			cfg.Params = &SlackBotSessionParamsOutput{
				Message:      info.SessionConfig.Params.Message,
				AgentType:    info.SessionConfig.Params.AgentType,
				Oneshot:      info.SessionConfig.Params.Oneshot,
				RepoFullName: info.SessionConfig.Params.RepoFullName,
			}
		}
		out.SessionConfig = &cfg
	}
	return out
}

func fromSlackBotSessionConfigOutput(out *SlackBotSessionConfigOutput) *mcpusecases.SlackBotSessionConfigInfo {
	if out == nil {
		return nil
	}
	info := &mcpusecases.SlackBotSessionConfigInfo{
		Environment:            out.Environment,
		Tags:                   out.Tags,
		InitialMessageTemplate: out.InitialMessageTemplate,
		ReuseMessageTemplate:   out.ReuseMessageTemplate,
		MemoryKey:              out.MemoryKey,
		ReuseSession:           out.ReuseSession,
		MountPayload:           out.MountPayload,
	}
	if out.Params != nil {
		info.Params = &mcpusecases.SlackBotSessionParams{
			Message:      out.Params.Message,
			AgentType:    out.Params.AgentType,
			Oneshot:      out.Params.Oneshot,
			RepoFullName: out.Params.RepoFullName,
		}
	}
	return info
}

// --- SlackBot Tool Handlers ---

func (s *MCPServer) handleListSlackBots(ctx context.Context, req *mcp.CallToolRequest, input ListSlackBotsToolInput) (*mcp.CallToolResult, ListSlackBotsToolOutput, error) {
	if s.slackBotUseCase == nil {
		return nil, ListSlackBotsToolOutput{}, fmt.Errorf("slackbot tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, ListSlackBotsToolOutput{}, fmt.Errorf("authentication required")
	}

	bots, err := s.slackBotUseCase.ListSlackBots(ctx, mcpusecases.ListSlackBotsInput{
		Scope:  input.Scope,
		TeamID: input.TeamID,
		Status: input.Status,
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListSlackBotsToolOutput{}, fmt.Errorf("failed to list slackbots: %w", err)
	}

	out := ListSlackBotsToolOutput{
		SlackBots: make([]SlackBotOutput, 0, len(bots)),
		Total:     len(bots),
	}
	for _, b := range bots {
		out.SlackBots = append(out.SlackBots, toSlackBotOutput(b))
	}
	return nil, out, nil
}

func (s *MCPServer) handleGetSlackBot(ctx context.Context, req *mcp.CallToolRequest, input GetSlackBotToolInput) (*mcp.CallToolResult, GetSlackBotToolOutput, error) {
	if s.slackBotUseCase == nil {
		return nil, GetSlackBotToolOutput{}, fmt.Errorf("slackbot tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, GetSlackBotToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.SlackBotID == "" {
		return nil, GetSlackBotToolOutput{}, fmt.Errorf("slackbot_id is required")
	}

	info, err := s.slackBotUseCase.GetSlackBot(ctx, input.SlackBotID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, GetSlackBotToolOutput{}, fmt.Errorf("failed to get slackbot: %w", err)
	}

	return nil, GetSlackBotToolOutput{SlackBot: toSlackBotOutput(*info)}, nil
}

func (s *MCPServer) handleCreateSlackBot(ctx context.Context, req *mcp.CallToolRequest, input CreateSlackBotToolInput) (*mcp.CallToolResult, CreateSlackBotToolOutput, error) {
	if s.slackBotUseCase == nil {
		return nil, CreateSlackBotToolOutput{}, fmt.Errorf("slackbot tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, CreateSlackBotToolOutput{}, fmt.Errorf("authentication required")
	}

	info, err := s.slackBotUseCase.CreateSlackBot(ctx, mcpusecases.CreateSlackBotInput{
		Name:                   input.Name,
		Scope:                  input.Scope,
		TeamID:                 input.TeamID,
		BotTokenSecretName:     input.BotTokenSecretName,
		AllowedEventTypes:      input.AllowedEventTypes,
		AllowedChannelNames:    input.AllowedChannelNames,
		AllowedUserIDs:         input.AllowedUserIDs,
		SessionConfig:          fromSlackBotSessionConfigOutput(input.SessionConfig),
		MaxSessions:            input.MaxSessions,
		NotifyOnSessionCreated: input.NotifyOnSessionCreated,
		AllowBotMessages:       input.AllowBotMessages,
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, CreateSlackBotToolOutput{}, fmt.Errorf("failed to create slackbot: %w", err)
	}

	return nil, CreateSlackBotToolOutput{SlackBot: toSlackBotOutput(*info)}, nil
}

func (s *MCPServer) handleUpdateSlackBot(ctx context.Context, req *mcp.CallToolRequest, input UpdateSlackBotToolInput) (*mcp.CallToolResult, UpdateSlackBotToolOutput, error) {
	if s.slackBotUseCase == nil {
		return nil, UpdateSlackBotToolOutput{}, fmt.Errorf("slackbot tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, UpdateSlackBotToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.SlackBotID == "" {
		return nil, UpdateSlackBotToolOutput{}, fmt.Errorf("slackbot_id is required")
	}

	info, err := s.slackBotUseCase.UpdateSlackBot(ctx, input.SlackBotID, mcpusecases.UpdateSlackBotInput{
		Name:                   input.Name,
		Status:                 input.Status,
		BotTokenSecretName:     input.BotTokenSecretName,
		AllowedEventTypes:      input.AllowedEventTypes,
		AllowedChannelNames:    input.AllowedChannelNames,
		AllowedUserIDs:         input.AllowedUserIDs,
		SessionConfig:          fromSlackBotSessionConfigOutput(input.SessionConfig),
		MaxSessions:            input.MaxSessions,
		NotifyOnSessionCreated: input.NotifyOnSessionCreated,
		AllowBotMessages:       input.AllowBotMessages,
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, UpdateSlackBotToolOutput{}, fmt.Errorf("failed to update slackbot: %w", err)
	}

	return nil, UpdateSlackBotToolOutput{SlackBot: toSlackBotOutput(*info)}, nil
}

func (s *MCPServer) handleDeleteSlackBot(ctx context.Context, req *mcp.CallToolRequest, input DeleteSlackBotToolInput) (*mcp.CallToolResult, DeleteSlackBotToolOutput, error) {
	if s.slackBotUseCase == nil {
		return nil, DeleteSlackBotToolOutput{}, fmt.Errorf("slackbot tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, DeleteSlackBotToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.SlackBotID == "" {
		return nil, DeleteSlackBotToolOutput{}, fmt.Errorf("slackbot_id is required")
	}

	if err := s.slackBotUseCase.DeleteSlackBot(ctx, input.SlackBotID, s.authenticatedUserID, s.authenticatedTeams); err != nil {
		return nil, DeleteSlackBotToolOutput{}, fmt.Errorf("failed to delete slackbot: %w", err)
	}

	return nil, DeleteSlackBotToolOutput{
		Message:    "SlackBot deleted successfully",
		SlackBotID: input.SlackBotID,
	}, nil
}
