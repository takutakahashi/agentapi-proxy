package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/client"
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

func TestClientIntegration(t *testing.T) {
	cfg := config.DefaultConfig()
	proxyServer := proxy.NewProxy(cfg, false)

	server := httptest.NewServer(proxyServer.GetEcho())
	defer server.Close()

	clientInstance := client.NewClient(server.URL)
	ctx := context.Background()

	// Test 1: Start a new session
	startReq := &client.StartRequest{
		UserID: "integration-test-user",
		Environment: map[string]string{
			"TEST_ENV": "integration_test",
		},
	}

	startResp, err := clientInstance.Start(ctx, startReq)
	if err != nil {
		t.Fatalf("Failed to start session: %v", err)
	}

	if startResp.SessionID == "" {
		t.Fatal("Expected non-empty session ID")
	}

	sessionID := startResp.SessionID

	// Test 2: Search for sessions
	searchResp, err := clientInstance.Search(ctx, "integration-test-user", "")
	if err != nil {
		t.Fatalf("Failed to search sessions: %v", err)
	}

	found := false
	for _, session := range searchResp.Sessions {
		if session.SessionID == sessionID {
			found = true
			if session.UserID != "integration-test-user" {
				t.Errorf("Expected UserID 'integration-test-user', got %s", session.UserID)
			}
			if session.Status != "active" {
				t.Errorf("Expected status 'active', got %s", session.Status)
			}
			break
		}
	}

	if !found {
		t.Errorf("Session %s not found in search results", sessionID)
	}

	// Test 3: Search with filters
	filteredResp, err := clientInstance.Search(ctx, "integration-test-user", "active")
	if err != nil {
		t.Fatalf("Failed to search filtered sessions: %v", err)
	}

	found = false
	for _, session := range filteredResp.Sessions {
		if session.SessionID == sessionID {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Session %s not found in filtered search results", sessionID)
	}

	// Test 4: Search with non-matching filter
	noMatchResp, err := clientInstance.Search(ctx, "nonexistent-user", "")
	if err != nil {
		t.Fatalf("Failed to search sessions: %v", err)
	}

	for _, session := range noMatchResp.Sessions {
		if session.SessionID == sessionID {
			t.Errorf("Session %s should not appear in filtered results", sessionID)
		}
	}
}

func TestClientConcurrentOperations(t *testing.T) {
	cfg := config.DefaultConfig()
	proxyServer := proxy.NewProxy(cfg, false)

	server := httptest.NewServer(proxyServer.GetEcho())
	defer server.Close()

	clientInstance := client.NewClient(server.URL)
	ctx := context.Background()

	const numOperations = 10
	results := make(chan error, numOperations)

	// Concurrent session creation
	for i := 0; i < numOperations; i++ {
		go func(id int) {
			startReq := &client.StartRequest{
				UserID: "concurrent-test-user",
				Environment: map[string]string{
					"OPERATION_ID": string(rune(id + '0')),
				},
			}

			_, err := clientInstance.Start(ctx, startReq)
			results <- err
		}(i)
	}

	// Check all operations completed successfully
	for i := 0; i < numOperations; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Errorf("Operation %d failed: %v", i, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("Operation %d timed out", i)
		}
	}

	// Verify all sessions were created
	searchResp, err := clientInstance.Search(ctx, "concurrent-test-user", "")
	if err != nil {
		t.Fatalf("Failed to search sessions: %v", err)
	}

	if len(searchResp.Sessions) != numOperations {
		t.Errorf("Expected %d sessions, got %d", numOperations, len(searchResp.Sessions))
	}
}

func TestClientErrorHandling(t *testing.T) {
	// Test with non-existent server
	clientInstance := client.NewClient("http://localhost:99999")
	ctx := context.Background()

	_, err := clientInstance.Start(ctx, &client.StartRequest{UserID: "test"})
	if err == nil {
		t.Error("Expected error when connecting to non-existent server")
	}

	_, err = clientInstance.Search(ctx, "", "")
	if err == nil {
		t.Error("Expected error when connecting to non-existent server")
	}

	// Test with invalid session ID
	cfg := config.DefaultConfig()
	proxyServer := proxy.NewProxy(cfg, false)

	server := httptest.NewServer(proxyServer.GetEcho())
	defer server.Close()

	validClient := client.NewClient(server.URL)

	_, err = validClient.SendMessage(ctx, "invalid-session", &client.Message{
		Content: "test",
		Type:    "user",
	})
	if err == nil {
		t.Error("Expected error when sending message to invalid session")
	}

	_, err = validClient.GetMessages(ctx, "invalid-session")
	if err == nil {
		t.Error("Expected error when getting messages from invalid session")
	}

	_, err = validClient.GetStatus(ctx, "invalid-session")
	if err == nil {
		t.Error("Expected error when getting status from invalid session")
	}
}
