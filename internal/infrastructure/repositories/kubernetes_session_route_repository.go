package repositories

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	SessionRouteSecretPrefix = "agentapi-session-route-"
	SessionRouteSecretKey    = "route.json"
	LabelSessionRoute        = "agentapi.proxy/session-route"
)

type routeJSON struct {
	SessionID       string `json:"session_id"`
	RemoteSessionID string `json:"remote_session_id"`
	ProxyURL        string `json:"proxy_url"`
	HMACSecret      string `json:"hmac_secret"`
}

// KubernetesSessionRouteRepository implements SessionRouteRepository using Kubernetes Secrets
type KubernetesSessionRouteRepository struct {
	client    kubernetes.Interface
	namespace string
}

// NewKubernetesSessionRouteRepository creates a new KubernetesSessionRouteRepository
func NewKubernetesSessionRouteRepository(client kubernetes.Interface, namespace string) *KubernetesSessionRouteRepository {
	return &KubernetesSessionRouteRepository{client: client, namespace: namespace}
}

func (r *KubernetesSessionRouteRepository) secretName(sessionID string) string {
	name := SessionRouteSecretPrefix + sessionID
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

// Save creates or updates a session route secret
func (r *KubernetesSessionRouteRepository) Save(ctx context.Context, route *portrepos.SessionRoute) error {
	data, err := json.Marshal(&routeJSON{
		SessionID:       route.SessionID,
		RemoteSessionID: route.RemoteSessionID,
		ProxyURL:        route.ProxyURL,
		HMACSecret:      route.HMACSecret,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal route: %w", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.secretName(route.SessionID),
			Namespace: r.namespace,
			Labels: map[string]string{
				LabelSessionRoute: "true",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SessionRouteSecretKey: data,
		},
	}

	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, secret, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update session route secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create session route secret: %w", err)
	}
	return nil
}

// Get retrieves routing information for the given session ID; returns nil, nil if not found
func (r *KubernetesSessionRouteRepository) Get(ctx context.Context, sessionID string) (*portrepos.SessionRoute, error) {
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, r.secretName(sessionID), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get session route secret: %w", err)
	}

	raw, ok := secret.Data[SessionRouteSecretKey]
	if !ok {
		return nil, fmt.Errorf("session route secret missing data key")
	}

	var rj routeJSON
	if err := json.Unmarshal(raw, &rj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal route: %w", err)
	}

	return &portrepos.SessionRoute{
		SessionID:       rj.SessionID,
		RemoteSessionID: rj.RemoteSessionID,
		ProxyURL:        rj.ProxyURL,
		HMACSecret:      rj.HMACSecret,
	}, nil
}

// Delete removes the routing information for the given session ID
func (r *KubernetesSessionRouteRepository) Delete(ctx context.Context, sessionID string) error {
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, r.secretName(sessionID), metav1.DeleteOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // Idempotent delete
		}
		return fmt.Errorf("failed to delete session route secret: %w", err)
	}
	return nil
}
