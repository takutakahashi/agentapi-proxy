package sessionmanagerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	coreallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const maxResponseBodyBytes = 16 << 20

// HTTPError is returned for a non-2xx response from the session-manager API.
type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("session-manager API %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// Client is the API-side implementation of the session lifecycle and allocation
// ports. It has no Kubernetes dependency.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type ClientOption func(*Client)

// WithHTTPClient replaces the default client. It is primarily useful for
// custom transports and tests.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		if client != nil {
			c.http = client
		}
	}
}

func NewClient(baseURL, bearerToken string, options ...ClientOption) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("valid session-manager API base URL is required")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported session-manager API URL scheme %q", parsed.Scheme)
	}
	if bearerToken == "" {
		return nil, errors.New("session-manager API bearer token is required")
	}
	client := &Client{
		baseURL: baseURL,
		token:   bearerToken,
		http:    &http.Client{Timeout: 40 * time.Second},
	}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

var _ portrepos.SessionManager = (*Client)(nil)
var _ portrepos.SessionWorkloadEnsurer = (*Client)(nil)
var _ portrepos.SessionToucher = (*Client)(nil)
var _ portrepos.SessionSandboxDomainReader = (*Client)(nil)
var _ portrepos.SessionStatusWatcher = (*Client)(nil)
var _ portrepos.SessionMessageWatcher = (*Client)(nil)
var _ portrepos.RemoteProvisionSettingsBuilder = (*Client)(nil)
var _ coreallocation.Queue = (*Client)(nil)
var _ SessionAnnotationUpdater = (*Client)(nil)
var _ StockManager = (*Client)(nil)
var _ PendingAllocationDeleter = (*Client)(nil)
var _ ProvisionRequestDeleter = (*Client)(nil)

func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/health", nil, nil)
}

func (c *Client) ExternalRuntimeProfile() *sessionsettings.RuntimeProfile {
	var profile sessionsettings.RuntimeProfile
	if err := c.do(context.Background(), http.MethodGet, "/runtime-profile", nil, &profile); err != nil {
		return nil
	}
	return &profile
}

// SubscribeStatusEvents preserves the public API's watcher capability without
// giving it Redis or Kubernetes credentials. The private client observes the
// manager's authoritative DTOs and emits changes locally.
func (c *Client) SubscribeStatusEvents() (<-chan portrepos.SessionStatusEvent, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan portrepos.SessionStatusEvent, 32)
	go func() {
		defer close(events)
		known := make(map[string]string)
		initialized := false
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			sessions, err := c.ListSessionsContext(ctx, entities.SessionFilter{})
			if err == nil {
				next := make(map[string]string, len(sessions))
				for _, session := range sessions {
					next[session.ID()] = session.Status()
					// Emit the initial authoritative snapshot as well as later changes.
					// Browser tabs intentionally disconnect while hidden; without this
					// snapshot a transition that happened during that gap is lost forever.
					if !initialized || known[session.ID()] != session.Status() {
						select {
						case events <- portrepos.SessionStatusEvent{SessionID: session.ID(), Status: session.Status(), Timestamp: time.Now()}:
						case <-ctx.Done():
							return
						}
					}
				}
				known, initialized = next, true
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return events, cancel
}

func (c *Client) SubscribeMessageEvents(sessionID string) (<-chan portrepos.SessionMessageEvent, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan portrepos.SessionMessageEvent, 1)
	go func() {
		defer close(events)
		var last time.Time
		initialized := false
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			session, err := c.GetSessionContext(ctx, sessionID)
			if err == nil && session != nil {
				current := session.LastMessageAt()
				if initialized && current.After(last) {
					select {
					case events <- portrepos.SessionMessageEvent{SessionID: sessionID, Timestamp: current}:
					case <-ctx.Done():
						return
					}
				}
				last, initialized = current, true
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return events, cancel
}

func (c *Client) CreateSession(ctx context.Context, id string, request *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	var response SessionDTO
	input := createSessionRequest{Request: request, WebhookPayload: webhookPayload}
	if err := c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id), input, &response); err != nil {
		return nil, err
	}
	return response.entity(), nil
}

func (c *Client) BuildRemoteProvisionSettings(ctx context.Context, id string, request *entities.RunServerRequest) (*sessionsettings.SessionSettings, error) {
	var response sessionsettings.SessionSettings
	input := provisionSettingsRequest{Request: request}
	if err := c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/provision-settings", input, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) GetSession(id string) entities.Session {
	session, err := c.GetSessionContext(context.Background(), id)
	if err != nil {
		return nil
	}
	return session
}

// GetSessionContext is the error-preserving variant of GetSession. A 404 is
// returned as (nil, nil).
func (c *Client) GetSessionContext(ctx context.Context, id string) (entities.Session, error) {
	var response SessionDTO
	err := c.do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id), nil, &response)
	if isHTTPStatus(err, http.StatusNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return response.entity(), nil
}

func (c *Client) ListSessions(filter entities.SessionFilter) []entities.Session {
	sessions, err := c.ListSessionsContext(context.Background(), filter)
	if err != nil {
		return nil
	}
	return sessions
}

// ListSessionsContext is the error-preserving variant of ListSessions.
func (c *Client) ListSessionsContext(ctx context.Context, filter entities.SessionFilter) ([]entities.Session, error) {
	query := make(url.Values)
	query.Set("user_id", filter.UserID)
	query.Set("status", filter.Status)
	query.Set("scope", string(filter.Scope))
	query.Set("team_id", filter.TeamID)
	if len(filter.TeamIDs) > 0 {
		query.Set("team_ids", strings.Join(filter.TeamIDs, ","))
	}
	keys := make([]string, 0, len(filter.Tags))
	for key := range filter.Tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		query.Set("tag."+key, filter.Tags[key])
	}
	var response sessionsResponse
	path := "/sessions"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := c.do(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	result := make([]entities.Session, 0, len(response.Sessions))
	for _, dto := range response.Sessions {
		result = append(result, dto.entity())
	}
	return result, nil
}

func (c *Client) DeleteSession(id string) error {
	return c.DeleteSessionContext(context.Background(), id)
}

func (c *Client) DeleteSessionContext(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id), nil, nil)
}

func (c *Client) SendMessage(ctx context.Context, id, message string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/messages", messageRequest{Message: message}, nil)
}

func (c *Client) StopAgent(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/stop", nil, nil)
}

func (c *Client) GetMessages(ctx context.Context, id string) ([]portrepos.Message, error) {
	var response messagesResponse
	if err := c.do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/messages", nil, &response); err != nil {
		return nil, err
	}
	return response.Messages, nil
}

// Shutdown intentionally does not stop the remote process or its sessions. It
// only satisfies the lifecycle port used by the API process.
func (c *Client) Shutdown(time.Duration) error { return nil }

func (c *Client) EnsureSessionWorkload(ctx context.Context, id string) (entities.Session, bool, error) {
	var response ensureWorkloadResponse
	if err := c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/ensure", nil, &response); err != nil {
		return nil, false, err
	}
	if response.Session == nil {
		return nil, response.Restoring, nil
	}
	return response.Session.entity(), response.Restoring, nil
}

func (c *Client) TouchSession(ctx context.Context, id string, at time.Time) error {
	return c.do(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/touch", touchRequest{At: at}, nil)
}

func (c *Client) GetSessionSandboxDomains(ctx context.Context, id string) (*portrepos.SandboxDomains, error) {
	var response portrepos.SandboxDomains
	if err := c.do(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/sandbox-domains", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) UpdateSessionAnnotations(ctx context.Context, id string, patch entities.UpdateSessionAnnotationsRequest) (entities.SessionAnnotations, error) {
	var response annotationsResponse
	if err := c.do(ctx, http.MethodPatch, "/sessions/"+url.PathEscape(id)+"/annotations", patch, &response); err != nil {
		return entities.SessionAnnotations{}, err
	}
	return response.Annotations, nil
}

func (c *Client) CreateStockSession(ctx context.Context, dind bool) error {
	return c.do(ctx, http.MethodPost, stockPath(dind), nil, nil)
}

func (c *Client) CountStockSessions(ctx context.Context, dind bool) (int, error) {
	var response stockCountResponse
	if err := c.do(ctx, http.MethodGet, stockPath(dind), nil, &response); err != nil {
		return 0, err
	}
	return response.Count, nil
}

func (c *Client) CreateStockSessionForPool(ctx context.Context, pool string, dind bool) error {
	return c.do(ctx, http.MethodPost, stockPoolPath(pool, dind), nil, nil)
}

func (c *Client) CountStockSessionsForPool(ctx context.Context, pool string, dind bool) (int, error) {
	var response stockCountResponse
	if err := c.do(ctx, http.MethodGet, stockPoolPath(pool, dind), nil, &response); err != nil {
		return 0, err
	}
	return response.Count, nil
}

func (c *Client) PurgeStaleStockSessions(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/stock", nil, nil)
}

func stockPath(dind bool) string {
	return "/stock?dind=" + fmt.Sprintf("%t", dind)
}

func stockPoolPath(pool string, dind bool) string {
	values := url.Values{}
	values.Set("pool", pool)
	values.Set("dind", fmt.Sprintf("%t", dind))
	return "/stock?" + values.Encode()
}

func (c *Client) DeletePendingSessionAllocation(ctx context.Context, id string) (bool, error) {
	var response pendingAllocationDeleteResponse
	if err := c.do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id)+"/pending-allocation", nil, &response); err != nil {
		return false, err
	}
	return response.Deleted, nil
}

func (c *Client) DeleteProvisionRequest(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id)+"/provision-request", nil, nil)
}

func (c *Client) SubmitExternalSessionAllocation(ctx context.Context, managerID, sessionID string, settings *sessionsettings.SessionSettings, request *entities.RunServerRequest, runtime *coreallocation.RuntimeBootstrap) error {
	input := submitExternalAllocationRequest{
		ManagerID:         managerID,
		ProvisionSettings: settings,
		Request:           request,
		Runtime:           runtime,
	}
	return c.do(ctx, http.MethodPost, "/allocations/external/"+url.PathEscape(sessionID), input, nil)
}

func (c *Client) NextSessionAllocation(ctx context.Context, wait time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return c.nextAllocation(ctx, "/allocations/next", wait, "")
}

func (c *Client) CompleteSessionAllocation(ctx context.Context, sessionID string, result coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	var response coreallocation.AllocationRequest
	if err := c.do(ctx, http.MethodPost, "/allocations/"+url.PathEscape(sessionID)+"/result", result, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) NextExternalSessionAllocation(ctx context.Context, managerID string, wait time.Duration) (*coreallocation.AllocationRequest, bool, error) {
	return c.nextAllocation(ctx, "/allocations/external/next", wait, managerID)
}

func (c *Client) CompleteExternalSessionAllocation(ctx context.Context, sessionID string, result coreallocation.AllocationResult) (*coreallocation.AllocationRequest, error) {
	var response coreallocation.AllocationRequest
	if err := c.do(ctx, http.MethodPost, "/allocations/external/"+url.PathEscape(sessionID)+"/result", result, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) nextAllocation(ctx context.Context, path string, wait time.Duration, managerID string) (*coreallocation.AllocationRequest, bool, error) {
	query := make(url.Values)
	if wait > 0 {
		query.Set("wait", wait.String())
	}
	if managerID != "" {
		query.Set("manager_id", managerID)
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var response coreallocation.AllocationRequest
	status, err := c.doWithStatus(ctx, http.MethodGet, path, nil, &response)
	if status == http.StatusNoContent {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &response, true, nil
}

func (c *Client) do(ctx context.Context, method, path string, input, output any) error {
	_, err := c.doWithStatus(ctx, method, path, input, output)
	return err
}

func (c *Client) doWithStatus(ctx context.Context, method, path string, input, output any) (int, error) {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return 0, fmt.Errorf("encode session-manager request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+RoutePrefix+path, body)
	if err != nil {
		return 0, fmt.Errorf("create session-manager request: %w", err)
	}
	req.Header.Set(echoAuthorizationHeader, "Bearer "+c.token)
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("call session-manager API: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := readErrorMessage(response.Body)
		return response.StatusCode, &HTTPError{Method: method, Path: path, StatusCode: response.StatusCode, Message: message}
	}
	if response.StatusCode == http.StatusNoContent || output == nil {
		return response.StatusCode, nil
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBodyBytes))
	if err := decoder.Decode(output); err != nil {
		return response.StatusCode, fmt.Errorf("decode session-manager response: %w", err)
	}
	return response.StatusCode, nil
}

const echoAuthorizationHeader = "Authorization"

func readErrorMessage(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil {
		return "unable to read error response"
	}
	var response errorResponse
	if json.Unmarshal(data, &response) == nil && response.Error != "" {
		return response.Error
	}
	if message := strings.TrimSpace(string(data)); message != "" {
		return message
	}
	return http.StatusText(http.StatusInternalServerError)
}

func isHTTPStatus(err error, status int) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == status
}
