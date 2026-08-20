package repositories

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestLibSQLUsageRepositoryDeduplicatesEvents(t *testing.T) {
	repo, err := NewLibSQLUsageRepository(context.Background(), "file://"+filepath.Join(t.TempDir(), "usage.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	event := entities.UsageEvent{EventID: "event-1", SessionID: "session-1", UserID: "user-1", Scope: "user", AgentType: "claude-acp", Model: "model-1", InputTokens: 10, OutputTokens: 2, OwnerUserID: "user-1", SessionProfileID: "profile-1", TriggerType: "schedule", TriggerID: "schedule-1", ScheduleID: "schedule-1", OccurredAt: time.Now()}
	first, err := repo.InsertEvents(context.Background(), []entities.UsageEvent{event})
	if err != nil {
		t.Fatal(err)
	}
	if first.Accepted != 1 || first.Duplicates != 0 {
		t.Fatalf("first = %#v", first)
	}
	second, err := repo.InsertEvents(context.Background(), []entities.UsageEvent{event})
	if err != nil {
		t.Fatal(err)
	}
	if second.Accepted != 0 || second.Duplicates != 1 {
		t.Fatalf("second = %#v", second)
	}
	summary, err := repo.Aggregate(context.Background(), entities.UsageQuery{UserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Events != 1 || summary.InputTokens != 10 || summary.OutputTokens != 2 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(summary.ByModel) != 1 || summary.ByModel[0].Key != "model-1" || len(summary.BySession) != 1 || summary.BySession[0].Key != "session-1" {
		t.Fatalf("breakdowns = model:%#v session:%#v", summary.ByModel, summary.BySession)
	}
	events, err := repo.ListEvents(context.Background(), entities.UsageQuery{UserID: "user-1", Model: "model-1", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].SessionID != "session-1" || events[0].InputTokens != 10 || events[0].OwnerUserID != "user-1" || events[0].SessionProfileID != "profile-1" || events[0].ScheduleID != "schedule-1" {
		t.Fatalf("events = %#v", events)
	}
}
