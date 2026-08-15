package provisioner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestDirectRuntimePollAndExecute(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message" || r.URL.Query().Get("mode") != "fast" {
			t.Errorf("local request URI=%s", r.URL.RequestURI())
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer local.Close()

	framesCh := make(chan []core.ResponseFrame, 4)
	parent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer runtime-secret" || r.URL.Query().Get("generation") != "4" {
			t.Errorf("missing runtime authentication: %s %s", r.Header.Get("Authorization"), r.URL.RawQuery)
		}
		switch r.Method + " " + r.URL.Path {
		case http.MethodGet + " /internal/session-runtime/public-session/requests":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"requests":    []core.Command{{ID: "request-1", StreamID: "1-0", Method: http.MethodPost, Path: "/message", RawQuery: "mode=fast", Body: []byte(`{"content":"hello"}`), Deadline: time.Now().Add(time.Minute)}},
				"next_cursor": "1-0",
			})
		case http.MethodPost + " /internal/session-runtime/public-session/frames":
			var body struct {
				Frames []core.ResponseFrame `json:"frames"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			framesCh <- body.Frames
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer parent.Close()

	worker := &directRuntimeWorker{
		cfg:    &sessionsettings.ParentRuntimeConfig{Enabled: true, Endpoint: parent.URL, SessionID: "public-session", Token: "runtime-secret", Generation: 4},
		client: parent.Client(), localURL: local.URL, instanceID: "pod-a",
	}
	requests, next, err := worker.poll(context.Background(), "0-0")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || next != "1-0" {
		t.Fatalf("requests=%#v next=%q", requests, next)
	}
	worker.execute(context.Background(), requests[0])

	var all []core.ResponseFrame
	for len(all) < 3 {
		select {
		case frames := <-framesCh:
			all = append(all, frames...)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for frames: %#v", all)
		}
	}
	if all[0].Status != http.StatusCreated || string(all[1].Body) != `{"ok":true}` || !all[2].Done || all[2].CommandStreamID != "1-0" {
		t.Fatalf("unexpected frames: %#v", all)
	}
}
