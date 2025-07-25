package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

// Client represents an agentapi-proxy client
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new agentapi-proxy client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: utils.NewDefaultHTTPClient(),
	}
}

// StartRequest represents the request body for starting a new agentapi server
type StartRequest struct {
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// StartResponse represents the response from starting a new agentapi server
type StartResponse struct {
	SessionID string `json:"session_id"`
}

// SessionInfo represents information about a session
type SessionInfo struct {
	SessionID string            `json:"session_id"`
	UserID    string            `json:"user_id"`
	Status    string            `json:"status"`
	StartedAt time.Time         `json:"started_at"`
	Port      int               `json:"port"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// SearchResponse represents the response from searching sessions
type SearchResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// Message represents an agentapi message
type Message struct {
	Content   string    `json:"content"`
	Type      string    `json:"type"` // "user" or "raw"
	Role      string    `json:"role,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	ID        string    `json:"id,omitempty"`
}

// MessageResponse represents the response from sending a message
type MessageResponse struct {
	Message
}

// MessagesResponse represents the response from getting messages
type MessagesResponse struct {
	Messages []Message `json:"messages"`
}

// StatusResponse represents the agent status
type StatusResponse struct {
	Status string `json:"status"` // "stable" or "running"
}

// DeleteResponse represents the response from deleting a session
type DeleteResponse struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id"`
}

// Start creates a new agentapi session
func (c *Client) Start(ctx context.Context, req *StartRequest) (*StartResponse, error) {
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/start", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var startResp StartResponse
	if err := json.NewDecoder(resp.Body).Decode(&startResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &startResp, nil
}

// Search lists and filters sessions
func (c *Client) Search(ctx context.Context, status string) (*SearchResponse, error) {
	return c.SearchWithTags(ctx, status, nil)
}

// SearchWithTags lists and filters sessions with tag support
func (c *Client) SearchWithTags(ctx context.Context, status string, tags map[string]string) (*SearchResponse, error) {
	u, err := url.Parse(c.baseURL + "/search")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	q := u.Query()
	if status != "" {
		q.Set("status", status)
	}
	// Add tag filters
	for key, value := range tags {
		q.Set("tag."+key, value)
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &searchResp, nil
}

// DeleteSession terminates and deletes a session
func (c *Client) DeleteSession(ctx context.Context, sessionID string) (*DeleteResponse, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("session ID is required")
	}

	url := fmt.Sprintf("%s/sessions/%s", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("session not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var deleteResp DeleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&deleteResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &deleteResp, nil
}

// SendMessage sends a message to an agentapi session
func (c *Client) SendMessage(ctx context.Context, sessionID string, message *Message) (*MessageResponse, error) {
	jsonData, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	url := fmt.Sprintf("%s/%s/message", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var msgResp MessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &msgResp, nil
}

// GetMessages retrieves conversation history from an agentapi session
func (c *Client) GetMessages(ctx context.Context, sessionID string) (*MessagesResponse, error) {
	url := fmt.Sprintf("%s/%s/messages", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var messagesResp MessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&messagesResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &messagesResp, nil
}

// GetStatus retrieves the current agent status from an agentapi session
func (c *Client) GetStatus(ctx context.Context, sessionID string) (*StatusResponse, error) {
	url := fmt.Sprintf("%s/%s/status", c.baseURL, sessionID)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var statusResp StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &statusResp, nil
}

// StreamEvents subscribes to Server-Sent Events (SSE) from an agentapi session
func (c *Client) StreamEvents(ctx context.Context, sessionID string) (<-chan string, <-chan error) {
	eventChan := make(chan string, 100)
	errorChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errorChan)

		url := fmt.Sprintf("%s/%s/events", c.baseURL, sessionID)
		httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			errorChan <- fmt.Errorf("failed to create request: %w", err)
			return
		}
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.Header.Set("Cache-Control", "no-cache")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			errorChan <- fmt.Errorf("failed to send request: %w", err)
			return
		}
		defer func() {
			_ = resp.Body.Close()
		}()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errorChan <- fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			select {
			case eventChan <- line:
			case <-ctx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errorChan <- fmt.Errorf("error reading response: %w", err)
		}
	}()

	return eventChan, errorChan
}
