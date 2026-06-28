package bridge

import (
	"encoding/json"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/acp"
)

func TestMessagesReturnsMessagesSinceLastUserMessage(t *testing.T) {
	b := New(nil, "session-1", false, "", false)

	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindUserMessageChunk, 0))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindAgentMessageChunk, 1))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindToolCall, 2))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindUserMessageChunk, 3))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindToolCall, 4))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindToolCallUpdate, 5))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindAgentMessageChunk, 6))

	msgs := b.Messages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}

	for i, msg := range msgs {
		want := i + 3
		if got := messageIndex(t, msg); got != want {
			t.Fatalf("message %d index = %d, want %d", i, got, want)
		}
	}
}

func TestMessagesReturnsAllMessagesWhenNoUserMessageExists(t *testing.T) {
	b := New(nil, "session-1", false, "", false)

	for i := 0; i < 3; i++ {
		b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindToolCall, i))
	}

	msgs := b.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

func testIndexedUpdate(t *testing.T, kind acp.SessionUpdateKind, index int) jsonRPCMsg {
	t.Helper()
	content, err := json.Marshal(map[string]interface{}{
		"type":  "text",
		"text":  "message",
		"index": index,
	})
	if err != nil {
		t.Fatalf("failed to marshal content: %v", err)
	}
	return jsonRPCMsg{
		JSONRPC: "2.0",
		Method:  "session/update",
		Params: sessionUpdateParams{
			SessionId: "session-1",
			Update: acp.SessionUpdate{
				Kind:    kind,
				Content: content,
			},
		},
	}
}

func messageIndex(t *testing.T, msg json.RawMessage) int {
	t.Helper()
	var got struct {
		Params struct {
			Update struct {
				Content json.RawMessage `json:"content"`
			} `json:"update"`
		} `json:"params"`
	}
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}
	var content struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal(got.Params.Update.Content, &content); err != nil {
		t.Fatalf("failed to unmarshal content: %v", err)
	}
	return content.Index
}
