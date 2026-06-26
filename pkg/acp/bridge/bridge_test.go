package bridge

import (
	"encoding/json"
	"testing"
)

func TestMessagesReturnsMostRecentTwentyMessages(t *testing.T) {
	b := New(nil, "session-1", false, "", false)

	for i := 0; i < 25; i++ {
		b.broadcast(jsonRPCMsg{
			JSONRPC: "2.0",
			Method:  "session/update",
			Params:  map[string]int{"index": i},
		})
	}

	msgs := b.Messages()
	if len(msgs) != messagesHistoryLimit {
		t.Fatalf("expected %d messages, got %d", messagesHistoryLimit, len(msgs))
	}

	for i, msg := range msgs {
		var got struct {
			Params map[string]int `json:"params"`
		}
		if err := json.Unmarshal(msg, &got); err != nil {
			t.Fatalf("failed to unmarshal message %d: %v", i, err)
		}
		want := i + 5
		if got.Params["index"] != want {
			t.Fatalf("message %d index = %d, want %d", i, got.Params["index"], want)
		}
	}
}

func TestMessagesReturnsAllMessagesWhenHistoryIsUnderLimit(t *testing.T) {
	b := New(nil, "session-1", false, "", false)

	for i := 0; i < 3; i++ {
		b.broadcast(jsonRPCMsg{
			JSONRPC: "2.0",
			Method:  "session/update",
			Params:  map[string]int{"index": i},
		})
	}

	msgs := b.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}
