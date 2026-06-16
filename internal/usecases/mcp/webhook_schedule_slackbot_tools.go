package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/controllers"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/schedule"
)

type ScheduleManager = schedule.Manager

// MCPWebhookToolsUseCase provides MCP access to webhook resources.
type MCPWebhookToolsUseCase struct {
	repo             portrepos.WebhookRepository
	githubController *controllers.WebhookGitHubController
	customController *controllers.WebhookCustomController
}

func NewMCPWebhookToolsUseCase(repo portrepos.WebhookRepository, sessionManager portrepos.SessionManager, memoryRepo portrepos.MemoryRepository, sessionProfileRepo portrepos.SessionProfileRepository) *MCPWebhookToolsUseCase {
	return &MCPWebhookToolsUseCase{
		repo:             repo,
		githubController: controllers.NewWebhookGitHubController(repo, sessionManager, memoryRepo, sessionProfileRepo),
		customController: controllers.NewWebhookCustomController(repo, sessionManager, memoryRepo, sessionProfileRepo),
	}
}

// MCPScheduleToolsUseCase provides MCP access to schedule resources.
type MCPScheduleToolsUseCase struct {
	manager  schedule.Manager
	launcher *sessionuc.LaunchUseCase
}

func NewMCPScheduleToolsUseCase(manager schedule.Manager, sessionManager portrepos.SessionManager, memoryRepo portrepos.MemoryRepository, sessionProfileRepo portrepos.SessionProfileRepository) *MCPScheduleToolsUseCase {
	return &MCPScheduleToolsUseCase{
		manager: manager,
		launcher: sessionuc.NewLaunchUseCase(sessionManager).
			WithMemoryRepository(memoryRepo).
			WithSessionProfileRepository(sessionProfileRepo),
	}
}

// MCPSlackBotToolsUseCase provides MCP access to SlackBot resources.
type MCPSlackBotToolsUseCase struct {
	repo portrepos.SlackBotRepository
}

func NewMCPSlackBotToolsUseCase(repo portrepos.SlackBotRepository) *MCPSlackBotToolsUseCase {
	return &MCPSlackBotToolsUseCase{repo: repo}
}

type ListWebhooksInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Type   string `json:"type,omitempty"`
	Status string `json:"status,omitempty"`
}

type TriggerWebhookInput struct {
	WebhookID string                 `json:"webhook_id"`
	Event     string                 `json:"event,omitempty"`
	Payload   map[string]interface{} `json:"payload"`
	DryRun    bool                   `json:"dry_run,omitempty"`
}

type WebhookInfo struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	UserID          string               `json:"user_id"`
	Scope           string               `json:"scope"`
	TeamID          string               `json:"team_id,omitempty"`
	Status          string               `json:"status"`
	Type            string               `json:"type"`
	SignatureHeader string               `json:"signature_header,omitempty"`
	SignatureType   string               `json:"signature_type,omitempty"`
	SignaturePrefix string               `json:"signature_prefix,omitempty"`
	GitHub          *GitHubConfigInfo    `json:"github,omitempty"`
	Triggers        []WebhookTriggerInfo `json:"triggers"`
	SessionConfig   *SessionConfigInfo   `json:"session_config,omitempty"`
	MaxSessions     int                  `json:"max_sessions"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	LastDelivery    *DeliveryInfo        `json:"last_delivery,omitempty"`
	DeliveryCount   int64                `json:"delivery_count"`
}

type GitHubConfigInfo struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

type WebhookTriggerInfo struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	Priority      int                   `json:"priority"`
	Enabled       bool                  `json:"enabled"`
	Conditions    WebhookConditionsInfo `json:"conditions"`
	SessionConfig *SessionConfigInfo    `json:"session_config,omitempty"`
	StopOnMatch   bool                  `json:"stop_on_match"`
}

type WebhookConditionsInfo struct {
	GitHub     *GitHubConditionsInfo `json:"github,omitempty"`
	GoTemplate string                `json:"go_template,omitempty"`
}

type GitHubConditionsInfo struct {
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

type SessionConfigInfo struct {
	Environment            map[string]string  `json:"environment,omitempty"`
	Tags                   map[string]string  `json:"tags,omitempty"`
	InitialMessageTemplate string             `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string             `json:"reuse_message_template,omitempty"`
	Params                 *SessionParamsInfo `json:"params,omitempty"`
	ReuseSession           bool               `json:"reuse_session,omitempty"`
	MountPayload           bool               `json:"mount_payload,omitempty"`
	MemoryKey              map[string]string  `json:"memory_key,omitempty"`
	SessionProfileID       string             `json:"session_profile_id,omitempty"`
}

type SessionParamsInfo struct {
	AgentType    string `json:"agent_type,omitempty"`
	Oneshot      bool   `json:"oneshot,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
}

type DeliveryInfo struct {
	ID             string    `json:"id"`
	ReceivedAt     time.Time `json:"received_at"`
	Status         string    `json:"status"`
	MatchedTrigger string    `json:"matched_trigger,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	Error          string    `json:"error,omitempty"`
	SessionReused  bool      `json:"session_reused,omitempty"`
}

type TriggerWebhookResult struct {
	Matched        bool                       `json:"matched"`
	MatchedTrigger *TriggerMatchedTriggerInfo `json:"matched_trigger,omitempty"`
	SessionID      string                     `json:"session_id,omitempty"`
	SessionReused  bool                       `json:"session_reused,omitempty"`
	DryRun         bool                       `json:"dry_run"`
	InitialMessage string                     `json:"initial_message,omitempty"`
	Tags           map[string]string          `json:"tags,omitempty"`
	Environment    map[string]string          `json:"environment,omitempty"`
	Error          string                     `json:"error,omitempty"`
}

type TriggerMatchedTriggerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (uc *MCPWebhookToolsUseCase) ListWebhooks(ctx context.Context, input ListWebhooksInput, userID string, teamIDs []string) ([]WebhookInfo, error) {
	filter := portrepos.WebhookFilter{
		Scope:   entities.ResourceScope(input.Scope),
		TeamID:  input.TeamID,
		TeamIDs: teamIDs,
		Type:    entities.WebhookType(input.Type),
		Status:  entities.WebhookStatus(input.Status),
	}
	if input.Scope != string(entities.ScopeTeam) && input.TeamID == "" {
		filter.UserID = userID
	}

	webhooks, err := uc.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	result := make([]WebhookInfo, 0, len(webhooks))
	for _, webhook := range webhooks {
		if !visibleInScope(input.Scope, webhook.Scope()) || !canAccessOwnedResource(webhook.Scope(), webhook.UserID(), webhook.TeamID(), userID, teamIDs) {
			continue
		}
		result = append(result, toWebhookInfo(webhook))
	}
	return result, nil
}

func (uc *MCPWebhookToolsUseCase) GetWebhook(ctx context.Context, webhookID, userID string, teamIDs []string) (*WebhookInfo, error) {
	webhook, err := uc.repo.Get(ctx, webhookID)
	if err != nil {
		return nil, err
	}
	if !canAccessOwnedResource(webhook.Scope(), webhook.UserID(), webhook.TeamID(), userID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}
	info := toWebhookInfo(webhook)
	return &info, nil
}

func (uc *MCPWebhookToolsUseCase) TriggerWebhook(ctx context.Context, input TriggerWebhookInput, userID string, teamIDs []string) (*TriggerWebhookResult, error) {
	if input.WebhookID == "" {
		return nil, fmt.Errorf("webhook_id is required")
	}
	if input.Payload == nil {
		return nil, fmt.Errorf("payload is required")
	}

	webhook, err := uc.repo.Get(ctx, input.WebhookID)
	if err != nil {
		return nil, err
	}
	if !canAccessOwnedResource(webhook.Scope(), webhook.UserID(), webhook.TeamID(), userID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	switch webhook.WebhookType() {
	case entities.WebhookTypeGitHub:
		return uc.triggerGitHubWebhook(ctx, webhook, input)
	case entities.WebhookTypeCustom:
		return uc.triggerCustomWebhook(ctx, webhook, input)
	default:
		return nil, fmt.Errorf("unsupported webhook type")
	}
}

func (uc *MCPWebhookToolsUseCase) triggerGitHubWebhook(ctx context.Context, webhook *entities.Webhook, input TriggerWebhookInput) (*TriggerWebhookResult, error) {
	if input.Event == "" {
		return nil, fmt.Errorf("event is required for GitHub webhooks")
	}

	payloadBytes, _ := json.Marshal(input.Payload)
	var payload controllers.GitHubPayload
	_ = json.Unmarshal(payloadBytes, &payload)
	payload.Raw = input.Payload

	match := uc.githubController.MatchTriggersForTest(webhook.Triggers(), input.Event, &payload)
	result := &TriggerWebhookResult{DryRun: input.DryRun, Matched: match != nil}
	if match == nil {
		return result, nil
	}
	result.MatchedTrigger = &TriggerMatchedTriggerInfo{ID: match.ID(), Name: match.Name()}

	tags := uc.githubController.BuildGitHubTagsForTest(webhook, match, input.Event, &payload)
	defaultMessage := uc.githubController.BuildDefaultInitialMessageForTest(input.Event, &payload)
	if input.DryRun {
		dryResult, err := uc.githubController.SessionService().DryRunSessionConfig(controllers.SessionCreationParams{
			Webhook: webhook, Trigger: match, Payload: input.Payload, Tags: tags, DefaultMessage: defaultMessage,
		})
		applyDryRunResult(result, dryResult, err)
		return result, nil
	}

	sessionID, reused, err := uc.githubController.SessionService().CreateSessionFromWebhook(ctx, controllers.SessionCreationParams{
		Webhook: webhook, Trigger: match, Payload: input.Payload, Tags: tags, DefaultMessage: defaultMessage,
	})
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.SessionID = sessionID
	result.SessionReused = reused
	return result, nil
}

func (uc *MCPWebhookToolsUseCase) triggerCustomWebhook(ctx context.Context, webhook *entities.Webhook, input TriggerWebhookInput) (*TriggerWebhookResult, error) {
	match := uc.customController.MatchTriggersForTest(webhook.Triggers(), input.Payload)
	result := &TriggerWebhookResult{DryRun: input.DryRun, Matched: match != nil}
	if match == nil {
		return result, nil
	}
	result.MatchedTrigger = &TriggerMatchedTriggerInfo{ID: match.ID(), Name: match.Name()}

	tags := uc.customController.BuildCustomTagsForTest(webhook, match)
	defaultMessage := uc.customController.BuildDefaultMessageForTest(input.Payload)
	if input.DryRun {
		dryResult, err := uc.customController.SessionService().DryRunSessionConfig(controllers.SessionCreationParams{
			Webhook: webhook, Trigger: match, Payload: input.Payload, Tags: tags, DefaultMessage: defaultMessage,
		})
		applyDryRunResult(result, dryResult, err)
		return result, nil
	}

	sessionID, reused, err := uc.customController.SessionService().CreateSessionFromWebhook(ctx, controllers.SessionCreationParams{
		Webhook: webhook, Trigger: match, Payload: input.Payload, Tags: tags, DefaultMessage: defaultMessage,
	})
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.SessionID = sessionID
	result.SessionReused = reused
	return result, nil
}

func applyDryRunResult(result *TriggerWebhookResult, dryResult *controllers.DryRunResult, err error) {
	if err != nil {
		result.Error = err.Error()
		return
	}
	if dryResult.Error != "" {
		result.Error = dryResult.Error
		return
	}
	result.InitialMessage = dryResult.InitialMessage
	result.Tags = dryResult.Tags
	result.Environment = dryResult.Environment
}

type ListSchedulesInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Status string `json:"status,omitempty"`
}

type ScheduleInfo struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	UserID          string                    `json:"user_id"`
	Scope           string                    `json:"scope"`
	TeamID          string                    `json:"team_id,omitempty"`
	Status          string                    `json:"status"`
	ScheduledAt     *time.Time                `json:"scheduled_at,omitempty"`
	CronExpr        string                    `json:"cron_expr,omitempty"`
	Timezone        string                    `json:"timezone,omitempty"`
	SessionConfig   schedule.SessionConfig    `json:"session_config"`
	LastExecution   *schedule.ExecutionRecord `json:"last_execution,omitempty"`
	NextExecutionAt *time.Time                `json:"next_execution_at,omitempty"`
	ExecutionCount  int                       `json:"execution_count"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

type TriggerScheduleResult struct {
	SessionID     string    `json:"session_id"`
	TriggeredAt   time.Time `json:"triggered_at"`
	SessionReused bool      `json:"session_reused"`
}

func (uc *MCPScheduleToolsUseCase) ListSchedules(ctx context.Context, input ListSchedulesInput, userID string, teamIDs []string) ([]ScheduleInfo, error) {
	filter := schedule.ScheduleFilter{
		Scope:   entities.ResourceScope(input.Scope),
		TeamID:  input.TeamID,
		TeamIDs: teamIDs,
		Status:  schedule.ScheduleStatus(input.Status),
	}
	if input.Scope != string(entities.ScopeTeam) && input.TeamID == "" {
		filter.UserID = userID
	}

	schedules, err := uc.manager.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	result := make([]ScheduleInfo, 0, len(schedules))
	for _, item := range schedules {
		if !visibleInScope(input.Scope, item.GetScope()) || !canAccessOwnedResource(item.GetScope(), item.UserID, item.TeamID, userID, teamIDs) {
			continue
		}
		result = append(result, toScheduleInfo(item))
	}
	return result, nil
}

func (uc *MCPScheduleToolsUseCase) GetSchedule(ctx context.Context, scheduleID, userID string, teamIDs []string) (*ScheduleInfo, error) {
	item, err := uc.manager.Get(ctx, scheduleID)
	if err != nil {
		return nil, err
	}
	if !canAccessOwnedResource(item.GetScope(), item.UserID, item.TeamID, userID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}
	info := toScheduleInfo(item)
	return &info, nil
}

func (uc *MCPScheduleToolsUseCase) TriggerSchedule(ctx context.Context, scheduleID, userID string, teamIDs []string) (*TriggerScheduleResult, error) {
	item, err := uc.manager.Get(ctx, scheduleID)
	if err != nil {
		return nil, err
	}
	if !canAccessOwnedResource(item.GetScope(), item.UserID, item.TeamID, userID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	sessionID := uuid.New().String()
	scope := item.GetScope()
	teams := sessionuc.ResolveTeams(scope, item.TeamID, teamIDs)
	if len(teams) == 0 {
		teams = sessionuc.ResolveTeams(scope, item.TeamID, item.UserTeams)
	}

	tags := item.SessionConfig.Tags
	if tags == nil {
		tags = make(map[string]string)
	}
	tags["schedule_id"] = item.ID
	tags["schedule_name"] = item.Name

	req := sessionuc.LaunchRequest{
		UserID:           item.UserID,
		Scope:            scope,
		TeamID:           item.TeamID,
		Teams:            teams,
		Environment:      item.SessionConfig.Environment,
		Tags:             tags,
		MemoryKey:        item.SessionConfig.MemoryKey,
		RepoInfo:         app.ExtractRepositoryInfo(tags, sessionID),
		SessionProfileID: item.SessionConfig.SessionProfileID,
		ReuseSession:     item.SessionConfig.ReuseSession,
		ReuseMatchTags:   map[string]string{"schedule_id": item.ID},
		ReuseMessage:     item.SessionConfig.ReuseMessage,
	}
	if item.SessionConfig.Params != nil {
		req.InitialMessage = item.SessionConfig.Params.Message
		if scope != entities.ScopeTeam {
			req.GithubToken = item.SessionConfig.Params.GithubToken
		}
		req.AgentType = item.SessionConfig.Params.AgentType
		req.SlackParams = item.SessionConfig.Params.Slack
		req.Sandbox = item.SessionConfig.Params.Sandbox
		req.Docker = item.SessionConfig.Params.Docker
		req.Oneshot = item.SessionConfig.Params.Oneshot
		req.InitialMessageWaitSecond = item.SessionConfig.Params.InitialMessageWaitSecond
		req.CycleMessage = item.SessionConfig.Params.CycleMessage
		req.CycleMaxCount = item.SessionConfig.Params.CycleMaxCount
		req.SessionTTL = item.SessionConfig.Params.SessionTTL
	}

	launchResult, err := uc.launcher.Launch(ctx, sessionID, req)
	record := schedule.ExecutionRecord{ExecutedAt: time.Now()}
	if err != nil {
		record.Status = "failed"
		record.Error = err.Error()
		_ = uc.manager.RecordExecution(ctx, scheduleID, record)
		return nil, err
	}

	record.Status = "success"
	record.SessionID = launchResult.SessionID
	record.SessionReused = launchResult.SessionReused
	_ = uc.manager.RecordExecution(ctx, scheduleID, record)
	return &TriggerScheduleResult{SessionID: launchResult.SessionID, TriggeredAt: record.ExecutedAt, SessionReused: launchResult.SessionReused}, nil
}

type ListSlackBotsInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Status string `json:"status,omitempty"`
}

type SlackBotInfo struct {
	ID                     string             `json:"id"`
	Name                   string             `json:"name"`
	UserID                 string             `json:"user_id"`
	Scope                  string             `json:"scope"`
	TeamID                 string             `json:"team_id,omitempty"`
	Teams                  []string           `json:"teams,omitempty"`
	Status                 string             `json:"status"`
	BotTokenSecretName     string             `json:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey      string             `json:"bot_token_secret_key,omitempty"`
	AppTokenSecretKey      string             `json:"app_token_secret_key,omitempty"`
	AllowedEventTypes      []string           `json:"allowed_event_types,omitempty"`
	AllowedChannelNames    []string           `json:"allowed_channel_names,omitempty"`
	AllowedUserIDs         []string           `json:"allowed_user_ids,omitempty"`
	SessionConfig          *SessionConfigInfo `json:"session_config,omitempty"`
	MaxSessions            int                `json:"max_sessions"`
	NotifyOnSessionCreated bool               `json:"notify_on_session_created"`
	AllowBotMessages       bool               `json:"allow_bot_messages"`
	CreatedAt              time.Time          `json:"created_at"`
	UpdatedAt              time.Time          `json:"updated_at"`
}

func (uc *MCPSlackBotToolsUseCase) ListSlackBots(ctx context.Context, input ListSlackBotsInput, userID string, teamIDs []string) ([]SlackBotInfo, error) {
	bots, err := uc.repo.List(ctx, portrepos.SlackBotFilter{
		UserID:  userID,
		TeamIDs: teamIDs,
		Scope:   entities.ResourceScope(input.Scope),
		TeamID:  input.TeamID,
		Status:  entities.SlackBotStatus(input.Status),
	})
	if err != nil {
		return nil, err
	}

	result := make([]SlackBotInfo, 0, len(bots))
	for _, bot := range bots {
		if !canAccessOwnedResource(bot.Scope(), bot.UserID(), bot.TeamID(), userID, teamIDs) {
			continue
		}
		result = append(result, toSlackBotInfo(bot))
	}
	return result, nil
}

func (uc *MCPSlackBotToolsUseCase) GetSlackBot(ctx context.Context, botID, userID string, teamIDs []string) (*SlackBotInfo, error) {
	bot, err := uc.repo.Get(ctx, botID)
	if err != nil {
		return nil, err
	}
	if !canAccessOwnedResource(bot.Scope(), bot.UserID(), bot.TeamID(), userID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}
	info := toSlackBotInfo(bot)
	return &info, nil
}

func visibleInScope(requestedScope string, resourceScope entities.ResourceScope) bool {
	if requestedScope == string(entities.ScopeTeam) {
		return resourceScope == entities.ScopeTeam
	}
	return resourceScope != entities.ScopeTeam
}

func canAccessOwnedResource(scope entities.ResourceScope, ownerID, teamID, userID string, teamIDs []string) bool {
	switch scope {
	case entities.ScopeTeam:
		return containsTeam(teamIDs, teamID)
	default:
		return ownerID == userID
	}
}

func toWebhookInfo(webhook *entities.Webhook) WebhookInfo {
	info := WebhookInfo{
		ID:              webhook.ID(),
		Name:            webhook.Name(),
		UserID:          webhook.UserID(),
		Scope:           string(webhook.Scope()),
		TeamID:          webhook.TeamID(),
		Status:          string(webhook.Status()),
		Type:            string(webhook.WebhookType()),
		SignatureHeader: webhook.SignatureHeader(),
		SignatureType:   string(webhook.SignatureType()),
		SignaturePrefix: webhook.SignaturePrefix(),
		SessionConfig:   toSessionConfigInfo(webhook.SessionConfig()),
		MaxSessions:     webhook.MaxSessions(),
		CreatedAt:       webhook.CreatedAt(),
		UpdatedAt:       webhook.UpdatedAt(),
		DeliveryCount:   webhook.DeliveryCount(),
	}
	if github := webhook.GitHub(); github != nil {
		info.GitHub = &GitHubConfigInfo{
			EnterpriseURL:       github.EnterpriseURL(),
			AllowedEvents:       github.AllowedEvents(),
			AllowedRepositories: github.AllowedRepositories(),
		}
	}
	for _, trigger := range webhook.Triggers() {
		info.Triggers = append(info.Triggers, toWebhookTriggerInfo(trigger))
	}
	if delivery := webhook.LastDelivery(); delivery != nil {
		info.LastDelivery = &DeliveryInfo{
			ID:             delivery.ID(),
			ReceivedAt:     delivery.ReceivedAt(),
			Status:         string(delivery.Status()),
			MatchedTrigger: delivery.MatchedTrigger(),
			SessionID:      delivery.SessionID(),
			Error:          delivery.Error(),
			SessionReused:  delivery.SessionReused(),
		}
	}
	return info
}

func toWebhookTriggerInfo(trigger entities.WebhookTrigger) WebhookTriggerInfo {
	conditions := trigger.Conditions()
	info := WebhookTriggerInfo{
		ID:            trigger.ID(),
		Name:          trigger.Name(),
		Priority:      trigger.Priority(),
		Enabled:       trigger.Enabled(),
		Conditions:    WebhookConditionsInfo{GoTemplate: conditions.GoTemplate()},
		SessionConfig: toSessionConfigInfo(trigger.SessionConfig()),
		StopOnMatch:   trigger.StopOnMatch(),
	}
	if github := conditions.GitHub(); github != nil {
		info.Conditions.GitHub = &GitHubConditionsInfo{
			Events:       github.Events(),
			Actions:      github.Actions(),
			Branches:     github.Branches(),
			Repositories: github.Repositories(),
			Labels:       github.Labels(),
			Paths:        github.Paths(),
			BaseBranches: github.BaseBranches(),
			Draft:        github.Draft(),
			Sender:       github.Sender(),
		}
	}
	return info
}

func toSessionConfigInfo(config *entities.WebhookSessionConfig) *SessionConfigInfo {
	if config == nil {
		return nil
	}
	info := &SessionConfigInfo{
		Environment:            config.Environment(),
		Tags:                   config.Tags(),
		InitialMessageTemplate: config.InitialMessageTemplate(),
		ReuseMessageTemplate:   config.ReuseMessageTemplate(),
		ReuseSession:           config.ReuseSession(),
		MountPayload:           config.MountPayload(),
		MemoryKey:              config.MemoryKey(),
		SessionProfileID:       config.SessionProfileID(),
	}
	if params := config.Params(); params != nil {
		info.Params = &SessionParamsInfo{
			AgentType:    params.AgentType,
			Oneshot:      params.Oneshot,
			RepoFullName: params.RepoFullName,
		}
	}
	return info
}

func toScheduleInfo(item *schedule.Schedule) ScheduleInfo {
	return ScheduleInfo{
		ID:              item.ID,
		Name:            item.Name,
		UserID:          item.UserID,
		Scope:           string(item.GetScope()),
		TeamID:          item.TeamID,
		Status:          string(item.Status),
		ScheduledAt:     item.ScheduledAt,
		CronExpr:        item.CronExpr,
		Timezone:        item.Timezone,
		SessionConfig:   item.SessionConfig,
		LastExecution:   item.LastExecution,
		NextExecutionAt: item.NextExecutionAt,
		ExecutionCount:  item.ExecutionCount,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}

func toSlackBotInfo(bot *entities.SlackBot) SlackBotInfo {
	return SlackBotInfo{
		ID:                     bot.ID(),
		Name:                   bot.Name(),
		UserID:                 bot.UserID(),
		Scope:                  string(bot.Scope()),
		TeamID:                 bot.TeamID(),
		Teams:                  bot.Teams(),
		Status:                 string(bot.Status()),
		BotTokenSecretName:     bot.BotTokenSecretName(),
		BotTokenSecretKey:      bot.BotTokenSecretKey(),
		AppTokenSecretKey:      bot.AppTokenSecretKey(),
		AllowedEventTypes:      bot.AllowedEventTypes(),
		AllowedChannelNames:    bot.AllowedChannelNames(),
		AllowedUserIDs:         bot.AllowedUserIDs(),
		SessionConfig:          toSessionConfigInfo(bot.SessionConfig()),
		MaxSessions:            bot.MaxSessions(),
		NotifyOnSessionCreated: bot.NotifyOnSessionCreated(),
		AllowBotMessages:       bot.AllowBotMessages(),
		CreatedAt:              bot.CreatedAt(),
		UpdatedAt:              bot.UpdatedAt(),
	}
}
