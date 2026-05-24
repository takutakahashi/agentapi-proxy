package repositories

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

const (
	oauthStateConfigMapPrefix = "agentapi-oauth-state-"
	oauthStateLabelType       = "agentapi.proxy/oauth-state"
)

// KubernetesOAuthStateRepository implements auth.OAuthStateStore.
// Each OAuth state is stored in its own ConfigMap named
// "agentapi-oauth-state-{state}". One ConfigMap per state means no write
// conflicts between pods — each Create/Delete targets a unique resource.
type KubernetesOAuthStateRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesOAuthStateRepository(client kubernetes.Interface, namespace string) *KubernetesOAuthStateRepository {
	return &KubernetesOAuthStateRepository{client: client, namespace: namespace}
}

func (r *KubernetesOAuthStateRepository) cmName(state string) string {
	// SHA256 hex digest: always 64 lowercase hex chars regardless of input length.
	// Total name = len("agentapi-oauth-state-") + 64 = 85 chars < 253 (K8s limit).
	// Characters are [0-9a-f] only, satisfying the DNS subdomain naming rules.
	h := sha256.Sum256([]byte(state))
	return oauthStateConfigMapPrefix + hex.EncodeToString(h[:])
}

func (r *KubernetesOAuthStateRepository) Store(ctx context.Context, state string, entry *auth.OAuthState) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal oauth state: %w", err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.cmName(state),
			Namespace: r.namespace,
			Labels: map[string]string{
				oauthStateLabelType: "true",
			},
		},
		Data: map[string]string{"state": string(data)},
	}
	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if k8serrors.IsAlreadyExists(err) {
		// Idempotent: state already stored (e.g. duplicate request)
		return nil
	}
	return err
}

func (r *KubernetesOAuthStateRepository) Load(ctx context.Context, state string) (*auth.OAuthState, bool, error) {
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.cmName(state), metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get oauth state configmap: %w", err)
	}
	var entry auth.OAuthState
	if err := json.Unmarshal([]byte(cm.Data["state"]), &entry); err != nil {
		return nil, false, fmt.Errorf("unmarshal oauth state: %w", err)
	}
	return &entry, true, nil
}

func (r *KubernetesOAuthStateRepository) Delete(ctx context.Context, state string) error {
	err := r.client.CoreV1().ConfigMaps(r.namespace).Delete(ctx, r.cmName(state), metav1.DeleteOptions{})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *KubernetesOAuthStateRepository) Range(ctx context.Context, fn func(string, *auth.OAuthState) bool) error {
	list, err := r.client.CoreV1().ConfigMaps(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: oauthStateLabelType + "=true",
	})
	if err != nil {
		return fmt.Errorf("list oauth state configmaps: %w", err)
	}
	for i := range list.Items {
		cm := &list.Items[i]
		raw, ok := cm.Data["state"]
		if !ok {
			continue
		}
		var entry auth.OAuthState
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			log.Printf("[OAUTH_STATE] skip malformed configmap %q: %v", cm.Name, err)
			continue
		}
		if !fn(entry.State, &entry) {
			break
		}
	}
	return nil
}
