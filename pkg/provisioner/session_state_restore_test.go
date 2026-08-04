package provisioner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRestoreSessionStateNotFoundIsAnEmptyInitialSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/session-state/session-1/download-url" {
			w.WriteHeader(http.StatusNotImplemented)
			return
		}
		if r.URL.Path != "/internal/session-state/session-1" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer provisioner-token" {
			t.Fatalf("authorization header was not propagated")
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	t.Setenv("PROVISIONER_PROXY_URL", server.URL)
	t.Setenv("PROVISIONER_TOKEN", "provisioner-token")

	found, err := (&Server{httpClient: server.Client()}).restoreSessionState(context.Background(), "session-1", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("missing initial snapshot was reported as restored")
	}
}

func TestRestoreSessionStateUnavailableCanBeSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("PROVISIONER_PROXY_URL", server.URL)
	t.Setenv("PROVISIONER_TOKEN", "provisioner-token")

	found, err := (&Server{httpClient: server.Client()}).restoreSessionState(context.Background(), "session-1", t.TempDir())
	if found {
		t.Fatal("unavailable backend was reported as restored")
	}
	if !errors.Is(err, errSessionStateBackendUnavailable) {
		t.Fatalf("error = %v, want errSessionStateBackendUnavailable", err)
	}
}
