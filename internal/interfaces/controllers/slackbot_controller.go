package controllers

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// SlackBotController handles SlackBot management API requests
type SlackBotController struct {
	repo repositories.SlackBotRepository
}

// NewSlackBotController creates a new SlackBotController
func NewSlackBotController(repo repositories.SlackBotRepository) *SlackBotController {
	return &SlackBotController{repo: repo}
}

// --- Request/Response DTOs ---

// CreateSlackBotRequest is the request body for creating a SlackBot
type CreateSlackBotRequest struct {
	Name   string                 `json:"name"`
	Scope  entities.ResourceScope `json:"scope,omitempty"`
	TeamID string                 `json:"team_id,omitempty"`
	// Teams is an explicit list of team IDs (e.g. ["org/team-slug"]) whose settings
	// (MCP servers, env vars, Bedrock config, etc.) will be merged into sessions
	// created by this bot. When omitted, the server falls back to the authenticated
	// user's team memberships at creation time.
	Teams               []string               `json:"teams,omitempty"`
	BotTokenSecretName  string                 `json:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey   string                 `json:"bot_token_secret_key,omitempty"`
	AllowedEventTypes   []string               `json:"allowed_event_types,omitempty"`
	AllowedChannelNames []string               `json:"allowed_channel_names,omitempty"`
	SessionConfig       *SlackBotSessionConfig `json:"session_config,omitempty"`
	MaxSessions         int                    `json:"max_sessions,omitempty"`
	// NotifyOnSessionCreated controls whether the bot posts a Slack message with
	// the session URL when a new session is created. Defaults to true.
	NotifyOnSessionCreated *bool `json:"notify_on_session_created,omitempty"`
	// AllowBotMessages controls whether the bot processes messages posted by other bots.
	// Defaults to false to prevent recursive session creation.
	AllowBotMessages *bool `json:"allow_bot_messages,omitempty"`
}

// UpdateSlackBotRequest is the request body for updating a SlackBot
type UpdateSlackBotRequest struct {
	Name   string                  `json:"name,omitempty"`
	Status entities.SlackBotStatus `json:"status,omitempty"`
	// Teams is an explicit list of team IDs (e.g. ["org/team-slug"]) whose settings
	// will be merged into sessions created by this bot. When omitted, the server
	// refreshes from the authenticated user's current team memberships.
	Teams               []string               `json:"teams,omitempty"`
	BotTokenSecretName  string                 `json:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey   string                 `json:"bot_token_secret_key,omitempty"`
	AllowedEventTypes   []string               `json:"allowed_event_types,omitempty"`
	AllowedChannelNames []string               `json:"allowed_channel_names,omitempty"`
	SessionConfig       *SlackBotSessionConfig `json:"session_config,omitempty"`
	MaxSessions         int                    `json:"max_sessions,omitempty"`
	// NotifyOnSessionCreated controls whether the bot posts a Slack message with
	// the session URL when a new session is created. Defaults to true.
	NotifyOnSessionCreated *bool `json:"notify_on_session_created,omitempty"`
	// AllowBotMessages controls whether the bot processes messages posted by other bots.
	// Defaults to false to prevent recursive session creation.
	AllowBotMessages *bool `json:"allow_bot_messages,omitempty"`
}

// SlackBotSessionConfig is the session configuration for a SlackBot
type SlackBotSessionConfig struct {
	InitialMessageTemplate string                 `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                 `json:"reuse_message_template,omitempty"`
	Tags                   map[string]string      `json:"tags,omitempty"`
	Environment            map[string]string      `json:"environment,omitempty"`
	Params                 *SlackBotSessionParams `json:"params,omitempty"`
	MemoryKey              map[string]string      `json:"memory_key,omitempty"`
}

// SlackBotSessionParams contains session parameters for SlackBot sessions
type SlackBotSessionParams struct {
	AgentType string `json:"agent_type,omitempty"`
	Oneshot   bool   `json:"oneshot,omitempty"`
}

// SlackBotResponse is the API response for a SlackBot
type SlackBotResponse struct {
	ID                     string                  `json:"id"`
	Name                   string                  `json:"name"`
	UserID                 string                  `json:"user_id"`
	Scope                  entities.ResourceScope  `json:"scope,omitempty"`
	TeamID                 string                  `json:"team_id,omitempty"`
	Teams                  []string                `json:"teams,omitempty"`
	Status                 entities.SlackBotStatus `json:"status"`
	BotTokenSecretName     string                  `json:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey      string                  `json:"bot_token_secret_key,omitempty"`
	AllowedEventTypes      []string                `json:"allowed_event_types,omitempty"`
	AllowedChannelNames    []string                `json:"allowed_channel_names,omitempty"`
	SessionConfig          *SlackBotSessionConfig  `json:"session_config,omitempty"`
	MaxSessions            int                     `json:"max_sessions"`
	NotifyOnSessionCreated bool                    `json:"notify_on_session_created"`
	AllowBotMessages       bool                    `json:"allow_bot_messages"`
	CreatedAt              time.Time               `json:"created_at"`
	UpdatedAt              time.Time               `json:"updated_at"`
}

// --- Handler methods ---

// getSlackBotUserID extracts the user ID from the echo context (same pattern as webhook_controller.go)
// It looks for the internal_user stored in the context by auth middleware.
func getSlackBotUserID(ctx echo.Context) string {
	if user := auth.GetUserFromContext(ctx); user != nil {
		return string(user.ID())
	}
	return ""
}

// CreateSlackBot handles POST /slackbots
func (c *SlackBotController) CreateSlackBot(ctx echo.Context) error {
	var req CreateSlackBotRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}

	userID := getSlackBotUserID(ctx)
	if userID == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	id := uuid.New().String()
	bot := entities.NewSlackBot(id, req.Name, userID)

	if req.Scope != "" {
		bot.SetScope(req.Scope)
	}
	if req.TeamID != "" {
		bot.SetTeamID(req.TeamID)
	}
	if req.BotTokenSecretName != "" {
		bot.SetBotTokenSecretName(req.BotTokenSecretName)
	}
	if req.BotTokenSecretKey != "" {
		bot.SetBotTokenSecretKey(req.BotTokenSecretKey)
	}
	if len(req.AllowedEventTypes) > 0 {
		bot.SetAllowedEventTypes(req.AllowedEventTypes)
	}
	if len(req.AllowedChannelNames) > 0 {
		bot.SetAllowedChannelNames(req.AllowedChannelNames)
	}
	if req.MaxSessions > 0 {
		bot.SetMaxSessions(req.MaxSessions)
	}
	if req.SessionConfig != nil {
		bot.SetSessionConfig(toEntitySessionConfig(req.SessionConfig))
	}
	if req.NotifyOnSessionCreated != nil {
		bot.SetNotifyOnSessionCreated(req.NotifyOnSessionCreated)
	}
	if req.AllowBotMessages != nil {
		bot.SetAllowBotMessages(req.AllowBotMessages)
	}

	// Set team memberships so that sessions created by this bot receive team-level
	// settings (MCP servers, Bedrock config, env vars, etc.).
	// Prefer an explicitly supplied list from the request; fall back to snapshotting
	// the authenticated user's current memberships (useful for GitHub OAuth users).
	if req.Teams != nil {
		bot.SetTeams(req.Teams)
	} else if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx != nil {
		bot.SetTeams(authzCtx.TeamScope.Teams)
	}

	if err := bot.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := c.repo.Create(ctx.Request().Context(), bot); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create slackbot")
	}

	return ctx.JSON(http.StatusCreated, c.toResponse(bot))
}

// ListSlackBots handles GET /slackbots
func (c *SlackBotController) ListSlackBots(ctx echo.Context) error {
	userID := getSlackBotUserID(ctx)

	// Collect team IDs the user belongs to for team-scoped bot visibility
	var teamIDs []string
	if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx != nil {
		teamIDs = authzCtx.TeamScope.Teams
	}

	// Read optional query parameters for filtering
	scopeParam := ctx.QueryParam("scope")
	teamIDParam := ctx.QueryParam("team_id")
	statusParam := ctx.QueryParam("status")

	filter := repositories.SlackBotFilter{
		UserID:  userID,
		TeamIDs: teamIDs,
		Scope:   entities.ResourceScope(scopeParam),
		TeamID:  teamIDParam,
		Status:  entities.SlackBotStatus(statusParam),
	}

	bots, err := c.repo.List(ctx.Request().Context(), filter)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list slackbots")
	}

	responses := make([]*SlackBotResponse, 0, len(bots))
	for _, bot := range bots {
		responses = append(responses, c.toResponse(bot))
	}
	return ctx.JSON(http.StatusOK, responses)
}

// GetSlackBot handles GET /slackbots/:id
func (c *SlackBotController) GetSlackBot(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	bot, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSlackBotNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "slackbot not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get slackbot")
	}

	userID := getSlackBotUserID(ctx)
	if !c.userCanAccess(ctx, bot, userID) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(bot))
}

// UpdateSlackBot handles PUT /slackbots/:id
func (c *SlackBotController) UpdateSlackBot(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	bot, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSlackBotNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "slackbot not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get slackbot")
	}

	userID := getSlackBotUserID(ctx)
	if !c.userCanAccess(ctx, bot, userID) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	var req UpdateSlackBotRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if req.Name != "" {
		bot.SetName(req.Name)
	}
	if req.Status != "" {
		bot.SetStatus(req.Status)
	}
	if req.BotTokenSecretName != "" {
		bot.SetBotTokenSecretName(req.BotTokenSecretName)
	}
	if req.BotTokenSecretKey != "" {
		bot.SetBotTokenSecretKey(req.BotTokenSecretKey)
	}
	if req.AllowedEventTypes != nil {
		bot.SetAllowedEventTypes(req.AllowedEventTypes)
	}
	if req.AllowedChannelNames != nil {
		bot.SetAllowedChannelNames(req.AllowedChannelNames)
	}
	if req.MaxSessions > 0 {
		bot.SetMaxSessions(req.MaxSessions)
	}
	if req.SessionConfig != nil {
		bot.SetSessionConfig(toEntitySessionConfig(req.SessionConfig))
	}
	if req.NotifyOnSessionCreated != nil {
		bot.SetNotifyOnSessionCreated(req.NotifyOnSessionCreated)
	}
	if req.AllowBotMessages != nil {
		bot.SetAllowBotMessages(req.AllowBotMessages)
	}

	// Update team memberships.
	// If an explicit list is supplied in the request, use it (allows API-key users
	// to pin specific teams). Otherwise refresh from the requester's current auth
	// context (picks up GitHub team membership changes for OAuth users).
	if req.Teams != nil {
		bot.SetTeams(req.Teams)
	} else if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx != nil {
		bot.SetTeams(authzCtx.TeamScope.Teams)
	}

	if err := c.repo.Update(ctx.Request().Context(), bot); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update slackbot")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(bot))
}

// DeleteSlackBot handles DELETE /slackbots/:id
func (c *SlackBotController) DeleteSlackBot(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	bot, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSlackBotNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "slackbot not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get slackbot")
	}

	userID := getSlackBotUserID(ctx)
	if !c.userCanAccess(ctx, bot, userID) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete slackbot")
	}

	return ctx.NoContent(http.StatusNoContent)
}

// --- Helpers ---

func (c *SlackBotController) toResponse(bot *entities.SlackBot) *SlackBotResponse {
	resp := &SlackBotResponse{
		ID:                     bot.ID(),
		Name:                   bot.Name(),
		UserID:                 bot.UserID(),
		Scope:                  bot.Scope(),
		TeamID:                 bot.TeamID(),
		Teams:                  bot.Teams(),
		Status:                 bot.Status(),
		BotTokenSecretName:     bot.BotTokenSecretName(),
		BotTokenSecretKey:      bot.BotTokenSecretKey(),
		AllowedEventTypes:      bot.AllowedEventTypes(),
		AllowedChannelNames:    bot.AllowedChannelNames(),
		MaxSessions:            bot.MaxSessions(),
		NotifyOnSessionCreated: bot.NotifyOnSessionCreated(),
		AllowBotMessages:       bot.AllowBotMessages(),
		CreatedAt:              bot.CreatedAt(),
		UpdatedAt:              bot.UpdatedAt(),
	}
	if bot.SessionConfig() != nil {
		resp.SessionConfig = fromEntitySessionConfig(bot.SessionConfig())
	}
	return resp
}

func (c *SlackBotController) userCanAccess(ctx echo.Context, bot *entities.SlackBot, userID string) bool {
	if bot.Scope() == entities.ScopeTeam {
		// For team-scoped bots, allow access (team membership checked by middleware)
		return true
	}
	return bot.UserID() == userID
}

func toEntitySessionConfig(cfg *SlackBotSessionConfig) *entities.WebhookSessionConfig {
	if cfg == nil {
		return nil
	}
	sc := entities.NewWebhookSessionConfig()
	sc.SetInitialMessageTemplate(cfg.InitialMessageTemplate)
	sc.SetReuseMessageTemplate(cfg.ReuseMessageTemplate)
	if cfg.Tags != nil {
		sc.SetTags(cfg.Tags)
	}
	if cfg.Environment != nil {
		sc.SetEnvironment(cfg.Environment)
	}
	if cfg.MemoryKey != nil {
		sc.SetMemoryKey(cfg.MemoryKey)
	}
	if cfg.Params != nil {
		params := &entities.SessionParams{
			AgentType: cfg.Params.AgentType,
			Oneshot:   cfg.Params.Oneshot,
		}
		sc.SetParams(params)
	}
	return sc
}

func fromEntitySessionConfig(sc *entities.WebhookSessionConfig) *SlackBotSessionConfig {
	if sc == nil {
		return nil
	}
	cfg := &SlackBotSessionConfig{
		InitialMessageTemplate: sc.InitialMessageTemplate(),
		ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
		Tags:                   sc.Tags(),
		Environment:            sc.Environment(),
		MemoryKey:              sc.MemoryKey(),
	}
	if sc.Params() != nil {
		cfg.Params = &SlackBotSessionParams{
			AgentType: sc.Params().AgentType,
			Oneshot:   sc.Params().Oneshot,
		}
	}
	return cfg
}
