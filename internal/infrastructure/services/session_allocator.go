package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	sessionAllocationSecretPrefix = "agentapi-session-allocation-"
	sessionAllocationDataKey      = "request.json"
)

// SessionAllocationRequest is the cluster-visible request consumed by the
// leader-elected SessionAllocator.
type SessionAllocationRequest struct {
	SessionID          string                     `json:"session_id"`
	Request            *entities.RunServerRequest `json:"request"`
	WebhookPayload     []byte                     `json:"webhook_payload,omitempty"`
	Status             string                     `json:"status"`
	Message            string                     `json:"message,omitempty"`
	AllocatedSessionID string                     `json:"allocated_session_id,omitempty"`
	Requirements       SessionRequirements        `json:"requirements"`
	UpdatedAt          time.Time                  `json:"updated_at"`
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

	timeout := time.Duration(m.k8sConfig.PodStartTimeout) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = m.deleteSessionAllocation(context.Background(), id)
			return nil, ctx.Err()
		case <-deadline.C:
			_ = m.deleteSessionAllocation(context.Background(), id)
			return nil, fmt.Errorf("session allocation timed out for %s", id)
		case <-ticker.C:
			current, err := m.getSessionAllocation(ctx, id)
			if err != nil {
				_ = m.deleteSessionAllocation(context.Background(), id)
				return nil, fmt.Errorf("failed to read session allocation: %w", err)
			}
			switch current.Status {
			case "assigned":
				allocatedID := current.AllocatedSessionID
				if allocatedID == "" {
					allocatedID = id
				}
				if sess := m.GetSession(allocatedID); sess != nil {
					_ = m.deleteSessionAllocation(context.Background(), id)
					return sess, nil
				}
			case "error":
				_ = m.deleteSessionAllocation(context.Background(), id)
				return nil, fmt.Errorf("session allocation failed: %s", current.Message)
			default:
				log.Printf("[SESSION_ALLOCATOR] Waiting for allocation request %s status=%s", id, current.Status)
			}
		}
	}
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
	return err
}

func (m *KubernetesSessionManager) deleteSessionAllocation(ctx context.Context, sessionID string) error {
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, sessionAllocationSecretName(sessionID), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// SessionAllocator is a leader-only worker that binds a session allocation
// request to either matching stock capacity or a newly created session Pod.
type SessionAllocator struct {
	manager *KubernetesSessionManager

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

func NewSessionAllocator(manager *KubernetesSessionManager) *SessionAllocator {
	return &SessionAllocator{manager: manager, stopCh: make(chan struct{})}
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
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.processPending(ctx)
		}
	}
}

func (a *SessionAllocator) processPending(ctx context.Context) {
	svcs, err := a.manager.client.CoreV1().Secrets(a.manager.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "agentapi.proxy/session-allocation=true,agentapi.proxy/session-allocation-status in (pending,allocating)",
	})
	if err != nil {
		log.Printf("[SESSION_ALLOCATOR] Failed to list pending allocations: %v", err)
		return
	}
	for i := range svcs.Items {
		sec := &svcs.Items[i]
		req, err := a.manager.getSessionAllocation(ctx, sec.Labels["agentapi.proxy/session-id"])
		if err != nil {
			log.Printf("[SESSION_ALLOCATOR] Failed to read allocation %s: %v", sec.Name, err)
			continue
		}
		a.processOne(ctx, req)
	}
}

func (a *SessionAllocator) processOne(ctx context.Context, req *SessionAllocationRequest) {
	req.Status = "allocating"
	if err := a.manager.saveSessionAllocation(ctx, req); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Failed to claim allocation %s: %v", req.SessionID, err)
		return
	}

	log.Printf("[SESSION_ALLOCATOR] Allocating session %s (sandbox=%t dind=%t agent_type=%s)",
		req.SessionID, req.Requirements.Sandbox, req.Requirements.DinD, req.Requirements.AgentType)
	sess, err := a.manager.allocateSessionDirect(ctx, req.SessionID, req.Request, req.WebhookPayload)
	if err != nil {
		req.Status = "error"
		req.Message = err.Error()
		_ = a.manager.saveSessionAllocation(context.Background(), req)
		log.Printf("[SESSION_ALLOCATOR] Allocation failed for %s: %v", req.SessionID, err)
		return
	}
	req.Status = "assigned"
	req.AllocatedSessionID = sess.ID()
	req.Message = ""
	if err := a.manager.saveSessionAllocation(context.Background(), req); err != nil {
		log.Printf("[SESSION_ALLOCATOR] Failed to mark allocation %s assigned: %v", req.SessionID, err)
		return
	}
	log.Printf("[SESSION_ALLOCATOR] Allocated session %s as %s", req.SessionID, sess.ID())
}
