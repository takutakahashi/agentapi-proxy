package webhook

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/core/configrender"
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
func NewWebhookSessionService(repo repositories.WebhookRepository, sessionManager repositories.SessionManager, memoryRepo repositories.MemoryRepository, sessionProfileRepo repositories.SessionProfileRepository) *WebhookSessionService {
	return &WebhookSessionService{
		repo:           repo,
		sessionManager: sessionManager,
		launcher: sessionuc.NewLaunchUseCase(sessionManager).
			WithMemoryRepository(memoryRepo).
			WithSessionProfileRepository(sessionProfileRepo),
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

	sessionConfig := configrender.MergeSessionConfigs(webhook.SessionConfig(), trigger.SessionConfig())

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
	renderedParams, err := configrender.RenderSessionParams(sessionConfig, params.Payload)
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
	var initialMessageWaitSecond *int
	var cycleMessage, sessionTTL string
	var cycleMaxCount int
	if renderedParams != nil {
		githubToken = renderedParams.GithubToken
		agentType = renderedParams.AgentType
		oneshot = renderedParams.Oneshot
		initialMessageWaitSecond = renderedParams.InitialMessageWaitSecond
		cycleMessage = renderedParams.CycleMessage
		cycleMaxCount = renderedParams.CycleMaxCount
		sessionTTL = renderedParams.SessionTTL
	}

	// Sandbox is not a template field — read directly from the merged session config params.
	var sandbox *entities.SandboxParams
	var docker *entities.DockerParams
	var authProxy *bool
	if sessionConfig != nil && sessionConfig.Params() != nil {
		sandbox = sessionConfig.Params().Sandbox
		docker = sessionConfig.Params().Docker
		authProxy = sessionConfig.Params().AuthProxy
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
		if rendered, renderErr := configrender.RenderTemplate(sessionConfig.ReuseMessageTemplate(), params.Payload); renderErr == nil {
			reuseMessage = rendered
		} else {
			log.Printf("[WEBHOOK] Failed to render reuse message template: %v", renderErr)
		}
	}

	// Delegate reuse, limit-check, and session creation to LaunchUseCase.
	// Teams is resolved here so it is never accidentally omitted (fixes the bug where
	// webhook-triggered sessions were created without team-level settings injection).
	var sessionProfileID string
	if sessionConfig != nil {
		sessionProfileID = sessionConfig.SessionProfileID()
	}
	result, err := s.launcher.Launch(ctx, sessionID, sessionuc.LaunchRequest{
		UserID:                   webhook.UserID(),
		Scope:                    webhook.Scope(),
		TeamID:                   webhook.TeamID(),
		Teams:                    sessionuc.ResolveTeams(webhook.Scope(), webhook.TeamID(), webhook.UserTeams()),
		Environment:              env,
		Tags:                     tags,
		InitialMessage:           initialMessage,
		GithubToken:              githubToken,
		AgentType:                agentType,
		Oneshot:                  oneshot,
		InitialMessageWaitSecond: initialMessageWaitSecond,
		CycleMessage:             cycleMessage,
		CycleMaxCount:            cycleMaxCount,
		Sandbox:                  sandbox,
		Docker:                   docker,
		AuthProxy:                authProxy,
		SessionTTL:               sessionTTL,
		RepoInfo:                 repoInfo,
		WebhookPayload:           webhookPayload,
		SessionProfileID:         sessionProfileID,
		ReuseSession:             sessionConfig != nil && sessionConfig.ReuseSession(),
		ReuseMatchTags:           tags,
		ReuseMessage:             reuseMessage,
		StopBeforeReuse:          true,
		MaxSessions:              webhook.MaxSessions(),
		LimitMatchTags:           map[string]string{"webhook_id": webhook.ID()},
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

	sessionConfig := configrender.MergeSessionConfigs(webhook.SessionConfig(), trigger.SessionConfig())

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

	renderedParams, err := configrender.RenderSessionParams(sessionConfig, params.Payload)
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
	return configrender.RenderTemplateMap(values, payload)
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
		msg, err := configrender.RenderTemplate(sessionConfig.InitialMessageTemplate(), payload)
		if err != nil {
			return "", fmt.Errorf("failed to render initial message template: %w", err)
		}
		return msg, nil
	}

	return defaultMessage, nil
}
