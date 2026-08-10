package bridge

import (
	"encoding/json"
	"testing"
)

func TestPendingAgentRequestsAreAvailableUntilReplyFinishes(t *testing.T) {
	b := &Bridge{
		pendingReplies:  make(map[int64]chan json.RawMessage),
		pendingRequests: make(map[int64]json.RawMessage),
	}

	id, _ := b.beginAgentRequest("session/request_permission", map[string]string{
		"sessionId": "session-1",
	})

	pending := b.PendingAgentRequests()
	if len(pending) != 1 {
		t.Fatalf("got %d pending requests, want 1", len(pending))
	}

	var msg jsonRPCMsg
	if err := json.Unmarshal(pending[0], &msg); err != nil {
		t.Fatalf("unmarshal pending request: %v", err)
	}
	if msg.Method != "session/request_permission" {
		t.Fatalf("unexpected pending method: %q", msg.Method)
	}

	b.finishAgentRequest(id)
	if pending := b.PendingAgentRequests(); len(pending) != 0 {
		t.Fatalf("got %d pending requests after finishing, want 0", len(pending))
	}
}
