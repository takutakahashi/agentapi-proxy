package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AgentapiClient is a minimal HTTP client for the claude-agentapi HTTP API
// (https://github.com/takutakahashi/claude-agentapi).
//
// All methods target the single-session API running at baseURL (e.g.
// http://localhost:8080).
type AgentapiClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewAgentapiClient creates a client that talks to the agentapi at baseURL.
func NewAgentapiClient(baseURL string) *AgentapiClient {
	return &AgentapiClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// ---- response types -----------------------------------------------------

// AgentStatus is the response from GET /status.
type AgentStatus struct {
	AgentType string `json:"agent_type"`
	Status    string `json:"status"` // "running" | "stable"
}

// AgentMessage is one entry in the conversation history.
type AgentMessage struct {
	ID              int    `json:"id"`
	Role            string `json:"role"` // "user","assistant","agent","tool_result"
	Content         string `json:"content"`
	Time            string `json:"time"`
	Type            string `json:"type,omitempty"` // "normal","question","plan"
	ToolUseID       string `json:"toolUseId,omitempty"`
	ParentToolUseID string `json:"parentToolUseId,omitempty"`
	Status          string `json:"status,omitempty"` // "success","error"
	Error           string `json:"error,omitempty"`
}

// MessagesResponse is the response from GET /messages.
type MessagesResponse struct {
	Messages []AgentMessage `json:"messages"`
	Total    int            `json:"total"`
	HasMore  bool           `json:"hasMore"`
}

// PendingAction is an action awaiting a user response from GET /action.
type PendingAction struct {
	Type      string          `json:"type"` // "answer_question","approve_plan"
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

// ActionsResponse is the response from GET /action.
type ActionsResponse struct {
	PendingActions []PendingAction `json:"pending_actions"`
}

// SSEEvent is a single event received from GET /events (Server-Sent Events).
type SSEEvent struct {
	Event string // "init","message_update","status_change"
	Data  json.RawMessage
}

// ---- HTTP methods -------------------------------------------------------

// GetStatus calls GET /status.
func (c *AgentapiClient) GetStatus(ctx context.Context) (*AgentStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var s AgentStatus
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// GetMessages calls GET /messages and returns the full history.
func (c *AgentapiClient) GetMessages(ctx context.Context) (*MessagesResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/messages", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var m MessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// PostMessage calls POST /message.  msgType should be "user".
func (c *AgentapiClient) PostMessage(ctx context.Context, content, msgType string) error {
	payload := map[string]string{"content": content, "type": msgType}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/message", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /message returned %s", resp.Status)
	}
	return nil
}

// PostAction calls POST /action.
//   - actionType: "stop_agent", "approve_plan", or "answer_question"
//   - approved:   pointer to bool (only for "approve_plan")
//   - answers:    map[toolUseId]answer (only for "answer_question")
func (c *AgentapiClient) PostAction(ctx context.Context, actionType string, approved *bool, answers map[string]string) error {
	payload := map[string]interface{}{"type": actionType}
	if approved != nil {
		payload["approved"] = *approved
	}
	if answers != nil {
		payload["answers"] = answers
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/action", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /action returned %s", resp.Status)
	}
	return nil
}

// GetActions calls GET /action and returns any pending actions.
func (c *AgentapiClient) GetActions(ctx context.Context) (*ActionsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/action", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var a ActionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return nil, err
	}
	return &a, nil
}

// StreamEvents subscribes to GET /events and streams parsed SSE events.
//
// The returned eventCh is closed when the connection ends (context cancelled
// or server closes the stream).  errCh receives at most one non-nil error.
// Callers should drain eventCh until it is closed.
func (c *AgentapiClient) StreamEvents(ctx context.Context) (<-chan SSEEvent, <-chan error) {
	eventCh := make(chan SSEEvent, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		// The SSE connection must not timeout while the agent is thinking.
		sseHTTP := &http.Client{}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/events", nil)
		if err != nil {
			errCh <- err
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")

		resp, err := sseHTTP.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				errCh <- err
			}
			return
		}
		defer func() { _ = resp.Body.Close() }()

		scanner := bufio.NewScanner(resp.Body)
		var eventType string
		var dataLines []string

		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				// Blank line → end of one event block.
				if len(dataLines) > 0 {
					evt := SSEEvent{
						Event: eventType,
						Data:  json.RawMessage(strings.Join(dataLines, "\n")),
					}
					select {
					case eventCh <- evt:
					case <-ctx.Done():
						return
					}
				}
				eventType = ""
				dataLines = nil
				continue
			}

			if after, ok := strings.CutPrefix(line, "event: "); ok {
				eventType = after
			} else if after, ok := strings.CutPrefix(line, "data: "); ok {
				dataLines = append(dataLines, after)
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errCh <- err
		}
	}()

	return eventCh, errCh
}
