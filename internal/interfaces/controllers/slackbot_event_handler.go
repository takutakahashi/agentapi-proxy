package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/webhook"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	// slackBotDefaultID is the special ID for the server-configured default SlackBot
	slackBotDefaultID = "default"
	// defaultSlackAgentType is the default agent type for SlackBot sessions
	defaultSlackAgentType = "claude-agentapi"
)

// SlackBotEventHandler handles incoming Slack events and manages sessions
type SlackBotEventHandler struct {
	repo           repositories.SlackBotRepository
	sessionManager repositories.SessionManager
	// Default SlackBot configuration (from server startup config)
	defaultSigningSecret      string
	defaultBotTokenSecretName string
	defaultBotTokenSecretKey  string
	sigVerifier               *webhook.SlackSignatureVerifier
}

// NewSlackBotEventHandler creates a new SlackBotEventHandler
func NewSlackBotEventHandler(
	repo repositories.SlackBotRepository,
	sessionManager repositories.SessionManager,
	defaultSigningSecret string,
	defaultBotTokenSecretName string,
	defaultBotTokenSecretKey string,
) *SlackBotEventHandler {
	return &SlackBotEventHandler{
		repo:                      repo,
		sessionManager:            sessionManager,
		defaultSigningSecret:      defaultSigningSecret,
		defaultBotTokenSecretName: defaultBotTokenSecretName,
		defaultBotTokenSecretKey:  defaultBotTokenSecretKey,
		sigVerifier:               webhook.NewSlackSignatureVerifier(),
	}
}

// slackPayload represents the outer Slack event payload structure
type slackPayload struct {
	Type      string      `json:"type"`
	Challenge string      `json:"challenge,omitempty"`
	TeamID    string      `json:"team_id,omitempty"`
	Event     *slackEvent `json:"event,omitempty"`
}

// slackEvent represents the inner Slack event
type slackEvent struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	User     string `json:"user"`
	Channel  string `json:"channel"`
	Ts       string `json:"ts"`
	ThreadTs string `json:"thread_ts,omitempty"`
}

// HandleSlackEvent handles POST /hooks/slack/:id
func (h *SlackBotEventHandler) HandleSlackEvent(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "missing slackbot id"})
	}

	// Read raw body for signature verification
	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read body"})
	}
	ctx.Request().Body = io.NopCloser(bytes.NewBuffer(body))

	log.Printf("[SLACKBOT] Received event: id=%s, size=%d", id, len(body))

	// Quick parse to handle url_verification challenge (before signature check per Slack docs)
	var quickParse struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &quickParse); err == nil && quickParse.Type == "url_verification" {
		log.Printf("[SLACKBOT] Responding to url_verification challenge")
		return ctx.JSON(http.StatusOK, map[string]string{"challenge": quickParse.Challenge})
	}

	// Resolve signing secret (default or from repo)
	signingSecret, bot, err := h.resolveSlackBot(ctx, id)
	if err != nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	// Verify Slack v0 HMAC-SHA256 signature
	timestamp := ctx.Request().Header.Get("X-Slack-Request-Timestamp")
	signature := ctx.Request().Header.Get("X-Slack-Signature")
	if timestamp == "" || signature == "" {
		log.Printf("[SLACKBOT] Missing signature headers: id=%s", id)
		return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "missing signature headers"})
	}

	valid, verifyErr := h.sigVerifier.Verify(body, timestamp, signature, signingSecret)
	if verifyErr != nil {
		log.Printf("[SLACKBOT] Signature timestamp error: id=%s, err=%v", id, verifyErr)
		return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "request timestamp expired"})
	}
	if !valid {
		log.Printf("[SLACKBOT] Signature verification failed: id=%s", id)
		return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "signature verification failed"})
	}

	// Parse full payload
	var payload slackPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("[SLACKBOT] Failed to parse payload: id=%s, err=%v", id, err)
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
	}

	// We only process event_callback type
	if payload.Type != "event_callback" || payload.Event == nil {
		log.Printf("[SLACKBOT] Ignoring non-event payload: id=%s, type=%s", id, payload.Type)
		return ctx.JSON(http.StatusOK, map[string]string{"message": "ignored"})
	}

	event := payload.Event

	// Apply filters (if this is a registered bot, not default)
	if bot != nil {
		if bot.Status() == entities.SlackBotStatusPaused {
			log.Printf("[SLACKBOT] Bot is paused: id=%s", id)
			return ctx.JSON(http.StatusOK, map[string]string{"message": "bot paused"})
		}
		if !bot.IsEventTypeAllowed(event.Type) {
			log.Printf("[SLACKBOT] Event type not allowed: id=%s, type=%s", id, event.Type)
			return ctx.JSON(http.StatusOK, map[string]string{"message": "event type not allowed"})
		}
		if !bot.IsChannelAllowed(event.Channel) {
			log.Printf("[SLACKBOT] Channel not allowed: id=%s, channel=%s", id, event.Channel)
			return ctx.JSON(http.StatusOK, map[string]string{"message": "channel not allowed"})
		}
	}

	// Normalize thread key: use thread_ts if present (indicates a reply), otherwise use ts (root message)
	threadKey := event.ThreadTs
	if threadKey == "" {
		threadKey = event.Ts
	}

	channel := event.Channel
	log.Printf("[SLACKBOT] Processing event: id=%s, type=%s, channel=%s, thread=%s", id, event.Type, channel, threadKey)

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

	// Search for existing active session with matching tags
	searchTags := map[string]string{
		"slackbot_id":     id,
		"slack_channel":   channel,
		"slack_thread_ts": threadKey,
	}
	existingSessions := h.sessionManager.ListSessions(entities.SessionFilter{
		Tags:   searchTags,
		Status: "active",
	})

	if len(existingSessions) > 0 {
		// Reuse existing session
		existingSession := existingSessions[0]
		log.Printf("[SLACKBOT] Reusing session %s for thread %s", existingSession.ID(), threadKey)

		reuseMessage := h.buildMessage(bot, payloadMap, event.Text, true)
		if err := h.sessionManager.SendMessage(ctx.Request().Context(), existingSession.ID(), reuseMessage); err != nil {
			log.Printf("[SLACKBOT] Failed to send message to session %s: %v", existingSession.ID(), err)
			return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to send message"})
		}

		return ctx.JSON(http.StatusOK, map[string]string{
			"message":     "message sent to existing session",
			"session_id":  existingSession.ID(),
			"slackbot_id": id,
		})
	}

	// Create new session
	sessionID := uuid.New().String()

	// Build session tags
	tags := map[string]string{
		"slackbot_id":     id,
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

	// Check session limit
	if bot != nil {
		limitFilter := entities.SessionFilter{
			Tags: map[string]string{"slackbot_id": id},
		}
		activeSessions := h.sessionManager.ListSessions(limitFilter)
		if len(activeSessions) >= bot.MaxSessions() {
			log.Printf("[SLACKBOT] Session limit reached: id=%s, limit=%d", id, bot.MaxSessions())
			return ctx.JSON(http.StatusTooManyRequests, map[string]string{"error": fmt.Sprintf("session limit reached: maximum %d sessions", bot.MaxSessions())})
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

	session, err := h.sessionManager.CreateSession(ctx.Request().Context(), sessionID, req, nil)
	if err != nil {
		log.Printf("[SLACKBOT] Failed to create session: %v", err)
		return ctx.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
	}

	log.Printf("[SLACKBOT] Created session %s for thread %s", session.ID(), threadKey)

	return ctx.JSON(http.StatusOK, map[string]string{
		"message":     "session created",
		"session_id":  session.ID(),
		"slackbot_id": id,
	})
}

// resolveSlackBot retrieves the signing secret and optionally the SlackBot entity.
// Returns (signingSecret, bot, error)
// For id="default", returns the server-configured secret and nil bot.
func (h *SlackBotEventHandler) resolveSlackBot(ctx echo.Context, id string) (string, *entities.SlackBot, error) {
	if id == slackBotDefaultID {
		if h.defaultSigningSecret == "" {
			return "", nil, fmt.Errorf("default slackbot not configured")
		}
		return h.defaultSigningSecret, nil, nil
	}

	bot, err := h.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		return "", nil, fmt.Errorf("slackbot not found: %s", id)
	}
	return bot.SigningSecret(), bot, nil
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
