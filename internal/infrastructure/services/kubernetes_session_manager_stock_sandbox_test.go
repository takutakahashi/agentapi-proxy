package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestStockSandboxParamsUsesCountMode(t *testing.T) {
	sandbox := stockSandboxParams()

	assert.True(t, sandbox.Enabled)
	assert.True(t, sandbox.CountMode)
	assert.Empty(t, sandbox.AllowedDomains)
	assert.Empty(t, sandbox.DeniedDomains)
}

func TestApplySandboxDefaultsUsesCountModeWithoutRules(t *testing.T) {
	req := &entities.RunServerRequest{}

	applySandboxDefaults(req)

	assert.NotNil(t, req.Sandbox)
	assert.True(t, req.Sandbox.Enabled)
	assert.True(t, req.Sandbox.CountMode)
}

func TestApplySandboxDefaultsPreservesEnforcedRules(t *testing.T) {
	req := &entities.RunServerRequest{Sandbox: &entities.SandboxParams{
		AllowedDomains: []string{"slack.com"},
	}}

	applySandboxDefaults(req)

	assert.True(t, req.Sandbox.Enabled)
	assert.False(t, req.Sandbox.CountMode)
}

func TestPostSandboxPolicy(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s content-type=%q", r.Method, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := postSandboxPolicy(context.Background(), server.Client(), server.URL, []byte(`{"allowed":["slack.com"]}`))

	assert.NoError(t, err)
	assert.JSONEq(t, `{"allowed":["slack.com"]}`, gotBody)
}
