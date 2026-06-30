package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
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

func TestMessagesForUserPromptIndexReturnsTurnSlice(t *testing.T) {
	b := New(nil, "session-1", false, "", false)

	b.broadcast(userPromptUpdate(t, "first prompt"))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindAgentMessageChunk, 1))
	b.broadcast(userPromptUpdate(t, "second prompt"))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindToolCall, 3))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindAgentMessageChunk, 4))

	if got := b.UserPromptCount(); got != 2 {
		t.Fatalf("UserPromptCount = %d, want 2", got)
	}

	turn0, ok := b.MessagesForUserPromptIndex(0)
	if !ok {
		t.Fatal("expected turn 0 to exist")
	}
	if len(turn0) != 2 {
		t.Fatalf("turn 0 length = %d, want 2", len(turn0))
	}

	turn1, ok := b.MessagesForUserPromptIndex(1)
	if !ok {
		t.Fatal("expected turn 1 to exist")
	}
	if len(turn1) != 3 {
		t.Fatalf("turn 1 length = %d, want 3", len(turn1))
	}

	if _, ok := b.MessagesForUserPromptIndex(2); ok {
		t.Fatal("expected turn 2 to be out of range")
	}
}

func TestUserPromptInfosIncludePreview(t *testing.T) {
	b := New(nil, "session-1", false, "", false)
	b.broadcast(userPromptUpdate(t, "hello from user"))

	infos := b.UserPromptInfos()
	if len(infos) != 1 {
		t.Fatalf("expected 1 user prompt info, got %d", len(infos))
	}
	if infos[0].Index != 0 {
		t.Fatalf("index = %d, want 0", infos[0].Index)
	}
	if infos[0].Preview != "hello from user" {
		t.Fatalf("preview = %q, want %q", infos[0].Preview, "hello from user")
	}
}

func TestHandleGetMessagesUserPromptIndex(t *testing.T) {
	b := New(nil, "session-1", false, "", false)
	b.broadcast(userPromptUpdate(t, "turn one"))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindAgentMessageChunk, 1))
	b.broadcast(userPromptUpdate(t, "turn two"))
	b.broadcast(testIndexedUpdate(t, acp.SessionUpdateKindAgentMessageChunk, 3))

	srv := NewServer(b, false)
	e := echo.New()

	req := httptest.NewRequest(http.MethodGet, "/messages?userPromptIndex=0", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/messages")

	if err := srv.handleGetMessages(c); err != nil {
		t.Fatalf("handleGetMessages returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Messages        []json.RawMessage `json:"messages"`
		UserPromptCount int               `json:"userPromptCount"`
		UserPromptIndex int               `json:"userPromptIndex"`
		UserPrompts     []UserPromptInfo  `json:"userPrompts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("messages length = %d, want 2", len(resp.Messages))
	}
	if resp.UserPromptCount != 2 {
		t.Fatalf("userPromptCount = %d, want 2", resp.UserPromptCount)
	}
	if resp.UserPromptIndex != 0 {
		t.Fatalf("userPromptIndex = %d, want 0", resp.UserPromptIndex)
	}
	if len(resp.UserPrompts) != 2 {
		t.Fatalf("userPrompts length = %d, want 2", len(resp.UserPrompts))
	}
}

func TestHandleGetMessagesInvalidUserPromptIndex(t *testing.T) {
	b := New(nil, "session-1", false, "", false)
	srv := NewServer(b, false)
	e := echo.New()

	req := httptest.NewRequest(http.MethodGet, "/messages?userPromptIndex=abc", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/messages")

	if err := srv.handleGetMessages(c); err != nil {
		t.Fatalf("handleGetMessages returned error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
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

func userPromptUpdate(t *testing.T, text string) jsonRPCMsg {
	t.Helper()
	content, err := json.Marshal(map[string]interface{}{
		"type": "text",
		"text": text,
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
				Kind:    acp.SessionUpdateKindUserMessageChunk,
				Content: content,
			},
		},
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

func TestUserPromptPreviewTruncation(t *testing.T) {
	longText := strings.Repeat("あ", userPromptPreviewMaxLen+10)
	preview := userPromptPreviewFromRawLocked(mustMarshalUserPrompt(t, longText))
	if len([]rune(preview)) != userPromptPreviewMaxLen+1 {
		t.Fatalf("preview rune length = %d, want %d", len([]rune(preview)), userPromptPreviewMaxLen+1)
	}
	if !strings.HasSuffix(preview, "…") {
		t.Fatalf("preview should be truncated with ellipsis: %q", preview)
	}
}

func mustMarshalUserPrompt(t *testing.T, text string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(userPromptUpdate(t, text))
	if err != nil {
		t.Fatalf("failed to marshal user prompt: %v", err)
	}
	return raw
}
