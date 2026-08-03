package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEnrollNativeManager(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/external-session-managers/enroll", r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(nativeRegistrationResponse{ID: "manager-1", ConnectionToken: "connection-token", Created: true})
	}))
	defer server.Close()

	result, err := enrollNativeManager(server.URL, map[string]string{"registration_token": "one-time-token"})
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

func TestNativeGUIJSONContracts(t *testing.T) {
	statusJSON, err := json.Marshal(nativeStatusOutput{
		Instance: "default", Service: "running", ManagerID: "manager-1", Upstream: "https://parent.example",
		PublicURL: "https://mac.example", ActiveSessions: 2, Health: "ok",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{
		"instance":"default","service":"running","manager_id":"manager-1","upstream":"https://parent.example",
		"public_url":"https://mac.example","labels":null,"version":"",
		"filesystem_sandbox":false,"active_sessions":2,"health":"ok","state":""
	}`, string(statusJSON))

	doctorJSON, err := json.Marshal(nativeDoctorOutput{OK: false, Checks: []nativeDoctorCheck{{
		ID: "local_health", Status: "error", Message: "unreachable",
	}}})
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":false,"checks":[{"id":"local_health","status":"error","message":"unreachable"}]}`, string(doctorJSON))
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

func TestValidateNativeInstanceName(t *testing.T) {
	require.NoError(t, validateNativeInstanceName(""))
	require.NoError(t, validateNativeInstanceName("ios"))
	require.NoError(t, validateNativeInstanceName("build-1"))
	require.NoError(t, validateNativeInstanceName("a"))
	require.NoError(t, validateNativeInstanceName("ab"))
	require.NoError(t, validateNativeInstanceName("default"))
	require.ErrorContains(t, validateNativeInstanceName("UPPER"), "invalid --instance")
	require.ErrorContains(t, validateNativeInstanceName("-leading"), "invalid --instance")
	require.ErrorContains(t, validateNativeInstanceName("trailing-"), "invalid --instance")
	require.ErrorContains(t, validateNativeInstanceName("has space"), "invalid --instance")
	require.ErrorContains(t, validateNativeInstanceName("has_underscore"), "invalid --instance")
	require.ErrorContains(t, validateNativeInstanceName(strings.Repeat("a", 33)), "invalid --instance")
}

func TestNativeInstallPathsDefaultLinuxPreservesHistoricalPaths(t *testing.T) {
	paths, err := nativeInstallPathsFor("linux", nativeDefaultInstance, "")
	require.NoError(t, err)
	require.Equal(t, "/etc/agentapi-native/config.json", paths.config)
	require.Equal(t, "/etc/agentapi-native/credentials.json", paths.credentials)
	require.Equal(t, "/var/lib/agentapi-native", paths.state)
	require.Equal(t, "/usr/local/libexec/agentapi-proxy/agentapi-proxy", paths.binary)
	require.Equal(t, "/etc/systemd/system/agentapi-native.service", paths.service)
	require.Equal(t, "/var/log/agentapi-native", paths.logDir)
}

func TestNativeInstallPathsNonDefaultLinuxIsIsolated(t *testing.T) {
	paths, err := nativeInstallPathsFor("linux", "ci", "")
	require.NoError(t, err)
	require.Equal(t, "/etc/agentapi-native-ci/config.json", paths.config)
	require.Equal(t, "/etc/agentapi-native-ci/credentials.json", paths.credentials)
	require.Equal(t, "/var/lib/agentapi-native-ci", paths.state)
	// The managed binary is shared across instances on Linux.
	require.Equal(t, "/usr/local/libexec/agentapi-proxy/agentapi-proxy", paths.binary)
	require.Equal(t, "/etc/systemd/system/agentapi-native-ci.service", paths.service)
	require.Equal(t, "/var/log/agentapi-native-ci", paths.logDir)
}

func TestNativeInstallPathsConfigOverrideRejectedForNonDefault(t *testing.T) {
	// Config override is supported for the default instance and must not move a
	// non-default instance off its isolated path. The CLI enforces this in
	// runNativeInstall; nativeInstallPathsFor still honors an explicit override
	// only for the default instance's config location.
	paths, err := nativeInstallPathsFor("linux", nativeDefaultInstance, "/custom/config.json")
	require.NoError(t, err)
	require.Equal(t, "/custom/config.json", paths.config)
	require.Equal(t, "/etc/agentapi-native/credentials.json", paths.credentials)
}

func TestNativeInstallPathsDefaultDarwinPreservesHistoricalPaths(t *testing.T) {
	paths, err := nativeInstallPathsFor("darwin", nativeDefaultInstance, "")
	if err != nil {
		t.Skipf("darwin paths require a home directory on this host: %v", err)
	}
	home, _ := os.UserHomeDir()
	require.Equal(t, filepath.Join(home, "Library", "Application Support", "agentapi-native", "config.json"), paths.config)
	require.Equal(t, filepath.Join(home, "Library", "Application Support", "agentapi-native", "credentials.json"), paths.credentials)
	require.Equal(t, filepath.Join(home, "Library", "Application Support", "agentapi-native", "state"), paths.state)
	require.Equal(t, filepath.Join(home, "Library", "Application Support", "agentapi-native", "bin", "agentapi-proxy"), paths.binary)
	require.Equal(t, filepath.Join(home, "Library", "LaunchAgents", "com.agentapi.native.plist"), paths.service)
	require.Equal(t, filepath.Join(home, "Library", "Logs", "agentapi-native"), paths.logDir)
}

func TestNativeInstallPathsNonDefaultDarwinIsIsolated(t *testing.T) {
	paths, err := nativeInstallPathsFor("darwin", "ios", "")
	if err != nil {
		t.Skipf("darwin paths require a home directory on this host: %v", err)
	}
	home, _ := os.UserHomeDir()
	require.Equal(t, filepath.Join(home, "Library", "Application Support", "agentapi-native-ios", "config.json"), paths.config)
	require.Equal(t, filepath.Join(home, "Library", "Application Support", "agentapi-native-ios", "credentials.json"), paths.credentials)
	// macOS installs a per-instance binary copy, so it is not shared.
	require.Equal(t, filepath.Join(home, "Library", "Application Support", "agentapi-native-ios", "bin", "agentapi-proxy"), paths.binary)
	require.Equal(t, filepath.Join(home, "Library", "LaunchAgents", "com.agentapi.native.ios.plist"), paths.service)
	require.Equal(t, filepath.Join(home, "Library", "Logs", "agentapi-native-ios"), paths.logDir)
}

func TestNativeServiceNameDefaultPreservedAndInstanceScoped(t *testing.T) {
	require.Equal(t, "agentapi-native.service", nativeServiceNameFor("linux", nativeDefaultInstance))
	require.Equal(t, "agentapi-native.service", nativeServiceNameFor("linux", ""))
	require.Equal(t, "agentapi-native-ci.service", nativeServiceNameFor("linux", "ci"))
	require.Equal(t, "com.agentapi.native", nativeServiceNameFor("darwin", nativeDefaultInstance))
	require.Equal(t, "com.agentapi.native", nativeServiceNameFor("darwin", ""))
	require.Equal(t, "com.agentapi.native.ios", nativeServiceNameFor("darwin", "ios"))
	require.Equal(t, "agentapi-native-ci.service", nativeServiceUnitName("/etc/systemd/system/agentapi-native-ci.service"))
	require.Equal(t, "com.agentapi.native.ios", nativeLaunchLabel("/Users/x/Library/LaunchAgents/com.agentapi.native.ios.plist"))
}

func TestStableNativeInstanceIDDefaultUnchangedByInstanceFlag(t *testing.T) {
	// The default instance ID must not change when instance selection is omitted,
	// matching the historical stableNativeInstanceID(hostname) behavior.
	defaultID := stableNativeInstanceID("host-1", "")
	defaultIDExplicit := stableNativeInstanceID("host-1", nativeDefaultInstance)
	require.Equal(t, defaultID, defaultIDExplicit)
	require.True(t, strings.HasPrefix(defaultID, "native-host-1-"))

	// A named instance must get a distinct but stable ID.
	namedID := stableNativeInstanceID("host-1", "ci")
	require.NotEqual(t, defaultID, namedID)
	require.Equal(t, namedID, stableNativeInstanceID("host-1", "ci"))
	require.True(t, strings.HasPrefix(namedID, "native-host-1-"))
}

func TestRenderNativeLaunchAgentUsesInstanceLabel(t *testing.T) {
	paths := nativeInstallPaths{binary: "/bin/agentapi-proxy", config: "/tmp/c.json", service: "/Users/u/Library/LaunchAgents/com.agentapi.native.ios.plist", logDir: "/tmp/logs"}
	plist := renderNativeLaunchAgent(paths, nil)
	require.Contains(t, plist, "<key>Label</key><string>com.agentapi.native.ios</string>")
	require.Contains(t, plist, "<string>--config</string><string>/tmp/c.json</string>")

	defaultPaths := nativeInstallPaths{binary: "/bin/agentapi-proxy", config: "/tmp/c.json", service: "/Users/u/Library/LaunchAgents/com.agentapi.native.plist", logDir: "/tmp/logs"}
	defaultPlist := renderNativeLaunchAgent(defaultPaths, nil)
	require.Contains(t, defaultPlist, "<key>Label</key><string>com.agentapi.native</string>")
}

func TestDiscoverNativeInstancesListsDefaultAndNamed(t *testing.T) {
	root := t.TempDir()
	defaultConfig := filepath.Join(root, "agentapi-native", "config.json")
	namedConfig := filepath.Join(root, "agentapi-native-ios", "config.json")
	for _, p := range []string{defaultConfig, namedConfig} {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	}
	sharedBinary := "/usr/local/libexec/agentapi-proxy/agentapi-proxy"
	writeConfig := func(path, instance, binary string) {
		cfg := nativeDaemonConfig{ManagerID: "mgr-" + instance, UpstreamURL: "https://parent", PublicURL: "https://child", StateDir: filepath.Join(filepath.Dir(path), "state"), BinaryPath: binary}
		data, err := json.Marshal(cfg)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}
	writeConfig(defaultConfig, "default", sharedBinary)
	writeConfig(namedConfig, "ios", sharedBinary)

	pattern := filepath.Join(root, "agentapi-native-*", "config.json")
	entries := discoverNativeInstances(defaultConfig, pattern)
	require.Len(t, entries, 2)
	require.Equal(t, "default", entries[0].Instance)
	require.Equal(t, "agentapi-native.service", entries[0].Service)
	require.Equal(t, "mgr-default", entries[0].ManagerID)
	require.Equal(t, sharedBinary, entries[0].BinaryPath)
	require.Equal(t, "ios", entries[1].Instance)
	require.Equal(t, "agentapi-native-ios.service", entries[1].Service)
	require.Equal(t, "mgr-ios", entries[1].ManagerID)
}

func TestDiscoverNativeInstancesSkipsUnreadableConfig(t *testing.T) {
	root := t.TempDir()
	defaultConfig := filepath.Join(root, "agentapi-native", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(defaultConfig), 0o755))
	// No config written; default is missing, only a named instance exists.
	namedConfig := filepath.Join(root, "agentapi-native-ios", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(namedConfig), 0o755))
	cfg := nativeDaemonConfig{ManagerID: "mgr-ios", UpstreamURL: "https://parent", StateDir: "/state"}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(namedConfig, data, 0o600))

	pattern := filepath.Join(root, "agentapi-native-*", "config.json")
	entries := discoverNativeInstances(defaultConfig, pattern)
	require.Len(t, entries, 1)
	require.Equal(t, "ios", entries[0].Instance)
}

func TestBinarySharedWith(t *testing.T) {
	shared := "/usr/local/libexec/agentapi-proxy/agentapi-proxy"
	entries := []nativeInstanceListEntry{
		{Instance: "default", BinaryPath: shared},
		{Instance: "ci", BinaryPath: shared},
	}
	// Uninstalling either instance while the other still references the shared
	// binary must keep the binary on disk.
	require.True(t, binarySharedWith(entries, "default", shared))
	require.True(t, binarySharedWith(entries, "ci", shared))
	// When only one instance remains and it is the one being uninstalled, the
	// binary is no longer shared and can be removed.
	solo := []nativeInstanceListEntry{{Instance: "default", BinaryPath: shared}}
	require.False(t, binarySharedWith(solo, "default", shared))
	// macOS-style per-instance binaries are never shared across instances.
	macEntries := []nativeInstanceListEntry{
		{Instance: "default", BinaryPath: "/Users/u/Library/Application Support/agentapi-native/bin/agentapi-proxy"},
		{Instance: "ios", BinaryPath: "/Users/u/Library/Application Support/agentapi-native-ios/bin/agentapi-proxy"},
	}
	require.False(t, binarySharedWith(macEntries, "default", "/Users/u/Library/Application Support/agentapi-native/bin/agentapi-proxy"))
	require.False(t, binarySharedWith(nil, "default", ""))
}
