package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

func TestAgentAPIServer_handleStartSession(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		mockResponse   string
		mockStatusCode int
		expectError    bool
		expectContent  string
	}{
		{
			name: "successful session start",
			args: map[string]interface{}{
				"user_id": "test-user",
				"environment": map[string]interface{}{
					"KEY1": "value1",
					"KEY2": "value2",
				},
			},
			mockResponse:   `{"session_id": "test-session-123"}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Session started successfully. Session ID: test-session-123",
		},
		{
			name: "missing user_id",
			args: map[string]interface{}{
				"environment": map[string]interface{}{
					"KEY1": "value1",
				},
			},
			expectError: true,
		},
		{
			name: "server error",
			args: map[string]interface{}{
				"user_id": "test-user",
			},
			mockResponse:   `{"error": "internal server error"}`,
			mockStatusCode: http.StatusInternalServerError,
			expectError:    false,
			expectContent:  "Failed to start session:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					w.WriteHeader(tt.mockStatusCode)
					_, _ = w.Write([]byte(tt.mockResponse))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer mockServer.Close()

			server := &AgentAPIServer{
				client: client.NewClient(mockServer.URL),
			}

			ctx := context.Background()
			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: tt.args,
				},
			}
			result, err := server.handleStartSession(ctx, request)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.expectContent != "" {
				require.Len(t, result.Content, 1)
				textContent, ok := mcp.AsTextContent(result.Content[0])
				require.True(t, ok, "Expected TextContent")
				assert.Contains(t, textContent.Text, tt.expectContent)
			}
		})
	}
}

func TestAgentAPIServer_handleSearchSessions(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		mockResponse   string
		mockStatusCode int
		expectError    bool
		expectContent  string
	}{
		{
			name: "successful search with results",
			args: map[string]interface{}{
				"user_id": "test-user",
				"status":  "active",
			},
			mockResponse: `{
				"sessions": [
					{
						"session_id": "session-1",
						"user_id": "test-user",
						"status": "active",
						"started_at": "2024-01-01T12:00:00Z",
						"port": 9000
					}
				]
			}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Found 1 sessions:",
		},
		{
			name: "search with no results",
			args: map[string]interface{}{
				"status": "inactive",
			},
			mockResponse:   `{"sessions": []}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Found 0 sessions:",
		},
		{
			name: "server error",
			args: map[string]interface{}{
				"user_id": "test-user",
			},
			mockResponse:   `{"error": "internal server error"}`,
			mockStatusCode: http.StatusInternalServerError,
			expectError:    false,
			expectContent:  "Failed to search sessions:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/search" {
					w.WriteHeader(tt.mockStatusCode)
					_, _ = w.Write([]byte(tt.mockResponse))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer mockServer.Close()

			server := &AgentAPIServer{
				client: client.NewClient(mockServer.URL),
			}

			ctx := context.Background()
			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: tt.args,
				},
			}
			result, err := server.handleSearchSessions(ctx, request)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.expectContent != "" {
				require.Len(t, result.Content, 1)
				textContent, ok := mcp.AsTextContent(result.Content[0])
				require.True(t, ok, "Expected TextContent")
				assert.Contains(t, textContent.Text, tt.expectContent)
			}
		})
	}
}

func TestAgentAPIServer_handleSendMessage(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		mockResponse   string
		mockStatusCode int
		expectError    bool
		expectContent  string
	}{
		{
			name: "successful message send",
			args: map[string]interface{}{
				"session_id": "test-session",
				"message":    "Hello, world!",
				"type":       "user",
			},
			mockResponse:   `{"id": "msg-123", "role": "user", "content": "Hello, world!", "timestamp": "2024-01-01T12:00:00Z"}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Message sent successfully.",
		},
		{
			name: "missing session_id",
			args: map[string]interface{}{
				"message": "Hello, world!",
			},
			expectError: true,
		},
		{
			name: "missing message",
			args: map[string]interface{}{
				"session_id": "test-session",
			},
			expectError: true,
		},
		{
			name: "default message type",
			args: map[string]interface{}{
				"session_id": "test-session",
				"message":    "Hello, world!",
			},
			mockResponse:   `{"id": "msg-123", "role": "user", "content": "Hello, world!", "timestamp": "2024-01-01T12:00:00Z"}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Message sent successfully.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/test-session/message") {
					w.WriteHeader(tt.mockStatusCode)
					_, _ = w.Write([]byte(tt.mockResponse))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer mockServer.Close()

			server := &AgentAPIServer{
				client: client.NewClient(mockServer.URL),
			}

			ctx := context.Background()
			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: tt.args,
				},
			}
			result, err := server.handleSendMessage(ctx, request)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.expectContent != "" {
				require.Len(t, result.Content, 1)
				textContent, ok := mcp.AsTextContent(result.Content[0])
				require.True(t, ok, "Expected TextContent")
				assert.Contains(t, textContent.Text, tt.expectContent)
			}
		})
	}
}

func TestAgentAPIServer_handleGetMessages(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		mockResponse   string
		mockStatusCode int
		expectError    bool
		expectContent  string
	}{
		{
			name: "successful get messages",
			args: map[string]interface{}{
				"session_id": "test-session",
			},
			mockResponse: `{
				"messages": [
					{
						"id": "msg-1",
						"role": "user",
						"content": "Hello",
						"timestamp": "2024-01-01T12:00:00Z"
					},
					{
						"id": "msg-2",
						"role": "assistant",
						"content": "Hi there!",
						"timestamp": "2024-01-01T12:01:00Z"
					}
				]
			}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Conversation History (2 messages):",
		},
		{
			name:        "missing session_id",
			args:        map[string]interface{}{},
			expectError: true,
		},
		{
			name: "empty message history",
			args: map[string]interface{}{
				"session_id": "test-session",
			},
			mockResponse:   `{"messages": []}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Conversation History (0 messages):",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/test-session/messages") {
					w.WriteHeader(tt.mockStatusCode)
					_, _ = w.Write([]byte(tt.mockResponse))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer mockServer.Close()

			server := &AgentAPIServer{
				client: client.NewClient(mockServer.URL),
			}

			ctx := context.Background()
			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: tt.args,
				},
			}
			result, err := server.handleGetMessages(ctx, request)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.expectContent != "" {
				require.Len(t, result.Content, 1)
				textContent, ok := mcp.AsTextContent(result.Content[0])
				require.True(t, ok, "Expected TextContent")
				assert.Contains(t, textContent.Text, tt.expectContent)
			}
		})
	}
}

func TestAgentAPIServer_handleGetStatus(t *testing.T) {
	tests := []struct {
		name           string
		args           map[string]interface{}
		mockResponse   string
		mockStatusCode int
		expectError    bool
		expectContent  string
	}{
		{
			name: "successful get status",
			args: map[string]interface{}{
				"session_id": "test-session",
			},
			mockResponse:   `{"status": "stable"}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Agent Status: stable",
		},
		{
			name:        "missing session_id",
			args:        map[string]interface{}{},
			expectError: true,
		},
		{
			name: "running status",
			args: map[string]interface{}{
				"session_id": "test-session",
			},
			mockResponse:   `{"status": "running"}`,
			mockStatusCode: http.StatusOK,
			expectError:    false,
			expectContent:  "Agent Status: running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/test-session/status") {
					w.WriteHeader(tt.mockStatusCode)
					_, _ = w.Write([]byte(tt.mockResponse))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer mockServer.Close()

			server := &AgentAPIServer{
				client: client.NewClient(mockServer.URL),
			}

			ctx := context.Background()
			request := mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Arguments: tt.args,
				},
			}
			result, err := server.handleGetStatus(ctx, request)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.expectContent != "" {
				require.Len(t, result.Content, 1)
				textContent, ok := mcp.AsTextContent(result.Content[0])
				require.True(t, ok, "Expected TextContent")
				assert.Contains(t, textContent.Text, tt.expectContent)
			}
		})
	}
}

func TestMCPCmdFlags(t *testing.T) {
	// Test that flags are properly initialized
	assert.NotNil(t, MCPCmd)
	assert.Equal(t, "mcp", MCPCmd.Use)
	assert.Equal(t, "Model Context Protocol Server", MCPCmd.Short)
	assert.NotNil(t, MCPCmd.Flags())

	// Test flag defaults
	flag := MCPCmd.Flags().Lookup("port")
	assert.NotNil(t, flag)
	assert.Equal(t, "3000", flag.DefValue)

	flag = MCPCmd.Flags().Lookup("proxy-url")
	assert.NotNil(t, flag)
	assert.Equal(t, "http://localhost:8080", flag.DefValue)

	flag = MCPCmd.Flags().Lookup("verbose")
	assert.NotNil(t, flag)
	assert.Equal(t, "false", flag.DefValue)
}

func TestAgentAPIServer_contextTimeout(t *testing.T) {
	// Test that context timeout is properly handled
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"session_id": "test"}`))
	}))
	defer slowServer.Close()

	server := &AgentAPIServer{
		client: client.NewClient(slowServer.URL),
	}

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	args := map[string]interface{}{
		"user_id": "test-user",
	}

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
	result, err := server.handleStartSession(ctx, request)

	// Should not return error, but result should indicate failure
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)

	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok, "Expected TextContent")
	assert.Contains(t, textContent.Text, "Failed to start session:")
}

func TestAgentAPIServer_invalidJSON(t *testing.T) {
	// Test handling of invalid JSON responses
	invalidJSONServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer invalidJSONServer.Close()

	server := &AgentAPIServer{
		client: client.NewClient(invalidJSONServer.URL),
	}

	ctx := context.Background()
	args := map[string]interface{}{
		"session_id": "test-session",
	}

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
	result, err := server.handleGetMessages(ctx, request)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)

	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok, "Expected TextContent")
	assert.Contains(t, textContent.Text, "Failed to get messages:")
}
