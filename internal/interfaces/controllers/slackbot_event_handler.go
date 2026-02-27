package controllers

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// slackBotDefaultID is the special ID for the server-configured default SlackBot
	slackBotDefaultID = "default"
)

// SlackBotEventHandler handles incoming Slack events (via Socket Mode) and manages sessions
type SlackBotEventHandler struct {
	repo            repositories.SlackBotRepository
	sessionManager  repositories.SessionManager
	channelResolver *services.SlackChannelResolver
	// Default SlackBot configuration (from server startup config)
	defaultBotTokenSecretName string
	defaultBotTokenSecretKey  string
	// baseURL is used to construct session URLs posted back to Slack threads.
	// If empty, NOTIFICATION_BASE_URL env var is checked as a fallback.
	baseURL string
	// dryRun disables actual session creation and Slack posts; actions are only logged.
	// Enabled via AGENTAPI_SLACK_DRY_RUN environment variable.
	dryRun bool
	// pendingThreads tracks channel+thread combinations that have a session creation
	// in-flight. Slack may emit both "message" and "app_mention" events for the same
	// @mention within milliseconds of each other. Without this guard both events would
	// pass the reuse check (no session exists yet) and spawn duplicate sessions.
	// Key: "channel:threadKey"  Value: struct{}
	pendingThreads sync.Map
}

// NewSlackBotEventHandler creates a new SlackBotEventHandler
func NewSlackBotEventHandler(
	repo repositories.SlackBotRepository,
	sessionManager repositories.SessionManager,
	defaultBotTokenSecretName string,
	defaultBotTokenSecretKey string,
	channelResolver *services.SlackChannelResolver,
	baseURL string,
	dryRun bool,
) *SlackBotEventHandler {
	return &SlackBotEventHandler{
		repo:                      repo,
		sessionManager:            sessionManager,
		channelResolver:           channelResolver,
		defaultBotTokenSecretName: defaultBotTokenSecretName,
		defaultBotTokenSecretKey:  defaultBotTokenSecretKey,
		baseURL:                   baseURL,
		dryRun:                    dryRun,
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
	bot, err := h.resolveSlackBot(ctx, botID)
	if err != nil {
		return fmt.Errorf("failed to resolve slackbot: %w", err)
	}

	// For default ID: try to identify the registered bot by channel name filter.
	// If no matching bot is found, notify the user via Slack and drop the event
	// to avoid creating sessions with empty userID.
	if botID == slackBotDefaultID && bot == nil {
		resolvedBot := h.resolveBotByChannel(ctx, event.Channel)
		if resolvedBot == nil {
			log.Printf("[SLACKBOT] Default endpoint: no matching bot found for channel=%s, dropping event", event.Channel)
			threadKey := event.ThreadTs
			if threadKey == "" {
				threadKey = event.Ts
			}
			if botToken, tokenErr := h.getBotToken(ctx, nil); tokenErr == nil {
				h.postErrorToSlack(ctx, event.Channel, threadKey,
					":warning: このチャンネルに対応する bot が登録されていません。チャンネルの設定を確認してください。",
					botToken)
			}
			return nil
		}
		bot = resolvedBot
		botID = resolvedBot.ID()
		log.Printf("[SLACKBOT] Default endpoint: identified bot by channel filter: id=%s, channel=%s", botID, event.Channel)
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
				threadTS := event.ThreadTs
				if threadTS == "" {
					threadTS = event.Ts
				}
				h.postErrorToSlack(ctx, event.Channel, threadTS,
					":warning: チャンネル情報を取得できませんでした。しばらく待ってから再度お試しください。",
					botToken)
				return fmt.Errorf("failed to resolve channel name for channel %s: %w", event.Channel, resolveErr)
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

	// Handle /stop command: interrupt the running agent in the associated session
	if isStopCommand(event.Text) {
		h.handleStopCommand(ctx, channel, threadKey, bot)
		return nil
	}

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

	// Determine agent type: default to "claude-agentapi"; bot session_config may override.
	agentType := "claude-agentapi"
	if bot != nil && bot.SessionConfig() != nil && bot.SessionConfig().Params() != nil {
		if bot.SessionConfig().Params().AgentType != "" {
			agentType = bot.SessionConfig().Params().AgentType
		}
	}

	// Try to reuse an existing active session for this channel+thread.
	// Follow-up messages in the same Slack thread are routed to the existing session
	// rather than spawning a new one (mirrors the webhook reuse-session behaviour).
	reuseFilter := entities.SessionFilter{
		Tags: map[string]string{
			"slack_channel":   channel,
			"slack_thread_ts": threadKey,
		},
		Status: "active",
	}
	if activeSessions := h.sessionManager.ListSessions(reuseFilter); len(activeSessions) > 0 {
		existingSession := activeSessions[0]
		reuseMessage := h.buildMessage(bot, payloadMap, event.Text, true)
		go func() {
			bgCtx := context.Background()
			if err := h.sessionManager.SendMessage(bgCtx, existingSession.ID(), reuseMessage); err != nil {
				log.Printf("[SLACKBOT] Failed to route message to existing session %s: %v", existingSession.ID(), err)
				return
			}
			// Update internal slack-last-message-at annotation (not a tag/label) so the
			// Slackbot cleanup worker knows this session has received a follow-up message.
			if err := h.sessionManager.UpdateSlackLastMessageAt(existingSession.ID(), time.Now()); err != nil {
				log.Printf("[SLACKBOT] Failed to update slack-last-message-at for session %s: %v", existingSession.ID(), err)
			}
			log.Printf("[SLACKBOT] Routed message to existing session %s for thread %s", existingSession.ID(), threadKey)
		}()
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
	var teams []string
	if bot != nil {
		scope = bot.Scope()
		userID = bot.UserID()
		teamID = bot.TeamID()
		// Follow the same pattern as the schedule worker:
		// - team-scoped bot: use only the bot's teamID as the sole team credential
		//   so that exactly the team's settings (MCP, env, Bedrock, etc.) are applied.
		// - user-scoped bot: use the explicit team list stored on the bot
		//   (set at create/update time via the teams field).
		if scope == entities.ScopeTeam && teamID != "" {
			teams = []string{teamID}
		} else {
			teams = bot.Teams()
		}
	}

	sessionID := uuid.New().String()
	req := &entities.RunServerRequest{
		UserID:         userID,
		Environment:    env,
		Tags:           tags,
		Scope:          scope,
		TeamID:         teamID,
		Teams:          teams,
		InitialMessage: initialMessage,
		AgentType:      agentType,
		SlackParams: &entities.SlackParams{
			Channel:  channel,
			ThreadTS: threadKey,
		},
	}

	// Dedup guard: Slack can emit both "message" and "app_mention" events for the same
	// @mention within milliseconds. Both would pass the reuse check above (the session
	// doesn't exist yet when they run concurrently) and would each spawn a new session.
	// Use LoadOrStore so that only the first event proceeds; the second is dropped.
	// The key is released once session creation completes (success or failure).
	pendingKey := channel + ":" + threadKey
	if _, alreadyPending := h.pendingThreads.LoadOrStore(pendingKey, struct{}{}); alreadyPending {
		log.Printf("[SLACKBOT] Session creation already in progress for thread %s (event type=%s), skipping duplicate", threadKey, event.Type)
		return nil
	}

	// Create session asynchronously so we don't block event processing
	go func() {
		defer h.pendingThreads.Delete(pendingKey)
		bgCtx := context.Background()

		if h.dryRun {
			log.Printf("[SLACKBOT] [DRY-RUN] Would create session: id=%s, channel=%s, thread=%s, agentType=%s, scope=%s",
				sessionID, channel, threadKey, agentType, scope)
			h.postSessionURLToSlack(bgCtx, channel, threadKey, sessionID, bot)
			return
		}

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

// resolveSlackBot retrieves the SlackBot entity.
// Returns (bot, error). For id="default", returns nil bot (uses server defaults).
func (h *SlackBotEventHandler) resolveSlackBot(ctx context.Context, id string) (*entities.SlackBot, error) {
	if id == slackBotDefaultID {
		return nil, nil
	}

	bot, err := h.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("slackbot not found: %s", id)
	}
	return bot, nil
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

// postErrorToSlack posts an error message to the Slack thread where the triggering event occurred.
// This is a best-effort operation; errors are logged but never propagated.
// In dry-run mode the post is only logged and not sent to Slack.
func (h *SlackBotEventHandler) postErrorToSlack(ctx context.Context, channel, threadTS, message, botToken string) {
	if h.channelResolver == nil {
		return
	}
	if h.dryRun {
		log.Printf("[SLACKBOT] [DRY-RUN] Would post error to Slack: channel=%s, thread=%s, message=%q", channel, threadTS, message)
		return
	}
	if err := h.channelResolver.PostMessage(ctx, channel, threadTS, message, botToken); err != nil {
		log.Printf("[SLACKBOT] Failed to post error message to Slack thread: channel=%s, err=%v", channel, err)
	}
}

// isStopCommand checks whether the Slack message text is a /stop command.
// It strips any bot mention tokens (<@UXXXXXXX>) from the text before comparing.
func isStopCommand(text string) bool {
	// Remove all Slack mention tokens of the form <@USER_ID> or <@USER_ID|username>
	cleaned := text
	for {
		start := strings.Index(cleaned, "<@")
		if start == -1 {
			break
		}
		end := strings.Index(cleaned[start:], ">")
		if end == -1 {
			break
		}
		cleaned = cleaned[:start] + cleaned[start+end+1:]
	}
	return strings.TrimSpace(cleaned) == "/stop"
}

// handleStopCommand processes a /stop command by finding the active session for the
// given channel+thread and sending a stop signal (Ctrl+C) to its agent.
// The result (success or failure) is posted back to the Slack thread.
func (h *SlackBotEventHandler) handleStopCommand(ctx context.Context, channel, threadKey string, bot *entities.SlackBot) {
	stopFilter := entities.SessionFilter{
		Tags: map[string]string{
			"slack_channel":   channel,
			"slack_thread_ts": threadKey,
		},
		Status: "active",
	}
	activeSessions := h.sessionManager.ListSessions(stopFilter)
	if len(activeSessions) == 0 {
		log.Printf("[SLACKBOT] /stop: no active session found for channel=%s, thread=%s", channel, threadKey)
		botToken, tokenErr := h.getBotToken(ctx, bot)
		if tokenErr == nil {
			h.postErrorToSlack(ctx, channel, threadKey,
				":warning: 停止するアクティブなセッションが見つかりません。",
				botToken)
		}
		return
	}

	session := activeSessions[0]
	go func() {
		bgCtx := context.Background()

		if h.dryRun {
			log.Printf("[SLACKBOT] [DRY-RUN] Would stop agent for session %s", session.ID())
			h.postStopConfirmationToSlack(bgCtx, channel, threadKey, bot)
			return
		}

		if err := h.sessionManager.StopAgent(bgCtx, session.ID()); err != nil {
			log.Printf("[SLACKBOT] Failed to stop agent for session %s: %v", session.ID(), err)
			botToken, tokenErr := h.getBotToken(bgCtx, bot)
			if tokenErr == nil {
				h.postErrorToSlack(bgCtx, channel, threadKey,
					fmt.Sprintf(":warning: セッションの停止に失敗しました: %v", err),
					botToken)
			}
			return
		}

		log.Printf("[SLACKBOT] Successfully stopped agent for session %s", session.ID())
		h.postStopConfirmationToSlack(bgCtx, channel, threadKey, bot)
	}()
}

// postStopConfirmationToSlack posts a confirmation message to the Slack thread
// after successfully sending the stop signal to the agent.
func (h *SlackBotEventHandler) postStopConfirmationToSlack(ctx context.Context, channel, threadTS string, bot *entities.SlackBot) {
	if h.channelResolver == nil {
		return
	}
	if h.dryRun {
		log.Printf("[SLACKBOT] [DRY-RUN] Would post stop confirmation to Slack: channel=%s, thread=%s", channel, threadTS)
		return
	}

	botToken, err := h.getBotToken(ctx, bot)
	if err != nil {
		log.Printf("[SLACKBOT] Failed to get bot token for stop confirmation: %v", err)
		return
	}

	message := "セッションを停止しました :stop_sign:"
	if err := h.channelResolver.PostMessage(ctx, channel, threadTS, message, botToken); err != nil {
		log.Printf("[SLACKBOT] Failed to post stop confirmation to Slack: %v", err)
	}
}

// postSessionURLToSlack posts the session URL back to the Slack thread.
// This is a best-effort operation; errors are logged but never propagated.
// In dry-run mode the post is only logged and not sent to Slack.
func (h *SlackBotEventHandler) postSessionURLToSlack(ctx context.Context, channel, threadTS, sessionID string, bot *entities.SlackBot) {
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

	if h.dryRun {
		log.Printf("[SLACKBOT] [DRY-RUN] Would post to Slack: channel=%s, thread=%s, message=%q", channel, threadTS, message)
		return
	}

	if h.channelResolver == nil {
		return
	}

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
