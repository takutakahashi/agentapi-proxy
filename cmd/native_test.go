package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
	want := nativeDaemonConfig{ManagerID: "manager-1", InstanceID: "machine-1", ConnectionToken: "secret", CredentialsPath: credentialsPath}
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

func TestSafeNativeStateDir(t *testing.T) {
	require.False(t, safeNativeStateDir("/"))
	require.False(t, safeNativeStateDir("."))
	require.True(t, safeNativeStateDir("/var/lib/agentapi-native"))
}
