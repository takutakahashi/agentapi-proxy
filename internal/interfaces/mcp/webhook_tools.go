package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpusecases "github.com/takutakahashi/agentapi-proxy/internal/usecases/mcp"
)

// --- Webhook Tool Input/Output types ---

type ListWebhooksToolInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Status string `json:"status,omitempty"`
	Type   string `json:"type,omitempty"`
}

type WebhookSessionParamsOutput struct {
	Message      string `json:"message,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	Oneshot      bool   `json:"oneshot,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
}

type WebhookSessionConfigOutput struct {
	Environment            map[string]string           `json:"environment,omitempty"`
	Tags                   map[string]string           `json:"tags,omitempty"`
	InitialMessageTemplate string                      `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                      `json:"reuse_message_template,omitempty"`
	Params                 *WebhookSessionParamsOutput `json:"params,omitempty"`
	ReuseSession           bool                        `json:"reuse_session,omitempty"`
	MountPayload           bool                        `json:"mount_payload,omitempty"`
	MemoryKey              map[string]string           `json:"memory_key,omitempty"`
}

type WebhookGitHubConfigOutput struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

type WebhookGitHubConditionsOutput struct {
	Events       []string `json:"events,omitempty"`
	Actions      []string `json:"actions,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	Repositories []string `json:"repositories,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	BaseBranches []string `json:"base_branches,omitempty"`
	Draft        *bool    `json:"draft,omitempty"`
	Sender       []string `json:"sender,omitempty"`
}

type WebhookTriggerConditionsOutput struct {
	GitHub     *WebhookGitHubConditionsOutput `json:"github,omitempty"`
	GoTemplate string                         `json:"go_template,omitempty"`
}

type WebhookTriggerOutput struct {
	ID            string                         `json:"id"`
	Name          string                         `json:"name"`
	Priority      int                            `json:"priority"`
	Enabled       bool                           `json:"enabled"`
	Conditions    WebhookTriggerConditionsOutput `json:"conditions"`
	SessionConfig *WebhookSessionConfigOutput    `json:"session_config,omitempty"`
	StopOnMatch   bool                           `json:"stop_on_match"`
}

type WebhookDeliveryOutput struct {
	ID             string    `json:"id"`
	ReceivedAt     time.Time `json:"received_at"`
	Status         string    `json:"status"`
	MatchedTrigger string    `json:"matched_trigger,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	SessionReused  bool      `json:"session_reused,omitempty"`
}

type WebhookOutput struct {
	ID              string                      `json:"id"`
	Name            string                      `json:"name"`
	UserID          string                      `json:"user_id"`
	Scope           string                      `json:"scope,omitempty"`
	TeamID          string                      `json:"team_id,omitempty"`
	Status          string                      `json:"status"`
	Type            string                      `json:"type"`
	SignatureHeader string                      `json:"signature_header,omitempty"`
	SignatureType   string                      `json:"signature_type,omitempty"`
	GitHub          *WebhookGitHubConfigOutput  `json:"github,omitempty"`
	Triggers        []WebhookTriggerOutput      `json:"triggers"`
	SessionConfig   *WebhookSessionConfigOutput `json:"session_config,omitempty"`
	MaxSessions     int                         `json:"max_sessions"`
	DeliveryCount   int64                       `json:"delivery_count"`
	LastDelivery    *WebhookDeliveryOutput      `json:"last_delivery,omitempty"`
	CreatedAt       time.Time                   `json:"created_at"`
	UpdatedAt       time.Time                   `json:"updated_at"`
}

type ListWebhooksToolOutput struct {
	Webhooks []WebhookOutput `json:"webhooks"`
	Total    int             `json:"total"`
}

type GetWebhookToolInput struct {
	WebhookID string `json:"webhook_id"`
}

type GetWebhookToolOutput struct {
	Webhook WebhookOutput `json:"webhook"`
}

type CreateWebhookToolInput struct {
	Name            string                      `json:"name"`
	Scope           string                      `json:"scope,omitempty"`
	TeamID          string                      `json:"team_id,omitempty"`
	Type            string                      `json:"type"`
	Secret          string                      `json:"secret,omitempty"`
	SignatureHeader string                      `json:"signature_header,omitempty"`
	SignatureType   string                      `json:"signature_type,omitempty"`
	GitHub          *WebhookGitHubConfigOutput  `json:"github,omitempty"`
	Triggers        []WebhookTriggerOutput      `json:"triggers"`
	SessionConfig   *WebhookSessionConfigOutput `json:"session_config,omitempty"`
	MaxSessions     int                         `json:"max_sessions,omitempty"`
}

type CreateWebhookToolOutput struct {
	Webhook WebhookOutput `json:"webhook"`
}

type UpdateWebhookToolInput struct {
	WebhookID     string                      `json:"webhook_id"`
	Name          *string                     `json:"name,omitempty"`
	Status        *string                     `json:"status,omitempty"`
	Secret        *string                     `json:"secret,omitempty"`
	GitHub        *WebhookGitHubConfigOutput  `json:"github,omitempty"`
	Triggers      []WebhookTriggerOutput      `json:"triggers,omitempty"`
	SessionConfig *WebhookSessionConfigOutput `json:"session_config,omitempty"`
	MaxSessions   *int                        `json:"max_sessions,omitempty"`
}

type UpdateWebhookToolOutput struct {
	Webhook WebhookOutput `json:"webhook"`
}

type DeleteWebhookToolInput struct {
	WebhookID string `json:"webhook_id"`
}

type DeleteWebhookToolOutput struct {
	Message   string `json:"message"`
	WebhookID string `json:"webhook_id"`
}

// --- Conversion helpers ---

func toWebhookOutput(info mcpusecases.WebhookInfo) WebhookOutput {
	out := WebhookOutput{
		ID:              info.ID,
		Name:            info.Name,
		UserID:          info.UserID,
		Scope:           info.Scope,
		TeamID:          info.TeamID,
		Status:          info.Status,
		Type:            info.Type,
		SignatureHeader: info.SignatureHeader,
		SignatureType:   info.SignatureType,
		MaxSessions:     info.MaxSessions,
		DeliveryCount:   info.DeliveryCount,
		CreatedAt:       info.CreatedAt,
		UpdatedAt:       info.UpdatedAt,
	}

	if info.GitHub != nil {
		out.GitHub = &WebhookGitHubConfigOutput{
			EnterpriseURL:       info.GitHub.EnterpriseURL,
			AllowedEvents:       info.GitHub.AllowedEvents,
			AllowedRepositories: info.GitHub.AllowedRepositories,
		}
	}

	out.Triggers = make([]WebhookTriggerOutput, 0, len(info.Triggers))
	for _, t := range info.Triggers {
		out.Triggers = append(out.Triggers, toWebhookTriggerOutput(t))
	}

	if info.SessionConfig != nil {
		cfg := toWebhookSessionConfigOutput(*info.SessionConfig)
		out.SessionConfig = &cfg
	}

	if info.LastDelivery != nil {
		d := WebhookDeliveryOutput{
			ID:             info.LastDelivery.ID,
			ReceivedAt:     info.LastDelivery.ReceivedAt,
			Status:         info.LastDelivery.Status,
			MatchedTrigger: info.LastDelivery.MatchedTrigger,
			SessionID:      info.LastDelivery.SessionID,
			ErrorMessage:   info.LastDelivery.ErrorMessage,
			SessionReused:  info.LastDelivery.SessionReused,
		}
		out.LastDelivery = &d
	}

	return out
}

func toWebhookTriggerOutput(t mcpusecases.WebhookTriggerInfo) WebhookTriggerOutput {
	out := WebhookTriggerOutput{
		ID:          t.ID,
		Name:        t.Name,
		Priority:    t.Priority,
		Enabled:     t.Enabled,
		StopOnMatch: t.StopOnMatch,
		Conditions:  WebhookTriggerConditionsOutput{GoTemplate: t.Conditions.GoTemplate},
	}
	if t.Conditions.GitHub != nil {
		out.Conditions.GitHub = &WebhookGitHubConditionsOutput{
			Events:       t.Conditions.GitHub.Events,
			Actions:      t.Conditions.GitHub.Actions,
			Branches:     t.Conditions.GitHub.Branches,
			Repositories: t.Conditions.GitHub.Repositories,
			Labels:       t.Conditions.GitHub.Labels,
			Paths:        t.Conditions.GitHub.Paths,
			BaseBranches: t.Conditions.GitHub.BaseBranches,
			Draft:        t.Conditions.GitHub.Draft,
			Sender:       t.Conditions.GitHub.Sender,
		}
	}
	if t.SessionConfig != nil {
		cfg := toWebhookSessionConfigOutput(*t.SessionConfig)
		out.SessionConfig = &cfg
	}
	return out
}

func toWebhookSessionConfigOutput(info mcpusecases.WebhookSessionConfigInfo) WebhookSessionConfigOutput {
	out := WebhookSessionConfigOutput{
		Environment:            info.Environment,
		Tags:                   info.Tags,
		InitialMessageTemplate: info.InitialMessageTemplate,
		ReuseMessageTemplate:   info.ReuseMessageTemplate,
		ReuseSession:           info.ReuseSession,
		MountPayload:           info.MountPayload,
		MemoryKey:              info.MemoryKey,
	}
	if info.Params != nil {
		out.Params = &WebhookSessionParamsOutput{
			Message:      info.Params.Message,
			AgentType:    info.Params.AgentType,
			Oneshot:      info.Params.Oneshot,
			RepoFullName: info.Params.RepoFullName,
		}
	}
	return out
}

func fromWebhookSessionConfigOutput(out *WebhookSessionConfigOutput) *mcpusecases.WebhookSessionConfigInfo {
	if out == nil {
		return nil
	}
	info := &mcpusecases.WebhookSessionConfigInfo{
		Environment:            out.Environment,
		Tags:                   out.Tags,
		InitialMessageTemplate: out.InitialMessageTemplate,
		ReuseMessageTemplate:   out.ReuseMessageTemplate,
		ReuseSession:           out.ReuseSession,
		MountPayload:           out.MountPayload,
		MemoryKey:              out.MemoryKey,
	}
	if out.Params != nil {
		info.Params = &mcpusecases.WebhookSessionParams{
			Message:      out.Params.Message,
			AgentType:    out.Params.AgentType,
			Oneshot:      out.Params.Oneshot,
			RepoFullName: out.Params.RepoFullName,
		}
	}
	return info
}

func fromWebhookGitHubConfigOutput(out *WebhookGitHubConfigOutput) *mcpusecases.WebhookGitHubConfigInfo {
	if out == nil {
		return nil
	}
	return &mcpusecases.WebhookGitHubConfigInfo{
		EnterpriseURL:       out.EnterpriseURL,
		AllowedEvents:       out.AllowedEvents,
		AllowedRepositories: out.AllowedRepositories,
	}
}

func fromWebhookTriggerOutputs(triggers []WebhookTriggerOutput) []mcpusecases.WebhookTriggerInfo {
	result := make([]mcpusecases.WebhookTriggerInfo, 0, len(triggers))
	for _, t := range triggers {
		info := mcpusecases.WebhookTriggerInfo{
			ID:          t.ID,
			Name:        t.Name,
			Priority:    t.Priority,
			Enabled:     t.Enabled,
			StopOnMatch: t.StopOnMatch,
			Conditions:  mcpusecases.WebhookTriggerConditionsInfo{GoTemplate: t.Conditions.GoTemplate},
		}
		if t.Conditions.GitHub != nil {
			info.Conditions.GitHub = &mcpusecases.WebhookGitHubConditionsInfo{
				Events:       t.Conditions.GitHub.Events,
				Actions:      t.Conditions.GitHub.Actions,
				Branches:     t.Conditions.GitHub.Branches,
				Repositories: t.Conditions.GitHub.Repositories,
				Labels:       t.Conditions.GitHub.Labels,
				Paths:        t.Conditions.GitHub.Paths,
				BaseBranches: t.Conditions.GitHub.BaseBranches,
				Draft:        t.Conditions.GitHub.Draft,
				Sender:       t.Conditions.GitHub.Sender,
			}
		}
		info.SessionConfig = fromWebhookSessionConfigOutput(t.SessionConfig)
		result = append(result, info)
	}
	return result
}

// --- Webhook Tool Handlers ---

func (s *MCPServer) handleListWebhooks(ctx context.Context, req *mcp.CallToolRequest, input ListWebhooksToolInput) (*mcp.CallToolResult, ListWebhooksToolOutput, error) {
	if s.webhookUseCase == nil {
		return nil, ListWebhooksToolOutput{}, fmt.Errorf("webhook tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, ListWebhooksToolOutput{}, fmt.Errorf("authentication required")
	}

	webhooks, err := s.webhookUseCase.ListWebhooks(ctx, mcpusecases.ListWebhooksInput{
		Scope:  input.Scope,
		TeamID: input.TeamID,
		Status: input.Status,
		Type:   input.Type,
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListWebhooksToolOutput{}, fmt.Errorf("failed to list webhooks: %w", err)
	}

	out := ListWebhooksToolOutput{
		Webhooks: make([]WebhookOutput, 0, len(webhooks)),
		Total:    len(webhooks),
	}
	for _, w := range webhooks {
		out.Webhooks = append(out.Webhooks, toWebhookOutput(w))
	}
	return nil, out, nil
}

func (s *MCPServer) handleGetWebhook(ctx context.Context, req *mcp.CallToolRequest, input GetWebhookToolInput) (*mcp.CallToolResult, GetWebhookToolOutput, error) {
	if s.webhookUseCase == nil {
		return nil, GetWebhookToolOutput{}, fmt.Errorf("webhook tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, GetWebhookToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.WebhookID == "" {
		return nil, GetWebhookToolOutput{}, fmt.Errorf("webhook_id is required")
	}

	info, err := s.webhookUseCase.GetWebhook(ctx, input.WebhookID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, GetWebhookToolOutput{}, fmt.Errorf("failed to get webhook: %w", err)
	}

	return nil, GetWebhookToolOutput{Webhook: toWebhookOutput(*info)}, nil
}

func (s *MCPServer) handleCreateWebhook(ctx context.Context, req *mcp.CallToolRequest, input CreateWebhookToolInput) (*mcp.CallToolResult, CreateWebhookToolOutput, error) {
	if s.webhookUseCase == nil {
		return nil, CreateWebhookToolOutput{}, fmt.Errorf("webhook tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, CreateWebhookToolOutput{}, fmt.Errorf("authentication required")
	}

	info, err := s.webhookUseCase.CreateWebhook(ctx, mcpusecases.CreateWebhookInput{
		Name:            input.Name,
		Scope:           input.Scope,
		TeamID:          input.TeamID,
		Type:            input.Type,
		Secret:          input.Secret,
		SignatureHeader: input.SignatureHeader,
		SignatureType:   input.SignatureType,
		GitHub:          fromWebhookGitHubConfigOutput(input.GitHub),
		Triggers:        fromWebhookTriggerOutputs(input.Triggers),
		SessionConfig:   fromWebhookSessionConfigOutput(input.SessionConfig),
		MaxSessions:     input.MaxSessions,
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, CreateWebhookToolOutput{}, fmt.Errorf("failed to create webhook: %w", err)
	}

	return nil, CreateWebhookToolOutput{Webhook: toWebhookOutput(*info)}, nil
}

func (s *MCPServer) handleUpdateWebhook(ctx context.Context, req *mcp.CallToolRequest, input UpdateWebhookToolInput) (*mcp.CallToolResult, UpdateWebhookToolOutput, error) {
	if s.webhookUseCase == nil {
		return nil, UpdateWebhookToolOutput{}, fmt.Errorf("webhook tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, UpdateWebhookToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.WebhookID == "" {
		return nil, UpdateWebhookToolOutput{}, fmt.Errorf("webhook_id is required")
	}

	var triggers []mcpusecases.WebhookTriggerInfo
	if input.Triggers != nil {
		triggers = fromWebhookTriggerOutputs(input.Triggers)
	}

	info, err := s.webhookUseCase.UpdateWebhook(ctx, input.WebhookID, mcpusecases.UpdateWebhookInput{
		Name:          input.Name,
		Status:        input.Status,
		Secret:        input.Secret,
		GitHub:        fromWebhookGitHubConfigOutput(input.GitHub),
		Triggers:      triggers,
		SessionConfig: fromWebhookSessionConfigOutput(input.SessionConfig),
		MaxSessions:   input.MaxSessions,
	}, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, UpdateWebhookToolOutput{}, fmt.Errorf("failed to update webhook: %w", err)
	}

	return nil, UpdateWebhookToolOutput{Webhook: toWebhookOutput(*info)}, nil
}

func (s *MCPServer) handleDeleteWebhook(ctx context.Context, req *mcp.CallToolRequest, input DeleteWebhookToolInput) (*mcp.CallToolResult, DeleteWebhookToolOutput, error) {
	if s.webhookUseCase == nil {
		return nil, DeleteWebhookToolOutput{}, fmt.Errorf("webhook tools not available")
	}
	if s.authenticatedUserID == "" {
		return nil, DeleteWebhookToolOutput{}, fmt.Errorf("authentication required")
	}
	if input.WebhookID == "" {
		return nil, DeleteWebhookToolOutput{}, fmt.Errorf("webhook_id is required")
	}

	if err := s.webhookUseCase.DeleteWebhook(ctx, input.WebhookID, s.authenticatedUserID, s.authenticatedTeams); err != nil {
		return nil, DeleteWebhookToolOutput{}, fmt.Errorf("failed to delete webhook: %w", err)
	}

	return nil, DeleteWebhookToolOutput{
		Message:   "Webhook deleted successfully",
		WebhookID: input.WebhookID,
	}, nil
}
