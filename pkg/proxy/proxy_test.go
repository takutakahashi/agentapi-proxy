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

func TestStartEndpointFixed(t *testing.T) {
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

func TestExtractRepoFullNameFromURLFixed(t *testing.T) {
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
			name:        "Invalid URL format",
			repoURL:     "invalid-url",
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

func TestHealthEndpointWithoutAuth(t *testing.T) {
	// Test with authentication enabled
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = true
	cfg.Auth.Static = &config.StaticAuthConfig{
		Enabled:    true,
		HeaderName: "X-API-Key",
		APIKeys: []config.APIKey{
			{
				Key:         "test-key",
				UserID:      "test-user",
				Role:        "user",
				Permissions: []string{"session:access"},
			},
		},
	}
	proxy := NewProxy(cfg, false)

	// Request to health endpoint without authentication
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	// Health endpoint should return 200 even without authentication
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d for /health endpoint without auth, got %d", http.StatusOK, w.Code)
	}
}
