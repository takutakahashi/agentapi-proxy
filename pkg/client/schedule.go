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

// ListSchedulesOptions specifies optional filters for listing schedules.
type ListSchedulesOptions struct {
	Status string // "active", "paused", or "completed"
	Scope  string // "user" or "team"
	TeamID string
}

// ListSchedules lists schedules and returns the raw JSON response.
func (c *Client) ListSchedules(ctx context.Context, opts *ListSchedulesOptions) (json.RawMessage, error) {
	u, err := url.Parse(c.baseURL + "/schedules")
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

// GetSchedule retrieves a schedule by ID and returns the raw JSON response.
func (c *Client) GetSchedule(ctx context.Context, id string) (json.RawMessage, error) {
	if id == "" {
		return nil, fmt.Errorf("schedule ID is required")
	}

	reqURL := fmt.Sprintf("%s/schedules/%s", c.baseURL, id)
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
		return nil, fmt.Errorf("schedule not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// CreateSchedule creates a new schedule from the given JSON body and returns the raw JSON response.
func (c *Client) CreateSchedule(ctx context.Context, data []byte) (json.RawMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("request body is required")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/schedules", bytes.NewBuffer(data))
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

// ApplySchedule partially updates a schedule by sending the given JSON as a PUT request.
// Only fields present in the JSON body are updated (merge-patch semantics).
func (c *Client) ApplySchedule(ctx context.Context, id string, data []byte) (json.RawMessage, error) {
	if id == "" {
		return nil, fmt.Errorf("schedule ID is required")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("request body is required")
	}

	reqURL := fmt.Sprintf("%s/schedules/%s", c.baseURL, id)
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
		return nil, fmt.Errorf("schedule not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return json.RawMessage(body), nil
}

// DeleteSchedule deletes a schedule by ID.
func (c *Client) DeleteSchedule(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("schedule ID is required")
	}

	reqURL := fmt.Sprintf("%s/schedules/%s", c.baseURL, id)
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
		return fmt.Errorf("schedule not found")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
