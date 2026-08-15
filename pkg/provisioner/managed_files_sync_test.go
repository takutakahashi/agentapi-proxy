package provisioner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestSaveManagedFilesUsesSessionControlToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/internal/session-control/session-1/managed-files", r.URL.Path)
		require.Equal(t, "Bearer token-1", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	require.NoError(t, saveManagedFiles(context.Background(), server.Client(), server.URL, "session-1", "token-1", []sessionsettings.ManagedFile{{Path: sessionsettings.ManagedFileTypes[sessionsettings.FileTypeCodexAuth], Content: "updated"}}))
}
