package acp

import (
	"encoding/json"
	"testing"
)

func TestRequestPermissionParamsPreservesExitPlanModeContent(t *testing.T) {
	var params RequestPermissionParams
	err := json.Unmarshal([]byte(`{
		"sessionId":"session-1",
		"options":[],
		"toolCall":{
			"toolCallId":"tool-1",
			"kind":"switch_mode",
			"title":"Ready to code?",
			"content":[{"type":"content","content":{"type":"text","text":"# Plan\n\nImplement it."}}],
			"rawInput":{"plan":"# Plan\n\nImplement it."}
		}
	}`), &params)
	if err != nil {
		t.Fatalf("unmarshal request permission params: %v", err)
	}

	if params.ToolCall.Title != "Ready to code?" {
		t.Fatalf("unexpected title: %q", params.ToolCall.Title)
	}
	if len(params.ToolCall.Content) == 0 {
		t.Fatal("structured plan content was dropped")
	}
	if len(params.ToolCall.RawInput) == 0 {
		t.Fatal("raw plan input was dropped")
	}
}
