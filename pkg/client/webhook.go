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

// ListWebhooksOptions specifies optional filters for listing webhooks.
type ListWebhooksOptions struct {
	Type   string // "github" or "custom"
	Status string // "active" or "paused"
	Scope  string // "user" or "team"
	TeamID string
}

// ListWebhooks lists webhooks and returns the raw JSON response.
func (c *Client) ListWebhooks(ctx context.Context, opts *ListWebhooksOptions) (json.RawMessage, error) {
	u, err := url.Parse(c.baseURL + "/webhooks")
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if opts != nil {
		q := u.Query()
		if opts.Type != "" {
			q.Set("type", opts.Type)
		}
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

// GetWebhook retrieves a webhook by ID and returns the raw JSON response.
func (c *Client) GetWebhook(ctx context.Context, id string) (json.RawMessage, error) {
	if id == "" {
		return nil, fmt.Errorf("webhook ID is required")
	}

	reqURL := fmt.Sprintf("%s/webhooks/%s", c.baseURL, id)
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
		return nil, fmt.Errorf("webhook not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// CreateWebhook creates a new webhook from the given JSON body and returns the raw JSON response.
func (c *Client) CreateWebhook(ctx context.Context, data []byte) (json.RawMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("request body is required")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/webhooks", bytes.NewBuffer(data))
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

// ApplyWebhook partially updates a webhook by sending the given JSON as a PUT request.
// Only fields present in the JSON body are updated (merge-patch semantics).
func (c *Client) ApplyWebhook(ctx context.Context, id string, data []byte) (json.RawMessage, error) {
	if id == "" {
		return nil, fmt.Errorf("webhook ID is required")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("request body is required")
	}

	reqURL := fmt.Sprintf("%s/webhooks/%s", c.baseURL, id)
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
		return nil, fmt.Errorf("webhook not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// DeleteWebhook deletes a webhook by ID.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("webhook ID is required")
	}

	reqURL := fmt.Sprintf("%s/webhooks/%s", c.baseURL, id)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("webhook not found")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// RegenerateWebhookSecret regenerates the secret for a webhook and returns the raw JSON response.
func (c *Client) RegenerateWebhookSecret(ctx context.Context, id string) (json.RawMessage, error) {
	if id == "" {
		return nil, fmt.Errorf("webhook ID is required")
	}

	reqURL := fmt.Sprintf("%s/webhooks/%s/regenerate-secret", c.baseURL, id)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
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
		return nil, fmt.Errorf("webhook not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}
