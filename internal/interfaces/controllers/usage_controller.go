package controllers

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/parquet-go/parquet-go"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

const (
	maxUsageEventsPerRequest = 1000
	maxUsageExportEvents     = 100000
	maxUsageExportRange      = 90 * 24 * time.Hour
)

type usageParquetRow struct {
	OccurredAt         time.Time `parquet:"occurred_at,timestamp(microsecond)"`
	SessionID          string    `parquet:"session_id,dict"`
	AgentSessionID     string    `parquet:"agent_session_id,dict"`
	AgentType          string    `parquet:"agent_type,dict"`
	Provider           string    `parquet:"provider,dict"`
	Model              string    `parquet:"model,dict"`
	OwnerUserID        string    `parquet:"owner_user_id,dict"`
	TriggeredUserID    string    `parquet:"triggered_user_id,dict"`
	SessionProfileID   string    `parquet:"session_profile_id,dict"`
	TriggerType        string    `parquet:"trigger_type,dict"`
	TriggerID          string    `parquet:"trigger_id,dict"`
	WebhookID          string    `parquet:"webhook_id,dict"`
	ScheduleID         string    `parquet:"schedule_id,dict"`
	SlackbotID         string    `parquet:"slackbot_id,dict"`
	InputTokens        int64     `parquet:"input_tokens"`
	OutputTokens       int64     `parquet:"output_tokens"`
	CachedInputTokens  int64     `parquet:"cached_input_tokens"`
	CacheCreationToken int64     `parquet:"cache_creation_tokens"`
	ReasoningTokens    int64     `parquet:"reasoning_tokens"`
}

type UsageController struct {
	repo     portrepos.UsageRepository
	sessions portrepos.SessionManager
}

func (c *UsageController) Get(ctx echo.Context) error {
	query, err := c.authorizedQuery(ctx)
	if err != nil {
		return err
	}
	if err := bindUsageRange(ctx, &query); err != nil {
		return err
	}
	return c.aggregate(ctx, query)
}

func (c *UsageController) authorizedQuery(ctx echo.Context) (entities.UsageQuery, error) {
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil {
		return entities.UsageQuery{}, echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	query := entities.UsageQuery{
		SessionID: ctx.QueryParam("session_id"), AgentType: ctx.QueryParam("agent_type"),
		Provider: ctx.QueryParam("provider"), Model: ctx.QueryParam("model"),
	}
	if teamID := ctx.QueryParam("team_id"); teamID != "" {
		if !authz.CanAccessTeam(teamID) {
			return entities.UsageQuery{}, echo.NewHTTPError(http.StatusForbidden, "not authorized for team")
		}
		query.TeamID = teamID
	} else {
		query.UserID = authz.PersonalScope.UserID
	}
	return query, nil
}

func (c *UsageController) ExportParquet(ctx echo.Context) error {
	query, err := c.authorizedQuery(ctx)
	if err != nil {
		return err
	}
	if err := bindUsageRange(ctx, &query); err != nil {
		return err
	}
	now := time.Now().UTC()
	if query.To == nil {
		query.To = &now
	}
	if query.From == nil {
		from := query.To.Add(-30 * 24 * time.Hour)
		query.From = &from
	}
	if query.To.Sub(*query.From) > maxUsageExportRange {
		return echo.NewHTTPError(http.StatusBadRequest, "export range must not exceed 90 days")
	}
	query.Limit = maxUsageExportEvents + 1
	events, err := c.repo.ListEvents(ctx.Request().Context(), query)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to export usage")
	}
	if len(events) > maxUsageExportEvents {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "usage export exceeds 100000 events; narrow the filters")
	}
	output, err := encodeUsageParquet(events)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode usage export")
	}
	ctx.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="usage.parquet"`)
	ctx.Response().Header().Set("X-Usage-Event-Count", fmt.Sprintf("%d", len(events)))
	return ctx.Blob(http.StatusOK, "application/vnd.apache.parquet", output)
}

func encodeUsageParquet(events []entities.UsageEvent) ([]byte, error) {
	rows := make([]usageParquetRow, len(events))
	for i, event := range events {
		rows[i] = usageParquetRow{
			OccurredAt: event.OccurredAt, SessionID: event.SessionID, AgentSessionID: event.AgentSessionID,
			AgentType: event.AgentType, Provider: event.Provider, Model: event.Model,
			OwnerUserID: event.OwnerUserID, TriggeredUserID: event.TriggeredUserID,
			SessionProfileID: event.SessionProfileID, TriggerType: event.TriggerType, TriggerID: event.TriggerID,
			WebhookID: event.WebhookID, ScheduleID: event.ScheduleID, SlackbotID: event.SlackbotID,
			InputTokens: event.InputTokens, OutputTokens: event.OutputTokens, CachedInputTokens: event.CachedInputTokens,
			CacheCreationToken: event.CacheCreationTokens, ReasoningTokens: event.ReasoningTokens,
		}
	}
	var output bytes.Buffer
	writer := parquet.NewGenericWriter[usageParquetRow](&output)
	if _, err := writer.Write(rows); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (c *UsageController) GetSession(ctx echo.Context) error {
	session := c.sessions.GetSession(ctx.Param("sessionId"))
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "not authorized for session")
	}
	query := entities.UsageQuery{SessionID: session.ID()}
	if err := bindUsageRange(ctx, &query); err != nil {
		return err
	}
	return c.aggregate(ctx, query)
}

func bindUsageRange(ctx echo.Context, query *entities.UsageQuery) error {
	for name, target := range map[string]**time.Time{"from": &query.From, "to": &query.To} {
		value := ctx.QueryParam(name)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, name+" must be RFC3339")
		}
		*target = &parsed
	}
	if query.From != nil && query.To != nil && !query.From.Before(*query.To) {
		return echo.NewHTTPError(http.StatusBadRequest, "from must be before to")
	}
	return nil
}

func (c *UsageController) aggregate(ctx echo.Context, query entities.UsageQuery) error {
	summary, err := c.repo.Aggregate(ctx.Request().Context(), query)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to aggregate usage")
	}
	return ctx.JSON(http.StatusOK, summary)
}

func NewUsageController(repo portrepos.UsageRepository, sessions portrepos.SessionManager) *UsageController {
	return &UsageController{repo: repo, sessions: sessions}
}

func (c *UsageController) Create(ctx echo.Context) error {
	var batch entities.UsageEventBatch
	if err := ctx.Bind(&batch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid usage payload")
	}
	if batch.SessionID == "" || len(batch.Events) == 0 || len(batch.Events) > maxUsageEventsPerRequest {
		return echo.NewHTTPError(http.StatusBadRequest, "session_id and 1-1000 events are required")
	}
	session := c.sessions.GetSession(batch.SessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}
	authz := auth.GetAuthorizationContext(ctx)
	if authz == nil || !authz.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "not authorized for session")
	}
	for i := range batch.Events {
		event := &batch.Events[i]
		if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.Model) == "" ||
			event.InputTokens < 0 || event.OutputTokens < 0 || event.CachedInputTokens < 0 ||
			event.CacheCreationTokens < 0 || event.ReasoningTokens < 0 || event.OccurredAt.IsZero() {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid usage event")
		}
		event.SessionID = batch.SessionID
		event.UserID = session.UserID()
		event.Scope = string(session.Scope())
		event.TeamID = session.TeamID()
		event.OwnerUserID = session.UserID()
		tags := session.Tags()
		event.TriggeredUserID = tags["triggered_user_id"]
		event.SessionProfileID = tags["session_profile_id"]
		event.WebhookID = tags["webhook_id"]
		event.ScheduleID = tags["schedule_id"]
		event.SlackbotID = tags["slackbot_id"]
		switch {
		case event.WebhookID != "":
			event.TriggerType, event.TriggerID = "webhook", event.WebhookID
		case event.ScheduleID != "":
			event.TriggerType, event.TriggerID = "schedule", event.ScheduleID
		case event.SlackbotID != "":
			event.TriggerType, event.TriggerID = "slackbot", event.SlackbotID
		default:
			event.TriggerType = "manual"
		}
	}
	result, err := c.repo.InsertEvents(ctx.Request().Context(), batch.Events)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to store usage events")
	}
	return ctx.JSON(http.StatusOK, result)
}
