package controllers

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestEncodeUsageParquet(t *testing.T) {
	occurredAt := time.Date(2026, 8, 8, 12, 34, 56, 0, time.UTC)
	data, err := encodeUsageParquet([]entities.UsageEvent{{
		SessionID: "session-1", AgentSessionID: "thread-1", AgentType: "codex-acp",
		Provider: "openai", Model: "gpt-test", InputTokens: 10, OutputTokens: 2,
		OwnerUserID: "user-1", TriggeredUserID: "actor-1", SessionProfileID: "profile-1",
		TriggerType: "webhook", TriggerID: "webhook-1", WebhookID: "webhook-1",
		CachedInputTokens: 4, CacheCreationTokens: 1, ReasoningTokens: 3, OccurredAt: occurredAt,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || string(data[:4]) != "PAR1" || string(data[len(data)-4:]) != "PAR1" {
		t.Fatal("output is not a parquet file")
	}
	reader := parquet.NewGenericReader[usageParquetRow](bytes.NewReader(data))
	defer func() { _ = reader.Close() }()
	rows := make([]usageParquetRow, 2)
	n, err := reader.Read(rows)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n != 1 || rows[0].SessionID != "session-1" || rows[0].Model != "gpt-test" || rows[0].InputTokens != 10 ||
		rows[0].OwnerUserID != "user-1" || rows[0].SessionProfileID != "profile-1" || rows[0].TriggerID != "webhook-1" || !rows[0].OccurredAt.Equal(occurredAt) {
		t.Fatalf("unexpected parquet row: %#v", rows[:n])
	}
}
