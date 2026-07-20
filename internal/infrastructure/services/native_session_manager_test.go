package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestNativeSessionManagerRestoresLiveSessionState(t *testing.T) {
	stateDir := t.TempDir()
	root := filepath.Join(stateDir, "sessions", "native-1")
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(statusServer.Close)
	port, err := strconv.Atoi(statusServer.URL[strings.LastIndex(statusServer.URL, ":")+1:])
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command("sleep", "30")
	process.Env = append(os.Environ(), "AGENTAPI_NATIVE_SESSION_ROOT="+root)
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = process.Process.Kill() })
	now := time.Now().UTC().Truncate(time.Second)
	state := nativeSessionState{ID: "native-1", Request: &entities.RunServerRequest{UserID: "user-1", Tags: map[string]string{"allocator.os": "linux"}}, RootDir: root, AgentPort: port, ProvisionerPort: 42001, PID: process.Process.Pid, StartedAt: now, UpdatedAt: now, LastMessageAt: now, Status: "running", FilesystemSandbox: false}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(root, "runtime", "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewNativeSessionManager(stateDir, "http://127.0.0.1:8080", "token", "", os.Args[0], false)
	if err != nil {
		t.Fatal(err)
	}
	s := m.GetSession("native-1")
	if s == nil || s.UserID() != "user-1" || s.Addr() != "127.0.0.1:"+strconv.Itoa(port) || s.Status() != "running" {
		t.Fatalf("unexpected restored session: %#v", s)
	}
}

func TestNativeProvisionRequestPullLifecycle(t *testing.T) {
	m, err := NewNativeSessionManager(t.TempDir(), "http://127.0.0.1:8080", "token", "", os.Args[0], false)
	if err != nil {
		t.Fatal(err)
	}
	m.provisionRequests["session-1"] = &ProvisionRequest{RequestID: "request-1", SessionID: "session-1", Status: "pending"}
	if err := m.ConnectProvisioner(context.Background(), ProvisionerConnectRequest{SessionID: "session-1", PodName: "native-worker"}); err != nil {
		t.Fatal(err)
	}
	req, ok, err := m.ClaimProvisionRequest(context.Background(), "session-1", "native-worker")
	if err != nil || !ok || req.RequestID != "request-1" {
		t.Fatalf("claim = %#v, %v, %v", req, ok, err)
	}
	if _, ok, err := m.ClaimProvisionRequest(context.Background(), "session-1", "other"); err != nil || ok {
		t.Fatalf("duplicate claim ok=%v err=%v", ok, err)
	}
}
