package controllers

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// slackBotDefaultID is the special ID for the server-configured default SlackBot
	slackBotDefaultID = "default"
	// defaultSlackAgentType is the default agent type for SlackBot sessions
	defaultSlackAgentType = "claude-agentapi"
)

// SlackBotEventHandler handles incoming Slack events (via Socket Mode) and manages sessions
type SlackBotEventHandler struct {
	repo            repositories.SlackBotRepository
	sessionManager  repositories.SessionManager
	channelResolver *services.SlackChannelResolver
	// Default SlackBot configuration (from server startup config)
	defaultSigningSecret      string
	defaultBotTokenSecretName string
	defaultBotTokenSecretKey  string
	// baseURL is used to construct session URLs posted back to Slack threads.
	// If empty, NOTIFICATION_BASE_URL env var is checked as a fallback.
	baseURL string
}

// NewSlackBotEventHandler creates a new SlackBotEventHandler
func NewSlackBotEventHandler(
	repo repositories.SlackBotRepository,
	sessionManager repositories.SessionManager,
	defaultSigningSecret string,
	defaultBotTokenSecretName string,
	defaultBotTokenSecretKey string,
	channelResolver *services.SlackChannelResolver,
	baseURL string,
) *SlackBotEventHandler {
	return &SlackBotEventHandler{
		repo:                      repo,
		sessionManager:            sessionManager,
		channelResolver:           channelResolver,
		defaultSigningSecret:      defaultSigningSecret,
		defaultBotTokenSecretName: defaultBotTokenSecretName,
		defaultBotTokenSecretKey:  defaultBotTokenSecretKey,
		baseURL:                   baseURL,
	}
}

// SlackPayload represents the outer Slack event payload structure
type SlackPayload struct {
	Type      string      `json:"type"`
	Challenge string      `json:"challenge,omitempty"`
	TeamID    string      `json:"team_id,omitempty"`
	Event     *SlackEvent `json:"event,omitempty"`
}

// SlackEvent represents the inner Slack event
type SlackEvent struct {
	Type     string `json:"type"`
	SubType  string `json:"subtype,omitempty"`
	BotID    string `json:"bot_id,omitempty"`
	Text     string `json:"text"`
	User     string `json:"user"`
	Channel  string `json:"channel"`
	Ts       string `json:"ts"`
	ThreadTs string `json:"thread_ts,omitempty"`
}

// ProcessEvent processes a parsed Slack event received via Socket Mode.
// botID should be the SlackBot entity ID or slackBotDefaultID ("default").
// This method is called by SlackSocketWorker after acknowledging the event to Slack.
func (h *SlackBotEventHandler) ProcessEvent(ctx context.Context, botID string, payload SlackPayload) error {
	log.Printf("[SLACKBOT] ProcessEvent called: botID=%s, type=%s", botID, payload.Type)
	// We only process event_callback type
	if payload.Type != "event_callback" || payload.Event == nil {
		log.Printf("[SLACKBOT] Ignoring non-event payload: id=%s, type=%s", botID, payload.Type)
		return nil
	}

	event := payload.Event

	// Ignore messages posted by bots (including this bot itself) to prevent
	// recursive session creation: bot posts "session created" → triggers another event
	// → creates another session → infinite loop.
	if event.BotID != "" || event.SubType == "bot_message" {
		log.Printf("[SLACKBOT] Ignoring bot message: botID=%s, event.bot_id=%s, subtype=%s", botID, event.BotID, event.SubType)
		return nil
	}

	// Resolve the bot entity (nil for "default" when no registered bot matches)
	_, bot, err := h.resolveSlackBot(ctx, botID)
	if err != nil {
		return fmt.Errorf("failed to resolve slackbot: %w", err)
	}

	// For default ID: try to identify the registered bot by channel name filter.
	if botID == slackBotDefaultID && bot == nil {
		if resolvedBot := h.resolveBotByChannel(ctx, event.Channel); resolvedBot != nil {
			bot = resolvedBot
			botID = resolvedBot.ID()
			log.Printf("[SLACKBOT] Default endpoint: identified bot by channel filter: id=%s, channel=%s", botID, event.Channel)
		}
	}

	// Apply filters (if this is a registered bot, not default)
	if bot != nil {
		if bot.Status() == entities.SlackBotStatusPaused {
			log.Printf("[SLACKBOT] Bot is paused: id=%s", botID)
			return nil
		}
		if !bot.IsEventTypeAllowed(event.Type) {
			log.Printf("[SLACKBOT] Event type not allowed: id=%s, type=%s", botID, event.Type)
			return nil
		}
		// Channel name filter: resolve channel ID → name, then apply partial-match filter
		if len(bot.AllowedChannelNames()) > 0 && h.channelResolver != nil {
			secretName := bot.BotTokenSecretName()
			if secretName == "" {
				secretName = h.defaultBotTokenSecretName
			}
			secretKey := bot.BotTokenSecretKey()
			if secretKey == "" {
				secretKey = h.defaultBotTokenSecretKey
			}
			botToken, tokenErr := h.channelResolver.GetBotToken(ctx, secretName, secretKey)
			if tokenErr != nil {
				log.Printf("[SLACKBOT] Failed to get bot token for channel filter: id=%s, err=%v", botID, tokenErr)
				return fmt.Errorf("failed to get bot token: %w", tokenErr)
			}
			channelName, resolveErr := h.channelResolver.ResolveChannelName(ctx, event.Channel, botToken)
			if resolveErr != nil {
				log.Printf("[SLACKBOT] Failed to resolve channel name: id=%s, channel=%s, err=%v", botID, event.Channel, resolveErr)
				// Non-fatal: skip filter and allow the event through
			} else if !bot.IsChannelNameAllowed(channelName) {
				log.Printf("[SLACKBOT] Channel name not allowed: id=%s, channel=%s, name=%s", botID, event.Channel, channelName)
				return nil
			}
		}
	}

	// Normalize thread key: use thread_ts if present (indicates a reply), otherwise use ts (root message)
	threadKey := event.ThreadTs
	if threadKey == "" {
		threadKey = event.Ts
	}

	channel := event.Channel
	log.Printf("[SLACKBOT] Processing event: id=%s, type=%s, channel=%s, thread=%s", botID, event.Type, channel, threadKey)

	// Build payload map for template rendering
	payloadMap := map[string]interface{}{
		"event": map[string]interface{}{
			"type":      event.Type,
			"text":      event.Text,
			"user":      event.User,
			"channel":   event.Channel,
			"ts":        event.Ts,
			"thread_ts": event.ThreadTs,
		},
		"team_id": payload.TeamID,
	}

	// Build session tags
	tags := map[string]string{
		"slackbot_id":     botID,
		"slack_channel":   channel,
		"slack_thread_ts": threadKey,
	}

	// Apply session config tags if present
	if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Tags() != nil {
		renderedTags, err := RenderTemplateMap(bot.SessionConfig().Tags(), payloadMap)
		if err != nil {
			log.Printf("[SLACKBOT] Failed to render tags: %v", err)
		} else {
			for k, v := range renderedTags {
				tags[k] = v
			}
		}
	}

	// Build environment variables
	var env map[string]string
	if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Environment() != nil {
		env, err = RenderTemplateMap(bot.SessionConfig().Environment(), payloadMap)
		if err != nil {
			log.Printf("[SLACKBOT] Failed to render environment: %v", err)
			env = nil
		}
	}

	// Build initial message
	initialMessage := h.buildMessage(bot, payloadMap, event.Text, false)

	// Determine agent type
	agentType := defaultSlackAgentType
	if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Params() != nil {
		if bot.SessionConfig().Params().AgentType != "" {
			agentType = bot.SessionConfig().Params().AgentType
		}
	}

	// Check for duplicate session: if a session already exists for this channel+thread,
	// skip creation to avoid multiple sessions triggered by subsequent messages in the
	// same thread (e.g. replies after the initial message).
	dupFilter := entities.SessionFilter{
		Tags: map[string]string{
			"slack_channel":   channel,
			"slack_thread_ts": threadKey,
		},
	}
	if existing := h.sessionManager.ListSessions(dupFilter); len(existing) > 0 {
		log.Printf("[SLACKBOT] Session already exists for channel=%s thread=%s, skipping", channel, threadKey)
		return nil
	}

	// Check session limit
	if bot != nil {
		limitFilter := entities.SessionFilter{
			Tags: map[string]string{"slackbot_id": botID},
		}
		activeSessions := h.sessionManager.ListSessions(limitFilter)
		if len(activeSessions) >= bot.MaxSessions() {
			log.Printf("[SLACKBOT] Session limit reached: id=%s, limit=%d", botID, bot.MaxSessions())
			return fmt.Errorf("session limit reached: maximum %d sessions", bot.MaxSessions())
		}
	}

	// Determine scope and ownership
	scope := entities.ScopeUser
	userID := ""
	teamID := ""
	if bot != nil {
		scope = bot.Scope()
		userID = bot.UserID()
		teamID = bot.TeamID()
	}

	sessionID := uuid.New().String()
	req := &entities.RunServerRequest{
		UserID:         userID,
		Environment:    env,
		Tags:           tags,
		Scope:          scope,
		TeamID:         teamID,
		InitialMessage: initialMessage,
		AgentType:      agentType,
		SlackParams: &entities.SlackParams{
			Channel:  channel,
			ThreadTS: threadKey,
		},
	}

	// Create session asynchronously so we don't block event processing
	go func() {
		bgCtx := context.Background()
		session, err := h.sessionManager.CreateSession(bgCtx, sessionID, req, nil)
		if err != nil {
			log.Printf("[SLACKBOT] Failed to create session: %v", err)
			return
		}
		log.Printf("[SLACKBOT] Created session %s for thread %s", session.ID(), threadKey)
		h.postSessionURLToSlack(bgCtx, channel, threadKey, session.ID(), bot)
	}()

	return nil
}

// resolveSlackBot retrieves the signing secret and optionally the SlackBot entity.
// Returns (signingSecret, bot, error)
// For id="default", returns the server-configured secret and nil bot.
func (h *SlackBotEventHandler) resolveSlackBot(ctx context.Context, id string) (string, *entities.SlackBot, error) {
	if id == slackBotDefaultID {
		return h.defaultSigningSecret, nil, nil
	}

	bot, err := h.repo.Get(ctx, id)
	if err != nil {
		return "", nil, fmt.Errorf("slackbot not found: %s", id)
	}
	return bot.SigningSecret(), bot, nil
}

// resolveBotByChannel attempts to identify a registered SlackBot by the Slack channel ID.
// It resolves the channel ID to a name using the server-default bot token, then
// searches active bots (those using default credentials) whose AllowedChannelNames matches.
// Returns nil if the bot cannot be identified.
func (h *SlackBotEventHandler) resolveBotByChannel(ctx context.Context, channelID string) *entities.SlackBot {
	if h.channelResolver == nil || h.defaultBotTokenSecretName == "" {
		return nil
	}
	botToken, err := h.channelResolver.GetBotToken(
		ctx,
		h.defaultBotTokenSecretName,
		h.defaultBotTokenSecretKey,
	)
	if err != nil {
		log.Printf("[SLACKBOT] resolveBotByChannel: failed to get default bot token: %v", err)
		return nil
	}
	channelName, err := h.channelResolver.ResolveChannelName(ctx, channelID, botToken)
	if err != nil {
		log.Printf("[SLACKBOT] resolveBotByChannel: failed to resolve channel name: channelID=%s, err=%v", channelID, err)
		return nil
	}
	allBots, err := h.repo.List(ctx, repositories.SlackBotFilter{})
	if err != nil {
		log.Printf("[SLACKBOT] resolveBotByChannel: failed to list bots: %v", err)
		return nil
	}
	for _, candidate := range allBots {
		// Only match bots that rely on the default bot token
		if candidate.BotTokenSecretName() != "" {
			continue
		}
		// Must have at least one AllowedChannelName to be identifiable via the default endpoint
		if len(candidate.AllowedChannelNames()) == 0 {
			continue
		}
		if candidate.IsChannelNameAllowed(channelName) {
			log.Printf("[SLACKBOT] resolveBotByChannel: matched bot id=%s for channel=%s (name=%s)",
				candidate.ID(), channelID, channelName)
			return candidate
		}
	}
	return nil
}

// buildMessage constructs the message to send to the session
func (h *SlackBotEventHandler) buildMessage(bot *entities.SlackBot, payload map[string]interface{}, fallbackText string, isReuse bool) string {
	if bot != nil && bot.SessionConfig() != nil {
		var tmpl string
		if isReuse && bot.SessionConfig().ReuseMessageTemplate() != "" {
			tmpl = bot.SessionConfig().ReuseMessageTemplate()
		} else if bot.SessionConfig().InitialMessageTemplate() != "" {
			tmpl = bot.SessionConfig().InitialMessageTemplate()
		}
		if tmpl != "" {
			rendered, err := RenderTemplate(tmpl, payload)
			if err == nil {
				return rendered
			}
			log.Printf("[SLACKBOT] Failed to render message template: %v", err)
		}
	}
	return fallbackText
}

// getBotToken retrieves the Slack bot token for the given bot.
// Falls back to the default bot token secret when the bot has no custom one.
func (h *SlackBotEventHandler) getBotToken(ctx context.Context, bot *entities.SlackBot) (string, error) {
	if h.channelResolver == nil {
		return "", fmt.Errorf("channel resolver is nil; cannot get bot token")
	}
	secretName := h.defaultBotTokenSecretName
	secretKey := h.defaultBotTokenSecretKey
	if bot != nil {
		if bot.BotTokenSecretName() != "" {
			secretName = bot.BotTokenSecretName()
		}
		if bot.BotTokenSecretKey() != "" {
			secretKey = bot.BotTokenSecretKey()
		}
	}
	return h.channelResolver.GetBotToken(ctx, secretName, secretKey)
}

// postSessionURLToSlack posts the session URL back to the Slack thread.
// This is a best-effort operation; errors are logged but never propagated.
func (h *SlackBotEventHandler) postSessionURLToSlack(ctx context.Context, channel, threadTS, sessionID string, bot *entities.SlackBot) {
	if h.channelResolver == nil {
		return
	}

	// Determine the base URL: prefer NOTIFICATION_BASE_URL env, then h.baseURL
	sessionBaseURL := os.Getenv("NOTIFICATION_BASE_URL")
	if sessionBaseURL == "" {
		sessionBaseURL = h.baseURL
	}
	if sessionBaseURL == "" {
		log.Printf("[SLACKBOT] Skipping session URL notification: no base URL configured (set NOTIFICATION_BASE_URL or webhook.base_url)")
		return
	}

	sessionURL := fmt.Sprintf("%s/sessions/%s", strings.TrimRight(sessionBaseURL, "/"), sessionID)
	message := fmt.Sprintf("セッションを作成しました :robot_face:\n%s", sessionURL)

	botToken, err := h.getBotToken(ctx, bot)
	if err != nil {
		log.Printf("[SLACKBOT] Failed to get bot token for Slack notification: %v", err)
		return
	}

	if err := h.channelResolver.PostMessage(ctx, channel, threadTS, message, botToken); err != nil {
		log.Printf("[SLACKBOT] Failed to post session URL to Slack thread: %v", err)
		return
	}

	log.Printf("[SLACKBOT] Posted session URL to Slack thread: sessionID=%s, channel=%s, thread=%s", sessionID, channel, threadTS)
}
