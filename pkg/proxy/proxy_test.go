package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

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