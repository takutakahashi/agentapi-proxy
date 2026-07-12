package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	githubBrokerSecretName = "agentapi-github-broker-secret"
	githubBrokerSecretKey  = "broker-secret"
)

// ensureGitHubBrokerSecret loads or generates the proxy-wide HMAC secret used to
// derive per-session GitHub token broker credentials. The secret is persisted in a
// Kubernetes Secret so it survives proxy restarts; without it, sessions created
// before a restart could no longer authenticate to the broker.
func (m *KubernetesSessionManager) ensureGitHubBrokerSecret(ctx context.Context) error {
	if m.githubBrokerSecret != "" {
		return nil
	}
	secret, err := m.loadGitHubBrokerSecret(ctx)
	if err == nil {
		m.githubBrokerSecret = secret
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	secret, err = generateGitHubBrokerSecret()
	if err != nil {
		return err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      githubBrokerSecretName,
			Namespace: m.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "agentapi-proxy",
				"agentapi.proxy/github-broker": "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			githubBrokerSecretKey: []byte(secret),
		},
	}
	if _, err := m.client.CoreV1().Secrets(m.namespace).Create(ctx, sec, metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			secret, err = m.loadGitHubBrokerSecret(ctx)
			if err != nil {
				return err
			}
			m.githubBrokerSecret = secret
			return nil
		}
		return err
	}
	m.githubBrokerSecret = secret
	log.Printf("[K8S_SESSION] Generated GitHub broker secret %s/%s", m.namespace, githubBrokerSecretName)
	return nil
}

func (m *KubernetesSessionManager) loadGitHubBrokerSecret(ctx context.Context) (string, error) {
	sec, err := m.client.CoreV1().Secrets(m.namespace).Get(ctx, githubBrokerSecretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	secret := string(sec.Data[githubBrokerSecretKey])
	if secret == "" {
		return "", fmt.Errorf("github broker secret %s/%s has no %q key", m.namespace, githubBrokerSecretName, githubBrokerSecretKey)
	}
	return secret, nil
}

func generateGitHubBrokerSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate github broker secret: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// githubBrokerTokenForSession derives the session-scoped broker credential from
// the proxy-wide HMAC secret and the session ID. Each session gets a distinct,
// unpredictable token bound to its ID; the token is only valid for this session's
// broker endpoint and cannot be used to call other proxy APIs.
func (m *KubernetesSessionManager) githubBrokerTokenForSession(sessionID string) string {
	if m.githubBrokerSecret == "" || sessionID == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(m.githubBrokerSecret))
	mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateGitHubBrokerToken reports whether the presented token is the valid
// broker credential for the given session ID. Comparison is constant-time. A
// token minted for session A will not validate for session B, and the broker
// credential is unrelated to the provisioner token so it cannot be reused against
// other proxy endpoints.
func (m *KubernetesSessionManager) ValidateGitHubBrokerToken(sessionID, token string) bool {
	if m.githubBrokerSecret == "" || sessionID == "" || token == "" {
		return false
	}
	expected := m.githubBrokerTokenForSession(sessionID)
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1
}
