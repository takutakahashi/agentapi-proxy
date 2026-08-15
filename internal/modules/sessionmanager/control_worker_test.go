package sessionmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
)

func TestControlWorkerPollsAndReturnsResponseFrames(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/remote/status" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"active"}`))
	}))
	defer local.Close()

	var mu sync.Mutex
	var received []core.ResponseFrame
	done := make(chan struct{})
	var once sync.Once
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer manager-token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/internal/external-session-manager/control/commands":
			if r.URL.Query().Get("after") != "0-0" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"commands": []core.Command{{ID: "request-1", StreamID: "1-0", ManagerID: "manager-a", SessionID: "public", RemoteSessionID: "remote", Method: http.MethodGet, Path: "/remote/status", Deadline: time.Now().Add(time.Minute)}}, "next_cursor": "1-0"})
		case "/internal/external-session-manager/control/frames":
			var body struct {
				Frames []core.ResponseFrame `json:"frames"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			received = append(received, body.Frames...)
			for _, frame := range body.Frames {
				if frame.Done {
					once.Do(func() { close(done) })
				}
			}
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer parent.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go NewControlWorker(parent.URL, "manager-token", "", local.URL, "instance-a", "local-secret").Start(ctx)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for frames")
	}
	cancel()
	mu.Lock()
	defer mu.Unlock()
	if len(received) < 3 || received[0].Status != http.StatusOK || string(received[1].Body) != `{"status":"active"}` || !received[len(received)-1].Done {
		t.Fatalf("unexpected frames: %#v", received)
	}
}
