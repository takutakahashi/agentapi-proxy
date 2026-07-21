package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegisterNativeManager(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/external-session-managers", r.URL.Path)
		require.Equal(t, "Bearer install-key", r.Header.Get("Authorization"))
		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "machine-1", body["instance_id"])
		_ = json.NewEncoder(w).Encode(nativeRegistrationResponse{ID: "manager-1", InstanceID: "machine-1", ConnectionToken: "connection-token", Created: true})
	}))
	defer server.Close()

	result, err := registerNativeManager(server.URL, "install-key", map[string]string{"instance_id": "machine-1"})
	require.NoError(t, err)
	require.Equal(t, "manager-1", result.ID)
	require.Equal(t, "connection-token", result.ConnectionToken)
}

func TestNativeConfigPersistsSeparateInstanceID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	credentialsPath := filepath.Join(dir, "credentials.json")
	want := nativeDaemonConfig{ManagerID: "manager-1", InstanceID: "machine-1", ConnectionToken: "secret", CredentialsPath: credentialsPath,
		ManagerEnvironment: map[string]string{"PATH": "/opt/mise/bin:/usr/bin:/bin"}, FilesystemSandbox: nativeFilesystemSandboxConfig{Enabled: true}}
	data, err := json.Marshal(want)
	require.NoError(t, err)
	var stored nativeDaemonConfig
	require.NoError(t, json.Unmarshal(data, &stored))
	stored.ConnectionToken = ""
	data, err = json.Marshal(stored)
	require.NoError(t, err)
	require.NoError(t, atomicWriteFile(path, data, 0o600))
	credentials, err := json.Marshal(map[string]string{"connection_token": want.ConnectionToken})
	require.NoError(t, err)
	require.NoError(t, atomicWriteFile(credentialsPath, credentials, 0o600))

	got, err := readNativeConfig(path)
	require.NoError(t, err)
	require.Equal(t, want, got)
	configBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(configBytes), "secret")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestParseNativeManagerEnvironment(t *testing.T) {
	key, value, err := parseNativeManagerEnvironment("PATH=/opt/mise/bin:/usr/bin:/bin")
	require.NoError(t, err)
	require.Equal(t, "PATH", key)
	require.Equal(t, "/opt/mise/bin:/usr/bin:/bin", value)

	_, _, err = parseNativeManagerEnvironment("1INVALID=value")
	require.EqualError(t, err, `invalid --manager-env "1INVALID=value"; expected KEY=VALUE`)
	_, _, err = parseNativeManagerEnvironment("MISSING_VALUE")
	require.EqualError(t, err, `invalid --manager-env "MISSING_VALUE"; expected KEY=VALUE`)
}

func TestRenderNativeLaunchAgentEnvironment(t *testing.T) {
	paths := nativeInstallPaths{binary: "/Applications/Agent & API/agentapi-proxy", config: "/tmp/config.json", logDir: "/tmp/logs"}
	plist := renderNativeLaunchAgent(paths, map[string]string{"PATH": "/opt/mise/bin:/usr/bin", "SPECIAL": "one&two"})
	require.Contains(t, plist, "<key>EnvironmentVariables</key><dict>")
	require.Contains(t, plist, "<key>PATH</key><string>/opt/mise/bin:/usr/bin</string>")
	require.Contains(t, plist, "<key>SPECIAL</key><string>one&amp;two</string>")
	require.Contains(t, plist, "<string>/Applications/Agent &amp; API/agentapi-proxy</string>")
}

func TestRenderNativeSystemdEnvironment(t *testing.T) {
	paths := nativeInstallPaths{binary: "/usr/local/libexec/agentapi-proxy", config: "/etc/agentapi-native/config.json"}
	unit := renderNativeSystemdUnit(paths, map[string]string{"PATH": "/opt/mise/bin:/usr/bin", "SPECIAL": `quote"slash\percent%`})
	require.Contains(t, unit, "Environment=\"PATH=/opt/mise/bin:/usr/bin\"")
	require.Contains(t, unit, `Environment="SPECIAL=quote\"slash\\percent%%"`)
}

func TestSafeNativeStateDir(t *testing.T) {
	require.False(t, safeNativeStateDir("/"))
	require.False(t, safeNativeStateDir("."))
	require.True(t, safeNativeStateDir("/var/lib/agentapi-native"))
}

func TestReadNativeSessionList(t *testing.T) {
	stateDir := t.TempDir()
	older := time.Date(2026, time.July, 20, 1, 2, 3, 0, time.UTC)
	newer := older.Add(time.Hour)
	for _, entry := range []nativeSessionListEntry{
		{ID: "session-new", PID: 22, Status: "running", StartedAt: newer},
		{ID: "session-old", PID: 11, Status: "stable", StartedAt: older},
	} {
		runtimeDir := filepath.Join(stateDir, "sessions", entry.ID, "runtime")
		require.NoError(t, os.MkdirAll(runtimeDir, 0o700))
		data, err := json.Marshal(entry)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(runtimeDir, "state.json"), data, 0o600))
	}

	entries, err := readNativeSessionList(stateDir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "session-old", entries[0].ID)
	require.Equal(t, filepath.Join(stateDir, "sessions", "session-old", "runtime", "provisioner.log"), entries[0].LogPath)
	require.Equal(t, "session-new", entries[1].ID)
}

func TestNativeLogPath(t *testing.T) {
	paths := nativeInstallPaths{logDir: "/logs"}
	cfg := nativeDaemonConfig{StateDir: "/state"}

	path, err := nativeLogPath(paths, cfg, "session-1", false)
	require.NoError(t, err)
	require.Equal(t, "/state/sessions/session-1/runtime/provisioner.log", path)

	path, err = nativeLogPath(paths, cfg, "", true)
	require.NoError(t, err)
	require.Equal(t, "/logs/native.log", path)

	_, err = nativeLogPath(paths, cfg, "../session-1", false)
	require.EqualError(t, err, "invalid session ID")
	_, err = nativeLogPath(paths, cfg, "", false)
	require.EqualError(t, err, "session ID is required (or use --daemon)")
}
