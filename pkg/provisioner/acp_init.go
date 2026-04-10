package provisioner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// initACPSession connects to the local acp-ws-server, performs the ACP handshake,
// optionally sends an initial message, and registers the resulting ACP session ID
// with agentapi-proxy so it can resume the session on reconnect.
func initACPSession(ctx context.Context, port, agentapiSessionID, initialMessage string) {
	wsURL := fmt.Sprintf("ws://localhost:%s/ws", port)
	log.Printf("[ACP_INIT] Connecting to acp-ws-server at %s", wsURL)

	client, err := dialACP(wsURL)
	if err != nil {
		log.Printf("[ACP_INIT] Failed to connect: %v", err)
		return
	}
	defer client.close()

	// 1. initialize
	_, err = client.call("initialize", map[string]interface{}{
		"protocolVersion": 0,
		"clientInfo":      map[string]string{"name": "agentapi-proxy-provisioner", "version": "1.0.0"},
		"clientCapabilities": map[string]interface{}{},
	}, 15*time.Second)
	if err != nil {
		log.Printf("[ACP_INIT] initialize failed: %v", err)
		return
	}

	// 2. session/new
	result, err := client.call("session/new", map[string]interface{}{
		"cwd":        "/",
		"mcpServers": []interface{}{},
	}, 30*time.Second)
	if err != nil {
		log.Printf("[ACP_INIT] session/new failed: %v", err)
		return
	}
	var newResult struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &newResult); err != nil || newResult.SessionID == "" {
		log.Printf("[ACP_INIT] Failed to parse session/new result: %v (raw: %s)", err, result)
		return
	}
	acpSessionID := newResult.SessionID
	log.Printf("[ACP_INIT] ACP session created: %s", acpSessionID)

	// 3. send initial message if provided
	if initialMessage != "" {
		log.Printf("[ACP_INIT] Sending initial message via ACP")
		_, err = client.call("session/prompt", map[string]interface{}{
			"sessionId": acpSessionID,
			"prompt":    []map[string]string{{"type": "text", "text": initialMessage}},
		}, 30*time.Second)
		if err != nil {
			log.Printf("[ACP_INIT] session/prompt failed: %v", err)
			// Non-fatal: still save the session ID
		}
	}

	// 4. POST the ACP session ID to agentapi-proxy
	proxyHost := os.Getenv("AGENTAPI_PROXY_SERVICE_HOST")
	proxyPort := os.Getenv("AGENTAPI_PROXY_SERVICE_PORT")
	agentapiKey := os.Getenv("AGENTAPI_KEY")
	if proxyHost == "" || proxyPort == "" || agentapiKey == "" {
		log.Printf("[ACP_INIT] Missing proxy env vars (AGENTAPI_PROXY_SERVICE_HOST/PORT/KEY), skipping registration")
		return
	}

	proxyURL := fmt.Sprintf("http://%s:%s/sessions/%s/acp-session", proxyHost, proxyPort, agentapiSessionID)
	body, _ := json.Marshal(map[string]string{"acp_session_id": acpSessionID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, proxyURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[ACP_INIT] Failed to create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentapiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[ACP_INIT] Failed to register ACP session ID with proxy: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[ACP_INIT] Proxy returned non-OK status %d", resp.StatusCode)
		return
	}
	log.Printf("[ACP_INIT] ACP session ID %s registered with proxy for agentapi session %s", acpSessionID, agentapiSessionID)
}
