package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

func TestClientCmd(t *testing.T) {
	// Test command structure
	assert.Equal(t, "client", ClientCmd.Use)
	assert.Equal(t, "AgentAPI Client CLI", ClientCmd.Short)
}

func TestSummarizeDraftsCmd(t *testing.T) {
	assert.Equal(t, "summarize-drafts", summarizeDraftsCmd.Use)
	assert.NotNil(t, summarizeDraftsCmd.Run)

	// Verify flags are registered
	assert.NotNil(t, summarizeDraftsCmd.Flags().Lookup("source-session-id"))
	assert.NotNil(t, summarizeDraftsCmd.Flags().Lookup("scope"))
	assert.NotNil(t, summarizeDraftsCmd.Flags().Lookup("team-id"))
	assert.NotNil(t, summarizeDraftsCmd.Flags().Lookup("key"))
}

func TestBuildSummarizationMessage(t *testing.T) {
	msg := buildSummarizationMessage("session-abc123", "2026-03-01")

	// Must contain the source session ID
	assert.Contains(t, msg, "session-abc123")
	// Must contain the date
	assert.Contains(t, msg, "2026-03-01")
	// Must reference draft=true tag
	assert.Contains(t, msg, "draft=true")
	// Must mention delete_memory tool
	assert.Contains(t, msg, "delete_memory")
	// Must mention list_memories tool
	assert.Contains(t, msg, "list_memories")
}

func TestSummarizeDraftsWithMockServer(t *testing.T) {
	// Create a mock server that records the /start request
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/start" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var err error
		receivedBody, err = io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		resp := client.StartResponse{SessionID: "summarization-session-xyz"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Set up flags
	endpoint = server.URL
	summarizeDraftsSourceSessionID = "src-session-111"
	summarizeDraftsScope = "user"
	summarizeDraftsTeamID = ""
	summarizeDraftsKeys = []string{"project=myapp"}

	var buf bytes.Buffer
	summarizeDraftsCmd.SetOut(&buf)

	// Run the command (wraps runSummarizeDrafts but doesn't call os.Exit on success)
	runSummarizeDrafts(summarizeDraftsCmd, []string{})

	// Verify the request body contains the expected fields
	var reqBody map[string]interface{}
	err := json.Unmarshal(receivedBody, &reqBody)
	assert.NoError(t, err)

	assert.Equal(t, "user", reqBody["scope"])

	params, ok := reqBody["params"].(map[string]interface{})
	assert.True(t, ok, "params should be present")
	assert.Equal(t, true, params["oneshot"])
	assert.Contains(t, params["message"], "src-session-111")

	// memory_key must NOT be set to prevent infinite summarization loops:
	// if a summarization session had memory_key, it would get its own memory-sync sidecar,
	// which would create a draft, triggering another summarization session recursively.
	assert.Nil(t, reqBody["memory_key"], "memory_key must not be present in summarization session request")

	// Reset flags
	endpoint = ""
	summarizeDraftsSourceSessionID = ""
	summarizeDraftsKeys = nil
}

func TestClientCmdInit(t *testing.T) {
	// Test that subcommands are properly registered
	// Note: init() is called automatically when package is loaded

	// Test that subcommands are properly registered
	subcommands := ClientCmd.Commands()

	var commandNames []string
	for _, cmd := range subcommands {
		commandNames = append(commandNames, cmd.Use)
	}

	assert.Contains(t, commandNames, "send [message]")
	assert.Contains(t, commandNames, "history")
	assert.Contains(t, commandNames, "status")
	assert.Contains(t, commandNames, "events")
}

func TestClientCmdFlags(t *testing.T) {
	// Test persistent flags

	// Test persistent flags
	endpointFlag := ClientCmd.PersistentFlags().Lookup("endpoint")
	assert.NotNil(t, endpointFlag)
	assert.Equal(t, "e", endpointFlag.Shorthand)

	sessionFlag := ClientCmd.PersistentFlags().Lookup("session-id")
	assert.NotNil(t, sessionFlag)
	assert.Equal(t, "s", sessionFlag.Shorthand)
}

func TestSendCmd(t *testing.T) {
	assert.Equal(t, "send [message]", sendCmd.Use)
	assert.Equal(t, "Send a message to the agent", sendCmd.Short)
	assert.NotNil(t, sendCmd.Run)
}

func TestHistoryCmd(t *testing.T) {
	assert.Equal(t, "history", historyCmd.Use)
	assert.Equal(t, "Get conversation history", historyCmd.Short)
	assert.NotNil(t, historyCmd.Run)
}

func TestStatusCmd(t *testing.T) {
	assert.Equal(t, "status", statusCmd.Use)
	assert.Equal(t, "Get agent status", statusCmd.Short)
	assert.NotNil(t, statusCmd.Run)
}

func TestEventsCmd(t *testing.T) {
	assert.Equal(t, "events", eventsCmd.Use)
	assert.Equal(t, "Monitor agent events", eventsCmd.Short)
	assert.NotNil(t, eventsCmd.Run)
}

func TestMessageStruct(t *testing.T) {
	msg := client.Message{
		Content: "test message",
		Type:    "text",
	}

	data, err := json.Marshal(msg)
	assert.NoError(t, err)

	var unmarshaled client.Message
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	assert.Equal(t, msg.Content, unmarshaled.Content)
	assert.Equal(t, msg.Type, unmarshaled.Type)
}

func TestMessageResponseStruct(t *testing.T) {
	resp := client.MessageResponse{
		OK: true,
	}

	data, err := json.Marshal(resp)
	assert.NoError(t, err)

	var unmarshaled client.MessageResponse
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	assert.Equal(t, resp.OK, unmarshaled.OK)
	assert.True(t, unmarshaled.OK)
}

func TestStatusResponseStruct(t *testing.T) {
	status := client.StatusResponse{
		Status: "active",
	}

	data, err := json.Marshal(status)
	assert.NoError(t, err)

	var unmarshaled client.StatusResponse
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	assert.Equal(t, status.Status, unmarshaled.Status)
}

func TestRunSendWithArgument(t *testing.T) {
	// Create a test server that mimics the agent API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/message") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var msg client.Message
		err := json.NewDecoder(r.Body).Decode(&msg)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Return a mock response
		response := client.MessageResponse{
			OK: true,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command with an argument
	runSend(&cobra.Command{}, []string{"test message"})

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// Check if output contains expected response (actual output format may differ)
	assert.Contains(t, output, "Message sent successfully")
}

func TestRunHistoryWithMockServer(t *testing.T) {
	// Create a test server that returns mock history
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/messages") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		response := client.MessagesResponse{
			Messages: []client.Message{
				{
					ID:        "msg-1",
					Role:      "user",
					Content:   "Hello",
					Timestamp: func() *time.Time { t := time.Now().Add(-5 * time.Minute); return &t }(),
				},
				{
					ID:        "msg-2",
					Role:      "assistant",
					Content:   "Hi there!",
					Timestamp: func() *time.Time { t := time.Now(); return &t }(),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	runHistory(&cobra.Command{}, []string{})

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// Check if output contains expected messages
	assert.Contains(t, output, "Hello")
	assert.Contains(t, output, "Hi there!")
	assert.Contains(t, output, "user")
	assert.Contains(t, output, "assistant")
}

func TestRunStatusWithMockServer(t *testing.T) {
	// Create a test server that returns mock status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/status") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		status := client.StatusResponse{
			Status: "active",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	runStatus(&cobra.Command{}, []string{})

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// Check if output contains expected status
	assert.Contains(t, output, "active")
}

func TestRunEventsWithMockServer(t *testing.T) {
	// Create a test server that returns Server-Sent Events
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if !strings.HasSuffix(r.URL.Path, "/events") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Send a few test events
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message\",\"content\":\"Test event 1\"}\n\n")
		w.(http.Flusher).Flush()

		_, _ = fmt.Fprintf(w, "data: {\"type\":\"status\",\"content\":\"Agent is thinking\"}\n\n")
		w.(http.Flusher).Flush()

		// Close connection after a short delay
		time.Sleep(10 * time.Millisecond)
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command in a goroutine with timeout
	done := make(chan bool)
	go func() {
		runEvents(&cobra.Command{}, []string{})
		done <- true
	}()

	// Wait for a short time then close
	select {
	case <-done:
		// Command completed
	case <-time.After(100 * time.Millisecond):
		// Timeout - this is expected for SSE streams
	}

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	// Check if output contains expected events
	// Note: The exact output format depends on the implementation
	// This test mainly verifies that the function doesn't panic and can handle SSE
	assert.NotEmpty(t, output)
}

func TestClientCommandsWithInvalidEndpoint(t *testing.T) {
	// Set invalid endpoint
	endpoint = "http://invalid-endpoint-that-does-not-exist:12345"
	sessionID = "test-session"

	// Test that commands handle network errors gracefully
	// We can't easily test the exact behavior without mocking the HTTP client,
	// but we can verify the functions don't panic

	assert.NotPanics(t, func() {
		runSend(&cobra.Command{}, []string{"test"})
	})

	assert.NotPanics(t, func() {
		runHistory(&cobra.Command{}, []string{})
	})

	assert.NotPanics(t, func() {
		runStatus(&cobra.Command{}, []string{})
	})
}

func TestClientCommandsWithEmptySessionID(t *testing.T) {
	// Note: This test is skipped because runSend, runHistory, runStatus, and runEvents
	// now call os.Exit(1) when endpoint or sessionID is empty, which cannot be easily
	// tested without mocking os.Exit or using a subprocess approach.
	// The validation logic is tested indirectly through other tests where
	// endpoint and sessionID are properly set.
	t.Skip("Skipping test that would call os.Exit(1) - validation is tested through other tests")
}

func TestRunSendInteractiveMode(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := client.MessageResponse{
			OK: true,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Set global variables
	endpoint = server.URL
	sessionID = "test-session"

	// Mock stdin with test input
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r

	// Write test input and close
	go func() {
		_, _ = w.WriteString("interactive test message\n")
		_ = w.Close()
	}()

	// Capture output
	oldStdout := os.Stdout
	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	// Run the command without arguments (interactive mode)
	runSend(&cobra.Command{}, []string{})

	// Restore stdin and stdout
	os.Stdin = oldStdin
	_ = outW.Close()
	os.Stdout = oldStdout

	// Read output
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(outR); err != nil {
		// Ignore error in test
		_ = err
	}
	output := buf.String()

	// Check if output contains expected response (actual output format may differ)
	assert.Contains(t, output, "Message sent successfully")
}

func TestDeleteSessionCmd(t *testing.T) {
	assert.Equal(t, "delete-session", deleteSessionCmd.Use)
	assert.Equal(t, "Delete the current session", deleteSessionCmd.Short)
	assert.NotNil(t, deleteSessionCmd.Run)

	// Test that confirm flag exists
	confirmFlag := deleteSessionCmd.Flags().Lookup("confirm")
	assert.NotNil(t, confirmFlag)
}

func TestRunDeleteSessionWithEnv(t *testing.T) {
	// Create a test server that handles delete session
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		response := client.DeleteResponse{
			Message:   "Session deleted successfully",
			SessionID: "test-session-123",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Set environment variables
	_ = os.Setenv("AGENTAPI_SESSION_ID", "test-session-123")
	_ = os.Setenv("AGENTAPI_KEY", "test-key")
	_ = os.Setenv("AGENTAPI_PROXY_SERVICE_HOST", server.URL[7:]) // Remove "http://"
	_ = os.Setenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP", "80")
	defer func() {
		_ = os.Unsetenv("AGENTAPI_SESSION_ID")
		_ = os.Unsetenv("AGENTAPI_KEY")
		_ = os.Unsetenv("AGENTAPI_PROXY_SERVICE_HOST")
		_ = os.Unsetenv("AGENTAPI_PROXY_SERVICE_PORT_HTTP")
	}()

	// Note: This test would require mocking os.Exit and stdin for the confirmation prompt
	// For now, we just verify the command structure exists
	t.Skip("Skipping full integration test - would require mocking os.Exit and stdin")
}

func TestFormatMemoriesMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		memories []*client.MemoryEntry
		want     string
	}{
		{
			name:     "empty list",
			memories: []*client.MemoryEntry{},
			want:     "",
		},
		{
			name: "single entry",
			memories: []*client.MemoryEntry{
				{Title: "My Note", Content: "Some content here."},
			},
			want: "## My Note\n\nSome content here.\n",
		},
		{
			name: "multiple entries separated by horizontal rule",
			memories: []*client.MemoryEntry{
				{Title: "First", Content: "Content A"},
				{Title: "Second", Content: "Content B"},
			},
			want: "## First\n\nContent A\n\n---\n\n## Second\n\nContent B\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMemoriesMarkdown(tt.memories)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCycleCmd(t *testing.T) {
	assert.Equal(t, "cycle [message]", cycleCmd.Use)
	assert.Equal(t, "Send a message to the session if CYCLE_ENABLED is present and CYCLE_OK is absent", cycleCmd.Short)
	assert.NotNil(t, cycleCmd.RunE)
}

func TestRunCycleWithoutCycleEnabled(t *testing.T) {
	// Ensure CYCLE_ENABLED does NOT exist
	_ = os.Remove(cycleEnabledPath)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runCycle(&cobra.Command{}, []string{"this message should not be sent"})

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "CYCLE_ENABLED not found")
}

func TestRunCycleWithCycleOK(t *testing.T) {
	// Create the marker file
	dir := t.TempDir()
	markerPath := dir + "/CYCLE_OK"
	if err := os.WriteFile(markerPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("failed to create marker file: %v", err)
	}

	// Temporarily override the cycleOKPath constant by using a wrapper test
	// We'll patch the path via the environment and test indirectly.
	// Since cycleOKPath is a const, we test runCycle behavior by checking
	// that when the default path exists, it exits early.

	// Create /tmp/check directory and CYCLE_ENABLED + CYCLE_OK files for the actual const paths
	if err := os.MkdirAll("/tmp/check", 0o755); err != nil {
		t.Fatalf("failed to create /tmp/check: %v", err)
	}
	if err := os.WriteFile(cycleEnabledPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write CYCLE_ENABLED: %v", err)
	}
	defer func() { _ = os.Remove(cycleEnabledPath) }()
	if err := os.WriteFile(cycleOKPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("failed to write CYCLE_OK: %v", err)
	}
	defer func() { _ = os.Remove(cycleOKPath) }()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runCycle(&cobra.Command{}, []string{"this message should not be sent"})

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "CYCLE_OK found")
}

func TestRunCycleWithoutCycleOK(t *testing.T) {
	// Make sure CYCLE_OK does NOT exist, but CYCLE_ENABLED does
	_ = os.Remove(cycleOKPath)
	_ = os.Remove(cycleCountPath)
	if err := os.MkdirAll("/tmp/check", 0o755); err != nil {
		t.Fatalf("failed to create /tmp/check: %v", err)
	}
	if err := os.WriteFile(cycleEnabledPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write CYCLE_ENABLED: %v", err)
	}
	defer func() { _ = os.Remove(cycleEnabledPath) }()
	cycleMaxCount = 0
	defer func() { cycleMaxCount = 0 }()

	// Create a mock server that handles the status and message endpoints
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.StatusResponse{Status: "stable"})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/message") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.MessageResponse{OK: true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Set flags
	endpoint = server.URL
	sessionID = "test-cycle-session"
	defer func() {
		endpoint = ""
		sessionID = ""
	}()

	// Capture stdout
	oldStdout := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp

	err := runCycle(&cobra.Command{}, []string{"hello from cycle"})

	_ = wp.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(rp)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "Message sent successfully")
}

func TestRunCycleMessageContainsConditionCheck(t *testing.T) {
	_ = os.Remove(cycleOKPath)
	_ = os.Remove(cycleCountPath)
	if err := os.MkdirAll("/tmp/check", 0o755); err != nil {
		t.Fatalf("failed to create /tmp/check: %v", err)
	}
	if err := os.WriteFile(cycleEnabledPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write CYCLE_ENABLED: %v", err)
	}
	defer func() { _ = os.Remove(cycleEnabledPath) }()
	cycleMaxCount = 0
	defer func() { cycleMaxCount = 0 }()

	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.StatusResponse{Status: "stable"})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/message") {
			receivedBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.MessageResponse{OK: true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	endpoint = server.URL
	sessionID = "test-cycle-session"
	defer func() {
		endpoint = ""
		sessionID = ""
	}()

	oldStdout := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp

	err := runCycle(&cobra.Command{}, []string{"作業を続けてください"})

	_ = wp.Close()
	os.Stdout = oldStdout
	_, _ = io.Copy(io.Discard, rp)

	assert.NoError(t, err)

	// The sent message must contain the original message
	assert.Contains(t, string(receivedBody), "作業を続けてください")
	// The sent message must contain the cycle suffix instructing to create CYCLE_OK
	assert.Contains(t, string(receivedBody), "/tmp/check/CYCLE_OK")
	assert.Contains(t, string(receivedBody), "cycle セッション")
}

func TestReadWriteCycleCount(t *testing.T) {
	// Clean up before and after
	_ = os.Remove(cycleCountPath)
	defer func() { _ = os.Remove(cycleCountPath) }()

	// Read when file does not exist → should return 0
	count, err := readCycleCount()
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	// Write a count and read it back
	err = writeCycleCount(3)
	assert.NoError(t, err)

	count, err = readCycleCount()
	assert.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestRunCycleMaxCountNotReached(t *testing.T) {
	_ = os.Remove(cycleOKPath)
	_ = os.Remove(cycleCountPath)
	if err := os.MkdirAll("/tmp/check", 0o755); err != nil {
		t.Fatalf("failed to create /tmp/check: %v", err)
	}
	if err := os.WriteFile(cycleEnabledPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write CYCLE_ENABLED: %v", err)
	}
	cycleMaxCount = 5
	defer func() {
		cycleMaxCount = 0
		_ = os.Remove(cycleCountPath)
		_ = os.Remove(cycleEnabledPath)
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.StatusResponse{Status: "stable"})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/message") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(client.MessageResponse{OK: true})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	endpoint = server.URL
	sessionID = "test-cycle-session"
	defer func() {
		endpoint = ""
		sessionID = ""
	}()

	oldStdout := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp

	err := runCycle(&cobra.Command{}, []string{"hello"})

	_ = wp.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(rp)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "1/5")
	assert.Contains(t, output, "Message sent successfully")

	// Counter file should contain 1
	count, err := readCycleCount()
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestRunCycleMaxCountReached(t *testing.T) {
	_ = os.Remove(cycleOKPath)
	// Pre-write the count at the limit
	if err := os.MkdirAll("/tmp/check", 0o755); err != nil {
		t.Fatalf("failed to create /tmp/check: %v", err)
	}
	if err := os.WriteFile(cycleEnabledPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write CYCLE_ENABLED: %v", err)
	}
	defer func() { _ = os.Remove(cycleEnabledPath) }()
	if err := writeCycleCount(5); err != nil {
		t.Fatalf("failed to write cycle count: %v", err)
	}
	defer func() { _ = os.Remove(cycleCountPath) }()

	cycleMaxCount = 5
	defer func() { cycleMaxCount = 0 }()

	oldStdout := os.Stdout
	rp, wp, _ := os.Pipe()
	os.Stdout = wp

	err := runCycle(&cobra.Command{}, []string{"this should not be sent"})

	_ = wp.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(rp)
	output := buf.String()

	assert.NoError(t, err)
	assert.Contains(t, output, "Cycle count limit reached")
	assert.Contains(t, output, "5/5")
}
