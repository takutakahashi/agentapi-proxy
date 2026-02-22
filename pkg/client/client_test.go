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

	// Verify it's the default HTTP client by checking if it's an *http.Client
	if _, ok := client.httpClient.(*http.Client); !ok {
		t.Error("Expected httpClient to be *http.Client")
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
			serverResponse: `{"ok": true}`,
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
				expectedPath := "/" + tt.sessionID + "/message"
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

			if !resp.OK {
				t.Error("Expected OK to be true, got false")
			}
		})
	}
}

func TestClient_GetMessages(t *testing.T) {
	sessionID := "test-session"
	serverResponse := `{"messages": [{"content": "Hello", "type": "user", "role": "user", "timestamp": "2023-01-01T00:00:00Z", "id": "msg1"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := "/" + sessionID + "/messages"
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
		expectedPath := "/" + sessionID + "/status"
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
		expectedPath := "/" + sessionID + "/events"
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

func TestClient_CreateTask(t *testing.T) {
	tests := []struct {
		name           string
		sessionID      string
		request        *CreateTaskRequest
		serverResponse string
		serverStatus   int
		wantErr        bool
		expectedTitle  string
	}{
		{
			name:      "successful create",
			sessionID: "session-abc",
			request: &CreateTaskRequest{
				Title:    "Test task",
				TaskType: "agent",
				Scope:    "user",
			},
			serverResponse: `{"id":"task-1","title":"Test task","status":"todo","task_type":"agent","scope":"user","owner_id":"user1","session_id":"session-abc","links":[],"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`,
			serverStatus:   http.StatusCreated,
			wantErr:        false,
			expectedTitle:  "Test task",
		},
		{
			name:      "empty session ID returns error",
			sessionID: "",
			request: &CreateTaskRequest{
				Title:    "Test task",
				TaskType: "agent",
				Scope:    "user",
			},
			wantErr: true,
		},
		{
			name:      "nil request returns error",
			sessionID: "session-abc",
			request:   nil,
			wantErr:   true,
		},
		{
			name:      "server error",
			sessionID: "session-abc",
			request: &CreateTaskRequest{
				Title:    "Test task",
				TaskType: "agent",
				Scope:    "user",
			},
			serverResponse: `{"error": "internal server error"}`,
			serverStatus:   http.StatusInternalServerError,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For cases that return error before making HTTP request, skip server
			if tt.sessionID == "" || tt.request == nil {
				c := NewClient("http://localhost:9999")
				_, err := c.CreateTask(context.Background(), tt.sessionID, tt.request)
				if tt.wantErr && err == nil {
					t.Error("Expected error, got nil")
				}
				if !tt.wantErr && err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				return
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/tasks" {
					t.Errorf("Expected POST /tasks, got %s %s", r.Method, r.URL.Path)
				}

				var req CreateTaskRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("Failed to decode request: %v", err)
				}

				if req.SessionID != tt.sessionID {
					t.Errorf("Expected session_id %s, got %s", tt.sessionID, req.SessionID)
				}

				w.WriteHeader(tt.serverStatus)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			c := NewClient(server.URL)
			resp, err := c.CreateTask(context.Background(), tt.sessionID, tt.request)

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

			if resp.Title != tt.expectedTitle {
				t.Errorf("Expected title %s, got %s", tt.expectedTitle, resp.Title)
			}
			if resp.SessionID != tt.sessionID {
				t.Errorf("Expected session_id %s, got %s", tt.sessionID, resp.SessionID)
			}
		})
	}
}

func TestClient_GetTask(t *testing.T) {
	tests := []struct {
		name           string
		taskID         string
		serverResponse string
		serverStatus   int
		wantErr        bool
	}{
		{
			name:           "successful get",
			taskID:         "task-1",
			serverResponse: `{"id":"task-1","title":"Test task","status":"todo","task_type":"agent","scope":"user","owner_id":"user1","session_id":"session-abc","links":[],"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`,
			serverStatus:   http.StatusOK,
			wantErr:        false,
		},
		{
			name:           "task not found",
			taskID:         "nonexistent",
			serverResponse: `{"error": "Task not found"}`,
			serverStatus:   http.StatusNotFound,
			wantErr:        true,
		},
		{
			name:    "empty task ID returns error",
			taskID:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.taskID == "" {
				c := NewClient("http://localhost:9999")
				_, err := c.GetTask(context.Background(), tt.taskID)
				if tt.wantErr && err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/tasks/" + tt.taskID
				if r.Method != "GET" || r.URL.Path != expectedPath {
					t.Errorf("Expected GET %s, got %s %s", expectedPath, r.Method, r.URL.Path)
				}

				w.WriteHeader(tt.serverStatus)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			c := NewClient(server.URL)
			resp, err := c.GetTask(context.Background(), tt.taskID)

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

			if resp.ID != tt.taskID {
				t.Errorf("Expected task ID %s, got %s", tt.taskID, resp.ID)
			}
		})
	}
}

func TestClient_ListTasks(t *testing.T) {
	tests := []struct {
		name           string
		opts           *ListTasksOptions
		serverResponse string
		serverStatus   int
		wantErr        bool
		expectedCount  int
		checkQuery     func(t *testing.T, r *http.Request)
	}{
		{
			name:           "list all tasks",
			opts:           nil,
			serverResponse: `{"tasks":[{"id":"task-1","title":"Task 1","status":"todo","task_type":"agent","scope":"user","owner_id":"user1","links":[],"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}],"total":1}`,
			serverStatus:   http.StatusOK,
			wantErr:        false,
			expectedCount:  1,
		},
		{
			name: "list with filters",
			opts: &ListTasksOptions{
				Scope:    "user",
				Status:   "todo",
				TaskType: "agent",
			},
			serverResponse: `{"tasks":[],"total":0}`,
			serverStatus:   http.StatusOK,
			wantErr:        false,
			expectedCount:  0,
			checkQuery: func(t *testing.T, r *http.Request) {
				if r.URL.Query().Get("scope") != "user" {
					t.Errorf("Expected scope=user in query")
				}
				if r.URL.Query().Get("status") != "todo" {
					t.Errorf("Expected status=todo in query")
				}
				if r.URL.Query().Get("task_type") != "agent" {
					t.Errorf("Expected task_type=agent in query")
				}
			},
		},
		{
			name:           "server error",
			opts:           nil,
			serverResponse: `{"error": "internal server error"}`,
			serverStatus:   http.StatusInternalServerError,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/tasks" {
					t.Errorf("Expected GET /tasks, got %s %s", r.Method, r.URL.Path)
				}

				if tt.checkQuery != nil {
					tt.checkQuery(t, r)
				}

				w.WriteHeader(tt.serverStatus)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			c := NewClient(server.URL)
			resp, err := c.ListTasks(context.Background(), tt.opts)

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

			if len(resp.Tasks) != tt.expectedCount {
				t.Errorf("Expected %d tasks, got %d", tt.expectedCount, len(resp.Tasks))
			}
		})
	}
}

func TestClient_UpdateTask(t *testing.T) {
	newStatus := "done"
	tests := []struct {
		name           string
		taskID         string
		request        *UpdateTaskRequest
		serverResponse string
		serverStatus   int
		wantErr        bool
	}{
		{
			name:   "successful update",
			taskID: "task-1",
			request: &UpdateTaskRequest{
				Status: &newStatus,
			},
			serverResponse: `{"id":"task-1","title":"Test task","status":"done","task_type":"agent","scope":"user","owner_id":"user1","links":[],"created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}`,
			serverStatus:   http.StatusOK,
			wantErr:        false,
		},
		{
			name:    "empty task ID returns error",
			taskID:  "",
			request: &UpdateTaskRequest{Status: &newStatus},
			wantErr: true,
		},
		{
			name:    "nil request returns error",
			taskID:  "task-1",
			request: nil,
			wantErr: true,
		},
		{
			name:           "task not found",
			taskID:         "nonexistent",
			request:        &UpdateTaskRequest{Status: &newStatus},
			serverResponse: `{"error": "Task not found"}`,
			serverStatus:   http.StatusNotFound,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.taskID == "" || tt.request == nil {
				c := NewClient("http://localhost:9999")
				_, err := c.UpdateTask(context.Background(), tt.taskID, tt.request)
				if tt.wantErr && err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/tasks/" + tt.taskID
				if r.Method != "PUT" || r.URL.Path != expectedPath {
					t.Errorf("Expected PUT %s, got %s %s", expectedPath, r.Method, r.URL.Path)
				}

				w.WriteHeader(tt.serverStatus)
				if _, err := w.Write([]byte(tt.serverResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			c := NewClient(server.URL)
			resp, err := c.UpdateTask(context.Background(), tt.taskID, tt.request)

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

			if resp.Status != "done" {
				t.Errorf("Expected status 'done', got %s", resp.Status)
			}
		})
	}
}

func TestClient_DeleteTask(t *testing.T) {
	tests := []struct {
		name         string
		taskID       string
		serverStatus int
		wantErr      bool
	}{
		{
			name:         "successful delete",
			taskID:       "task-1",
			serverStatus: http.StatusOK,
			wantErr:      false,
		},
		{
			name:    "empty task ID returns error",
			taskID:  "",
			wantErr: true,
		},
		{
			name:         "task not found",
			taskID:       "nonexistent",
			serverStatus: http.StatusNotFound,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.taskID == "" {
				c := NewClient("http://localhost:9999")
				err := c.DeleteTask(context.Background(), tt.taskID)
				if tt.wantErr && err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				expectedPath := "/tasks/" + tt.taskID
				if r.Method != "DELETE" || r.URL.Path != expectedPath {
					t.Errorf("Expected DELETE %s, got %s %s", expectedPath, r.Method, r.URL.Path)
				}

				if tt.serverStatus == http.StatusOK {
					w.WriteHeader(http.StatusOK)
					if _, err := w.Write([]byte(`{"success":true}`)); err != nil {
						t.Errorf("Failed to write response: %v", err)
					}
				} else {
					w.WriteHeader(tt.serverStatus)
					if _, err := w.Write([]byte(`{"error":"Task not found"}`)); err != nil {
						t.Errorf("Failed to write response: %v", err)
					}
				}
			}))
			defer server.Close()

			c := NewClient(server.URL)
			err := c.DeleteTask(context.Background(), tt.taskID)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}
