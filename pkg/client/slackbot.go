package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ListSlackBotsOptions specifies optional filters for listing SlackBots.
type ListSlackBotsOptions struct {
	Status string // "active" or "paused"
	Scope  string // "user" or "team"
	TeamID string
}

// ListSlackBots lists SlackBots and returns the raw JSON response.
func (c *Client) ListSlackBots(ctx context.Context, opts *ListSlackBotsOptions) (json.RawMessage, error) {
	u, err := url.Parse(c.baseURL + "/slackbots")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if opts != nil {
		q := u.Query()
		if opts.Status != "" {
			q.Set("status", opts.Status)
		}
		if opts.Scope != "" {
			q.Set("scope", opts.Scope)
		}
		if opts.TeamID != "" {
			q.Set("team_id", opts.TeamID)
		}
		u.RawQuery = q.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// GetSlackBot retrieves a SlackBot by ID and returns the raw JSON response.
func (c *Client) GetSlackBot(ctx context.Context, id string) (json.RawMessage, error) {
	if id == "" {
		return nil, fmt.Errorf("SlackBot ID is required")
	}

	reqURL := fmt.Sprintf("%s/slackbots/%s", c.baseURL, id)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("SlackBot not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// CreateSlackBot creates a new SlackBot from the given JSON body and returns the raw JSON response.
func (c *Client) CreateSlackBot(ctx context.Context, data []byte) (json.RawMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("request body is required")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/slackbots", bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// ApplySlackBot partially updates a SlackBot by sending the given JSON as a PUT request.
// Only fields present in the JSON body are updated (merge-patch semantics).
func (c *Client) ApplySlackBot(ctx context.Context, id string, data []byte) (json.RawMessage, error) {
	if id == "" {
		return nil, fmt.Errorf("SlackBot ID is required")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("request body is required")
	}

	reqURL := fmt.Sprintf("%s/slackbots/%s", c.baseURL, id)
	httpReq, err := http.NewRequestWithContext(ctx, "PUT", reqURL, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if err := c.applyMiddlewares(httpReq); err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("SlackBot not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// DeleteSlackBot deletes a SlackBot by ID.
func (c *Client) DeleteSlackBot(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("SlackBot ID is required")
	}

	reqURL := fmt.Sprintf("%s/slackbots/%s", c.baseURL, id)
	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if err := c.applyMiddlewares(httpReq); err != nil {
		return err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("SlackBot not found")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
