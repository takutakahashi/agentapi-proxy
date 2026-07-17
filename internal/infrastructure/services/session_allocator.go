package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	sessionallocation "github.com/takutakahashi/agentapi-proxy/internal/core/sessionallocation"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	sessionAllocationSecretPrefix = "agentapi-session-allocation-"
	sessionAllocationDataKey      = "request.json"
)

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
	allocation := &sessionallocation.AllocationRequest{
		SessionID:      id,
		Request:        req,
		WebhookPayload: webhookPayload,
		Status:         sessionallocation.StatusPending,
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

func (m *KubernetesSessionManager) SetSessionAllocationNotifier(notifier sessionallocation.Notifier) {
	if notifier == nil {
		return
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.sessionAllocationNotifier = notifier
}

func sessionRequirements(req *entities.RunServerRequest) sessionallocation.Requirements {
	return sessionallocation.RequirementsFromRunServerRequest(req)
}

func sessionAllocationSecretName(sessionID string) string {
	return sessionAllocationSecretPrefix + sessionID
}

func (m *KubernetesSessionManager) getSessionAllocation(ctx context.Context, sessionID string) (*sessionallocation.AllocationRequest, error) {
	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, sessionAllocationSecretName(sessionID), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	data := sec.Data[sessionAllocationDataKey]
	if len(data) == 0 {
		return nil, fmt.Errorf("session allocation Secret %s has no %s", sec.Name, sessionAllocationDataKey)
	}
	var req sessionallocation.AllocationRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("decode session allocation: %w", err)
	}
	return &req, nil
}

func (m *KubernetesSessionManager) saveSessionAllocation(ctx context.Context, req *sessionallocation.AllocationRequest) error {
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
		"agentapi.proxy/session-allocation-status": string(req.Status),
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

func (m *KubernetesSessionManager) NextSessionAllocation(ctx context.Context, wait time.Duration) (*sessionallocation.AllocationRequest, bool, error) {
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
		req, ok, err = m.claimNextSessionAllocation(ctx)
		if err != nil || ok {
			cancel()
			return req, ok, err
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

func (m *KubernetesSessionManager) claimNextSessionAllocation(ctx context.Context) (*sessionallocation.AllocationRequest, bool, error) {
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
		req.Status = sessionallocation.StatusAllocating
		if err := m.saveSessionAllocation(ctx, req); err != nil {
			log.Printf("[SESSION_ALLOCATOR] Failed to claim allocation %s: %v", req.SessionID, err)
			continue
		}
		return req, true, nil
	}
	return nil, false, nil
}

func (m *KubernetesSessionManager) SubmitExternalSessionAllocation(ctx context.Context, managerID, sessionID string, settings *sessionsettings.SessionSettings, req *entities.RunServerRequest) error {
	allocation := &sessionallocation.AllocationRequest{
		SessionID:         sessionID,
		ManagerID:         managerID,
		Request:           req,
		ProvisionSettings: settings,
		Status:            sessionallocation.StatusPending,
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

func (m *KubernetesSessionManager) NextExternalSessionAllocation(ctx context.Context, managerID string, wait time.Duration) (*sessionallocation.AllocationRequest, bool, error) {
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
		req, ok, err = m.claimNextExternalSessionAllocation(ctx, managerID)
		if err != nil || ok {
			cancel()
			return req, ok, err
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

func (m *KubernetesSessionManager) claimNextExternalSessionAllocation(ctx context.Context, managerID string) (*sessionallocation.AllocationRequest, bool, error) {
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
		req.Status = sessionallocation.StatusAllocating
		if err := m.saveSessionAllocation(ctx, req); err != nil {
			log.Printf("[EXTERNAL_SESSION_ALLOCATOR] Failed to claim allocation %s: %v", req.SessionID, err)
			continue
		}
		return req, true, nil
	}
	return nil, false, nil
}

func (m *KubernetesSessionManager) CompleteExternalSessionAllocation(ctx context.Context, sessionID string, result sessionallocation.AllocationResult) (*sessionallocation.AllocationRequest, error) {
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

func (m *KubernetesSessionManager) CompleteSessionAllocation(ctx context.Context, sessionID string, result sessionallocation.AllocationResult) (*sessionallocation.AllocationRequest, error) {
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
		log.Printf("[SESSION_ALLOCATOR] Warning: failed to delete completed allocation %s: %v", sessionID, err)
	}
	return req, nil
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

func (m *KubernetesSessionManager) allocationProxyURL() string {
	proxyURL := strings.TrimRight(m.k8sConfig.ProvisionerProxyURL, "/")
	if proxyURL != "" {
		return proxyURL
	}
	return fmt.Sprintf("http://agentapi-proxy.%s.svc.cluster.local:8080", m.namespace)
}

func (m *KubernetesSessionManager) AllocationProxyURL() string {
	return m.allocationProxyURL()
}
