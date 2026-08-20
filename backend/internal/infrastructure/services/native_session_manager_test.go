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

func TestNativeSessionWithNilRepositorySettingsDerivesPathsFromVirtualHome(t *testing.T) {
	req := &entities.RunServerRequest{}
	stateDir := t.TempDir()
	t.Setenv("AGENTAPI_WORKDIR", "/inherited/workdir")
	t.Setenv("AGENTAPI_REPO_DIR", "/inherited/repo")
	m, err := NewNativeSessionManager(stateDir, "http://127.0.0.1:8080", "token", "", "/bin/true", false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := m.CreateSessionDirect(context.Background(), "native-1", req, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.DeleteSession(session.ID()) })

	if req.RepoInfo != nil || req.ProvisionSettings != nil {
		t.Fatal("empty native request unexpectedly gained repository settings")
	}
	native := session.(*NativeSession)
	wantHome := filepath.Join(stateDir, "sessions", "native-1", "home")
	foundHome := false
	foundProxyBinary := false
	for _, value := range native.cmd.Env {
		if value == "HOME="+wantHome {
			foundHome = true
		}
		if strings.HasPrefix(value, "AGENTAPI_WORKDIR=") || strings.HasPrefix(value, "AGENTAPI_REPO_DIR=") {
			t.Fatalf("native path override was retained: %q", value)
		}
		if value == "CCPLANT_BINARY_PATH=/bin/true" {
			foundProxyBinary = true
		}
	}
	if !foundHome {
		t.Fatalf("virtual HOME %q was not configured", wantHome)
	}
	if !foundProxyBinary {
		t.Fatal("managed agentapi-proxy binary was not passed to provisioner")
	}
}

func TestNativeProvisionerEnvironmentOverridesInheritedProxyBinary(t *testing.T) {
	env := nativeProvisionerEnvironment(
		[]string{"PATH=/usr/bin", "HOME=/home/agentapi", "CCPLANT_BINARY_PATH=/usr/local/bin/agentapi-proxy"},
		"HOME=/var/lib/agentapi-native/sessions/one/home",
		"CCPLANT_BINARY_PATH=/app/Contents/MacOS/agentapi-proxy",
	)
	want := "CCPLANT_BINARY_PATH=/app/Contents/MacOS/agentapi-proxy"
	count := 0
	for _, value := range env {
		if strings.HasPrefix(value, "CCPLANT_BINARY_PATH=") {
			count++
			if value != want {
				t.Fatalf("proxy binary = %q, want %q", value, want)
			}
		}
	}
	if count != 1 {
		t.Fatalf("proxy binary entry count = %d, want 1", count)
	}
	homeCount := 0
	for _, value := range env {
		if strings.HasPrefix(value, "HOME=") {
			homeCount++
			if value != "HOME=/var/lib/agentapi-native/sessions/one/home" {
				t.Fatalf("HOME = %q", value)
			}
		}
	}
	if homeCount != 1 {
		t.Fatalf("HOME entry count = %d, want 1", homeCount)
	}
}

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
	deadline := time.Now().Add(2 * time.Second)
	for !nativeProcessMatchesSession(process.Process.Pid, root) {
		if time.Now().After(deadline) {
			t.Fatal("native process environment was not visible before timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
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

func TestNativeSessionManagerGetMissingSessionReturnsNil(t *testing.T) {
	m, err := NewNativeSessionManager(t.TempDir(), "http://127.0.0.1:8080", "token", "", os.Args[0], false)
	if err != nil {
		t.Fatal(err)
	}
	if session := m.GetSession("missing"); session != nil {
		t.Fatalf("missing session returned a non-nil interface: %#v", session)
	}
}

func TestNativeSessionManagerRemovesFinishedOneshotSession(t *testing.T) {
	stateDir := t.TempDir()
	m, err := NewNativeSessionManager(stateDir, "http://127.0.0.1:8080", "token", "", "/bin/true", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.CreateSessionDirect(context.Background(), "oneshot-1", &entities.RunServerRequest{Oneshot: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(stateDir, "sessions", "oneshot-1")
	deadline := time.Now().Add(3 * time.Second)
	for m.GetSession("oneshot-1") != nil {
		if time.Now().After(deadline) {
			t.Fatal("finished oneshot session was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("oneshot session directory still exists: %v", err)
	}
}

func TestNativeSessionManagerActiveSessionCountExcludesTerminalStates(t *testing.T) {
	m, err := NewNativeSessionManager(t.TempDir(), "http://127.0.0.1:8080", "token", "", os.Args[0], false)
	if err != nil {
		t.Fatal(err)
	}
	for id, status := range map[string]string{
		"creating": "creating", "running": "running", "error": "error", "stopped": "stopped",
	} {
		m.sessions[id] = &NativeSession{id: id, request: &entities.RunServerRequest{}, status: status}
	}
	if got := m.ActiveSessionCount(); got != 2 {
		t.Fatalf("active session count = %d, want 2", got)
	}
}

func TestNativeSessionManagerRemovesDeadSessionStateOnRestore(t *testing.T) {
	stateDir := t.TempDir()
	root := filepath.Join(stateDir, "sessions", "dead-1")
	if err := os.MkdirAll(filepath.Join(root, "runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	state := nativeSessionState{
		ID: "dead-1", Request: &entities.RunServerRequest{}, RootDir: root,
		PID: 1 << 30, Status: "running",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runtime", "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := NewNativeSessionManager(stateDir, "http://127.0.0.1:8080", "token", "", os.Args[0], false)
	if err != nil {
		t.Fatal(err)
	}
	if m.GetSession("dead-1") != nil {
		t.Fatal("dead session was restored")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("dead session directory still exists: %v", err)
	}
}

func TestNativeProvisionRequestPullLifecycle(t *testing.T) {
	m, err := NewNativeSessionManager(t.TempDir(), "http://127.0.0.1:8080", "token", "", os.Args[0], false)
	if err != nil {
		t.Fatal(err)
	}
	m.provisionRequests["session-1"] = &ProvisionRequest{RequestID: "request-1", SessionID: "session-1", Status: "pending"}
	m.sessions["session-1"] = &NativeSession{id: "session-1", request: &entities.RunServerRequest{}, rootDir: t.TempDir(), status: "starting"}
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
	if err := m.UpdateProvisionRequestStatus(context.Background(), "session-1", "request-1", ProvisionRequestStatusUpdate{
		Status: "error", Message: "failed to start agent: executable not found",
	}); err != nil {
		t.Fatal(err)
	}
	if got := m.sessions["session-1"].StatusMessage(); got != "failed to start agent: executable not found" {
		t.Fatalf("StatusMessage() = %q", got)
	}
}
