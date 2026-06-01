package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	LabelSandboxDomainType      = "agentapi.proxy/type"
	LabelSandboxDomainTypeValue = "sandbox-domains"

	sandboxDomainConfigMapPrefix = "agentapi-sandbox-domains-"
	sandboxDomainDataKey         = "data.json"
)

// SandboxDomainData holds the aggregated domain log for a sandbox policy.
type SandboxDomainData struct {
	Allowed   []string  `json:"allowed"`
	Denied    []string  `json:"denied"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KubernetesSandboxDomainRepository persists sandbox domain logs in Kubernetes ConfigMaps,
// one ConfigMap per sandbox policy.
type KubernetesSandboxDomainRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesSandboxDomainRepository(client kubernetes.Interface, namespace string) *KubernetesSandboxDomainRepository {
	return &KubernetesSandboxDomainRepository{client: client, namespace: namespace}
}

// Get returns the domain data for the given policy ID.
// Returns nil (no error) when no data has been collected yet.
func (r *KubernetesSandboxDomainRepository) Get(ctx context.Context, policyID string) (*SandboxDomainData, error) {
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(policyID), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get sandbox domain configmap: %w", err)
	}

	raw, ok := cm.Data[sandboxDomainDataKey]
	if !ok {
		return nil, nil
	}

	var data SandboxDomainData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("unmarshal sandbox domain data: %w", err)
	}
	return &data, nil
}

// Upsert creates or updates the domain data ConfigMap for the given policy.
func (r *KubernetesSandboxDomainRepository) Upsert(ctx context.Context, policyID string, data *SandboxDomainData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal sandbox domain data: %w", err)
	}

	name := r.configMapName(policyID)
	existing, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return fmt.Errorf("get sandbox domain configmap: %w", err)
		}
		// Create new ConfigMap
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: r.namespace,
				Labels: map[string]string{
					LabelSandboxDomainType: LabelSandboxDomainTypeValue,
					"agentapi.proxy/policy-id": policyID,
				},
			},
			Data: map[string]string{sandboxDomainDataKey: string(raw)},
		}
		_, err = r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
		return err
	}

	// Update existing ConfigMap
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	existing.Data[sandboxDomainDataKey] = string(raw)
	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	return err
}

func (r *KubernetesSandboxDomainRepository) configMapName(policyID string) string {
	return sandboxDomainConfigMapPrefix + policyID
}
