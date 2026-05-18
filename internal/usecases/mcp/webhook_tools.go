package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// MCPWebhookToolsUseCase provides use cases for MCP webhook tools
type MCPWebhookToolsUseCase struct {
	webhookRepo portrepos.WebhookRepository
}

// NewMCPWebhookToolsUseCase creates a new MCPWebhookToolsUseCase
func NewMCPWebhookToolsUseCase(repo portrepos.WebhookRepository) *MCPWebhookToolsUseCase {
	return &MCPWebhookToolsUseCase{webhookRepo: repo}
}

// WebhookSessionConfigInfo represents session configuration for a webhook
type WebhookSessionConfigInfo struct {
	Environment            map[string]string     `json:"environment,omitempty"`
	Tags                   map[string]string     `json:"tags,omitempty"`
	InitialMessageTemplate string                `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                `json:"reuse_message_template,omitempty"`
	Params                 *WebhookSessionParams `json:"params,omitempty"`
	ReuseSession           bool                  `json:"reuse_session,omitempty"`
	MountPayload           bool                  `json:"mount_payload,omitempty"`
	MemoryKey              map[string]string     `json:"memory_key,omitempty"`
}

// WebhookSessionParams represents session params for a webhook
type WebhookSessionParams struct {
	Message      string `json:"message,omitempty"`
	AgentType    string `json:"agent_type,omitempty"`
	Oneshot      bool   `json:"oneshot,omitempty"`
	RepoFullName string `json:"repo_full_name,omitempty"`
}

// WebhookGitHubConfigInfo represents GitHub-specific webhook configuration
type WebhookGitHubConfigInfo struct {
	EnterpriseURL       string   `json:"enterprise_url,omitempty"`
	AllowedEvents       []string `json:"allowed_events,omitempty"`
	AllowedRepositories []string `json:"allowed_repositories,omitempty"`
}

// WebhookGitHubConditionsInfo represents GitHub trigger conditions
type WebhookGitHubConditionsInfo struct {
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

// WebhookTriggerConditionsInfo represents trigger conditions
type WebhookTriggerConditionsInfo struct {
	GitHub     *WebhookGitHubConditionsInfo `json:"github,omitempty"`
	GoTemplate string                       `json:"go_template,omitempty"`
}

// WebhookTriggerInfo represents a webhook trigger
type WebhookTriggerInfo struct {
	ID            string                       `json:"id"`
	Name          string                       `json:"name"`
	Priority      int                          `json:"priority"`
	Enabled       bool                         `json:"enabled"`
	Conditions    WebhookTriggerConditionsInfo `json:"conditions"`
	SessionConfig *WebhookSessionConfigInfo    `json:"session_config,omitempty"`
	StopOnMatch   bool                         `json:"stop_on_match"`
}

// WebhookDeliveryInfo represents a delivery record
type WebhookDeliveryInfo struct {
	ID             string    `json:"id"`
	ReceivedAt     time.Time `json:"received_at"`
	Status         string    `json:"status"`
	MatchedTrigger string    `json:"matched_trigger,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	SessionReused  bool      `json:"session_reused,omitempty"`
}

// WebhookInfo represents webhook information returned by MCP tools
type WebhookInfo struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	UserID          string                    `json:"user_id"`
	Scope           string                    `json:"scope,omitempty"`
	TeamID          string                    `json:"team_id,omitempty"`
	Status          string                    `json:"status"`
	Type            string                    `json:"type"`
	SignatureHeader string                    `json:"signature_header,omitempty"`
	SignatureType   string                    `json:"signature_type,omitempty"`
	GitHub          *WebhookGitHubConfigInfo  `json:"github,omitempty"`
	Triggers        []WebhookTriggerInfo      `json:"triggers"`
	SessionConfig   *WebhookSessionConfigInfo `json:"session_config,omitempty"`
	MaxSessions     int                       `json:"max_sessions"`
	DeliveryCount   int64                     `json:"delivery_count"`
	LastDelivery    *WebhookDeliveryInfo      `json:"last_delivery,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
	UpdatedAt       time.Time                 `json:"updated_at"`
}

// ListWebhooksInput represents input for listing webhooks
type ListWebhooksInput struct {
	Scope  string `json:"scope,omitempty"`
	TeamID string `json:"team_id,omitempty"`
	Status string `json:"status,omitempty"`
	Type   string `json:"type,omitempty"`
}

// CreateWebhookInput represents input for creating a webhook
type CreateWebhookInput struct {
	Name            string                    `json:"name"`
	Scope           string                    `json:"scope,omitempty"`
	TeamID          string                    `json:"team_id,omitempty"`
	Type            string                    `json:"type"`
	Secret          string                    `json:"secret,omitempty"`
	SignatureHeader string                    `json:"signature_header,omitempty"`
	SignatureType   string                    `json:"signature_type,omitempty"`
	GitHub          *WebhookGitHubConfigInfo  `json:"github,omitempty"`
	Triggers        []WebhookTriggerInfo      `json:"triggers"`
	SessionConfig   *WebhookSessionConfigInfo `json:"session_config,omitempty"`
	MaxSessions     int                       `json:"max_sessions,omitempty"`
}

// UpdateWebhookInput represents input for updating a webhook
type UpdateWebhookInput struct {
	Name          *string                   `json:"name,omitempty"`
	Status        *string                   `json:"status,omitempty"`
	Secret        *string                   `json:"secret,omitempty"`
	GitHub        *WebhookGitHubConfigInfo  `json:"github,omitempty"`
	Triggers      []WebhookTriggerInfo      `json:"triggers,omitempty"`
	SessionConfig *WebhookSessionConfigInfo `json:"session_config,omitempty"`
	MaxSessions   *int                      `json:"max_sessions,omitempty"`
}

func toWebhookInfo(w *entities.Webhook) WebhookInfo {
	info := WebhookInfo{
		ID:              w.ID(),
		Name:            w.Name(),
		UserID:          w.UserID(),
		Scope:           string(w.Scope()),
		TeamID:          w.TeamID(),
		Status:          string(w.Status()),
		Type:            string(w.WebhookType()),
		SignatureHeader: w.SignatureHeader(),
		SignatureType:   string(w.SignatureType()),
		MaxSessions:     w.MaxSessions(),
		DeliveryCount:   w.DeliveryCount(),
		CreatedAt:       w.CreatedAt(),
		UpdatedAt:       w.UpdatedAt(),
	}

	if gh := w.GitHub(); gh != nil {
		info.GitHub = &WebhookGitHubConfigInfo{
			EnterpriseURL:       gh.EnterpriseURL(),
			AllowedEvents:       gh.AllowedEvents(),
			AllowedRepositories: gh.AllowedRepositories(),
		}
	}

	for _, t := range w.Triggers() {
		info.Triggers = append(info.Triggers, toWebhookTriggerInfo(t))
	}
	if info.Triggers == nil {
		info.Triggers = []WebhookTriggerInfo{}
	}

	if sc := w.SessionConfig(); sc != nil {
		cfg := toWebhookSessionConfigInfo(sc)
		info.SessionConfig = &cfg
	}

	if d := w.LastDelivery(); d != nil {
		delivery := WebhookDeliveryInfo{
			ID:             d.ID(),
			ReceivedAt:     d.ReceivedAt(),
			Status:         string(d.Status()),
			MatchedTrigger: d.MatchedTrigger(),
			SessionID:      d.SessionID(),
			ErrorMessage:   d.Error(),
			SessionReused:  d.SessionReused(),
		}
		info.LastDelivery = &delivery
	}

	return info
}

func toWebhookTriggerInfo(t entities.WebhookTrigger) WebhookTriggerInfo {
	info := WebhookTriggerInfo{
		ID:          t.ID(),
		Name:        t.Name(),
		Priority:    t.Priority(),
		Enabled:     t.Enabled(),
		StopOnMatch: t.StopOnMatch(),
		Conditions:  WebhookTriggerConditionsInfo{GoTemplate: t.Conditions().GoTemplate()},
	}

	if gh := t.Conditions().GitHub(); gh != nil {
		info.Conditions.GitHub = &WebhookGitHubConditionsInfo{
			Events:       gh.Events(),
			Actions:      gh.Actions(),
			Branches:     gh.Branches(),
			Repositories: gh.Repositories(),
			Labels:       gh.Labels(),
			Paths:        gh.Paths(),
			BaseBranches: gh.BaseBranches(),
			Draft:        gh.Draft(),
			Sender:       gh.Sender(),
		}
	}

	if sc := t.SessionConfig(); sc != nil {
		cfg := toWebhookSessionConfigInfo(sc)
		info.SessionConfig = &cfg
	}

	return info
}

func toWebhookSessionConfigInfo(sc *entities.WebhookSessionConfig) WebhookSessionConfigInfo {
	info := WebhookSessionConfigInfo{
		Environment:            sc.Environment(),
		Tags:                   sc.Tags(),
		InitialMessageTemplate: sc.InitialMessageTemplate(),
		ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
		ReuseSession:           sc.ReuseSession(),
		MountPayload:           sc.MountPayload(),
		MemoryKey:              sc.MemoryKey(),
	}
	if p := sc.Params(); p != nil {
		info.Params = &WebhookSessionParams{
			Message:      p.Message,
			AgentType:    p.AgentType,
			Oneshot:      p.Oneshot,
			RepoFullName: p.RepoFullName,
		}
	}
	return info
}

func fromWebhookSessionConfigInfo(info *WebhookSessionConfigInfo) *entities.WebhookSessionConfig {
	if info == nil {
		return nil
	}
	sc := entities.NewWebhookSessionConfig()
	sc.SetEnvironment(info.Environment)
	sc.SetTags(info.Tags)
	sc.SetInitialMessageTemplate(info.InitialMessageTemplate)
	sc.SetReuseMessageTemplate(info.ReuseMessageTemplate)
	sc.SetReuseSession(info.ReuseSession)
	sc.SetMountPayload(info.MountPayload)
	sc.SetMemoryKey(info.MemoryKey)
	if info.Params != nil {
		sc.SetParams(&entities.SessionParams{
			Message:      info.Params.Message,
			AgentType:    info.Params.AgentType,
			Oneshot:      info.Params.Oneshot,
			RepoFullName: info.Params.RepoFullName,
		})
	}
	return sc
}

func fromWebhookTriggerInfo(t WebhookTriggerInfo) entities.WebhookTrigger {
	id := t.ID
	if id == "" {
		id = uuid.New().String()
	}

	trigger := entities.NewWebhookTrigger(id, t.Name)
	trigger.SetPriority(t.Priority)
	trigger.SetEnabled(t.Enabled)
	trigger.SetStopOnMatch(t.StopOnMatch)

	var conditions entities.WebhookTriggerConditions
	conditions.SetGoTemplate(t.Conditions.GoTemplate)
	if t.Conditions.GitHub != nil {
		gh := entities.NewWebhookGitHubConditions()
		gh.SetEvents(t.Conditions.GitHub.Events)
		gh.SetActions(t.Conditions.GitHub.Actions)
		gh.SetBranches(t.Conditions.GitHub.Branches)
		gh.SetRepositories(t.Conditions.GitHub.Repositories)
		gh.SetLabels(t.Conditions.GitHub.Labels)
		gh.SetPaths(t.Conditions.GitHub.Paths)
		gh.SetBaseBranches(t.Conditions.GitHub.BaseBranches)
		gh.SetDraft(t.Conditions.GitHub.Draft)
		gh.SetSender(t.Conditions.GitHub.Sender)
		conditions.SetGitHub(gh)
	}
	trigger.SetConditions(conditions)

	if t.SessionConfig != nil {
		trigger.SetSessionConfig(fromWebhookSessionConfigInfo(t.SessionConfig))
	}
	return trigger
}

func canAccessWebhook(w *entities.Webhook, requestingUserID string, teamIDs []string) bool {
	switch w.Scope() {
	case entities.ScopeUser:
		return w.UserID() == requestingUserID
	case entities.ScopeTeam:
		return containsTeam(teamIDs, w.TeamID())
	default:
		return false
	}
}

// ListWebhooks lists webhooks for the requesting user
func (uc *MCPWebhookToolsUseCase) ListWebhooks(ctx context.Context, input ListWebhooksInput, requestingUserID string, teamIDs []string) ([]WebhookInfo, error) {
	if uc.webhookRepo == nil {
		return nil, fmt.Errorf("webhook repository not available")
	}

	var webhooks []*entities.Webhook

	switch entities.ResourceScope(input.Scope) {
	case entities.ScopeUser:
		result, err := uc.webhookRepo.List(ctx, portrepos.WebhookFilter{
			UserID: requestingUserID,
			Scope:  entities.ScopeUser,
			Status: entities.WebhookStatus(input.Status),
			Type:   entities.WebhookType(input.Type),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list webhooks: %w", err)
		}
		webhooks = result

	case entities.ScopeTeam:
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
		result, err := uc.webhookRepo.List(ctx, portrepos.WebhookFilter{
			Scope:  entities.ScopeTeam,
			TeamID: input.TeamID,
			Status: entities.WebhookStatus(input.Status),
			Type:   entities.WebhookType(input.Type),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list webhooks: %w", err)
		}
		webhooks = result

	default:
		userResult, err := uc.webhookRepo.List(ctx, portrepos.WebhookFilter{
			UserID: requestingUserID,
			Scope:  entities.ScopeUser,
			Status: entities.WebhookStatus(input.Status),
			Type:   entities.WebhookType(input.Type),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list user webhooks: %w", err)
		}
		var teamResult []*entities.Webhook
		if len(teamIDs) > 0 {
			teamResult, err = uc.webhookRepo.List(ctx, portrepos.WebhookFilter{
				TeamIDs: teamIDs,
				Scope:   entities.ScopeTeam,
				Status:  entities.WebhookStatus(input.Status),
				Type:    entities.WebhookType(input.Type),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to list team webhooks: %w", err)
			}
		}
		webhooks = append(userResult, teamResult...)
	}

	result := make([]WebhookInfo, 0, len(webhooks))
	for _, w := range webhooks {
		result = append(result, toWebhookInfo(w))
	}
	return result, nil
}

// GetWebhook retrieves a webhook by ID
func (uc *MCPWebhookToolsUseCase) GetWebhook(ctx context.Context, webhookID, requestingUserID string, teamIDs []string) (*WebhookInfo, error) {
	if uc.webhookRepo == nil {
		return nil, fmt.Errorf("webhook repository not available")
	}

	w, err := uc.webhookRepo.Get(ctx, webhookID)
	if err != nil {
		return nil, fmt.Errorf("webhook not found: %w", err)
	}

	if !canAccessWebhook(w, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	info := toWebhookInfo(w)
	return &info, nil
}

// CreateWebhook creates a new webhook
func (uc *MCPWebhookToolsUseCase) CreateWebhook(ctx context.Context, input CreateWebhookInput, requestingUserID string, teamIDs []string) (*WebhookInfo, error) {
	if uc.webhookRepo == nil {
		return nil, fmt.Errorf("webhook repository not available")
	}

	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if input.Type != string(entities.WebhookTypeGitHub) && input.Type != string(entities.WebhookTypeCustom) {
		return nil, fmt.Errorf("type must be 'github' or 'custom'")
	}
	if len(input.Triggers) == 0 {
		return nil, fmt.Errorf("at least one trigger is required")
	}

	scope := entities.ResourceScope(input.Scope)
	if scope == "" {
		scope = entities.ScopeUser
	}
	if scope == entities.ScopeTeam {
		if input.TeamID == "" {
			return nil, fmt.Errorf("team_id is required when scope is 'team'")
		}
		if !containsTeam(teamIDs, input.TeamID) {
			return nil, fmt.Errorf("access denied: not a member of the specified team")
		}
	}

	w := entities.NewWebhook(uuid.New().String(), input.Name, requestingUserID, entities.WebhookType(input.Type))
	w.SetScope(scope)
	w.SetTeamID(input.TeamID)
	if input.Secret != "" {
		w.SetSecret(input.Secret)
	}
	if input.SignatureHeader != "" {
		w.SetSignatureHeader(input.SignatureHeader)
	}
	if input.SignatureType != "" {
		w.SetSignatureType(entities.WebhookSignatureType(input.SignatureType))
	}
	if input.MaxSessions > 0 {
		w.SetMaxSessions(input.MaxSessions)
	}
	if scope != entities.ScopeTeam {
		w.SetUserTeams(teamIDs)
	}
	if input.GitHub != nil {
		gh := entities.NewWebhookGitHubConfig()
		gh.SetEnterpriseURL(input.GitHub.EnterpriseURL)
		gh.SetAllowedEvents(input.GitHub.AllowedEvents)
		gh.SetAllowedRepositories(input.GitHub.AllowedRepositories)
		w.SetGitHub(gh)
	}

	triggers := make([]entities.WebhookTrigger, 0, len(input.Triggers))
	for _, t := range input.Triggers {
		triggers = append(triggers, fromWebhookTriggerInfo(t))
	}
	w.SetTriggers(triggers)

	if input.SessionConfig != nil {
		w.SetSessionConfig(fromWebhookSessionConfigInfo(input.SessionConfig))
	}

	if err := uc.webhookRepo.Create(ctx, w); err != nil {
		return nil, fmt.Errorf("failed to create webhook: %w", err)
	}

	info := toWebhookInfo(w)
	return &info, nil
}

// UpdateWebhook updates an existing webhook
func (uc *MCPWebhookToolsUseCase) UpdateWebhook(ctx context.Context, webhookID string, input UpdateWebhookInput, requestingUserID string, teamIDs []string) (*WebhookInfo, error) {
	if uc.webhookRepo == nil {
		return nil, fmt.Errorf("webhook repository not available")
	}

	w, err := uc.webhookRepo.Get(ctx, webhookID)
	if err != nil {
		return nil, fmt.Errorf("webhook not found: %w", err)
	}

	if !canAccessWebhook(w, requestingUserID, teamIDs) {
		return nil, fmt.Errorf("access denied")
	}

	if input.Name != nil {
		if *input.Name == "" {
			return nil, fmt.Errorf("name cannot be empty")
		}
		w.SetName(*input.Name)
	}
	if input.Status != nil {
		w.SetStatus(entities.WebhookStatus(*input.Status))
	}
	if input.Secret != nil {
		w.SetSecret(*input.Secret)
	}
	if input.GitHub != nil {
		gh := entities.NewWebhookGitHubConfig()
		gh.SetEnterpriseURL(input.GitHub.EnterpriseURL)
		gh.SetAllowedEvents(input.GitHub.AllowedEvents)
		gh.SetAllowedRepositories(input.GitHub.AllowedRepositories)
		w.SetGitHub(gh)
	}
	if input.Triggers != nil {
		triggers := make([]entities.WebhookTrigger, 0, len(input.Triggers))
		for _, t := range input.Triggers {
			triggers = append(triggers, fromWebhookTriggerInfo(t))
		}
		w.SetTriggers(triggers)
	}
	if input.SessionConfig != nil {
		w.SetSessionConfig(fromWebhookSessionConfigInfo(input.SessionConfig))
	}
	if input.MaxSessions != nil {
		w.SetMaxSessions(*input.MaxSessions)
	}
	w.SetUpdatedAt(time.Now())

	if err := uc.webhookRepo.Update(ctx, w); err != nil {
		return nil, fmt.Errorf("failed to update webhook: %w", err)
	}

	info := toWebhookInfo(w)
	return &info, nil
}

// DeleteWebhook deletes a webhook
func (uc *MCPWebhookToolsUseCase) DeleteWebhook(ctx context.Context, webhookID, requestingUserID string, teamIDs []string) error {
	if uc.webhookRepo == nil {
		return fmt.Errorf("webhook repository not available")
	}

	w, err := uc.webhookRepo.Get(ctx, webhookID)
	if err != nil {
		return fmt.Errorf("webhook not found: %w", err)
	}

	if !canAccessWebhook(w, requestingUserID, teamIDs) {
		return fmt.Errorf("access denied")
	}

	return uc.webhookRepo.Delete(ctx, webhookID)
}
