package services

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const (
	provisionRequestType       = "provision"
	provisionerTokenSecretName = "agentapi-provisioner-token"
	provisionerTokenSecretKey  = "token"
)

// ProvisionerConnectRequest is sent by a session Pod when agent-provisioner starts.
type ProvisionerConnectRequest struct {
	SessionID          string   `json:"session_id"`
	PodName            string   `json:"pod_name"`
	Namespace          string   `json:"namespace"`
	ProvisionerVersion string   `json:"provisioner_version,omitempty"`
	Capabilities       []string `json:"capabilities,omitempty"`
}

// ProvisionRequestStatusUpdate is sent by a session Pod as provisioning progresses.
type ProvisionRequestStatusUpdate struct {
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	PodName   string `json:"pod_name,omitempty"`
}

// ProvisionRequest is the shared provisioning request document persisted in a Kubernetes Secret.
type ProvisionRequest struct {
	RequestID string                           `json:"request_id"`
	SessionID string                           `json:"session_id"`
	Type      string                           `json:"type"`
	Settings  *sessionsettings.SessionSettings `json:"settings,omitempty"`
	Status    string                           `json:"status"`
	Message   string                           `json:"message,omitempty"`
	ClaimedBy string                           `json:"claimed_by,omitempty"`
	UpdatedAt time.Time                        `json:"updated_at"`
}

func (m *KubernetesSessionManager) ValidateProvisionerToken(token string) bool {
	return m.k8sConfig != nil && m.k8sConfig.ProvisionerToken != "" && token == m.k8sConfig.ProvisionerToken
}

func (m *KubernetesSessionManager) ensureProvisionerToken(ctx context.Context) error {
	if m.k8sConfig.ProvisionerToken != "" {
		return nil
	}
	token, err := m.loadProvisionerToken(ctx)
	if err == nil {
		m.k8sConfig.ProvisionerToken = token
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	token, err = generateProvisionerToken()
	if err != nil {
		return err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      provisionerTokenSecretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":     "agentapi-proxy",
				"agentapi.proxy/provisioner-token": "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			provisionerTokenSecretKey: []byte(token),
		},
	}
	if _, err := m.client.CoreV1().Secrets(m.namespace).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			token, err = m.loadProvisionerToken(ctx)
			if err != nil {
				return err
			}
			m.k8sConfig.ProvisionerToken = token
			return nil
		}
		return err
	}
	m.k8sConfig.ProvisionerToken = token
	log.Printf("[K8S_SESSION] Generated local provisioner token Secret %s/%s", m.namespace, provisionerTokenSecretName)
	return nil
}

func (m *KubernetesSessionManager) loadProvisionerToken(ctx context.Context) (string, error) {
	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, provisionerTokenSecretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	token := string(sec.Data[provisionerTokenSecretKey])
	if token == "" {
		return "", fmt.Errorf("provisioner token Secret %s/%s has no %q key", m.namespace, provisionerTokenSecretName, provisionerTokenSecretKey)
	}
	return token, nil
}

func generateProvisionerToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate provisioner token: %w", err)
	}
	return fmt.Sprintf("%x", b[:]), nil
}

func provisionRequestSecretName(sessionID string) string {
	return fmt.Sprintf("agentapi-provision-request-%s", sessionID)
}

func (m *KubernetesSessionManager) CreateProvisionRequest(ctx context.Context, session *KubernetesSession) error {
	settings := session.ProvisionSettings()
	if settings == nil {
		return fmt.Errorf("session %s has no provision settings", session.id)
	}
	req := &ProvisionRequest{
		RequestID: fmt.Sprintf("%s-provision-1", session.id),
		SessionID: session.id,
		Type:      provisionRequestType,
		Settings:  settings,
		Status:    "pending",
		UpdatedAt: time.Now().UTC(),
	}
	return m.saveProvisionRequest(ctx, req)
}

func (m *KubernetesSessionManager) ConnectProvisioner(ctx context.Context, req ProvisionerConnectRequest) error {
	provisionReq, err := m.getProvisionRequest(ctx, req.SessionID)
	if err != nil {
		return err
	}
	provisionReq.ClaimedBy = req.PodName
	provisionReq.UpdatedAt = time.Now().UTC()
	return m.saveProvisionRequest(ctx, provisionReq)
}

func (m *KubernetesSessionManager) ClaimProvisionRequest(ctx context.Context, sessionID, podName string) (*ProvisionRequest, bool, error) {
	provisionReq, err := m.getProvisionRequest(ctx, sessionID)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if provisionReq.Status == "ready" {
		return nil, false, nil
	}
	provisionReq.Status = "claimed"
	provisionReq.ClaimedBy = podName
	provisionReq.UpdatedAt = time.Now().UTC()
	if err := m.saveProvisionRequest(ctx, provisionReq); err != nil {
		return nil, false, err
	}
	return provisionReq, true, nil
}

func (m *KubernetesSessionManager) UpdateProvisionRequestStatus(ctx context.Context, sessionID, requestID string, req ProvisionRequestStatusUpdate) error {
	provisionReq, err := m.getProvisionRequest(ctx, sessionID)
	if err != nil {
		return err
	}
	if provisionReq.RequestID != requestID {
		return fmt.Errorf("provision request id mismatch: got %s, want %s", requestID, provisionReq.RequestID)
	}
	provisionReq.Status = req.Status
	provisionReq.Message = req.Message
	if req.PodName != "" {
		provisionReq.ClaimedBy = req.PodName
	}
	provisionReq.UpdatedAt = time.Now().UTC()
	if err := m.saveProvisionRequest(ctx, provisionReq); err != nil {
		return err
	}

	if sess := m.GetSession(sessionID); sess != nil {
		if ks, ok := sess.(*KubernetesSession); ok {
			switch req.Status {
			case "provisioning":
				ks.SetStatus("starting")
			case "ready":
				if ks.Status() != "running" {
					ks.SetStatus("active")
				}
			case "error":
				ks.SetStatus("error")
			}
		}
	}
	return nil
}

func (m *KubernetesSessionManager) waitForPullProvisioner(ctx context.Context, session *KubernetesSession) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for pull provisioner")
		case <-ticker.C:
			provisionReq, err := m.getProvisionRequest(ctx, session.id)
			if err != nil {
				log.Printf("[K8S_SESSION] Failed to read provision request for session %s: %v", session.id, err)
				continue
			}
			switch provisionReq.Status {
			case "ready":
				return nil
			case "error":
				return fmt.Errorf("provisioner reported error: %s", provisionReq.Message)
			default:
				log.Printf("[K8S_SESSION] Pull provisioner status for session %s: %s", session.id, provisionReq.Status)
			}
		}
	}
}

func (m *KubernetesSessionManager) getProvisionRequest(ctx context.Context, sessionID string) (*ProvisionRequest, error) {
	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, provisionRequestSecretName(sessionID), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	data := sec.Data["request.json"]
	if len(data) == 0 {
		return nil, fmt.Errorf("provision request secret %s has no request.json", sec.Name)
	}
	var req ProvisionRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("decode provision request: %w", err)
	}
	return &req, nil
}

func (m *KubernetesSessionManager) saveProvisionRequest(ctx context.Context, req *ProvisionRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode provision request: %w", err)
	}
	name := provisionRequestSecretName(req.SessionID)
	labels := map[string]string{
		"app.kubernetes.io/managed-by":     "agentapi-proxy",
		"agentapi.proxy/session-id":        req.SessionID,
		"agentapi.proxy/provision-request": "true",
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
			Data: map[string][]byte{"request.json": data},
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
	sec.Data["request.json"] = data
	_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, sec, metav1.UpdateOptions{})
	return err
}

func (m *KubernetesSessionManager) deleteProvisionRequest(ctx context.Context, sessionID string) error {
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, provisionRequestSecretName(sessionID), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
