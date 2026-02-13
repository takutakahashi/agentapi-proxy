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
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// WebhookSessionService encapsulates common logic shared between
// GitHub and Custom webhook controllers: session creation, session reuse,
// template rendering, trigger sorting, session limits, and delivery recording.
type WebhookSessionService struct {
	repo           repositories.WebhookRepository
	sessionManager repositories.SessionManager
}

// NewWebhookSessionService creates a new WebhookSessionService.
func NewWebhookSessionService(repo repositories.WebhookRepository, sessionManager repositories.SessionManager) *WebhookSessionService {
	return &WebhookSessionService{
		repo:           repo,
		sessionManager: sessionManager,
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
func (s *WebhookSessionService) CreateSessionFromWebhook(ctx echo.Context, params SessionCreationParams) (string, bool, error) {
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

	// Try to reuse an existing session
	if sessionConfig != nil && sessionConfig.ReuseSession() {
		sessionID, reused, reuseErr := s.tryReuseSession(ctx, sessionConfig, tags, initialMessage, params.Payload)
		if reuseErr == nil && reused {
			return sessionID, true, nil
		}
		if reuseErr != nil {
			log.Printf("[WEBHOOK] Failed to reuse session: %v, creating new session instead", reuseErr)
		}
	}

	// Check session limit
	if err := s.checkSessionLimit(webhook); err != nil {
		return "", false, err
	}

	// Build and create the session
	sessionID := uuid.New().String()
	req := &entities.RunServerRequest{
		UserID:         webhook.UserID(),
		Environment:    env,
		Tags:           tags,
		Scope:          webhook.Scope(),
		TeamID:         webhook.TeamID(),
		InitialMessage: initialMessage,
	}

	if renderedParams != nil {
		if renderedParams.GithubToken != "" {
			req.GithubToken = renderedParams.GithubToken
		}
		if renderedParams.AgentType != "" {
			req.AgentType = renderedParams.AgentType
		}
		req.Oneshot = renderedParams.Oneshot
	}

	// Set repository info from tags
	if repoFullName, ok := tags["repository"]; ok && repoFullName != "" {
		req.RepoInfo = &entities.RepositoryInfo{
			FullName: repoFullName,
			CloneDir: sessionID,
		}
	}

	// Determine whether to mount the webhook payload
	var webhookPayload []byte
	if params.MountPayload || (sessionConfig != nil && sessionConfig.MountPayload()) {
		webhookPayload = params.RawPayload
	}

	session, err := s.sessionManager.CreateSession(ctx.Request().Context(), sessionID, req, webhookPayload)
	if err != nil {
		return "", false, fmt.Errorf("failed to create session: %w", err)
	}

	return session.ID(), false, nil
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

// tryReuseSession attempts to find and reuse an existing session.
func (s *WebhookSessionService) tryReuseSession(
	ctx echo.Context,
	sessionConfig *entities.WebhookSessionConfig,
	tags map[string]string,
	initialMessage string,
	payload map[string]interface{},
) (string, bool, error) {
	log.Printf("[WEBHOOK] Session reuse is enabled, searching for existing session with tags: %v", tags)

	filter := entities.SessionFilter{
		Tags:   tags,
		Status: "active",
	}
	existingSessions := s.sessionManager.ListSessions(filter)
	log.Printf("[WEBHOOK] Found %d existing sessions matching filter", len(existingSessions))

	if len(existingSessions) == 0 {
		log.Printf("[WEBHOOK] No existing sessions found with matching tags, creating new session")
		return "", false, fmt.Errorf("no existing sessions found")
	}

	existingSession := existingSessions[0]
	log.Printf("[WEBHOOK] Reusing existing session %s", existingSession.ID())

	reuseMessage := initialMessage
	if sessionConfig.ReuseMessageTemplate() != "" {
		msg, err := RenderTemplate(sessionConfig.ReuseMessageTemplate(), payload)
		if err != nil {
			log.Printf("[WEBHOOK] Failed to render reuse message template: %v", err)
		} else {
			reuseMessage = msg
		}
	}

	if err := s.sessionManager.SendMessage(ctx.Request().Context(), existingSession.ID(), reuseMessage); err != nil {
		return "", false, fmt.Errorf("failed to send message to existing session: %w", err)
	}

	return existingSession.ID(), true, nil
}

// checkSessionLimit verifies the webhook has not exceeded its maximum session count.
func (s *WebhookSessionService) checkSessionLimit(webhook *entities.Webhook) error {
	filter := entities.SessionFilter{
		Tags: map[string]string{
			"webhook_id": webhook.ID(),
		},
	}
	existingSessions := s.sessionManager.ListSessions(filter)
	if len(existingSessions) >= webhook.MaxSessions() {
		return fmt.Errorf("session limit reached: maximum %d sessions per webhook", webhook.MaxSessions())
	}
	return nil
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
