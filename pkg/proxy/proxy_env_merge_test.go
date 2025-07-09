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

	// Create role-based env file
	roleEnvFile := filepath.Join(tempDir, "test-role.env")
	roleEnvContent := `ROLE_VAR=role_value
COMMON_VAR=role_common
OVERRIDE_VAR=role_override`
	if err := os.WriteFile(roleEnvFile, []byte(roleEnvContent), 0644); err != nil {
		t.Fatalf("Failed to create role env file: %v", err)
	}

	// Create team env file
	teamEnvFile := filepath.Join(tempDir, "team.env")
	teamEnvContent := `TEAM_VAR=team_value
COMMON_VAR=team_common
OVERRIDE_VAR=team_override`
	if err := os.WriteFile(teamEnvFile, []byte(teamEnvContent), 0644); err != nil {
		t.Fatalf("Failed to create team env file: %v", err)
	}

	// Configure proxy with role-based env files
	cfg := config.DefaultConfig()
	cfg.Auth.Enabled = false
	cfg.RoleEnvFiles = config.RoleEnvFilesConfig{
		Enabled: true,
		Path:    tempDir,
	}

	proxy := NewProxy(cfg, false)

	tests := []struct {
		name     string
		request  StartRequest
		expected map[string]string
	}{
		{
			name: "Only role-based env vars",
			request: StartRequest{
				Tags: map[string]string{
					"user_role": "test-role",
				},
			},
			expected: map[string]string{
				"ROLE_VAR":     "role_value",
				"COMMON_VAR":   "role_common",
				"OVERRIDE_VAR": "role_override",
			},
		},
		{
			name: "Role + team env vars (team overrides role)",
			request: StartRequest{
				Tags: map[string]string{
					"user_role": "test-role",
					"env_file":  teamEnvFile,
				},
			},
			expected: map[string]string{
				"ROLE_VAR":     "role_value",
				"TEAM_VAR":     "team_value",
				"COMMON_VAR":   "team_common",
				"OVERRIDE_VAR": "team_override",
			},
		},
		{
			name: "Role + team + request env vars (request has highest priority)",
			request: StartRequest{
				Environment: map[string]string{
					"REQUEST_VAR":  "request_value",
					"COMMON_VAR":   "request_common",
					"OVERRIDE_VAR": "request_override",
				},
				Tags: map[string]string{
					"user_role": "test-role",
					"env_file":  teamEnvFile,
				},
			},
			expected: map[string]string{
				"ROLE_VAR":     "role_value",
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
					t.Errorf("Expected environment variable %s not found", key)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("Environment variable %s: expected %s, got %s", key, expectedValue, actualValue)
				}
			}

			// Verify no extra environment variables
			for key := range session.Environment {
				if _, expected := tt.expected[key]; !expected {
					t.Errorf("Unexpected environment variable: %s=%s", key, session.Environment[key])
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