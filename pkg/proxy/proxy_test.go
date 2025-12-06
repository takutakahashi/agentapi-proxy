package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestNewProxyFixed(t *testing.T) {
	cfg := &config.Config{
		StartPort: 9000,
		Auth: config.AuthConfig{
			Enabled: false,
		},
	}

	proxy := NewProxy(cfg, false)
	if proxy == nil {
		t.Fatal("NewProxy returned nil")
	}

	if proxy.config != cfg {
		t.Error("Proxy config not set correctly")
	}

	if proxy.echo == nil {
		t.Error("Echo instance not initialized")
	}

	if proxy.nextPort != cfg.StartPort {
		t.Errorf("Expected nextPort to be %d, got %d", cfg.StartPort, proxy.nextPort)
	}
}

func TestHealthEndpointFixed(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for /health endpoint, got %d", http.StatusOK, w.Code)
	}
}

func TestExtractRepoFullNameFromURLFixed(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL",
			url:      "https://github.com/owner/repo",
			expected: "owner/repo",
		},
		{
			name:     "HTTPS URL with .git",
			url:      "https://github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "SSH URL",
			url:      "git@github.com:owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "Invalid URL format",
			url:      "invalid-url",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := extractRepoFullNameFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestHealthEndpointWithoutAuth(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for /health endpoint, got %d", http.StatusOK, w.Code)
	}
}

type mockServerRunner struct {
	runCalled bool
	session   *AgentSession
}

func (m *mockServerRunner) Run(ctx context.Context, session *AgentSession, scriptName string, repoInfo *RepositoryInfo, initialMessage string) {
	m.runCalled = true
	m.session = session
}

func TestCustomServerRunner(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	// Set custom server runner
	mockRunner := &mockServerRunner{}
	proxy.SetServerRunner(mockRunner)

	// Create test session
	session := &AgentSession{
		ID:          "test-session",
		Port:        9001,
		UserID:      "test-user",
		Status:      "active",
		Environment: map[string]string{"TEST": "value"},
		StartedAt:   time.Now(),
	}

	// Run the server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go proxy.runAgentAPIServer(ctx, session, "", nil, "")

	// Give it time to call the mock runner
	time.Sleep(100 * time.Millisecond)

	// Verify mock runner was called
	if !mockRunner.runCalled {
		t.Error("Mock server runner was not called")
	}

	if mockRunner.session != session {
		t.Error("Mock server runner received different session")
	}
}

func TestDefaultServerRunner(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	// Verify default runner is returned when none is set
	runner := proxy.getServerRunner()
	if runner == nil {
		t.Error("getServerRunner should return a default runner")
	}

	// Check if it's an instance of defaultServerRunner
	if _, ok := runner.(*defaultServerRunner); !ok {
		t.Error("Default runner should be of type defaultServerRunner")
	}
}

