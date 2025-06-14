package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestNewProxy(t *testing.T) {
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

func TestStartEndpoint(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	req := httptest.NewRequest("POST", "/start", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for /start endpoint, got %d", http.StatusOK, w.Code)
	}

	// Check if response contains session_id
	response := w.Body.String()
	if response == "" {
		t.Error("Expected non-empty response from /start endpoint")
	}
}

func TestSearchEndpoint(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	req := httptest.NewRequest("GET", "/search", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for /search endpoint, got %d", http.StatusOK, w.Code)
	}

	// Check if response contains sessions array
	response := w.Body.String()
	if response == "" {
		t.Error("Expected non-empty response from /search endpoint")
	}
}

func TestSessionRoutingNotFound(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	proxy := NewProxy(cfg, false)

	// Test routing to non-existent session
	req := httptest.NewRequest("GET", "/nonexistent-session-id/health", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for non-existent session, got %d", http.StatusNotFound, w.Code)
	}
}

func TestExtractRepoFullNameFromURL(t *testing.T) {
	tests := []struct {
		name        string
		repoURL     string
		expected    string
		expectError bool
	}{
		{
			name:     "HTTPS URL",
			repoURL:  "https://github.com/owner/repo",
			expected: "owner/repo",
		},
		{
			name:     "HTTPS URL with .git",
			repoURL:  "https://github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "SSH URL",
			repoURL:  "git@github.com:owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "SSH URL without .git",
			repoURL:  "git@github.com:owner/repo",
			expected: "owner/repo",
		},
		{
			name:     "HTTP URL",
			repoURL:  "http://github.com/owner/repo",
			expected: "owner/repo",
		},
		{
			name:        "Invalid URL format",
			repoURL:     "invalid-url",
			expectError: true,
		},
		{
			name:        "Invalid repository path (too many parts)",
			repoURL:     "https://github.com/owner/repo/extra",
			expectError: true,
		},
		{
			name:        "Invalid repository path (too few parts)",
			repoURL:     "https://github.com/owner",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := extractRepoFullNameFromURL(tt.repoURL)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
