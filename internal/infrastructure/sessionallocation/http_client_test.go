package sessionallocation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestExternalClientTracksRuntimeProfileRevisionAndFetchesSnapshot(t *testing.T) {
	allocationPolls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/internal/external-session-manager/allocations/next":
			allocationPolls++
			wantRevision := ""
			if allocationPolls > 1 {
				wantRevision = "revision-1"
			}
			if got := r.URL.Query().Get("profile_revision"); got != wantRevision {
				t.Errorf("profile_revision = %q, want %q", got, wantRevision)
			}
			w.Header().Set("X-AgentAPI-Runtime-Profile-Revision", "revision-1")
			w.WriteHeader(http.StatusNoContent)
		case "/internal/external-session-manager/runtime-profile":
			_ = json.NewEncoder(w).Encode(core.RuntimeProfileSnapshot{
				Revision: "revision-1",
				Profile:  &sessionsettings.RuntimeProfile{Version: 1},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token")
	if _, ok, err := client.NextExternal(context.Background(), time.Second); err != nil || ok {
		t.Fatalf("NextExternal() = ok %t, err %v", ok, err)
	}
	if got := client.RuntimeProfileRevision(); got != "revision-1" {
		t.Fatalf("revision = %q, want revision-1", got)
	}
	if _, ok, err := client.NextExternal(context.Background(), time.Second); err != nil || ok {
		t.Fatalf("second NextExternal() = ok %t, err %v", ok, err)
	}
	snapshot, err := client.GetRuntimeProfile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != "revision-1" || snapshot.Profile == nil || snapshot.Profile.Version != 1 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}
