package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/tursodatabase/libsql-client-go/libsql"
)

type LibSQLUsageRepository struct{ db *sql.DB }

var _ portrepos.UsageRepository = (*LibSQLUsageRepository)(nil)

func NewLibSQLUsageRepository(ctx context.Context, databaseURL, authToken string) (*LibSQLUsageRepository, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("usage database URL is required")
	}
	opts := []libsql.Option{}
	if authToken != "" {
		opts = append(opts, libsql.WithAuthToken(authToken))
	}
	connector, err := libsql.NewConnector(databaseURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("create usage libSQL connector: %w", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(8)
	r := &LibSQLUsageRepository{db: db}
	if err := r.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}

func (r *LibSQLUsageRepository) initialize(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS agentapi_usage_events (
event_id TEXT PRIMARY KEY, session_id TEXT NOT NULL, agent_session_id TEXT,
turn_id TEXT, response_id TEXT, user_id TEXT NOT NULL, scope TEXT NOT NULL,
team_id TEXT, agent_type TEXT NOT NULL, provider TEXT, model TEXT NOT NULL,
input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
cached_input_tokens INTEGER NOT NULL DEFAULT 0, cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
reasoning_tokens INTEGER NOT NULL DEFAULT 0, occurred_at TEXT NOT NULL, received_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS agentapi_usage_events_session_time ON agentapi_usage_events(session_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS agentapi_usage_events_user_time ON agentapi_usage_events(user_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS agentapi_usage_events_team_time ON agentapi_usage_events(team_id, occurred_at)`,
		`CREATE INDEX IF NOT EXISTS agentapi_usage_events_model_time ON agentapi_usage_events(model, occurred_at)`,
	}
	for _, statement := range statements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize usage schema: %w", err)
		}
	}
	for _, column := range []string{
		"owner_user_id TEXT", "triggered_user_id TEXT", "session_profile_id TEXT",
		"trigger_type TEXT", "trigger_id TEXT", "webhook_id TEXT", "schedule_id TEXT", "slackbot_id TEXT",
	} {
		if err := r.ensureUsageColumn(ctx, column); err != nil {
			return err
		}
	}
	return nil
}

func (r *LibSQLUsageRepository) ensureUsageColumn(ctx context.Context, definition string) error {
	name := strings.Fields(definition)[0]
	rows, err := r.db.QueryContext(ctx, `PRAGMA table_info(agentapi_usage_events)`)
	if err != nil {
		return fmt.Errorf("inspect usage schema: %w", err)
	}
	found := false
	for rows.Next() {
		var cid int
		var columnName, columnType string
		var notNull, pk int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &pk); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan usage schema: %w", err)
		}
		found = found || columnName == name
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `ALTER TABLE agentapi_usage_events ADD COLUMN `+definition); err != nil {
		// Multiple proxy replicas can initialize the shared database concurrently.
		if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return nil
		}
		return fmt.Errorf("add usage column %s: %w", name, err)
	}
	return nil
}

func (r *LibSQLUsageRepository) InsertEvents(ctx context.Context, events []entities.UsageEvent) (entities.UsageInsertResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return entities.UsageInsertResult{}, fmt.Errorf("begin usage transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result := entities.UsageInsertResult{}
	receivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	for _, event := range events {
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO agentapi_usage_events
(event_id,session_id,agent_session_id,turn_id,response_id,user_id,scope,team_id,agent_type,provider,model,input_tokens,output_tokens,cached_input_tokens,cache_creation_tokens,reasoning_tokens,occurred_at,received_at,owner_user_id,triggered_user_id,session_profile_id,trigger_type,trigger_id,webhook_id,schedule_id,slackbot_id)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, event.EventID, event.SessionID, event.AgentSessionID, event.TurnID,
			event.ResponseID, event.UserID, event.Scope, event.TeamID, event.AgentType, event.Provider, event.Model,
			event.InputTokens, event.OutputTokens, event.CachedInputTokens, event.CacheCreationTokens, event.ReasoningTokens,
			event.OccurredAt.UTC().Format(time.RFC3339Nano), receivedAt, event.OwnerUserID, event.TriggeredUserID,
			event.SessionProfileID, event.TriggerType, event.TriggerID, event.WebhookID, event.ScheduleID, event.SlackbotID)
		if err != nil {
			return entities.UsageInsertResult{}, fmt.Errorf("insert usage event: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return entities.UsageInsertResult{}, fmt.Errorf("read usage insert result: %w", err)
		}
		if n == 0 {
			result.Duplicates++
		} else {
			result.Accepted++
		}
	}
	if err := tx.Commit(); err != nil {
		return entities.UsageInsertResult{}, fmt.Errorf("commit usage transaction: %w", err)
	}
	return result, nil
}

func (r *LibSQLUsageRepository) Close() error { return r.db.Close() }

func (r *LibSQLUsageRepository) Aggregate(ctx context.Context, query entities.UsageQuery) (entities.UsageSummary, error) {
	where, args := usageWhere(query)
	columns := `COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
COALESCE(SUM(cached_input_tokens),0), COALESCE(SUM(cache_creation_tokens),0), COALESCE(SUM(reasoning_tokens),0)`
	statement := `SELECT ` + columns + ` FROM agentapi_usage_events WHERE ` + where
	var summary entities.UsageSummary
	if err := r.db.QueryRowContext(ctx, statement, args...).Scan(&summary.Events, &summary.InputTokens, &summary.OutputTokens,
		&summary.CachedInputTokens, &summary.CacheCreationTokens, &summary.ReasoningTokens); err != nil {
		return entities.UsageSummary{}, fmt.Errorf("aggregate usage events: %w", err)
	}
	var err error
	summary.ByModel, err = r.aggregateBy(ctx, "model", where, args)
	if err != nil {
		return entities.UsageSummary{}, err
	}
	summary.BySession, err = r.aggregateBy(ctx, "session_id", where, args)
	if err != nil {
		return entities.UsageSummary{}, err
	}
	return summary, nil
}

func (r *LibSQLUsageRepository) ListEvents(ctx context.Context, query entities.UsageQuery) ([]entities.UsageEvent, error) {
	where, args := usageWhere(query)
	statement := `SELECT session_id, agent_session_id, agent_type, provider, model,
input_tokens, output_tokens, cached_input_tokens, cache_creation_tokens, reasoning_tokens, occurred_at,
COALESCE(owner_user_id,''), COALESCE(triggered_user_id,''), COALESCE(session_profile_id,''),
COALESCE(trigger_type,''), COALESCE(trigger_id,''), COALESCE(webhook_id,''), COALESCE(schedule_id,''), COALESCE(slackbot_id,'')
FROM agentapi_usage_events WHERE ` + where + ` ORDER BY occurred_at ASC`
	if query.Limit > 0 {
		statement += ` LIMIT ?`
		args = append(args, query.Limit)
	}
	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("list usage events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := []entities.UsageEvent{}
	for rows.Next() {
		var event entities.UsageEvent
		var occurredAt string
		if err := rows.Scan(&event.SessionID, &event.AgentSessionID, &event.AgentType, &event.Provider, &event.Model,
			&event.InputTokens, &event.OutputTokens, &event.CachedInputTokens, &event.CacheCreationTokens,
			&event.ReasoningTokens, &occurredAt, &event.OwnerUserID, &event.TriggeredUserID, &event.SessionProfileID,
			&event.TriggerType, &event.TriggerID, &event.WebhookID, &event.ScheduleID, &event.SlackbotID); err != nil {
			return nil, fmt.Errorf("scan usage event: %w", err)
		}
		event.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("parse usage event occurred_at: %w", err)
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func usageWhere(query entities.UsageQuery) (string, []interface{}) {
	where := []string{"1=1"}
	args := []interface{}{}
	for column, value := range map[string]string{
		"session_id": query.SessionID, "user_id": query.UserID, "team_id": query.TeamID,
		"agent_type": query.AgentType, "provider": query.Provider, "model": query.Model,
	} {
		if value != "" {
			where = append(where, column+" = ?")
			args = append(args, value)
		}
	}
	if query.From != nil {
		where = append(where, "occurred_at >= ?")
		args = append(args, query.From.UTC().Format(time.RFC3339Nano))
	}
	if query.To != nil {
		where = append(where, "occurred_at < ?")
		args = append(args, query.To.UTC().Format(time.RFC3339Nano))
	}
	return strings.Join(where, " AND "), args
}

func (r *LibSQLUsageRepository) aggregateBy(ctx context.Context, column, where string, args []interface{}) ([]entities.UsageBreakdown, error) {
	statement := `SELECT ` + column + `, COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
COALESCE(SUM(cached_input_tokens),0), COALESCE(SUM(cache_creation_tokens),0), COALESCE(SUM(reasoning_tokens),0)
FROM agentapi_usage_events WHERE ` + where + ` GROUP BY ` + column + ` ORDER BY SUM(input_tokens + output_tokens + cached_input_tokens + cache_creation_tokens) DESC`
	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate usage by %s: %w", column, err)
	}
	defer func() { _ = rows.Close() }()
	result := []entities.UsageBreakdown{}
	for rows.Next() {
		var item entities.UsageBreakdown
		if err := rows.Scan(&item.Key, &item.Events, &item.InputTokens, &item.OutputTokens, &item.CachedInputTokens, &item.CacheCreationTokens, &item.ReasoningTokens); err != nil {
			return nil, fmt.Errorf("scan usage by %s: %w", column, err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
