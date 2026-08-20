package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

func TestRetrySandboxPolicyWaitsForProvisioner(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := retrySandboxPolicy(context.Background(), server.Client(), server.URL, nil, 3, time.Millisecond)

	assert.NoError(t, err)
	assert.Equal(t, int32(3), attempts.Load())
}

func TestRetrySandboxPolicyHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "starting", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := retrySandboxPolicy(ctx, server.Client(), server.URL, nil, 60, time.Second)

	assert.ErrorIs(t, err, context.Canceled)
}
