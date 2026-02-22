package controllers

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// SlackBotController handles SlackBot management API requests
type SlackBotController struct {
	repo    repositories.SlackBotRepository
	baseURL string
}

// NewSlackBotController creates a new SlackBotController
func NewSlackBotController(repo repositories.SlackBotRepository, baseURL string) *SlackBotController {
	return &SlackBotController{repo: repo, baseURL: baseURL}
}

// --- Request/Response DTOs ---

// CreateSlackBotRequest is the request body for creating a SlackBot
type CreateSlackBotRequest struct {
	Name               string                 `json:"name"`
	Scope              entities.ResourceScope `json:"scope,omitempty"`
	TeamID             string                 `json:"team_id,omitempty"`
	SigningSecret      string                 `json:"signing_secret"`
	BotTokenSecretName string                 `json:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey  string                 `json:"bot_token_secret_key,omitempty"`
	AllowedEventTypes  []string               `json:"allowed_event_types,omitempty"`
	AllowedChannelIDs  []string               `json:"allowed_channel_ids,omitempty"`
	SessionConfig      *SlackBotSessionConfig `json:"session_config,omitempty"`
	MaxSessions        int                    `json:"max_sessions,omitempty"`
}

// UpdateSlackBotRequest is the request body for updating a SlackBot
type UpdateSlackBotRequest struct {
	Name               string                  `json:"name,omitempty"`
	Status             entities.SlackBotStatus `json:"status,omitempty"`
	SigningSecret      string                  `json:"signing_secret,omitempty"`
	BotTokenSecretName string                  `json:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey  string                  `json:"bot_token_secret_key,omitempty"`
	AllowedEventTypes  []string                `json:"allowed_event_types,omitempty"`
	AllowedChannelIDs  []string                `json:"allowed_channel_ids,omitempty"`
	SessionConfig      *SlackBotSessionConfig  `json:"session_config,omitempty"`
	MaxSessions        int                     `json:"max_sessions,omitempty"`
}

// SlackBotSessionConfig is the session configuration for a SlackBot
type SlackBotSessionConfig struct {
	InitialMessageTemplate string                 `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                 `json:"reuse_message_template,omitempty"`
	Tags                   map[string]string      `json:"tags,omitempty"`
	Environment            map[string]string      `json:"environment,omitempty"`
	Params                 *SlackBotSessionParams `json:"params,omitempty"`
}

// SlackBotSessionParams contains session parameters for SlackBot sessions
type SlackBotSessionParams struct {
	AgentType string `json:"agent_type,omitempty"`
	Oneshot   bool   `json:"oneshot,omitempty"`
}

// SlackBotResponse is the API response for a SlackBot
type SlackBotResponse struct {
	ID                 string                  `json:"id"`
	Name               string                  `json:"name"`
	UserID             string                  `json:"user_id"`
	Scope              entities.ResourceScope  `json:"scope,omitempty"`
	TeamID             string                  `json:"team_id,omitempty"`
	Status             entities.SlackBotStatus `json:"status"`
	SigningSecret      string                  `json:"signing_secret"` // masked
	HookURL            string                  `json:"hook_url"`
	BotTokenSecretName string                  `json:"bot_token_secret_name,omitempty"`
	BotTokenSecretKey  string                  `json:"bot_token_secret_key,omitempty"`
	AllowedEventTypes  []string                `json:"allowed_event_types,omitempty"`
	AllowedChannelIDs  []string                `json:"allowed_channel_ids,omitempty"`
	SessionConfig      *SlackBotSessionConfig  `json:"session_config,omitempty"`
	MaxSessions        int                     `json:"max_sessions"`
	CreatedAt          time.Time               `json:"created_at"`
	UpdatedAt          time.Time               `json:"updated_at"`
}

// --- Handler methods ---

// getSlackBotUserID extracts the user ID from the echo context (same pattern as webhook_controller.go)
// It looks for the "user_id" stored in the context by auth middleware.
func getSlackBotUserID(ctx echo.Context) string {
	if uid, ok := ctx.Get("user_id").(string); ok {
		return uid
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
	if req.SigningSecret == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "signing_secret is required")
	}

	userID := getSlackBotUserID(ctx)
	if userID == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}

	id := uuid.New().String()
	bot := entities.NewSlackBot(id, req.Name, userID)
	bot.SetSigningSecret(req.SigningSecret)

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
	if len(req.AllowedChannelIDs) > 0 {
		bot.SetAllowedChannelIDs(req.AllowedChannelIDs)
	}
	if req.MaxSessions > 0 {
		bot.SetMaxSessions(req.MaxSessions)
	}
	if req.SessionConfig != nil {
		bot.SetSessionConfig(toEntitySessionConfig(req.SessionConfig))
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

	filter := repositories.SlackBotFilter{
		UserID: userID,
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
	if req.SigningSecret != "" {
		bot.SetSigningSecret(req.SigningSecret)
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
	if req.AllowedChannelIDs != nil {
		bot.SetAllowedChannelIDs(req.AllowedChannelIDs)
	}
	if req.MaxSessions > 0 {
		bot.SetMaxSessions(req.MaxSessions)
	}
	if req.SessionConfig != nil {
		bot.SetSessionConfig(toEntitySessionConfig(req.SessionConfig))
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

func (c *SlackBotController) hookURL(id string) string {
	if c.baseURL != "" {
		return c.baseURL + "/hooks/slack/" + id
	}
	return "/hooks/slack/" + id
}

func (c *SlackBotController) toResponse(bot *entities.SlackBot) *SlackBotResponse {
	resp := &SlackBotResponse{
		ID:                 bot.ID(),
		Name:               bot.Name(),
		UserID:             bot.UserID(),
		Scope:              bot.Scope(),
		TeamID:             bot.TeamID(),
		Status:             bot.Status(),
		SigningSecret:      bot.MaskSigningSecret(),
		HookURL:            c.hookURL(bot.ID()),
		BotTokenSecretName: bot.BotTokenSecretName(),
		BotTokenSecretKey:  bot.BotTokenSecretKey(),
		AllowedEventTypes:  bot.AllowedEventTypes(),
		AllowedChannelIDs:  bot.AllowedChannelIDs(),
		MaxSessions:        bot.MaxSessions(),
		CreatedAt:          bot.CreatedAt(),
		UpdatedAt:          bot.UpdatedAt(),
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
	}
	if sc.Params() != nil {
		cfg.Params = &SlackBotSessionParams{
			AgentType: sc.Params().AgentType,
			Oneshot:   sc.Params().Oneshot,
		}
	}
	return cfg
}
