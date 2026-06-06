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
	provisionerJobType         = "provision"
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

// ProvisionerStatusRequest is sent by a session Pod as provisioning progresses.
type ProvisionerStatusRequest struct {
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	PodName   string `json:"pod_name,omitempty"`
}

// ProvisionerJob is the shared job document persisted in a Kubernetes Secret.
type ProvisionerJob struct {
	JobID     string                           `json:"job_id"`
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

func provisionerJobSecretName(sessionID string) string {
	return fmt.Sprintf("agentapi-provision-job-%s", sessionID)
}

func (m *KubernetesSessionManager) CreateProvisionerJob(ctx context.Context, session *KubernetesSession) error {
	settings := session.ProvisionSettings()
	if settings == nil {
		return fmt.Errorf("session %s has no provision settings", session.id)
	}
	job := &ProvisionerJob{
		JobID:     fmt.Sprintf("%s-provision-1", session.id),
		SessionID: session.id,
		Type:      provisionerJobType,
		Settings:  settings,
		Status:    "pending",
		UpdatedAt: time.Now().UTC(),
	}
	return m.saveProvisionerJob(ctx, job)
}

func (m *KubernetesSessionManager) ConnectProvisioner(ctx context.Context, req ProvisionerConnectRequest) error {
	job, err := m.getProvisionerJob(ctx, req.SessionID)
	if err != nil {
		return err
	}
	job.ClaimedBy = req.PodName
	job.UpdatedAt = time.Now().UTC()
	return m.saveProvisionerJob(ctx, job)
}

func (m *KubernetesSessionManager) ClaimProvisionerJob(ctx context.Context, sessionID, podName string) (*ProvisionerJob, bool, error) {
	job, err := m.getProvisionerJob(ctx, sessionID)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if job.Status == "ready" {
		return nil, false, nil
	}
	job.Status = "claimed"
	job.ClaimedBy = podName
	job.UpdatedAt = time.Now().UTC()
	if err := m.saveProvisionerJob(ctx, job); err != nil {
		return nil, false, err
	}
	return job, true, nil
}

func (m *KubernetesSessionManager) UpdateProvisionerJobStatus(ctx context.Context, sessionID, jobID string, req ProvisionerStatusRequest) error {
	job, err := m.getProvisionerJob(ctx, sessionID)
	if err != nil {
		return err
	}
	if job.JobID != jobID {
		return fmt.Errorf("job id mismatch: got %s, want %s", jobID, job.JobID)
	}
	job.Status = req.Status
	job.Message = req.Message
	if req.PodName != "" {
		job.ClaimedBy = req.PodName
	}
	job.UpdatedAt = time.Now().UTC()
	if err := m.saveProvisionerJob(ctx, job); err != nil {
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
			job, err := m.getProvisionerJob(ctx, session.id)
			if err != nil {
				log.Printf("[K8S_SESSION] Failed to read provisioner job for session %s: %v", session.id, err)
				continue
			}
			switch job.Status {
			case "ready":
				return nil
			case "error":
				return fmt.Errorf("provisioner reported error: %s", job.Message)
			default:
				log.Printf("[K8S_SESSION] Pull provisioner status for session %s: %s", session.id, job.Status)
			}
		}
	}
}

func (m *KubernetesSessionManager) getProvisionerJob(ctx context.Context, sessionID string) (*ProvisionerJob, error) {
	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, provisionerJobSecretName(sessionID), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	data := sec.Data["job.json"]
	if len(data) == 0 {
		return nil, fmt.Errorf("provisioner job secret %s has no job.json", sec.Name)
	}
	var job ProvisionerJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, fmt.Errorf("decode provisioner job: %w", err)
	}
	return &job, nil
}

func (m *KubernetesSessionManager) saveProvisionerJob(ctx context.Context, job *ProvisionerJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("encode provisioner job: %w", err)
	}
	name := provisionerJobSecretName(job.SessionID)
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/session-id":    job.SessionID,
		"agentapi.proxy/provision-job": "true",
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
			Data: map[string][]byte{"job.json": data},
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
	sec.Data["job.json"] = data
	_, err = m.client.CoreV1().Secrets(m.namespace).Update(ctx, sec, metav1.UpdateOptions{})
	return err
}

func (m *KubernetesSessionManager) deleteProvisionerJob(ctx context.Context, sessionID string) error {
	err := m.client.CoreV1().Secrets(m.namespace).Delete(ctx, provisionerJobSecretName(sessionID), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
