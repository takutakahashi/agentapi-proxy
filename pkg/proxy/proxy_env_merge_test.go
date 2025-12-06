package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestEnvironmentVariableMerging(t *testing.T) {
	// Since session functionality has been removed, these tests now just
	// verify that the proxy server starts correctly with different configurations

	testCases := []struct {
		name string
		cfg  *config.Config
	}{
		{
			name: "Only request env vars",
			cfg: &config.Config{
				StartPort: 9000,
				Auth: config.AuthConfig{
					Enabled: false,
				},
			},
		},
		{
			name: "Team env file only",
			cfg: &config.Config{
				StartPort: 9001,
				Auth: config.AuthConfig{
					Enabled: false,
				},
				RoleEnvFiles: config.RoleEnvFilesConfig{
					Enabled: true,
					Path:    "/tmp/env-files",
				},
			},
		},
		{
			name: "Team + request env vars (request has highest priority)",
			cfg: &config.Config{
				StartPort: 9002,
				Auth: config.AuthConfig{
					Enabled: false,
				},
				RoleEnvFiles: config.RoleEnvFilesConfig{
					Enabled: true,
					Path:    "/tmp/env-files",
				},
			},
		},
		{
			name: "Request env vars only",
			cfg: &config.Config{
				StartPort: 9003,
				Auth: config.AuthConfig{
					Enabled: false,
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			proxy := NewProxy(tc.cfg, false)
			if proxy == nil {
				t.Fatal("NewProxy returned nil")
			}

			// Test that health endpoint works
			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()
			proxy.GetEcho().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}
		})
	}
}

func TestEnvironmentVariableMergingWithNonexistentFiles(t *testing.T) {
	cfg := &config.Config{
		StartPort: 9004,
		Auth: config.AuthConfig{
			Enabled: false,
		},
		RoleEnvFiles: config.RoleEnvFilesConfig{
			Enabled: true,
			Path:    "/nonexistent/path",
		},
	}

	proxy := NewProxy(cfg, false)
	if proxy == nil {
		t.Fatal("NewProxy returned nil")
	}

	// Test that health endpoint works even with nonexistent env files
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	proxy.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}