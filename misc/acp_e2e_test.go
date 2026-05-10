//go:build ignore

// ACP E2E test script for the claude-acp agent type.
// Run with:
//
//	PROXY_URL=http://... API_KEY=... go run misc/acp_e2e_test.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	proxyURL := os.Getenv("PROXY_URL")
	if proxyURL == "" {
		proxyURL = "http://agentapi-proxy.agentapi-ui-dev.svc.cluster.local:8080"
	}
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "API_KEY env var required")
		os.Exit(1)
	}

	c := &acpClient{proxyURL: proxyURL, apiKey: apiKey}

	step("1. initialize")
	initResult := must(c.call("initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
	}))
	fmt.Printf("  protocolVersion: %v\n", dig(initResult, "protocolVersion"))
	fmt.Printf("  capabilities: %v\n", dig(initResult, "capabilities"))

	step("2. session/new (claude-acp)")
	newResult := must(c.call("session/new", map[string]any{
		"cwd":        "/home/agentapi/workdir",
		"mcpServers": []any{},
		"_meta": map[string]any{
			"tags": map[string]string{"source": "acp-e2e-test"},
			"params": map[string]string{
				"message":   "ACP e2e test – reply PONG when asked",
				"agentType": "claude-acp",
			},
		},
	}))
	sessionID, _ := newResult["sessionId"].(string)
	fmt.Printf("  sessionId: %s\n", sessionID)

	step("3. wait for session to become active")
	addr := waitActive(c, sessionID, 90*time.Second)
	fmt.Printf("  addr: %s\n", addr)

	step("4. session/list")
	listResult := must(c.call("session/list", map[string]any{}))
	sessions, _ := listResult["sessions"].([]any)
	found := false
	for _, s := range sessions {
		sm, _ := s.(map[string]any)
		if sm["sessionId"] == sessionID {
			fmt.Printf("  found in list: title=%v meta=%v\n", sm["title"], sm["_meta"])
			found = true
		}
	}
	if !found {
		fail("session not found in session/list")
	}

	step("5. session/resume")
	must(c.call("session/resume", map[string]any{"sessionId": sessionID}))
	fmt.Println("  OK")

	step("6. session/load")
	must(c.call("session/load", map[string]any{"sessionId": sessionID}))
	fmt.Println("  OK (history replay via GET /acp/sse)")

	step("7. session/prompt")
	must(c.call("session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []any{map[string]string{"type": "text", "text": "Reply with the single word PONG."}},
	}))
	fmt.Println("  sent (async)")

	step("8. wait for agent response via SSE")
	pong := waitForPong(c, sessionID, 30*time.Second)
	if pong {
		fmt.Println("  agent replied PONG ✓")
	} else {
		fail("did not see PONG in SSE within timeout")
	}

	step("9. session/cancel")
	must(c.call("session/cancel", map[string]any{"sessionId": sessionID}))
	fmt.Println("  OK")

	step("10. session/close")
	must(c.call("session/close", map[string]any{"sessionId": sessionID}))
	fmt.Println("  OK")

	step("11. verify session gone from list")
	listAfter := must(c.call("session/list", map[string]any{}))
	sessionsAfter, _ := listAfter["sessions"].([]any)
	for _, s := range sessionsAfter {
		sm, _ := s.(map[string]any)
		if sm["sessionId"] == sessionID {
			fail("session still appears in list after close")
		}
	}
	fmt.Println("  session removed ✓")

	fmt.Println("\n✓ All ACP E2E checks passed")
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

type acpClient struct {
	proxyURL string
	apiKey   string
	seq      int
}

func (c *acpClient) call(method string, params any) (map[string]any, error) {
	c.seq++
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.seq,
		"method":  method,
		"params":  params,
	}
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest(http.MethodPost, c.proxyURL+"/acp", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("%s: decode: %w", method, err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("%s: RPC error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// sseEvents fetches GET /acp/sse and returns all data lines received within timeout.
func (c *acpClient) sseEvents(sessionID string, timeout time.Duration) ([]string, error) {
	req, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/acp/sse?session_id=%s", c.proxyURL, sessionID), nil)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var lines []string
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			for _, line := range strings.Split(string(buf[:n]), "\n") {
				if strings.HasPrefix(line, "data: ") {
					lines = append(lines, strings.TrimPrefix(line, "data: "))
				}
			}
		}
		if err != nil {
			break
		}
	}
	return lines, nil
}

// waitActive polls session/list until the session is found with addr resolvable
// and the agentapi /status reports "stable".
func waitActive(c *acpClient, sessionID string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := sessionAddr(c, sessionID)
		if addr != "" {
			st := agentapiStatus(addr)
			if st == "stable" {
				return addr
			}
		}
		time.Sleep(5 * time.Second)
	}
	fail("session never became stable within timeout")
	return ""
}

// waitForPong reads SSE events and returns true if an agent_message_chunk containing
// "PONG" is observed within timeout.
func waitForPong(c *acpClient, sessionID string, timeout time.Duration) bool {
	lines, _ := c.sseEvents(sessionID, timeout)
	for _, line := range lines {
		if strings.Contains(strings.ToUpper(line), "PONG") {
			return true
		}
	}
	return false
}

func sessionAddr(c *acpClient, sessionID string) string {
	req, _ := http.NewRequest(http.MethodGet, c.proxyURL+"/search", nil)
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var result struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Addr      string `json:"addr"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	for _, s := range result.Sessions {
		if s.SessionID == sessionID {
			return s.Addr
		}
	}
	return ""
}

func agentapiStatus(addr string) string {
	resp, err := http.Get("http://" + addr + "/status") //nolint:gosec
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var st struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(b, &st)
	return st.Status
}

func dig(m map[string]any, key string) any { return m[key] }

func must(m map[string]any, err error) map[string]any {
	if err != nil {
		fail(err.Error())
	}
	return m
}

func step(s string) { fmt.Printf("\n=== %s ===\n", s) }
func fail(msg string) {
	fmt.Fprintf(os.Stderr, "FAIL: %s\n", msg)
	os.Exit(1)
}
