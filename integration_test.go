package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxy"
)

func TestIntegrationSessionAPI(t *testing.T) {
	cfg := config.DefaultConfig()
	proxyServer := proxy.NewProxy(cfg, true)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{
			name:           "Start new session",
			method:         "POST",
			path:           "/start",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Search sessions",
			method:         "GET",
			path:           "/search",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Search sessions with filter",
			method:         "GET",
			path:           "/search?user_id=test&status=active",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Route to non-existent session",
			method:         "GET",
			path:           "/non-existent-session/status",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			proxyServer.GetEcho().ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// For successful responses, check JSON format
			if w.Code == http.StatusOK {
				var result map[string]interface{}
				body := w.Body.String()
				if err := json.Unmarshal([]byte(body), &result); err != nil {
					t.Errorf("Response should be valid JSON: %v, body: %s", err, body)
				}
			}
		})
	}
}

func TestSessionLifecycle(t *testing.T) {
	cfg := config.DefaultConfig()
	proxyServer := proxy.NewProxy(cfg, false)

	// Step 1: Start a new session
	req := httptest.NewRequest("POST", "/start?user_id=testuser", nil)
	w := httptest.NewRecorder()
	proxyServer.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Failed to start session: status %d", w.Code)
	}

	var startResponse map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &startResponse); err != nil {
		t.Fatalf("Failed to parse start response: %v", err)
	}

	sessionID, ok := startResponse["session_id"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("Invalid session_id in response: %v", startResponse)
	}

	// Step 2: Verify session appears in search
	req = httptest.NewRequest("GET", "/search?user_id=testuser", nil)
	w = httptest.NewRecorder()
	proxyServer.GetEcho().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Failed to search sessions: status %d", w.Code)
	}

	var searchResponse map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &searchResponse); err != nil {
		t.Fatalf("Failed to parse search response: %v", err)
	}

	sessions, ok := searchResponse["sessions"].([]interface{})
	if !ok {
		t.Fatalf("Invalid sessions array in response: %v", searchResponse)
	}

	// Check if our session is in the list
	found := false
	for _, s := range sessions {
		if session, ok := s.(map[string]interface{}); ok {
			if session["session_id"] == sessionID {
				found = true
				break
			}
		}
	}

	if !found {
		t.Errorf("Started session %s not found in search results", sessionID)
	}
}

func TestConcurrentSessionRequests(t *testing.T) {
	cfg := config.DefaultConfig()
	proxyServer := proxy.NewProxy(cfg, false)

	// Test concurrent session creation
	const numSessions = 5
	results := make(chan string, numSessions)

	for i := 0; i < numSessions; i++ {
		go func(id int) {
			req := httptest.NewRequest("POST", "/start", nil)
			w := httptest.NewRecorder()
			proxyServer.GetEcho().ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				var response map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err == nil {
					if sessionID, ok := response["session_id"].(string); ok {
						results <- sessionID
						return
					}
				}
			}
			results <- ""
		}(i)
	}

	// Collect results
	sessionIDs := make(map[string]bool)
	for i := 0; i < numSessions; i++ {
		select {
		case sessionID := <-results:
			if sessionID == "" {
				t.Errorf("Session %d failed to start", i)
			} else if sessionIDs[sessionID] {
				t.Errorf("Duplicate session ID: %s", sessionID)
			} else {
				sessionIDs[sessionID] = true
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Session %d timed out", i)
		}
	}

	if len(sessionIDs) != numSessions {
		t.Errorf("Expected %d unique sessions, got %d", numSessions, len(sessionIDs))
	}
}
