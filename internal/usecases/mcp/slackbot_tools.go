package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// MCPSlackBotToolsUseCase provides use cases for MCP SlackBot tools
type MCPSlackBotToolsUseCase struct {
	slackBotRepo portrepos.SlackBotRepository
}

// NewMCPSlackBotToolsUseCase creates a new MCPSlackBotToolsUseCase
func NewMCPSlackBotToolsUseCase(repo portrepos.SlackBotRepository) *MCPSlackBotToolsUseCase {
	return &MCPSlackBotToolsUseCase{slackBotRepo: repo}
}

// SlackBotSessionConfigInfo represents session configuration for a SlackBot
type SlackBotSessionConfigInfo struct {
	Environment            map[string]string      `json:"environment,omitempty"`
	Tags                   map[string]string      `json:"tags,omitempty"`
	InitialMessageTemplate string                 `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                 `json:"reuse_message_template,omitempty"`
	Params                 *SlackBotSessionParams `json:"params,omitempty"`
	MemoryKey              map[string]string      `json:"memory_key,omitempty"`
	ReuseSession           bool                   `json:"reuse_session,omitempty"`
	MountPayload           bool                   `json:"mount_payload,omitempty"`
}

// SlackBotSessionParams represents session params for a SlackBot
type SlackBotSessionParams struct {
	Message      string `json:"message,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	Oneshot      bool   `json:"oneshot,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
}

// SlackBotInfo represents SlackBot information returned by MCP tools
type SlackBotInfo struct {
	ID                     string                     `json:"id"`
	Name                   string                     `json:"name"`
	UserID                 string                     `json:"user_id"`
	Scope                  string                     `json:"scope,omitempty"`
	TeamID                 string                     `json:"team_id,omitempty"`
	Teams                  []string                   `json:"teams,omitempty"`
	Status                 string                     `json:"status"`
	BotTokenSecretName     string                     `json:"bot_token_secret_name,omitempty"`
	AllowedEventTypes      []string                   `json:"allowed_event_types,omitempty"`
	AllowedChannelNames    []string                   `json:"allowed_channel_names,omitempty"`
	AllowedUserIDs         []string                   `json:"allowed_user_ids,omitempty"`
	SessionConfig          *SlackBotSessionConfigInfo `json:"session_config,omitempty"`
	MaxSessions            int                        `json:"max_sessions"`
	NotifyOnSessionCreated bool                       `json:"notify_on_session_created"`
	AllowBotMessages       bool                       `json:"allow_bot_messages"`
	CreatedAt              time.Time                  `json:"created_at"`
	UpdatedAt              time.Time                  `json:"updated_at"`
}

// ListSlackBotsInput represents input for listing SlackBots
type ListSlackBotsInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Status string `json:"status,omitempty"`
}

// CreateSlackBotInput represents input for creating a SlackBot
type CreateSlackBotInput struct {
	Name                   string                     `json:"name"`
	Scope                  string                     `json:"scope,omitempty"`
	TeamID                 string                     `json:"team_id,omitempty"`
	BotTokenSecretName     string                     `json:"bot_token_secret_name,omitempty"`
	AllowedEventTypes      []string                   `json:"allowed_event_types,omitempty"`
	AllowedChannelNames    []string                   `json:"allowed_channel_names,omitempty"`
	AllowedUserIDs         []string                   `json:"allowed_user_ids,omitempty"`
	SessionConfig          *SlackBotSessionConfigInfo `json:"session_config,omitempty"`
	MaxSessions            int                        `json:"max_sessions,omitempty"`
	NotifyOnSessionCreated *bool                      `json:"notify_on_session_created,omitempty"`
	AllowBotMessages       *bool                      `json:"allow_bot_messages,omitempty"`
}

// UpdateSlackBotInput represents input for updating a SlackBot
type UpdateSlackBotInput struct {
	Name                   *string                    `json:"name,omitempty"`
	Status                 *string                    `json:"status,omitempty"`
	BotTokenSecretName     *string                    `json:"bot_token_secret_name,omitempty"`
	AllowedEventTypes      []string                   `json:"allowed_event_types,omitempty"`
	AllowedChannelNames    []string                   `json:"allowed_channel_names,omitempty"`
	AllowedUserIDs         []string                   `json:"allowed_user_ids,omitempty"`
	SessionConfig          *SlackBotSessionConfigInfo `json:"session_config,omitempty"`
	MaxSessions            *int                       `json:"max_sessions,omitempty"`
	NotifyOnSessionCreated *bool                      `json:"notify_on_session_created,omitempty"`
	AllowBotMessages       *bool                      `json:"allow_bot_messages,omitempty"`
}

func toSlackBotInfo(s *entities.SlackBot) SlackBotInfo {
	info := SlackBotInfo{
		ID:                     s.ID(),
		Name:                   s.Name(),
		UserID:                 s.UserID(),
		Scope:                  string(s.Scope()),
		TeamID:                 s.TeamID(),
		Teams:                  s.Teams(),
		Status:                 string(s.Status()),
		BotTokenSecretName:     s.BotTokenSecretName(),
		AllowedEventTypes:      s.AllowedEventTypes(),
		AllowedChannelNames:    s.AllowedChannelNames(),
		AllowedUserIDs:         s.AllowedUserIDs(),
		MaxSessions:            s.MaxSessions(),
		NotifyOnSessionCreated: s.NotifyOnSessionCreated(),
		AllowBotMessages:       s.AllowBotMessages(),
		CreatedAt:              s.CreatedAt(),
		UpdatedAt:              s.UpdatedAt(),
	}

	if sc := s.SessionConfig(); sc != nil {
		cfg := SlackBotSessionConfigInfo{
			Environment:            sc.Environment(),
			Tags:                   sc.Tags(),
			InitialMessageTemplate: sc.InitialMessageTemplate(),
			ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
			MemoryKey:              sc.MemoryKey(),
			ReuseSession:           sc.ReuseSession(),
			MountPayload:           sc.MountPayload(),
		}
		if p := sc.Params(); p != nil {
			cfg.Params = &SlackBotSessionParams{
				Message:      p.Message,
				AgentType:    p.AgentType,
				Oneshot:      p.Oneshot,
				RepoFullName: p.RepoFullName,
			}
		}
		info.SessionConfig = &cfg
	}

	return info
}

func canAccessSlackBot(s *entities.SlackBot, requestingUserID string, teamIDs []string) bool {
	switch s.Scope() {
	case entities.ScopeUser:
		return s.UserID() == requestingUserID
	case entities.ScopeTeam:
		return containsTeam(teamIDs, s.TeamID())
	default:
		return false
	}
}

// ListSlackBots lists SlackBots for the requesting user
func (uc *MCPSlackBotToolsUseCase) ListSlackBots(ctx context.Context, input ListSlackBotsInput, requestingUserID string, teamIDs []string) ([]SlackBotInfo, error) {
	if uc.slackBotRepo == nil {
		return nil, fmt.Errorf("slackbot repository not available")
	}

	var bots []*entities.SlackBot

	switch entities.ResourceScope(input.Scope) {
	case entities.ScopeUser:
		result, err := uc.slackBotRepo.List(ctx, portrepos.SlackBotFilter{
			UserID: requestingUserID,
			Scope:  entities.ScopeUser,
			Status: entities.SlackBotStatus(input.Status),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list slackbots: %w", err)
		}
		bots = result

	case entities.ScopeTeam:
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
		result, err := uc.slackBotRepo.List(ctx, portrepos.SlackBotFilter{
			Scope:  entities.ScopeTeam,
			TeamID: input.TeamID,
			Status: entities.SlackBotStatus(input.Status),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list slackbots: %w", err)
		}
		bots = result

	default:
		userResult, err := uc.slackBotRepo.List(ctx, portrepos.SlackBotFilter{
			UserID: requestingUserID,
			Scope:  entities.ScopeUser,
			Status: entities.SlackBotStatus(input.Status),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list user slackbots: %w", err)
		}
		var teamResult []*entities.SlackBot
		if len(teamIDs) > 0 {
			teamResult, err = uc.slackBotRepo.List(ctx, portrepos.SlackBotFilter{
				TeamIDs: teamIDs,
				Scope:   entities.ScopeTeam,
				Status:  entities.SlackBotStatus(input.Status),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to list team slackbots: %w", err)
			}
		}
		bots = append(userResult, teamResult...)
	}

	result := make([]SlackBotInfo, 0, len(bots))
	for _, b := range bots {
		result = append(result, toSlackBotInfo(b))
	}
	return result, nil
}

// GetSlackBot retrieves a SlackBot by ID
func (uc *MCPSlackBotToolsUseCase) GetSlackBot(ctx context.Context, slackBotID, requestingUserID string, teamIDs []string) (*SlackBotInfo, error) {
	if uc.slackBotRepo == nil {
		return nil, fmt.Errorf("slackbot repository not available")
	}

	b, err := uc.slackBotRepo.Get(ctx, slackBotID)
	if err != nil {
		return nil, fmt.Errorf("slackbot not found: %w", err)
	}

	if !canAccessSlackBot(b, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	info := toSlackBotInfo(b)
	return &info, nil
}

// CreateSlackBot creates a new SlackBot
func (uc *MCPSlackBotToolsUseCase) CreateSlackBot(ctx context.Context, input CreateSlackBotInput, requestingUserID string, teamIDs []string) (*SlackBotInfo, error) {
	if uc.slackBotRepo == nil {
		return nil, fmt.Errorf("slackbot repository not available")
	}

	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	scope := entities.ResourceScope(input.Scope)
	if scope == "" {
		scope = entities.ScopeUser
	}
	if scope != entities.ScopeUser && scope != entities.ScopeTeam {
		return nil, fmt.Errorf("scope must be 'user' or 'team'")
	}
	if scope == entities.ScopeTeam {
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
	}

	b := entities.NewSlackBot(uuid.New().String(), input.Name, requestingUserID)
	b.SetScope(scope)
	b.SetTeamID(input.TeamID)
	b.SetTeams(teamIDs)
	if input.BotTokenSecretName != "" {
		b.SetBotTokenSecretName(input.BotTokenSecretName)
	}
	if len(input.AllowedEventTypes) > 0 {
		b.SetAllowedEventTypes(input.AllowedEventTypes)
	}
	if len(input.AllowedChannelNames) > 0 {
		b.SetAllowedChannelNames(input.AllowedChannelNames)
	}
	if len(input.AllowedUserIDs) > 0 {
		b.SetAllowedUserIDs(input.AllowedUserIDs)
	}
	if input.MaxSessions > 0 {
		b.SetMaxSessions(input.MaxSessions)
	}
	b.SetNotifyOnSessionCreated(input.NotifyOnSessionCreated)
	b.SetAllowBotMessages(input.AllowBotMessages)
	b.SetCreatedAt(time.Now())
	b.SetUpdatedAt(time.Now())

	if input.SessionConfig != nil {
		sc := entities.NewWebhookSessionConfig()
		sc.SetEnvironment(input.SessionConfig.Environment)
		sc.SetTags(input.SessionConfig.Tags)
		sc.SetInitialMessageTemplate(input.SessionConfig.InitialMessageTemplate)
		sc.SetReuseMessageTemplate(input.SessionConfig.ReuseMessageTemplate)
		sc.SetMemoryKey(input.SessionConfig.MemoryKey)
		sc.SetReuseSession(input.SessionConfig.ReuseSession)
		sc.SetMountPayload(input.SessionConfig.MountPayload)
		if input.SessionConfig.Params != nil {
			sc.SetParams(&entities.SessionParams{
				Message:      input.SessionConfig.Params.Message,
				AgentType:    input.SessionConfig.Params.AgentType,
				Oneshot:      input.SessionConfig.Params.Oneshot,
				RepoFullName: input.SessionConfig.Params.RepoFullName,
			})
		}
		b.SetSessionConfig(sc)
	}

	if err := uc.slackBotRepo.Create(ctx, b); err != nil {
		return nil, fmt.Errorf("failed to create slackbot: %w", err)
	}

	info := toSlackBotInfo(b)
	return &info, nil
}

// UpdateSlackBot updates an existing SlackBot
func (uc *MCPSlackBotToolsUseCase) UpdateSlackBot(ctx context.Context, slackBotID string, input UpdateSlackBotInput, requestingUserID string, teamIDs []string) (*SlackBotInfo, error) {
	if uc.slackBotRepo == nil {
		return nil, fmt.Errorf("slackbot repository not available")
	}

	b, err := uc.slackBotRepo.Get(ctx, slackBotID)
	if err != nil {
		return nil, fmt.Errorf("slackbot not found: %w", err)
	}

	if !canAccessSlackBot(b, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	if input.Name != nil {
		if *input.Name == "" {
			return nil, fmt.Errorf("name cannot be empty")
		}
		b.SetName(*input.Name)
	}
	if input.Status != nil {
		b.SetStatus(entities.SlackBotStatus(*input.Status))
	}
	if input.BotTokenSecretName != nil {
		b.SetBotTokenSecretName(*input.BotTokenSecretName)
	}
	if input.AllowedEventTypes != nil {
		b.SetAllowedEventTypes(input.AllowedEventTypes)
	}
	if input.AllowedChannelNames != nil {
		b.SetAllowedChannelNames(input.AllowedChannelNames)
	}
	if input.AllowedUserIDs != nil {
		b.SetAllowedUserIDs(input.AllowedUserIDs)
	}
	if input.MaxSessions != nil {
		b.SetMaxSessions(*input.MaxSessions)
	}
	if input.NotifyOnSessionCreated != nil {
		b.SetNotifyOnSessionCreated(input.NotifyOnSessionCreated)
	}
	if input.AllowBotMessages != nil {
		b.SetAllowBotMessages(input.AllowBotMessages)
	}
	if input.SessionConfig != nil {
		sc := entities.NewWebhookSessionConfig()
		sc.SetEnvironment(input.SessionConfig.Environment)
		sc.SetTags(input.SessionConfig.Tags)
		sc.SetInitialMessageTemplate(input.SessionConfig.InitialMessageTemplate)
		sc.SetReuseMessageTemplate(input.SessionConfig.ReuseMessageTemplate)
		sc.SetMemoryKey(input.SessionConfig.MemoryKey)
		sc.SetReuseSession(input.SessionConfig.ReuseSession)
		sc.SetMountPayload(input.SessionConfig.MountPayload)
		if input.SessionConfig.Params != nil {
			sc.SetParams(&entities.SessionParams{
				Message:      input.SessionConfig.Params.Message,
				AgentType:    input.SessionConfig.Params.AgentType,
				Oneshot:      input.SessionConfig.Params.Oneshot,
				RepoFullName: input.SessionConfig.Params.RepoFullName,
			})
		}
		b.SetSessionConfig(sc)
	}
	b.SetUpdatedAt(time.Now())

	if err := uc.slackBotRepo.Update(ctx, b); err != nil {
		return nil, fmt.Errorf("failed to update slackbot: %w", err)
	}

	info := toSlackBotInfo(b)
	return &info, nil
}

// DeleteSlackBot deletes a SlackBot
func (uc *MCPSlackBotToolsUseCase) DeleteSlackBot(ctx context.Context, slackBotID, requestingUserID string, teamIDs []string) error {
	if uc.slackBotRepo == nil {
		return fmt.Errorf("slackbot repository not available")
	}

	b, err := uc.slackBotRepo.Get(ctx, slackBotID)
	if err != nil {
		return fmt.Errorf("slackbot not found: %w", err)
	}

	if !canAccessSlackBot(b, requestingUserID, teamIDs) {
		return fmt.Errorf("access denied")
	}

	return uc.slackBotRepo.Delete(ctx, slackBotID)
}
