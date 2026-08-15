package usagecollector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectClaudeTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	content := `{"type":"user","message":{"content":"secret"}}
{"uuid":"turn-1","timestamp":"2026-08-08T12:00:00Z","message":{"id":"resp-1","model":"claude-test","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":30,"cache_creation_input_tokens":4}}}
{"uuid":"turn-2","message":{"id":"resp-2","model":"claude-test","usage":{"input_tokens":12,"output_tokens":3}}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	hook, _ := json.Marshal(map[string]string{"session_id": "agent-session", "transcript_path": path})
	events, err := Collect(hook, "claude-acp")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Model != "claude-test" || events[0].InputTokens != 100 || events[0].CachedInputTokens != 30 {
		t.Fatalf("unexpected first event: %#v", events[0])
	}
	if events[0].EventID == events[1].EventID {
		t.Fatal("event IDs must be distinct")
	}

	again, err := Collect(hook, "claude-acp")
	if err != nil {
		t.Fatal(err)
	}
	if again[0].EventID != events[0].EventID {
		t.Fatal("event ID is not stable across retries")
	}
}

func TestCollectRequiresTranscriptPath(t *testing.T) {
	if _, err := Collect([]byte(`{"session_id":"s"}`), "codex-acp"); err == nil {
		t.Fatal("expected missing transcript path error")
	}
}

func TestCollectCodexTokenCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	content := `{"timestamp":"2026-08-08T13:41:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":9999},"last_token_usage":{"input_tokens":14916,"cached_input_tokens":11008,"cache_write_input_tokens":7,"output_tokens":16,"reasoning_output_tokens":3,"total_tokens":14932}}}}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	hook, _ := json.Marshal(map[string]string{"thread_id": "codex-thread", "transcript_path": path})
	events, err := Collect(hook, "codex-acp")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.InputTokens != 14916 || got.OutputTokens != 16 || got.CachedInputTokens != 11008 || got.CacheCreationTokens != 7 || got.ReasoningTokens != 3 {
		t.Fatalf("unexpected Codex usage event: %#v", got)
	}
}
