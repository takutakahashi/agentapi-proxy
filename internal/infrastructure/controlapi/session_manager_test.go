package controlapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSessionManagerDelegatesCreateAndStockToControlAPI(t *testing.T) {
	requests := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		requests <- r.Method + " " + r.URL.RequestURI()
		switch r.URL.Path {
		case "/internal/worker/sessions/session-1":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(sessionInfo{ID: "session-1", UserID: "user", Scope: entities.ScopeUser, Status: "running"})
		case "/internal/worker/stock":
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewSessionManager(server.URL, "token")
	if _, err := client.CreateSession(context.Background(), "session-1", &entities.RunServerRequest{UserID: "user"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := client.CreateStockSession(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := <-requests; got != "POST /internal/worker/sessions/session-1" {
		t.Fatalf("request = %q", got)
	}
	if got := <-requests; got != "POST /internal/worker/stock?dind=true" {
		t.Fatalf("request = %q", got)
	}
}
