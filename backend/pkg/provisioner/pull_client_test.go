package provisioner

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPullHTTPClientLoadsSCIACA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	caFile := filepath.Join(t.TempDir(), "scia-ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	require.NoError(t, os.WriteFile(caFile, caPEM, 0o600))

	client, err := newPullHTTPClient(context.Background(), caFile)
	require.NoError(t, err)

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestAuthorizePullRequestWithParentAuthentication(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	authorizePullRequest(req, PullClientConfig{Token: "manager-token", UpstreamAuthToken: "parent-token"})
	require.Equal(t, "Bearer parent-token", req.Header.Get("Authorization"))
	require.Equal(t, "manager-token", req.Header.Get("X-Session-Manager-Token"))
}
