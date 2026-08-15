package provisioner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type controlCommand struct {
	ID       string          `json:"id"`
	StreamID string          `json:"stream_id"`
	Type     string          `json:"type"`
	Payload  json.RawMessage `json:"payload"`
}

type controlCommandResponse struct {
	Commands   []controlCommand `json:"commands"`
	NextCursor string           `json:"next_cursor"`
}

type controlEvent struct {
	ID              string      `json:"id"`
	Type            string      `json:"type"`
	CommandID       string      `json:"command_id,omitempty"`
	CommandStreamID string      `json:"command_stream_id,omitempty"`
	Payload         interface{} `json:"payload,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
}

func runSessionControlClient(ctx context.Context, client *http.Client, cfg PullClientConfig, agentType string) {
	cursorPath := os.Getenv("SESSION_CONTROL_CURSOR_FILE")
	if cursorPath == "" {
		cursorPath = "/workspace/.agentapi-session-control-cursor"
	}
	cursorBytes, _ := os.ReadFile(cursorPath)
	cursor := strings.TrimSpace(string(cursorBytes))
	if cursor == "" {
		cursor = "0-0"
	}
	go forwardRuntimeEvents(ctx, client, cfg)

	for ctx.Err() == nil {
		commands, err := pollControlCommands(ctx, client, cfg, cursor)
		if err != nil {
			log.Printf("[SESSION_CONTROL] Command poll failed: %v", err)
			sleepOrDone(ctx, 5*time.Second)
			continue
		}
		for _, command := range commands {
			err := executeControlCommand(ctx, client, agentType, command)
			eventType := "command_completed"
			payload := map[string]string{}
			if err != nil {
				eventType = "command_failed"
				payload["error"] = err.Error()
			}
			cursor = command.StreamID
			if err := persistControlCursor(cursorPath, cursor); err != nil {
				log.Printf("[SESSION_CONTROL] Failed to persist cursor: %v", err)
			}
			result := controlEvent{ID: uuid.NewString(), Type: eventType, CommandID: command.ID, CommandStreamID: command.StreamID, Payload: payload, CreatedAt: time.Now().UTC()}
			for ctx.Err() == nil {
				if postErr := postControlEvents(ctx, client, cfg, []controlEvent{result}); postErr == nil {
					break
				} else {
					log.Printf("[SESSION_CONTROL] Failed to report command %s result: %v", command.ID, postErr)
					sleepOrDone(ctx, 5*time.Second)
				}
			}
		}
	}
}

func pollControlCommands(ctx context.Context, client *http.Client, cfg PullClientConfig, cursor string) ([]controlCommand, error) {
	u, err := url.Parse(cfg.ProxyURL + "/internal/session-control/" + url.PathEscape(cfg.SessionID) + "/commands")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("after", cursor)
	q.Set("wait", "30s")
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	authorizeControlRequest(req, cfg)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("command poll returned HTTP %d", resp.StatusCode)
	}
	var body controlCommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Commands, nil
}

func executeControlCommand(ctx context.Context, client *http.Client, agentType string, command controlCommand) error {
	localBase := "http://127.0.0.1:9000"
	var endpoint string
	var payload interface{}
	switch command.Type {
	case "prompt":
		var prompt struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(command.Payload, &prompt); err != nil {
			return err
		}
		if strings.Contains(agentType, "acp") {
			sessionID, err := localACPSessionID(ctx, client, localBase)
			if err != nil {
				return err
			}
			endpoint = localBase + "/rpc"
			payload = map[string]interface{}{"jsonrpc": "2.0", "id": command.ID, "method": "session/prompt", "params": map[string]interface{}{"sessionId": sessionID, "prompt": []map[string]string{{"type": "text", "text": prompt.Content}}}}
		} else {
			endpoint = localBase + "/message"
			payload = map[string]string{"content": prompt.Content, "type": "user"}
		}
	case "cancel":
		endpoint = localBase + "/action"
		payload = map[string]string{"action": "stop_agent"}
		if strings.Contains(agentType, "acp") {
			sessionID, err := localACPSessionID(ctx, client, localBase)
			if err != nil {
				return err
			}
			endpoint = localBase + "/rpc"
			payload = map[string]interface{}{"jsonrpc": "2.0", "id": command.ID, "method": "session/cancel", "params": map[string]string{"sessionId": sessionID}}
		}
	default:
		return fmt.Errorf("unsupported command type %q", command.Type)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("local command returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func localACPSessionID(ctx context.Context, client *http.Client, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/session", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	var body struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.SessionID == "" {
		return "", fmt.Errorf("local ACP session ID is empty")
	}
	return body.SessionID, nil
}

func postControlEvents(ctx context.Context, client *http.Client, cfg PullClientConfig, events []controlEvent) error {
	data, err := json.Marshal(map[string]interface{}{"events": events})
	if err != nil {
		return err
	}
	path := "/internal/session-control/" + url.PathEscape(cfg.SessionID) + "/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.ProxyURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	authorizeControlRequest(req, cfg)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("event upload returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func authorizeControlRequest(req *http.Request, cfg PullClientConfig) {
	req.Header.Set("Authorization", "Bearer "+cfg.SessionControlToken)
}

func forwardRuntimeEvents(ctx context.Context, client *http.Client, cfg PullClientConfig) {
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:9000/events", nil)
		if err == nil {
			req.Header.Set("Accept", "text/event-stream")
			resp, requestErr := client.Do(req)
			if requestErr == nil && resp.StatusCode == http.StatusOK {
				scanRuntimeEventStream(ctx, resp.Body, client, cfg)
				_ = resp.Body.Close()
			} else if resp != nil {
				_ = resp.Body.Close()
			}
		}
		sleepOrDone(ctx, 2*time.Second)
	}
}

func scanRuntimeEventStream(ctx context.Context, body io.Reader, client *http.Client, cfg PullClientConfig) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "" {
			continue
		}
		if err := postControlEvents(ctx, client, cfg, []controlEvent{{ID: uuid.NewString(), Type: "runtime_event", Payload: map[string]string{"data": raw}, CreatedAt: time.Now().UTC()}}); err != nil {
			return
		}
	}
}

func persistControlCursor(path, cursor string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(cursor+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
