package provisioner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
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

func TestNativeSessionDoesNotImplicitlyRestoreNewSession(t *testing.T) {
	t.Setenv("AGENTAPI_NATIVE_SESSION_ROOT", t.TempDir())
	settings := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{PersistenceEnabled: true}}
	if shouldImplicitlyRestoreSessionState(settings) {
		t.Fatal("native session unexpectedly enabled implicit restore")
	}
}

func TestKubernetesSessionImplicitlyRestoresPersistentSession(t *testing.T) {
	t.Setenv("AGENTAPI_NATIVE_SESSION_ROOT", "")
	settings := &sessionsettings.SessionSettings{Session: sessionsettings.SessionMeta{PersistenceEnabled: true}}
	if !shouldImplicitlyRestoreSessionState(settings) {
		t.Fatal("persistent Kubernetes session unexpectedly disabled implicit restore")
	}
}
