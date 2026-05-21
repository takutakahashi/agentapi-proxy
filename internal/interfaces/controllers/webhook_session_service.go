package controllers

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"text/template"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
)

// WebhookSessionService encapsulates common logic shared between
// GitHub and Custom webhook controllers: session creation, session reuse,
// template rendering, trigger sorting, session limits, and delivery recording.
type WebhookSessionService struct {
	repo           repositories.WebhookRepository
	sessionManager repositories.SessionManager
	launcher       *sessionuc.LaunchUseCase
}

// NewWebhookSessionService creates a new WebhookSessionService.
func NewWebhookSessionService(repo repositories.WebhookRepository, sessionManager repositories.SessionManager, memoryRepo repositories.MemoryRepository) *WebhookSessionService {
	return &WebhookSessionService{
		repo:           repo,
		sessionManager: sessionManager,
		launcher:       sessionuc.NewLaunchUseCase(sessionManager).WithMemoryRepository(memoryRepo),
	}
}

// SessionCreationParams holds all parameters needed to create a session from a webhook delivery.
type SessionCreationParams struct {
	Webhook        *entities.Webhook
	Trigger        *entities.WebhookTrigger
	Payload        map[string]interface{}
	RawPayload     []byte
	Tags           map[string]string
	DefaultMessage string
	MountPayload   bool
}

// CreateSessionFromWebhook creates or reuses a session based on webhook and trigger configuration.
// Returns sessionID, sessionReused flag, and error.
//
// The ctx parameter is a plain context.Context (not echo.Context) so that this method can be
// called from background goroutines or unit tests without an HTTP request.
func (s *WebhookSessionService) CreateSessionFromWebhook(ctx context.Context, params SessionCreationParams) (string, bool, error) {
	webhook := params.Webhook
	trigger := params.Trigger

	sessionConfig := MergeSessionConfigs(webhook.SessionConfig(), trigger.SessionConfig())

	env, err := s.renderConfigMap(sessionConfig, params.Payload, func(sc *entities.WebhookSessionConfig) map[string]string {
		return sc.Environment()
	})
	if err != nil {
		return "", false, fmt.Errorf("failed to render environment variables: %w", err)
	}

	tags, err := s.renderConfigMap(sessionConfig, params.Payload, func(sc *entities.WebhookSessionConfig) map[string]string {
		return sc.Tags()
	})
	if err != nil {
		return "", false, fmt.Errorf("failed to render tags: %w", err)
	}

	// Merge caller-provided tags (webhook-type-specific metadata)
	for k, v := range params.Tags {
		tags[k] = v
	}

	// Render session params with template evaluation
	renderedParams, err := RenderSessionParams(sessionConfig, params.Payload)
	if err != nil {
		return "", false, fmt.Errorf("failed to render session params: %w", err)
	}

	initialMessage, err := s.determineInitialMessage(sessionConfig, renderedParams, params.Payload, params.DefaultMessage)
	if err != nil {
		return "", false, err
	}

	// Determine session params fields from rendered params
	var githubToken, agentType string
	var oneshot bool
	if renderedParams != nil {
		githubToken = renderedParams.GithubToken
		agentType = renderedParams.AgentType
		oneshot = renderedParams.Oneshot
	}

	// Build repository info from tags
	sessionID := uuid.New().String()
	var repoInfo *entities.RepositoryInfo
	if repoFullName, ok := tags["repository"]; ok && repoFullName != "" {
		repoInfo = &entities.RepositoryInfo{
			FullName: repoFullName,
			CloneDir: sessionID,
		}
	}

	// Determine whether to mount the webhook payload
	var webhookPayload []byte
	if params.MountPayload || (sessionConfig != nil && sessionConfig.MountPayload()) {
		webhookPayload = params.RawPayload
	}

	// Resolve the reuse message (for existing-session route)
	var reuseMessage string
	if sessionConfig != nil && sessionConfig.ReuseMessageTemplate() != "" {
		if rendered, renderErr := RenderTemplate(sessionConfig.ReuseMessageTemplate(), params.Payload); renderErr == nil {
			reuseMessage = rendered
		} else {
			log.Printf("[WEBHOOK] Failed to render reuse message template: %v", renderErr)
		}
	}

	// Delegate reuse, limit-check, and session creation to LaunchUseCase.
	// Teams is resolved here so it is never accidentally omitted (fixes the bug where
	// webhook-triggered sessions were created without team-level settings injection).
	result, err := s.launcher.Launch(ctx, sessionID, sessionuc.LaunchRequest{
		UserID:         webhook.UserID(),
		Scope:          webhook.Scope(),
		TeamID:         webhook.TeamID(),
		Teams:          sessionuc.ResolveTeams(webhook.Scope(), webhook.TeamID(), webhook.UserTeams()),
		Environment:    env,
		Tags:           tags,
		InitialMessage: initialMessage,
		GithubToken:    githubToken,
		AgentType:      agentType,
		Oneshot:        oneshot,
		RepoInfo:       repoInfo,
		WebhookPayload: webhookPayload,
		ReuseSession:   sessionConfig != nil && sessionConfig.ReuseSession(),
		ReuseMatchTags: tags,
		ReuseMessage:   reuseMessage,
		MaxSessions:    webhook.MaxSessions(),
		LimitMatchTags: map[string]string{"webhook_id": webhook.ID()},
	})
	if err != nil {
		return "", false, fmt.Errorf("failed to create session: %w", err)
	}

	return result.SessionID, result.SessionReused, nil
}

// RecordDelivery records a webhook delivery event.
func (s *WebhookSessionService) RecordDelivery(ctx context.Context, webhookID, deliveryID string, status entities.DeliveryStatus, trigger *entities.WebhookTrigger, sessionID string, sessionReused bool, deliveryErr error) {
	record := entities.NewWebhookDeliveryRecord(deliveryID, status)
	if trigger != nil {
		record.SetMatchedTrigger(trigger.ID())
	}
	if sessionID != "" {
		record.SetSessionID(sessionID)
	}
	record.SetSessionReused(sessionReused)
	if deliveryErr != nil {
		record.SetError(deliveryErr.Error())
	}
	if err := s.repo.RecordDelivery(ctx, webhookID, record); err != nil {
		log.Printf("[WEBHOOK] Failed to record delivery: %v", err)
	}
}

// SortTriggersByPriority returns a copy of triggers sorted by priority (lower number = higher priority).
func SortTriggersByPriority(triggers []entities.WebhookTrigger) []entities.WebhookTrigger {
	sorted := make([]entities.WebhookTrigger, len(triggers))
	copy(sorted, triggers)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority() < sorted[j].Priority()
	})
	return sorted
}

// MergeSessionConfigs merges two session configs, with override taking precedence over base.
func MergeSessionConfigs(base, override *entities.WebhookSessionConfig) *entities.WebhookSessionConfig {
	if base == nil && override == nil {
		return nil
	}
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	result := entities.NewWebhookSessionConfig()

	// Merge maps (override wins on key conflicts)
	result.SetEnvironment(mergeMaps(base.Environment(), override.Environment()))
	result.SetTags(mergeMaps(base.Tags(), override.Tags()))

	// Override scalar fields
	result.SetInitialMessageTemplate(firstNonEmpty(override.InitialMessageTemplate(), base.InitialMessageTemplate()))
	result.SetReuseMessageTemplate(firstNonEmpty(override.ReuseMessageTemplate(), base.ReuseMessageTemplate()))

	if override.Params() != nil {
		result.SetParams(override.Params())
	} else {
		result.SetParams(base.Params())
	}

	result.SetReuseSession(base.ReuseSession() || override.ReuseSession())
	result.SetMountPayload(base.MountPayload() || override.MountPayload())

	return result
}

// RenderTemplate renders a Go template with a payload data map.
func RenderTemplate(tmplStr string, payload map[string]interface{}) (string, error) {
	tmpl, err := template.New("webhook").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, payload); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// RenderTemplateMap renders all template values in a map.
func RenderTemplateMap(templates map[string]string, payload map[string]interface{}) (map[string]string, error) {
	result := make(map[string]string, len(templates))
	for key, tmplStr := range templates {
		rendered, err := RenderTemplate(tmplStr, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to render template for key '%s': %w", key, err)
		}
		result[key] = rendered
	}
	return result, nil
}

// RenderSessionParams renders all template fields in session params.
func RenderSessionParams(sessionConfig *entities.WebhookSessionConfig, payload map[string]interface{}) (*entities.SessionParams, error) {
	if sessionConfig == nil || sessionConfig.Params() == nil {
		return nil, nil
	}

	params := sessionConfig.Params()
	result := &entities.SessionParams{
		Oneshot: params.Oneshot,
	}

	fields := []struct {
		src  string
		dest *string
		name string
	}{
		{params.Message, &result.Message, "params.message"},
		{params.GithubToken, &result.GithubToken, "params.github_token"},
		{params.AgentType, &result.AgentType, "params.agent_type"},
	}

	for _, f := range fields {
		if f.src == "" {
			continue
		}
		rendered, err := RenderTemplate(f.src, payload)
		if err != nil {
			return nil, fmt.Errorf("failed to render template for %s: %w", f.name, err)
		}
		*f.dest = rendered
	}

	return result, nil
}

// IsSessionLimitError checks whether an error represents a session limit being reached.
func IsSessionLimitError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "session limit reached")
}

// DryRunResult holds the computed configuration from a dry-run test.
type DryRunResult struct {
	InitialMessage string
	Tags           map[string]string
	Environment    map[string]string
	Error          string
}

// DryRunSessionConfig evaluates all template rendering and config merging
// without creating a session. Returns the computed configuration.
func (s *WebhookSessionService) DryRunSessionConfig(params SessionCreationParams) (*DryRunResult, error) {
	webhook := params.Webhook
	trigger := params.Trigger

	sessionConfig := MergeSessionConfigs(webhook.SessionConfig(), trigger.SessionConfig())

	env, err := s.renderConfigMap(sessionConfig, params.Payload, func(sc *entities.WebhookSessionConfig) map[string]string {
		return sc.Environment()
	})
	if err != nil {
		return &DryRunResult{Error: fmt.Sprintf("failed to render environment variables: %v", err)}, nil
	}

	tags, err := s.renderConfigMap(sessionConfig, params.Payload, func(sc *entities.WebhookSessionConfig) map[string]string {
		return sc.Tags()
	})
	if err != nil {
		return &DryRunResult{Error: fmt.Sprintf("failed to render tags: %v", err)}, nil
	}

	// Merge caller-provided tags
	for k, v := range params.Tags {
		tags[k] = v
	}

	renderedParams, err := RenderSessionParams(sessionConfig, params.Payload)
	if err != nil {
		return &DryRunResult{Error: fmt.Sprintf("failed to render session params: %v", err)}, nil
	}

	initialMessage, err := s.determineInitialMessage(sessionConfig, renderedParams, params.Payload, params.DefaultMessage)
	if err != nil {
		return &DryRunResult{Error: fmt.Sprintf("failed to render initial message: %v", err)}, nil
	}

	return &DryRunResult{
		InitialMessage: initialMessage,
		Tags:           tags,
		Environment:    env,
	}, nil
}

// renderConfigMap renders either environment or tags from session config.
func (s *WebhookSessionService) renderConfigMap(
	sessionConfig *entities.WebhookSessionConfig,
	payload map[string]interface{},
	getter func(*entities.WebhookSessionConfig) map[string]string,
) (map[string]string, error) {
	if sessionConfig == nil {
		return make(map[string]string), nil
	}
	values := getter(sessionConfig)
	if values == nil {
		return make(map[string]string), nil
	}
	return RenderTemplateMap(values, payload)
}

// determineInitialMessage determines the initial message to use for a session.
// Priority: params.message > initial_message_template > defaultMessage
func (s *WebhookSessionService) determineInitialMessage(
	sessionConfig *entities.WebhookSessionConfig,
	renderedParams *entities.SessionParams,
	payload map[string]interface{},
	defaultMessage string,
) (string, error) {
	if renderedParams != nil && renderedParams.Message != "" {
		return renderedParams.Message, nil
	}

	if sessionConfig != nil && sessionConfig.InitialMessageTemplate() != "" {
		msg, err := RenderTemplate(sessionConfig.InitialMessageTemplate(), payload)
		if err != nil {
			return "", fmt.Errorf("failed to render initial message template: %w", err)
		}
		return msg, nil
	}

	return defaultMessage, nil
}

// Helper functions

func mergeMaps(base, override map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(override))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
