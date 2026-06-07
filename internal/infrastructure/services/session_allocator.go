package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	sessionAllocationSecretPrefix = "agentapi-session-allocation-"
	sessionAllocationDataKey      = "request.json"
	sessionAllocationNotifyTopic  = "agentapi:session-allocation:notify"
)

// SessionAllocationRequest is the cluster-visible request consumed by the
// leader-elected SessionAllocator.
type SessionAllocationRequest struct {
	SessionID          string                           `json:"session_id"`
	ManagerID          string                           `json:"manager_id,omitempty"`
	Request            *entities.RunServerRequest       `json:"request"`
	ProvisionSettings  *sessionsettings.SessionSettings `json:"provision_settings,omitempty"`
	WebhookPayload     []byte                           `json:"webhook_payload,omitempty"`
	Status             string                           `json:"status"`
	Message            string                           `json:"message,omitempty"`
	AllocatedSessionID string                           `json:"allocated_session_id,omitempty"`
	Requirements       SessionRequirements              `json:"requirements"`
	UpdatedAt          time.Time                        `json:"updated_at"`
}

type SessionAllocationResult struct {
	Status             string `json:"status"`
	Message            string `json:"message,omitempty"`
	AllocatedSessionID string `json:"allocated_session_id,omitempty"`
	ProxyURL           string `json:"proxy_url,omitempty"`
}

// SessionRequirements captures pod capabilities used for stock matching.
type SessionRequirements struct {
	AgentType string `json:"agent_type,omitempty"`
	Sandbox   bool   `json:"sandbox"`
	DinD      bool   `json:"dind"`
}

// CreateSession creates a session by submitting a SessionAllocationRequest when
// the leader-elected allocator is enabled. Tests and non-server usage fall back
// to direct allocation when the allocator has not been started.
func (m *KubernetesSessionManager) CreateSession(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	if !m.isSessionAllocatorEnabled() {
		return m.allocateSessionDirect(ctx, id, req, webhookPayload)
	}
	return m.submitSessionAllocation(ctx, id, req, webhookPayload)
}

// CreateSessionDirect allocates a session on this manager without submitting it
// to the cluster-wide allocator. External session manager workers use this to
// ensure the remote session ID they report matches the concrete local session.
func (m *KubernetesSessionManager) CreateSessionDirect(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	return m.allocateSessionDirect(ctx, id, req, webhookPayload)
}

func (m *KubernetesSessionManager) submitSessionAllocation(ctx context.Context, id string, req *entities.RunServerRequest, webhookPayload []byte) (entities.Session, error) {
	allocation := &SessionAllocationRequest{
		SessionID:      id,
		Request:        req,
		WebhookPayload: webhookPayload,
		Status:         "pending",
		Requirements:   sessionRequirements(req),
		UpdatedAt:      time.Now().UTC(),
	}
	if err := m.saveSessionAllocation(ctx, allocation); err != nil {
		return nil, fmt.Errorf("failed to submit session allocation: %w", err)
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Warning: failed to notify allocation request %s: %v", id, err)
	}

	return entities.NewProxySessionWithStatus(id, req.UserID, req.Scope, req.TeamID, req.Tags, allocation.UpdatedAt, "creating"), nil
}

func (m *KubernetesSessionManager) isSessionAllocatorEnabled() bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.sessionAllocatorEnabled
}

func (m *KubernetesSessionManager) setSessionAllocatorEnabled(enabled bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sessionAllocatorEnabled = enabled
}

func (m *KubernetesSessionManager) SetSessionAllocatorEnabled(enabled bool) {
	m.setSessionAllocatorEnabled(enabled)
}

func (m *KubernetesSessionManager) SetSessionAllocationNotifier(notifier SessionAllocationNotifier) {
	if notifier == nil {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sessionAllocationNotifier = notifier
}

func sessionRequirements(req *entities.RunServerRequest) SessionRequirements {
	return SessionRequirements{
		AgentType: req.AgentType,
		Sandbox:   req.Sandbox != nil && req.Sandbox.Enabled,
		DinD:      req.Docker != nil && req.Docker.Enabled,
	}
}

func sessionAllocationSecretName(sessionID string) string {
	return sessionAllocationSecretPrefix + sessionID
}

func (m *KubernetesSessionManager) getSessionAllocation(ctx context.Context, sessionID string) (*SessionAllocationRequest, error) {
	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, sessionAllocationSecretName(sessionID), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	data := sec.Data[sessionAllocationDataKey]
	if len(data) == 0 {
		return nil, fmt.Errorf("session allocation Secret %s has no %s", sec.Name, sessionAllocationDataKey)
	}
	var req SessionAllocationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("decode session allocation: %w", err)
	}
	return &req, nil
}

func (m *KubernetesSessionManager) saveSessionAllocation(ctx context.Context, req *SessionAllocationRequest) error {
	req.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode session allocation: %w", err)
	}
	name := sessionAllocationSecretName(req.SessionID)
	labels := map[string]string{
		"app.kubernetes.io/managed-by":             "agentapi-proxy",
		"agentapi.proxy/session-id":                req.SessionID,
		"agentapi.proxy/session-allocation":        "true",
		"agentapi.proxy/requirement-sandbox":       fmt.Sprintf("%t", req.Requirements.Sandbox),
		"agentapi.proxy/requirement-dind":          fmt.Sprintf("%t", req.Requirements.DinD),
		"agentapi.proxy/session-allocation-status": req.Status,
	}
	if req.ManagerID != "" {
		labels["agentapi.proxy/external-session-manager-id"] = "true"
	}
	if req.Requirements.AgentType != "" {
		labels["agentapi.proxy/agent-type"] = req.Requirements.AgentType
	}

	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		sec = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: m.namespace,
				Labels:    labels,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{sessionAllocationDataKey: data},
		}
		_, err = m.client.CoreV1().Secrets(m.namespace).Create(ctx, sec, metav1.CreateOptions{})
		if err == nil {
			m.invalidateSessionListCache("session allocation save")
		}
		return err
	}
	if err != nil {
		return err
	}
	sec.Labels = labels
	if sec.Data == nil {
		sec.Data = make(map[string][]byte)
	}
	sec.Data[sessionAllocationDataKey] = data
	_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, sec, metav1.UpdateOptions{})
	if err == nil {
		m.invalidateSessionListCache("session allocation save")
	}
	return err
}

func (m *KubernetesSessionManager) deleteSessionAllocation(ctx context.Context, sessionID string) error {
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, sessionAllocationSecretName(sessionID), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		m.invalidateSessionListCache("session allocation delete")
	}
	return err
}

func (m *KubernetesSessionManager) NextSessionAllocation(ctx context.Context, wait time.Duration) (*SessionAllocationRequest, bool, error) {
	deadline := time.Now().Add(wait)
	for {
		req, ok, err := m.claimNextSessionAllocation(ctx)
		if err != nil || ok || wait <= 0 || time.Now().After(deadline) {
			return req, ok, err
		}
		updates, cancel, err := m.subscribeSessionAllocation(ctx)
		if err != nil {
			return nil, false, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			cancel()
			return nil, false, nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			cancel()
			return nil, false, ctx.Err()
		case <-timer.C:
			cancel()
			return nil, false, nil
		case <-updates:
			timer.Stop()
			cancel()
		}
	}
}

func (m *KubernetesSessionManager) claimNextSessionAllocation(ctx context.Context) (*SessionAllocationRequest, bool, error) {
	secrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/session-allocation=true,!agentapi.proxy/external-session-manager-id,agentapi.proxy/session-allocation-status in (pending,allocating)",
	})
	if err != nil {
		return nil, false, fmt.Errorf("list session allocations: %w", err)
	}
	for i := range secrets.Items {
		sec := &secrets.Items[i]
		sessionID := sec.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			continue
		}
		req, err := m.getSessionAllocation(ctx, sessionID)
		if err != nil {
			log.Printf("[SESSION_ALLOCATOR] Failed to read allocation %s: %v", sec.Name, err)
			continue
		}
		req.Status = "allocating"
		if err := m.saveSessionAllocation(ctx, req); err != nil {
			log.Printf("[SESSION_ALLOCATOR] Failed to claim allocation %s: %v", req.SessionID, err)
			continue
		}
		return req, true, nil
	}
	return nil, false, nil
}

func (m *KubernetesSessionManager) SubmitExternalSessionAllocation(ctx context.Context, managerID, sessionID string, settings *sessionsettings.SessionSettings, req *entities.RunServerRequest) error {
	allocation := &SessionAllocationRequest{
		SessionID:         sessionID,
		ManagerID:         managerID,
		Request:           req,
		ProvisionSettings: settings,
		Status:            "pending",
		Requirements:      sessionRequirements(req),
		UpdatedAt:         time.Now().UTC(),
	}
	if err := m.saveSessionAllocation(ctx, allocation); err != nil {
		return fmt.Errorf("failed to submit external session allocation: %w", err)
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Warning: failed to notify allocation request %s: %v", sessionID, err)
	}
	return nil
}

func (m *KubernetesSessionManager) NextExternalSessionAllocation(ctx context.Context, managerID string, wait time.Duration) (*SessionAllocationRequest, bool, error) {
	deadline := time.Now().Add(wait)
	for {
		req, ok, err := m.claimNextExternalSessionAllocation(ctx, managerID)
		if err != nil || ok || wait <= 0 || time.Now().After(deadline) {
			return req, ok, err
		}
		updates, cancel, err := m.subscribeSessionAllocation(ctx)
		if err != nil {
			return nil, false, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			cancel()
			return nil, false, nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			cancel()
			return nil, false, ctx.Err()
		case <-timer.C:
			cancel()
			return nil, false, nil
		case <-updates:
			timer.Stop()
			cancel()
		}
	}
}

func (m *KubernetesSessionManager) claimNextExternalSessionAllocation(ctx context.Context, managerID string) (*SessionAllocationRequest, bool, error) {
	if managerID == "" {
		return nil, false, fmt.Errorf("managerID is required")
	}
	secrets, err := m.client.CoreV1().Secrets(m.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/session-allocation=true,agentapi.proxy/external-session-manager-id,agentapi.proxy/session-allocation-status in (pending,allocating)",
	})
	if err != nil {
		return nil, false, fmt.Errorf("list external session allocations: %w", err)
	}
	for i := range secrets.Items {
		sec := &secrets.Items[i]
		sessionID := sec.Labels["agentapi.proxy/session-id"]
		if sessionID == "" {
			continue
		}
		req, err := m.getSessionAllocation(ctx, sessionID)
		if err != nil {
			log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Failed to read allocation %s: %v", sec.Name, err)
			continue
		}
		if req.ManagerID != managerID {
			continue
		}
		req.Status = "allocating"
		if err := m.saveSessionAllocation(ctx, req); err != nil {
			log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Failed to claim allocation %s: %v", req.SessionID, err)
			continue
		}
		return req, true, nil
	}
	return nil, false, nil
}

func (m *KubernetesSessionManager) CompleteExternalSessionAllocation(ctx context.Context, sessionID string, result SessionAllocationResult) (*SessionAllocationRequest, error) {
	req, err := m.getSessionAllocation(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	req.Status = result.Status
	req.Message = result.Message
	req.AllocatedSessionID = result.AllocatedSessionID
	if err := m.saveSessionAllocation(ctx, req); err != nil {
		return nil, err
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		return nil, err
	}
	if err := m.deleteSessionAllocation(context.Background(), sessionID); err != nil {
		log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Warning: failed to delete completed allocation %s: %v", sessionID, err)
	}
	return req, nil
}

func (m *KubernetesSessionManager) CompleteSessionAllocation(ctx context.Context, sessionID string, result SessionAllocationResult) error {
	req, err := m.getSessionAllocation(ctx, sessionID)
	if err != nil {
		return err
	}
	req.Status = result.Status
	req.Message = result.Message
	req.AllocatedSessionID = result.AllocatedSessionID
	if err := m.saveSessionAllocation(ctx, req); err != nil {
		return err
	}
	if err := m.notifySessionAllocation(ctx); err != nil {
		return err
	}
	if err := m.deleteSessionAllocation(context.Background(), sessionID); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Warning: failed to delete completed allocation %s: %v", sessionID, err)
	}
	return nil
}

func (m *KubernetesSessionManager) notifySessionAllocation(ctx context.Context) error {
	m.mutex.RLock()
	notifier := m.sessionAllocationNotifier
	m.mutex.RUnlock()
	if notifier == nil {
		return nil
	}
	return notifier.Notify(ctx)
}

func (m *KubernetesSessionManager) subscribeSessionAllocation(ctx context.Context) (<-chan struct{}, func(), error) {
	m.mutex.RLock()
	notifier := m.sessionAllocationNotifier
	m.mutex.RUnlock()
	if notifier == nil {
		ch := make(chan struct{})
		return ch, func() { close(ch) }, nil
	}
	return notifier.Subscribe(ctx)
}

type SessionAllocationNotifier interface {
	Notify(ctx context.Context) error
	Subscribe(ctx context.Context) (<-chan struct{}, func(), error)
}

type LocalSessionAllocationNotifier struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

func NewLocalSessionAllocationNotifier() *LocalSessionAllocationNotifier {
	return &LocalSessionAllocationNotifier{subs: make(map[chan struct{}]struct{})}
}

func (n *LocalSessionAllocationNotifier) Notify(context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	for ch := range n.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}

func (n *LocalSessionAllocationNotifier) Subscribe(context.Context) (<-chan struct{}, func(), error) {
	ch := make(chan struct{}, 1)
	n.mu.Lock()
	n.subs[ch] = struct{}{}
	n.mu.Unlock()
	cancel := func() {
		n.mu.Lock()
		if _, ok := n.subs[ch]; ok {
			delete(n.subs, ch)
			close(ch)
		}
		n.mu.Unlock()
	}
	return ch, cancel, nil
}

type RedisSessionAllocationNotifier struct {
	client *redis.Client
	local  *LocalSessionAllocationNotifier
}

func NewRedisSessionAllocationNotifier(client *redis.Client) *RedisSessionAllocationNotifier {
	return &RedisSessionAllocationNotifier{client: client, local: NewLocalSessionAllocationNotifier()}
}

func (n *RedisSessionAllocationNotifier) Notify(ctx context.Context) error {
	_ = n.local.Notify(ctx)
	if n.client == nil {
		return nil
	}
	return n.client.Publish(ctx, sessionAllocationNotifyTopic, "ping").Err()
}

func (n *RedisSessionAllocationNotifier) Subscribe(ctx context.Context) (<-chan struct{}, func(), error) {
	if n.client == nil {
		return n.local.Subscribe(ctx)
	}
	localCh, localCancel, _ := n.local.Subscribe(ctx)
	pubsub := n.client.Subscribe(ctx, sessionAllocationNotifyTopic)
	if _, err := pubsub.Receive(ctx); err != nil {
		localCancel()
		_ = pubsub.Close()
		return nil, nil, err
	}
	out := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(out)
		redisCh := pubsub.Channel()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case _, ok := <-localCh:
				if !ok {
					localCh = nil
					continue
				}
				select {
				case out <- struct{}{}:
				default:
				}
			case _, ok := <-redisCh:
				if !ok {
					return
				}
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	cancel := func() {
		close(done)
		localCancel()
		_ = pubsub.Close()
	}
	return out, cancel, nil
}

// SessionAllocator is a leader-only worker that binds a session allocation
// request to either matching stock capacity or a newly created session Pod.
type SessionAllocator struct {
	manager *KubernetesSessionManager
	client  *SessionAllocatorClient

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

func NewSessionAllocator(manager *KubernetesSessionManager) *SessionAllocator {
	return &SessionAllocator{
		manager: manager,
		client:  NewSessionAllocatorClient(manager.allocationProxyURL(), manager.k8sConfig.ProvisionerToken),
		stopCh:  make(chan struct{}),
	}
}

func (a *SessionAllocator) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	a.running = true
	a.stopCh = make(chan struct{})
	a.mu.Unlock()

	go a.run(ctx)
	log.Printf("[SESSION_ALLOCATOR] Started")
	return nil
}

func (a *SessionAllocator) Stop() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return
	}
	close(a.stopCh)
	a.running = false
	a.mu.Unlock()
	log.Printf("[SESSION_ALLOCATOR] Stopped")
}

func (a *SessionAllocator) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		default:
		}
		req, ok, err := a.client.Next(ctx, 30*time.Second)
		if err != nil {
			log.Printf("[SESSION_ALLOCATOR] Failed to receive allocation request: %v", err)
			sleepOrContextDone(ctx, 2*time.Second)
			continue
		}
		if !ok {
			continue
		}
		a.processOne(ctx, req)
	}
}

func (a *SessionAllocator) processOne(ctx context.Context, req *SessionAllocationRequest) {
	log.Printf("[SESSION_ALLOCATOR] Allocating session %s (sandbox=%t dind=%t agent_type=%s)",
		req.SessionID, req.Requirements.Sandbox, req.Requirements.DinD, req.Requirements.AgentType)
	sess, err := a.manager.allocateSessionDirect(ctx, req.SessionID, req.Request, req.WebhookPayload)
	if err != nil {
		_ = a.client.Complete(context.Background(), req.SessionID, SessionAllocationResult{Status: "error", Message: err.Error()})
		log.Printf("[SESSION_ALLOCATOR] Allocation failed for %s: %v", req.SessionID, err)
		return
	}
	if err := a.client.Complete(context.Background(), req.SessionID, SessionAllocationResult{Status: "assigned", AllocatedSessionID: sess.ID()}); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Failed to mark allocation %s assigned: %v", req.SessionID, err)
		return
	}
	log.Printf("[SESSION_ALLOCATOR] Allocated session %s as %s", req.SessionID, sess.ID())
}

func (m *KubernetesSessionManager) allocationProxyURL() string {
	proxyURL := strings.TrimRight(m.k8sConfig.ProvisionerProxyURL, "/")
	if proxyURL != "" {
		return proxyURL
	}
	return fmt.Sprintf("http://agentapi-proxy.%s.svc.cluster.local:8080", m.namespace)
}

type SessionAllocatorClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewSessionAllocatorClient(baseURL, token string) *SessionAllocatorClient {
	return &SessionAllocatorClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 35 * time.Second},
	}
}

func (c *SessionAllocatorClient) Next(ctx context.Context, wait time.Duration) (*SessionAllocationRequest, bool, error) {
	return c.next(ctx, "/internal/session-allocations/next", wait)
}

func (c *SessionAllocatorClient) NextExternal(ctx context.Context, wait time.Duration) (*SessionAllocationRequest, bool, error) {
	return c.next(ctx, "/internal/external-session-manager/allocations/next", wait)
}

func (c *SessionAllocatorClient) next(ctx context.Context, path string, wait time.Duration) (*SessionAllocationRequest, bool, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, false, err
	}
	q := u.Query()
	q.Set("wait", wait.String())
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode == http.StatusNoContent {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, false, fmt.Errorf("GET allocation next returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	var allocation SessionAllocationRequest
	if err := json.NewDecoder(resp.Body).Decode(&allocation); err != nil {
		return nil, false, err
	}
	return &allocation, true, nil
}

func (c *SessionAllocatorClient) Complete(ctx context.Context, sessionID string, result SessionAllocationResult) error {
	return c.complete(ctx, "/internal/session-allocations/"+url.PathEscape(sessionID)+"/result", result)
}

func (c *SessionAllocatorClient) CompleteExternal(ctx context.Context, sessionID string, result SessionAllocationResult) error {
	return c.complete(ctx, "/internal/external-session-manager/allocations/"+url.PathEscape(sessionID)+"/result", result)
}

func (c *SessionAllocatorClient) complete(ctx context.Context, path string, result SessionAllocationResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
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

func sleepOrContextDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
