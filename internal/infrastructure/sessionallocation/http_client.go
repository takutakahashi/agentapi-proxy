package sessionallocation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	core "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
)

type Client struct {
	baseURL           string
	token             string
	upstreamAuthToken string
	metadataOnly      bool
	client            *http.Client
	profileRevision   string
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 35 * time.Second},
	}
}

func NewClientWithUpstreamAuth(baseURL, token, upstreamAuthToken string) *Client {
	c := NewClient(baseURL, token)
	c.upstreamAuthToken = upstreamAuthToken
	return c
}

func NewNativeAllocatorClient(baseURL, token, upstreamAuthToken string) *Client {
	c := NewClientWithUpstreamAuth(baseURL, token, upstreamAuthToken)
	c.metadataOnly = true
	return c
}

func (c *Client) authorize(req *http.Request) {
	if c.upstreamAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.upstreamAuthToken)
		req.Header.Set("X-Session-Manager-Token", c.token)
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
}

func (c *Client) Next(ctx context.Context, wait time.Duration) (*core.AllocationRequest, bool, error) {
	return c.next(ctx, "/internal/session-allocations/next", wait)
}

func (c *Client) NextExternal(ctx context.Context, wait time.Duration) (*core.AllocationRequest, bool, error) {
	path := "/internal/external-session-manager/allocations/next"
	if c.metadataOnly {
		path += "?metadata_only=true"
	}
	return c.next(ctx, path, wait)
}

func (c *Client) RuntimeProfileRevision() string {
	return c.profileRevision
}

func (c *Client) GetRuntimeProfile(ctx context.Context) (*core.RuntimeProfileSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/external-session-manager/runtime-profile", nil)
	if err != nil {
		return nil, err
	}
	c.authorize(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GET runtime profile returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var snapshot core.RuntimeProfileSnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (c *Client) next(ctx context.Context, path string, wait time.Duration) (*core.AllocationRequest, bool, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, false, err
	}
	q := u.Query()
	q.Set("wait", wait.String())
	if strings.Contains(path, "/external-session-manager/allocations/next") {
		q.Set("profile_revision", c.profileRevision)
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, err
	}
	c.authorize(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if revision := resp.Header.Get("X-AgentAPI-Runtime-Profile-Revision"); revision != "" {
		c.profileRevision = revision
	}
	if resp.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, false, fmt.Errorf("GET allocation next returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var allocation core.AllocationRequest
	if err := json.NewDecoder(resp.Body).Decode(&allocation); err != nil {
		return nil, false, err
	}
	return &allocation, true, nil
}

func (c *Client) Complete(ctx context.Context, sessionID string, result core.AllocationResult) error {
	return c.complete(ctx, "/internal/session-allocations/"+url.PathEscape(sessionID)+"/result", result)
}

func (c *Client) CompleteExternal(ctx context.Context, sessionID string, result core.AllocationResult) error {
	return c.complete(ctx, "/internal/external-session-manager/allocations/"+url.PathEscape(sessionID)+"/result", result)
}

func (c *Client) complete(ctx context.Context, path string, result core.AllocationResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	c.authorize(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("POST allocation result returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
