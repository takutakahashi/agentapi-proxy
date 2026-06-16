package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	mcpusecases "github.com/takutakahashi/agentapi-proxy/internal/usecases/mcp"
)

type GetWebhookToolInput struct {
	WebhookID string `json:"webhook_id"`
}

type ListWebhooksToolOutput struct {
	Webhooks []mcpusecases.WebhookInfo `json:"webhooks"`
	Total    int                       `json:"total"`
}

type GetWebhookToolOutput struct {
	Webhook mcpusecases.WebhookInfo `json:"webhook"`
}

type TriggerWebhookToolOutput struct {
	Result mcpusecases.TriggerWebhookResult `json:"result"`
}

type GetScheduleToolInput struct {
	ScheduleID string `json:"schedule_id"`
}

type TriggerScheduleToolInput struct {
	ScheduleID string `json:"schedule_id"`
}

type ListSchedulesToolOutput struct {
	Schedules []mcpusecases.ScheduleInfo `json:"schedules"`
	Total     int                        `json:"total"`
}

type GetScheduleToolOutput struct {
	Schedule mcpusecases.ScheduleInfo `json:"schedule"`
}

type TriggerScheduleToolOutput struct {
	Result mcpusecases.TriggerScheduleResult `json:"result"`
}

type GetSlackBotToolInput struct {
	SlackBotID string `json:"slackbot_id"`
}

type ListSlackBotsToolOutput struct {
	SlackBots []mcpusecases.SlackBotInfo `json:"slackbots"`
	Total     int                        `json:"total"`
}

type GetSlackBotToolOutput struct {
	SlackBot mcpusecases.SlackBotInfo `json:"slackbot"`
}

func (s *MCPServer) registerWebhookTools() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_webhooks",
		Description: "List webhooks with optional filters (scope, team_id, type, status). Secrets are never returned.",
	}, s.handleListWebhooks)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_webhook",
		Description: "Get a webhook by ID. The webhook secret is never returned.",
	}, s.handleGetWebhook)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "trigger_webhook",
		Description: "Manually trigger a webhook. GitHub webhooks require event; dry_run evaluates without creating a session.",
	}, s.handleTriggerWebhook)
	slog.Info("[MCP] Registered 3 webhook tools: list_webhooks, get_webhook, trigger_webhook")
}

func (s *MCPServer) registerScheduleTools() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_schedules",
		Description: "List schedules with optional filters (scope, team_id, status).",
	}, s.handleListSchedules)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_schedule",
		Description: "Get a schedule by ID.",
	}, s.handleGetSchedule)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "trigger_schedule",
		Description: "Manually trigger a schedule and create or reuse a session.",
	}, s.handleTriggerSchedule)
	slog.Info("[MCP] Registered 3 schedule tools: list_schedules, get_schedule, trigger_schedule")
}

func (s *MCPServer) registerSlackBotTools() {
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "list_slackbots",
		Description: "List SlackBots with optional filters (scope, team_id, status). Token values are never returned.",
	}, s.handleListSlackBots)
	mcp.AddTool(s.server, &mcp.Tool{
		Name:        "get_slackbot",
		Description: "Get a SlackBot by ID. Token values are never returned.",
	}, s.handleGetSlackBot)
	slog.Info("[MCP] Registered 2 slackbot tools: list_slackbots, get_slackbot")
}

func (s *MCPServer) handleListWebhooks(ctx context.Context, req *mcp.CallToolRequest, input mcpusecases.ListWebhooksInput) (*mcp.CallToolResult, ListWebhooksToolOutput, error) {
	if err := s.requireWebhookAuth(); err != nil {
		return nil, ListWebhooksToolOutput{}, err
	}
	webhooks, err := s.webhookUseCase.ListWebhooks(ctx, input, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListWebhooksToolOutput{}, fmt.Errorf("failed to list webhooks: %w", err)
	}
	return nil, ListWebhooksToolOutput{Webhooks: webhooks, Total: len(webhooks)}, nil
}

func (s *MCPServer) handleGetWebhook(ctx context.Context, req *mcp.CallToolRequest, input GetWebhookToolInput) (*mcp.CallToolResult, GetWebhookToolOutput, error) {
	if err := s.requireWebhookAuth(); err != nil {
		return nil, GetWebhookToolOutput{}, err
	}
	if input.WebhookID == "" {
		return nil, GetWebhookToolOutput{}, fmt.Errorf("webhook_id is required")
	}
	webhook, err := s.webhookUseCase.GetWebhook(ctx, input.WebhookID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, GetWebhookToolOutput{}, fmt.Errorf("failed to get webhook: %w", err)
	}
	return nil, GetWebhookToolOutput{Webhook: *webhook}, nil
}

func (s *MCPServer) handleTriggerWebhook(ctx context.Context, req *mcp.CallToolRequest, input mcpusecases.TriggerWebhookInput) (*mcp.CallToolResult, TriggerWebhookToolOutput, error) {
	if err := s.requireWebhookAuth(); err != nil {
		return nil, TriggerWebhookToolOutput{}, err
	}
	result, err := s.webhookUseCase.TriggerWebhook(ctx, input, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, TriggerWebhookToolOutput{}, fmt.Errorf("failed to trigger webhook: %w", err)
	}
	return nil, TriggerWebhookToolOutput{Result: *result}, nil
}

func (s *MCPServer) handleListSchedules(ctx context.Context, req *mcp.CallToolRequest, input mcpusecases.ListSchedulesInput) (*mcp.CallToolResult, ListSchedulesToolOutput, error) {
	if err := s.requireScheduleAuth(); err != nil {
		return nil, ListSchedulesToolOutput{}, err
	}
	schedules, err := s.scheduleUseCase.ListSchedules(ctx, input, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListSchedulesToolOutput{}, fmt.Errorf("failed to list schedules: %w", err)
	}
	return nil, ListSchedulesToolOutput{Schedules: schedules, Total: len(schedules)}, nil
}

func (s *MCPServer) handleGetSchedule(ctx context.Context, req *mcp.CallToolRequest, input GetScheduleToolInput) (*mcp.CallToolResult, GetScheduleToolOutput, error) {
	if err := s.requireScheduleAuth(); err != nil {
		return nil, GetScheduleToolOutput{}, err
	}
	if input.ScheduleID == "" {
		return nil, GetScheduleToolOutput{}, fmt.Errorf("schedule_id is required")
	}
	scheduleInfo, err := s.scheduleUseCase.GetSchedule(ctx, input.ScheduleID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, GetScheduleToolOutput{}, fmt.Errorf("failed to get schedule: %w", err)
	}
	return nil, GetScheduleToolOutput{Schedule: *scheduleInfo}, nil
}

func (s *MCPServer) handleTriggerSchedule(ctx context.Context, req *mcp.CallToolRequest, input TriggerScheduleToolInput) (*mcp.CallToolResult, TriggerScheduleToolOutput, error) {
	if err := s.requireScheduleAuth(); err != nil {
		return nil, TriggerScheduleToolOutput{}, err
	}
	if input.ScheduleID == "" {
		return nil, TriggerScheduleToolOutput{}, fmt.Errorf("schedule_id is required")
	}
	result, err := s.scheduleUseCase.TriggerSchedule(ctx, input.ScheduleID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, TriggerScheduleToolOutput{}, fmt.Errorf("failed to trigger schedule: %w", err)
	}
	return nil, TriggerScheduleToolOutput{Result: *result}, nil
}

func (s *MCPServer) handleListSlackBots(ctx context.Context, req *mcp.CallToolRequest, input mcpusecases.ListSlackBotsInput) (*mcp.CallToolResult, ListSlackBotsToolOutput, error) {
	if err := s.requireSlackBotAuth(); err != nil {
		return nil, ListSlackBotsToolOutput{}, err
	}
	bots, err := s.slackBotUseCase.ListSlackBots(ctx, input, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, ListSlackBotsToolOutput{}, fmt.Errorf("failed to list slackbots: %w", err)
	}
	return nil, ListSlackBotsToolOutput{SlackBots: bots, Total: len(bots)}, nil
}

func (s *MCPServer) handleGetSlackBot(ctx context.Context, req *mcp.CallToolRequest, input GetSlackBotToolInput) (*mcp.CallToolResult, GetSlackBotToolOutput, error) {
	if err := s.requireSlackBotAuth(); err != nil {
		return nil, GetSlackBotToolOutput{}, err
	}
	if input.SlackBotID == "" {
		return nil, GetSlackBotToolOutput{}, fmt.Errorf("slackbot_id is required")
	}
	bot, err := s.slackBotUseCase.GetSlackBot(ctx, input.SlackBotID, s.authenticatedUserID, s.authenticatedTeams)
	if err != nil {
		return nil, GetSlackBotToolOutput{}, fmt.Errorf("failed to get slackbot: %w", err)
	}
	return nil, GetSlackBotToolOutput{SlackBot: *bot}, nil
}

func (s *MCPServer) requireWebhookAuth() error {
	if s.webhookUseCase == nil {
		return fmt.Errorf("webhook tools not available")
	}
	if s.authenticatedUserID == "" {
		return fmt.Errorf("authentication required")
	}
	return nil
}

func (s *MCPServer) requireScheduleAuth() error {
	if s.scheduleUseCase == nil {
		return fmt.Errorf("schedule tools not available")
	}
	if s.authenticatedUserID == "" {
		return fmt.Errorf("authentication required")
	}
	return nil
}

func (s *MCPServer) requireSlackBotAuth() error {
	if s.slackBotUseCase == nil {
		return fmt.Errorf("slackbot tools not available")
	}
	if s.authenticatedUserID == "" {
		return fmt.Errorf("authentication required")
	}
	return nil
}
