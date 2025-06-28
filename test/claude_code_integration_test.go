//go:build e2e

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

const (
	proxyPort   = "18080"
	proxyURL    = "http://localhost:" + proxyPort
	testTimeout = 60 * time.Second
)

func TestClaudeCodeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if ANTHROPIC_API_KEY is available for real testing
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("CLAUDE_API_KEY")
	}
	if apiKey == "" || apiKey == "test-key-for-local-testing" {
		t.Skip("Skipping e2e test: ANTHROPIC_API_KEY not available")
	}

	// Verify Claude Code CLI is available
	if err := exec.Command("claude", "--version").Run(); err != nil {
		t.Skip("Skipping e2e test: Claude Code CLI not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Step 1: Start agentapi-proxy server
	proxyProcess, cleanup, err := startProxyServer(t)
	if err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer cleanup()

	// Wait for proxy to be ready
	if err := waitForProxyReady(ctx, proxyURL); err != nil {
		t.Fatalf("Proxy server failed to start: %v", err)
	}

	// Step 2: Create a test session
	clientInstance := client.NewClient(proxyURL)
	startReq := &client.StartRequest{
		Environment: map[string]string{
			"TEST_E2E": "true",
		},
	}

	startResp, err := clientInstance.Start(ctx, startReq)
	if err != nil {
		t.Fatalf("Failed to start session: %v", err)
	}

	sessionID := startResp.SessionID
	t.Logf("Created session: %s", sessionID)

	// Wait for agentapi server to start up for this session
	time.Sleep(5 * time.Second)

	// Step 3: Test Claude Code interaction through the proxy
	testMessage := "Hello, Claude! This is an e2e test message."

	// Send a message to Claude Code via the proxy
	response, err := sendMessageThroughProxy(ctx, proxyURL, sessionID, testMessage)
	if err != nil {
		t.Fatalf("Failed to send message through proxy: %v", err)
	}

	// Verify the response
	if response == "" {
		t.Error("Expected non-empty response from Claude Code")
	}

	t.Logf("Received response: %s", response)

	// Step 4: Verify session status
	status, err := clientInstance.GetStatus(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to get session status: %v", err)
	}

	if status.Status != "active" {
		t.Errorf("Expected session status 'active', got '%s'", status.Status)
	}

	// Step 5: Test message history
	messages, err := clientInstance.GetMessages(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to get messages: %v", err)
	}

	if len(messages.Messages) < 2 {
		t.Errorf("Expected at least 2 messages (user + assistant), got %d", len(messages.Messages))
	}

	// Verify our test message is in the history
	found := false
	for _, msg := range messages.Messages {
		if strings.Contains(msg.Content, testMessage) {
			found = true
			break
		}
	}

	if !found {
		t.Error("Test message not found in message history")
	}

	// Step 6: Test with a code-related query
	codeQuery := "Write a simple Hello World program in Go"
	codeResponse, err := sendMessageThroughProxy(ctx, proxyURL, sessionID, codeQuery)
	if err != nil {
		t.Fatalf("Failed to send code query: %v", err)
	}

	// Verify the code response contains relevant keywords
	if !strings.Contains(strings.ToLower(codeResponse), "package") &&
		!strings.Contains(strings.ToLower(codeResponse), "func") &&
		!strings.Contains(strings.ToLower(codeResponse), "hello") {
		t.Error("Code response doesn't seem to contain expected Go code elements")
	}

	t.Logf("Code response: %s", codeResponse)

	// Step 7: Clean up session
	deleteResp, err := clientInstance.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	if deleteResp.SessionID != sessionID {
		t.Errorf("Expected session_id %s in delete response, got %s", sessionID, deleteResp.SessionID)
	}

	// Stop proxy gracefully if possible
	if proxyProcess != nil && proxyProcess.Process != nil {
		_ = proxyProcess.Process.Signal(os.Interrupt)
		time.Sleep(1 * time.Second)
	}
}

func TestClaudeCodeWithMultipleSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if ANTHROPIC_API_KEY is available for real testing
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("CLAUDE_API_KEY")
	}
	if apiKey == "" || apiKey == "test-key-for-local-testing" {
		t.Skip("Skipping e2e test: ANTHROPIC_API_KEY not available")
	}

	// Verify Claude Code CLI is available
	if err := exec.Command("claude", "--version").Run(); err != nil {
		t.Skip("Skipping e2e test: Claude Code CLI not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout*2)
	defer cancel()

	// Start proxy server
	_, cleanup, err := startProxyServer(t)
	if err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer cleanup()

	// Wait for proxy to be ready
	if err := waitForProxyReady(ctx, proxyURL); err != nil {
		t.Fatalf("Proxy server failed to start: %v", err)
	}

	clientInstance := client.NewClient(proxyURL)

	// Create multiple sessions
	const numSessions = 3
	sessionIDs := make([]string, numSessions)

	for i := 0; i < numSessions; i++ {
		startReq := &client.StartRequest{
			Environment: map[string]string{
				"SESSION_INDEX": fmt.Sprintf("%d", i),
			},
		}

		startResp, err := clientInstance.Start(ctx, startReq)
		if err != nil {
			t.Fatalf("Failed to start session %d: %v", i, err)
		}

		sessionIDs[i] = startResp.SessionID
		t.Logf("Created session %d: %s", i, sessionIDs[i])
	}

	// Wait for all agentapi servers to start up
	time.Sleep(5 * time.Second)

	// Send different messages to each session
	for i, sessionID := range sessionIDs {
		message := fmt.Sprintf("This is message from session %d", i)
		response, err := sendMessageThroughProxy(ctx, proxyURL, sessionID, message)
		if err != nil {
			t.Errorf("Failed to send message to session %d: %v", i, err)
			continue
		}

		if response == "" {
			t.Errorf("Empty response from session %d", i)
		}

		t.Logf("Session %d response: %s", i, response)
	}

	// Verify all sessions are active
	for i, sessionID := range sessionIDs {
		status, err := clientInstance.GetStatus(ctx, sessionID)
		if err != nil {
			t.Errorf("Failed to get status for session %d: %v", i, err)
			continue
		}

		if status.Status != "active" {
			t.Errorf("Session %d status is not active: %s", i, status.Status)
		}
	}

	// Clean up all sessions
	for i, sessionID := range sessionIDs {
		_, err := clientInstance.DeleteSession(ctx, sessionID)
		if err != nil {
			t.Errorf("Failed to delete session %d: %v", i, err)
		}
	}
}

func startProxyServer(t *testing.T) (*exec.Cmd, func(), error) {
	// Try multiple possible binary locations
	possiblePaths := []string{
		"./bin/agentapi-proxy",
		"bin/agentapi-proxy",
		"../bin/agentapi-proxy",
	}
	
	var binaryPath string
	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			binaryPath = path
			break
		}
	}
	
	if binaryPath == "" {
		// Get current working directory for debugging
		wd, _ := os.Getwd()
		return nil, nil, fmt.Errorf("agentapi-proxy binary not found in any of %v. Current working directory: %s", possiblePaths, wd)
	}

	// Start the proxy server with config that disables auth
	cmd := exec.Command(binaryPath, "server", "--port", proxyPort, "--verbose", "--config", "test/e2e-config.json")

	// Set environment for the proxy
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("CLAUDE_API_KEY")
	}
	if apiKey == "" {
		apiKey = "test-key-for-local-testing"
	}

	cmd.Env = append(os.Environ(),
		"ANTHROPIC_API_KEY="+apiKey,
		"CLAUDE_API_KEY="+apiKey,
		"LOG_LEVEL=debug",
		"PATH="+os.Getenv("PATH"),
	)

	// Capture output for debugging
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start proxy server: %v", err)
	}

	cleanup := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		t.Logf("Proxy stdout: %s", stdout.String())
		t.Logf("Proxy stderr: %s", stderr.String())
	}

	return cmd, cleanup, nil
}

func waitForProxyReady(ctx context.Context, url string) error {
	client := &http.Client{Timeout: 2 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(url + "/search")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		time.Sleep(1 * time.Second)
	}
}

func sendMessageThroughProxy(ctx context.Context, proxyURL, sessionID, message string) (string, error) {
	// Create the message payload
	payload := map[string]interface{}{
		"content": message,
		"type":    "user",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal message: %v", err)
	}

	// Send the message
	url := fmt.Sprintf("%s/%s/message", proxyURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	// Read the response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	// Parse the response to extract the message content
	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %v", err)
	}

	// Extract message content from response
	if content, ok := response["content"].(string); ok {
		return content, nil
	}

	// If no content field, return the raw response
	return string(body), nil
}

func TestClaudeCodeToolUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if ANTHROPIC_API_KEY is available for real testing
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("CLAUDE_API_KEY")
	}
	if apiKey == "" || apiKey == "test-key-for-local-testing" {
		t.Skip("Skipping e2e test: ANTHROPIC_API_KEY not available")
	}

	// Verify Claude Code CLI is available
	if err := exec.Command("claude", "--version").Run(); err != nil {
		t.Skip("Skipping e2e test: Claude Code CLI not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout*2)
	defer cancel()

	// Start proxy server
	_, cleanup, err := startProxyServer(t)
	if err != nil {
		t.Fatalf("Failed to start proxy server: %v", err)
	}
	defer cleanup()

	// Wait for proxy to be ready
	if err := waitForProxyReady(ctx, proxyURL); err != nil {
		t.Fatalf("Proxy server failed to start: %v", err)
	}

	clientInstance := client.NewClient(proxyURL)

	// Create session with working directory
	tempDir := t.TempDir()
	startReq := &client.StartRequest{
		Environment: map[string]string{
			"WORKING_DIR": tempDir,
		},
	}

	startResp, err := clientInstance.Start(ctx, startReq)
	if err != nil {
		t.Fatalf("Failed to start session: %v", err)
	}

	sessionID := startResp.SessionID
	t.Logf("Created session with working directory: %s", sessionID)

	// Wait for agentapi server to start up for this session
	time.Sleep(5 * time.Second)

	// Test file operations through Claude Code
	toolMessage := "Create a file called test.txt with the content 'Hello from e2e test!' in the current directory"

	response, err := sendMessageThroughProxy(ctx, proxyURL, sessionID, toolMessage)
	if err != nil {
		t.Fatalf("Failed to send tool message: %v", err)
	}

	t.Logf("Tool usage response: %s", response)

	// Verify the file was created (if the working directory is accessible)
	testFilePath := filepath.Join(tempDir, "test.txt")
	if _, err := os.Stat(testFilePath); err == nil {
		content, err := os.ReadFile(testFilePath)
		if err == nil {
			expectedContent := "Hello from e2e test!"
			if strings.Contains(string(content), expectedContent) {
				t.Logf("File created successfully with correct content")
			} else {
				t.Logf("File created but content differs: %s", string(content))
			}
		}
	}

	// Test another tool usage - listing files
	listMessage := "List the files in the current directory"
	listResponse, err := sendMessageThroughProxy(ctx, proxyURL, sessionID, listMessage)
	if err != nil {
		t.Fatalf("Failed to send list message: %v", err)
	}

	t.Logf("List files response: %s", listResponse)

	// Clean up
	_, err = clientInstance.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Errorf("Failed to delete session: %v", err)
	}
}
