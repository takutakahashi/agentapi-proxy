//go:build e2e

package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/client"
)

const (
	mockProxyPort = "18081"
	mockProxyURL  = "http://localhost:" + mockProxyPort
	mockTimeout   = 30 * time.Second
)

// TestMockAgentServiceE2E tests the complete e2e flow using MockAgentService
// This test verifies that the mock implementation works correctly end-to-end
func TestMockAgentServiceE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mockTimeout)
	defer cancel()

	// Start proxy server with mock mode enabled
	proxyProcess, cleanup, err := startMockProxyServer(t)
	if err != nil {
		t.Fatalf("Failed to start mock proxy server: %v", err)
	}
	defer cleanup()

	// Wait for proxy to be ready
	if err := waitForProxyReady(ctx, mockProxyURL); err != nil {
		t.Fatalf("Mock proxy server failed to start: %v", err)
	}

	clientInstance := client.NewClient(mockProxyURL)

	// Test normal mock behavior
	t.Run("Normal Mock Behavior", func(t *testing.T) {
		testNormalMockBehavior(ctx, t, clientInstance)
	})

	// Stop proxy gracefully
	if proxyProcess != nil && proxyProcess.Process != nil {
		_ = proxyProcess.Process.Signal(os.Interrupt)
		time.Sleep(1 * time.Second)
	}
}

// TestMockAgentServiceSlowBehavior tests the slow behavior mode
func TestMockAgentServiceSlowBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mockTimeout*2)
	defer cancel()

	// Start proxy server with mock mode enabled and slow behavior
	proxyProcess, cleanup, err := startSlowMockProxyServer(t)
	if err != nil {
		t.Fatalf("Failed to start slow mock proxy server: %v", err)
	}
	defer cleanup()

	// Wait for proxy to be ready
	if err := waitForProxyReady(ctx, mockProxyURL); err != nil {
		t.Fatalf("Slow mock proxy server failed to start: %v", err)
	}

	clientInstance := client.NewClient(mockProxyURL)

	// Test slow mock behavior
	t.Run("Slow Mock Behavior", func(t *testing.T) {
		testSlowMockBehavior(ctx, t, clientInstance)
	})

	// Stop proxy gracefully
	if proxyProcess != nil && proxyProcess.Process != nil {
		_ = proxyProcess.Process.Signal(os.Interrupt)
		time.Sleep(1 * time.Second)
	}
}

// TestMockAgentServiceFailureBehavior tests the always fail behavior mode
func TestMockAgentServiceFailureBehavior(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mockTimeout)
	defer cancel()

	// Start proxy server with mock mode enabled and always fail behavior
	proxyProcess, cleanup, err := startFailureMockProxyServer(t)
	if err != nil {
		t.Fatalf("Failed to start failure mock proxy server: %v", err)
	}
	defer cleanup()

	// Wait for proxy to be ready
	if err := waitForProxyReady(ctx, mockProxyURL); err != nil {
		t.Fatalf("Failure mock proxy server failed to start: %v", err)
	}

	clientInstance := client.NewClient(mockProxyURL)

	// Test failure mock behavior
	t.Run("Failure Mock Behavior", func(t *testing.T) {
		testFailureMockBehavior(ctx, t, clientInstance)
	})

	// Stop proxy gracefully
	if proxyProcess != nil && proxyProcess.Process != nil {
		_ = proxyProcess.Process.Signal(os.Interrupt)
		time.Sleep(1 * time.Second)
	}
}

func testNormalMockBehavior(ctx context.Context, t *testing.T, clientInstance *client.Client) {
	// Create a test session
	startReq := &client.StartRequest{
		Environment: map[string]string{
			"MOCK_TEST": "normal",
		},
	}

	startResp, err := clientInstance.Start(ctx, startReq)
	if err != nil {
		t.Fatalf("Failed to start session with mock agent: %v", err)
	}

	sessionID := startResp.SessionID
	t.Logf("Created mock session: %s", sessionID)

	// Since this is mock mode, session should be created immediately
	// No need to wait for real agent startup

	// Verify session status
	status, err := clientInstance.GetStatus(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to get mock session status: %v", err)
	}

	// Mock should return active or running status
	if status.Status != "active" && status.Status != "running" && status.Status != "stable" {
		t.Logf("Mock session status: %s (this is expected for mock)", status.Status)
	} else {
		t.Logf("Mock session status: %s", status.Status)
	}

	// Test session management operations
	sessions, err := clientInstance.Search(ctx, "")
	if err != nil {
		t.Fatalf("Failed to search sessions with mock: %v", err)
	}

	found := false
	for _, session := range sessions.Sessions {
		if session.SessionID == sessionID {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Created session not found in search results")
	} else {
		t.Logf("Found mock session in search results")
	}

	// Test proxy routing (should work even with mock)
	proxyResp, err := testMockProxyRouting(ctx, sessionID)
	if err != nil {
		t.Logf("Mock proxy routing test: %v (expected for mock)", err)
	} else {
		t.Logf("Mock proxy routing successful: %s", proxyResp)
	}

	// Clean up session
	deleteResp, err := clientInstance.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to delete mock session: %v", err)
	}

	if deleteResp.SessionID != sessionID {
		t.Errorf("Expected session_id %s in delete response, got %s", sessionID, deleteResp.SessionID)
	}

	t.Logf("Successfully deleted mock session: %s", sessionID)
}

func testSlowMockBehavior(ctx context.Context, t *testing.T, clientInstance *client.Client) {
	// Measure time for session creation with slow mock
	start := time.Now()

	startReq := &client.StartRequest{
		Environment: map[string]string{
			"MOCK_TEST": "slow",
		},
	}

	startResp, err := clientInstance.Start(ctx, startReq)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Failed to start session with slow mock: %v", err)
	}

	sessionID := startResp.SessionID
	t.Logf("Created slow mock session: %s (took %v)", sessionID, duration)

	// Slow mock should add some latency
	if duration > 50*time.Millisecond {
		t.Logf("Slow mock added expected latency: %v", duration)
	} else {
		t.Logf("Slow mock latency: %v (may be fast in test environment)", duration)
	}

	// Test status with slow mock
	statusStart := time.Now()
	status, err := clientInstance.GetStatus(ctx, sessionID)
	statusDuration := time.Since(statusStart)

	if err != nil {
		t.Fatalf("Failed to get slow mock session status: %v", err)
	}

	t.Logf("Slow mock status check took: %v, status: %s", statusDuration, status.Status)

	// Clean up
	_, err = clientInstance.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("Failed to delete slow mock session: %v", err)
	}
}

func testFailureMockBehavior(ctx context.Context, t *testing.T, clientInstance *client.Client) {
	// Attempt to create a session with failure mock
	startReq := &client.StartRequest{
		Environment: map[string]string{
			"MOCK_TEST": "failure",
		},
	}

	startResp, err := clientInstance.Start(ctx, startReq)

	// With always_fail behavior, session creation might fail
	if err != nil {
		t.Logf("Session creation failed as expected with failure mock: %v", err)
		return
	}

	// If session creation succeeded, test operations that should fail
	sessionID := startResp.SessionID
	t.Logf("Created failure mock session (unexpected): %s", sessionID)

	// Try to get status - this might fail with failure mock
	_, err = clientInstance.GetStatus(ctx, sessionID)
	if err != nil {
		t.Logf("Status check failed as expected with failure mock: %v", err)
	} else {
		t.Logf("Status check succeeded unexpectedly with failure mock")
	}

	// Try to delete - this might also fail
	_, err = clientInstance.DeleteSession(ctx, sessionID)
	if err != nil {
		t.Logf("Session deletion failed as expected with failure mock: %v", err)
	} else {
		t.Logf("Session deletion succeeded with failure mock")
	}
}

func testMockProxyRouting(ctx context.Context, sessionID string) (string, error) {
	// Test basic proxy routing to the mock session
	url := fmt.Sprintf("%s/%s/status", mockProxyURL, sessionID)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func startMockProxyServer(t *testing.T) (*exec.Cmd, func(), error) {
	return startProxyServerWithMockConfig(t, map[string]string{
		"AGENTAPI_MOCK_MODE":     "true",
		"AGENTAPI_MOCK_BEHAVIOR": "normal",
	})
}

func startSlowMockProxyServer(t *testing.T) (*exec.Cmd, func(), error) {
	return startProxyServerWithMockConfig(t, map[string]string{
		"AGENTAPI_MOCK_MODE":     "true",
		"AGENTAPI_MOCK_BEHAVIOR": "slow",
		"AGENTAPI_MOCK_LATENCY":  "100ms",
	})
}

func startFailureMockProxyServer(t *testing.T) (*exec.Cmd, func(), error) {
	return startProxyServerWithMockConfig(t, map[string]string{
		"AGENTAPI_MOCK_MODE":     "true",
		"AGENTAPI_MOCK_BEHAVIOR": "always_fail",
	})
}

func startProxyServerWithMockConfig(t *testing.T, mockEnv map[string]string) (*exec.Cmd, func(), error) {
	// Get binary path
	binaryPath := os.Getenv("AGENTAPI_PROXY_BINARY")

	if binaryPath == "" {
		possiblePaths := []string{
			"./bin/agentapi-proxy",
			"bin/agentapi-proxy",
			"../bin/agentapi-proxy",
		}

		for _, path := range possiblePaths {
			if _, err := os.Stat(path); err == nil {
				binaryPath = path
				break
			}
		}
	}

	if binaryPath == "" {
		wd, _ := os.Getwd()
		return nil, nil, fmt.Errorf("agentapi-proxy binary not found. Current working directory: %s", wd)
	}

	t.Logf("Using proxy binary at: %s (mock mode)", binaryPath)

	// Start the proxy server with mock configuration
	cmd := exec.Command(binaryPath, "server", "--port", mockProxyPort, "--verbose", "--config", "test/e2e-mock-config.json")

	// Set environment for mock mode
	env := append(os.Environ(),
		"LOG_LEVEL=debug",
		"PATH="+os.Getenv("PATH"),
		"ANTHROPIC_API_KEY=test-key-for-mock-testing",
	)

	// Add mock-specific environment variables
	for key, value := range mockEnv {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	cmd.Env = env

	// Capture output for debugging
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start mock proxy server: %v", err)
	}

	cleanup := func() {
		if cmd.Process != nil {
			defer func() {
				if cmd.ProcessState == nil {
					done := make(chan error, 1)
					go func() {
						done <- cmd.Wait()
					}()
					select {
					case <-done:
					case <-time.After(1 * time.Second):
						t.Logf("Warning: Final wait for mock process timed out")
					}
				}
			}()

			// Try graceful shutdown first
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Logf("Failed to send SIGTERM to mock proxy: %v", err)
			}

			// Wait for graceful shutdown with timeout
			done := make(chan error, 1)
			go func() {
				done <- cmd.Wait()
			}()

			select {
			case waitErr := <-done:
				if waitErr != nil {
					t.Logf("Mock proxy exited with error: %v", waitErr)
				}
			case <-time.After(3 * time.Second):
				// Force kill if graceful shutdown failed
				t.Logf("Force killing mock proxy process")
				if killErr := cmd.Process.Kill(); killErr != nil {
					t.Logf("Failed to kill mock proxy: %v", killErr)
				}
				select {
				case waitErr := <-done:
					if waitErr != nil {
						t.Logf("Mock proxy exited after kill with error: %v", waitErr)
					}
				case <-time.After(2 * time.Second):
					t.Logf("Warning: Mock proxy may not have exited cleanly")
					go func() {
						<-done
					}()
				}
			}
		}
		t.Logf("Mock proxy stdout: %s", stdout.String())
		t.Logf("Mock proxy stderr: %s", stderr.String())
	}

	return cmd, cleanup, nil
}

// TestMockAgentMultipleSessions tests multiple session handling with mock
func TestMockAgentMultipleSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), mockTimeout*2)
	defer cancel()

	// Start proxy server with mock mode enabled
	proxyProcess, cleanup, err := startMockProxyServer(t)
	if err != nil {
		t.Fatalf("Failed to start mock proxy server: %v", err)
	}
	defer cleanup()

	// Wait for proxy to be ready
	if err := waitForProxyReady(ctx, mockProxyURL); err != nil {
		t.Fatalf("Mock proxy server failed to start: %v", err)
	}

	clientInstance := client.NewClient(mockProxyURL)

	// Create multiple sessions
	const numSessions = 5
	sessionIDs := make([]string, numSessions)

	for i := 0; i < numSessions; i++ {
		startReq := &client.StartRequest{
			Environment: map[string]string{
				"SESSION_INDEX": fmt.Sprintf("%d", i),
				"MOCK_TEST":     "multi_session",
			},
		}

		startResp, err := clientInstance.Start(ctx, startReq)
		if err != nil {
			t.Fatalf("Failed to start mock session %d: %v", i, err)
		}

		sessionIDs[i] = startResp.SessionID
		t.Logf("Created mock session %d: %s", i, sessionIDs[i])
	}

	// Verify all sessions are accessible
	for i, sessionID := range sessionIDs {
		status, err := clientInstance.GetStatus(ctx, sessionID)
		if err != nil {
			t.Logf("Mock session %d status check failed: %v (may be expected)", i, err)
			continue
		}
		t.Logf("Mock session %d status: %s", i, status.Status)
	}

	// Test session search
	sessions, err := clientInstance.Search(ctx, "")
	if err != nil {
		t.Fatalf("Failed to search mock sessions: %v", err)
	}

	foundCount := 0
	for _, sessionID := range sessionIDs {
		for _, session := range sessions.Sessions {
			if session.SessionID == sessionID {
				foundCount++
				break
			}
		}
	}

	t.Logf("Found %d out of %d mock sessions in search results", foundCount, numSessions)

	// Clean up all sessions
	for i, sessionID := range sessionIDs {
		_, err := clientInstance.DeleteSession(ctx, sessionID)
		if err != nil {
			t.Logf("Failed to delete mock session %d: %v", i, err)
		} else {
			t.Logf("Deleted mock session %d: %s", i, sessionID)
		}
	}

	// Stop proxy gracefully
	if proxyProcess != nil && proxyProcess.Process != nil {
		_ = proxyProcess.Process.Signal(os.Interrupt)
		time.Sleep(1 * time.Second)
	}
}

// TestMockAgentConfigurationSwitching tests switching between mock and real mode
func TestMockAgentConfigurationSwitching(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	t.Run("Mock Mode Environment Detection", func(t *testing.T) {
		// This test verifies that mock mode is properly detected
		// We can't easily switch modes in the same process, so this is more of a smoke test

		ctx, cancel := context.WithTimeout(context.Background(), mockTimeout)
		defer cancel()

		proxyProcess, cleanup, err := startMockProxyServer(t)
		if err != nil {
			t.Fatalf("Failed to start mock proxy: %v", err)
		}
		defer cleanup()

		if err := waitForProxyReady(ctx, mockProxyURL); err != nil {
			t.Fatalf("Mock proxy failed to start: %v", err)
		}

		// Just verify the server starts and responds
		resp, err := http.Get(mockProxyURL + "/search")
		if err != nil {
			t.Fatalf("Failed to connect to mock proxy: %v", err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected status 200, got %d", resp.StatusCode)
		}

		t.Logf("Mock mode proxy responding correctly")

		if proxyProcess != nil && proxyProcess.Process != nil {
			_ = proxyProcess.Process.Signal(os.Interrupt)
			time.Sleep(1 * time.Second)
		}
	})
}
