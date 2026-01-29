package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	baseURL := "http://localhost:8080"
	client := NewClient(baseURL)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}

	if client.baseURL != baseURL {
		t.Errorf("Expected baseURL %s, got %s", baseURL, client.baseURL)
	}

	if client.httpClient == nil {
		t.Fatal("httpClient is nil")
	}

	if client.httpClient.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client.httpClient.Timeout)
	}
}

func TestClient_Start(t *testing.T) {
	tests := []struct {
		name           string
		request        *StartRequest
		serverResponse string
		serverStatus   int
		wantErr        bool
		expectedID     string
	}{
		{
			name: "successful start",
			request: &StartRequest{
				Environment: map[string]string{"TEST": "value"},
			},
			serverResponse: `{"session_id": "test-session-123"}`,
			serverStatus:   http.StatusOK,
			wantErr:        false,
			expectedID:     "test-session-123",
		},
		{
			name:           "server error",
			request:        &StartRequest{},
			serverResponse: `{"error": "internal server error"}`,
			serverStatus:   http.StatusInternalServerError,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/start" {
					t.Errorf("Expected POST /start, got %s %s", r.Method, r.URL.Path)
				}

				var req StartRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode request: %v", err)
				}

				// UserID field removed from StartRequest

				w.WriteHeader(tt.serverStatus)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			client := NewClient(server.URL)
			resp, err := client.Start(context.Background(), tt.request)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if resp.SessionID != tt.expectedID {
				t.Errorf("Expected session ID %s, got %s", tt.expectedID, resp.SessionID)
			}
		})
	}
}

func TestClient_Search(t *testing.T) {
	tests := []struct {
		name           string
		status         string
		serverResponse string
		serverStatus   int
		wantErr        bool
		expectedCount  int
	}{
		{
			name:           "successful search",
			status:         "active",
			serverResponse: `{"sessions": [{"session_id": "session1", "user_id": "test-user", "status": "active", "started_at": "2023-01-01T00:00:00Z", "port": 9000}]}`,
			serverStatus:   http.StatusOK,
			wantErr:        false,
			expectedCount:  1,
		},
		{
			name:           "empty result",
			status:         "",
			serverResponse: `{"sessions": []}`,
			serverStatus:   http.StatusOK,
			wantErr:        false,
			expectedCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/search" {
					t.Errorf("Expected GET /search, got %s %s", r.Method, r.URL.Path)
				}

				status := r.URL.Query().Get("status")

				if status != tt.status {
					t.Errorf("Expected status %s, got %s", tt.status, status)
				}

				w.WriteHeader(tt.serverStatus)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			client := NewClient(server.URL)
			resp, err := client.Search(context.Background(), tt.status)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(resp.Sessions) != tt.expectedCount {
				t.Errorf("Expected %d sessions, got %d", tt.expectedCount, len(resp.Sessions))
			}
		})
	}
}

func TestClient_SendMessage(t *testing.T) {
	tests := []struct {
		name           string
		sessionID      string
		message        *Message
		serverResponse string
		serverStatus   int
		wantErr        bool
	}{
		{
			name:      "successful message send",
			sessionID: "test-session",
			message: &Message{
				Content: "Hello, agent!",
				Type:    "user",
			},
			serverResponse: `{"content": "Hello, agent!", "type": "user", "role": "user", "timestamp": "2023-01-01T00:00:00Z", "id": "msg123"}`,
			serverStatus:   http.StatusOK,
			wantErr:        false,
		},
		{
			name:      "session not found",
			sessionID: "nonexistent",
			message: &Message{
				Content: "Hello",
				Type:    "user",
			},
			serverResponse: `{"error": "Session not found"}`,
			serverStatus:   http.StatusNotFound,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/message"
				if r.Method != "POST" || r.URL.Path != expectedPath {
					t.Errorf("Expected POST %s, got %s %s", expectedPath, r.Method, r.URL.Path)
				}

				var msg Message
				if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
					t.Errorf("Failed to decode message: %v", err)
				}

				if msg.Content != tt.message.Content {
					t.Errorf("Expected content %s, got %s", tt.message.Content, msg.Content)
				}

				w.WriteHeader(tt.serverStatus)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			client := NewClient(server.URL)
			resp, err := client.SendMessage(context.Background(), tt.sessionID, tt.message)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if resp.Content != tt.message.Content {
				t.Errorf("Expected content %s, got %s", tt.message.Content, resp.Content)
			}
		})
	}
}

func TestClient_GetMessages(t *testing.T) {
	sessionID := "test-session"
	serverResponse := `{"messages": [{"content": "Hello", "type": "user", "role": "user", "timestamp": "2023-01-01T00:00:00Z", "id": "msg1"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/messages"
		if r.Method != "GET" || r.URL.Path != expectedPath {
			t.Errorf("Expected GET %s, got %s %s", expectedPath, r.Method, r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(serverResponse)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.GetMessages(context.Background(), sessionID)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	if len(resp.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(resp.Messages))
		return
	}

	if resp.Messages[0].Content != "Hello" {
		t.Errorf("Expected content 'Hello', got %s", resp.Messages[0].Content)
	}
}

func TestClient_GetStatus(t *testing.T) {
	sessionID := "test-session"
	serverResponse := `{"status": "stable"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/status"
		if r.Method != "GET" || r.URL.Path != expectedPath {
			t.Errorf("Expected GET %s, got %s %s", expectedPath, r.Method, r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(serverResponse)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.GetStatus(context.Background(), sessionID)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	if resp.Status != "stable" {
		t.Errorf("Expected status 'stable', got %s", resp.Status)
	}
}

func TestClient_StreamEvents(t *testing.T) {
	sessionID := "test-session"
	testEvents := []string{
		"data: {\"type\": \"message\", \"content\": \"Hello\"}",
		"data: {\"type\": \"status\", \"status\": \"running\"}",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/events"
		if r.Method != "GET" || r.URL.Path != expectedPath {
			t.Errorf("Expected GET %s, got %s %s", expectedPath, r.Method, r.URL.Path)
		}

		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Expected Accept: text/event-stream, got %s", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		for _, event := range testEvents {
			if _, err := w.Write([]byte(event + "\n")); err != nil {
				t.Errorf("Failed to write event: %v", err)
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eventChan, errorChan := client.StreamEvents(ctx, sessionID)

	var receivedEvents []string
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				goto end
			}
			receivedEvents = append(receivedEvents, event)
		case err := <-errorChan:
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
		case <-time.After(2 * time.Second):
			goto end
		}
	}

end:
	if len(receivedEvents) != len(testEvents) {
		t.Errorf("Expected %d events, got %d", len(testEvents), len(receivedEvents))
		return
	}

	for i, expected := range testEvents {
		if !strings.Contains(receivedEvents[i], expected) {
			t.Errorf("Event %d: expected to contain %s, got %s", i, expected, receivedEvents[i])
		}
	}
}
