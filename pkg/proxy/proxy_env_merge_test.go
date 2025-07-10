package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestEnvironmentVariableMerging(t *testing.T) {
	// Create temporary directory for test env files
	tempDir := t.TempDir()

	// Create team env file
	teamEnvFile := filepath.Join(tempDir, "team.env")
	teamEnvContent := `TEAM_VAR=team_value
COMMON_VAR=team_common
OVERRIDE_VAR=team_override`
	if err := os.WriteFile(teamEnvFile, []byte(teamEnvContent), 0644); err != nil {
		t.Fatalf("Failed to create team env file: %v", err)
	}

	// Configure proxy without role-based env files (for simpler testing)
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false

	proxy := NewProxy(cfg, false)

	tests := []struct {
		name     string
		request  StartRequest
		expected map[string]string
	}{
		{
			name: "Only request env vars",
			request: StartRequest{
				Environment: map[string]string{
					"REQUEST_VAR1": "value1",
					"REQUEST_VAR2": "value2",
				},
			},
			expected: map[string]string{
				"REQUEST_VAR1": "value1",
				"REQUEST_VAR2": "value2",
			},
		},
		{
			name: "Team env file only",
			request: StartRequest{
				Tags: map[string]string{
					"env_file": teamEnvFile,
				},
			},
			expected: map[string]string{
				"TEAM_VAR":     "team_value",
				"COMMON_VAR":   "team_common",
				"OVERRIDE_VAR": "team_override",
			},
		},
		{
			name: "Team + request env vars (request has highest priority)",
			request: StartRequest{
				Environment: map[string]string{
					"REQUEST_VAR":  "request_value",
					"COMMON_VAR":   "request_common",
					"OVERRIDE_VAR": "request_override",
				},
				Tags: map[string]string{
					"env_file": teamEnvFile,
				},
			},
			expected: map[string]string{
				"TEAM_VAR":     "team_value",
				"REQUEST_VAR":  "request_value",
				"COMMON_VAR":   "request_common",
				"OVERRIDE_VAR": "request_override",
			},
		},
		{
			name: "Request env vars only",
			request: StartRequest{
				Environment: map[string]string{
					"REQUEST_VAR1": "value1",
					"REQUEST_VAR2": "value2",
				},
			},
			expected: map[string]string{
				"REQUEST_VAR1": "value1",
				"REQUEST_VAR2": "value2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create request
			jsonBody, err := json.Marshal(tt.request)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}

			req := httptest.NewRequest("POST", "/start", bytes.NewReader(jsonBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// Handle request
			proxy.GetEcho().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
			}

			// Parse response
			var response map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			sessionID, ok := response["session_id"].(string)
			if !ok || sessionID == "" {
				t.Fatal("Response missing session_id")
			}

			// Get the session and check environment variables
			proxy.sessionsMutex.RLock()
			session, exists := proxy.sessions[sessionID]
			proxy.sessionsMutex.RUnlock()

			if !exists {
				t.Fatal("Session not found")
			}

			// Verify environment variables
			for key, expectedValue := range tt.expected {
				actualValue, exists := session.Environment[key]
				if !exists {
					t.Errorf("Test %s: Expected environment variable %s not found. Available vars: %+v", tt.name, key, session.Environment)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("Test %s: Environment variable %s: expected %s, got %s", tt.name, key, expectedValue, actualValue)
				}
			}

			// Only check for extra variables if we expect specific ones
			if len(tt.expected) > 0 {
				for key := range session.Environment {
					if _, expected := tt.expected[key]; !expected {
						t.Logf("Test %s: Unexpected environment variable: %s=%s", tt.name, key, session.Environment[key])
					}
				}
			}

			// Clean up session
			if session.Cancel != nil {
				session.Cancel()
			}
		})
	}
}

func TestEnvironmentVariableMergingWithNonexistentFiles(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false

	proxy := NewProxy(cfg, false)

	// Request with non-existent env_file
	request := StartRequest{
		Environment: map[string]string{
			"REQUEST_VAR": "value",
		},
		Tags: map[string]string{
			"env_file": "/non/existent/file.env",
		},
	}

	jsonBody, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req := httptest.NewRequest("POST", "/start", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.GetEcho().ServeHTTP(w, req)

	// Should still succeed, just with a warning in logs
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Parse response
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	sessionID, ok := response["session_id"].(string)
	if !ok || sessionID == "" {
		t.Fatal("Response missing session_id")
	}

	// Get the session and verify request env vars still work
	proxy.sessionsMutex.RLock()
	session, exists := proxy.sessions[sessionID]
	proxy.sessionsMutex.RUnlock()

	if !exists {
		t.Fatal("Session not found")
	}

	// Verify request environment variable is present
	if session.Environment["REQUEST_VAR"] != "value" {
		t.Errorf("Expected REQUEST_VAR=value, got %s", session.Environment["REQUEST_VAR"])
	}

	// Clean up session
	if session.Cancel != nil {
		session.Cancel()
	}
}
