package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	LabelSandboxPolicyType      = "agentapi.proxy/type"
	LabelSandboxPolicyTypeValue = "sandbox-policy"
	LabelSandboxPolicyScope     = "agentapi.proxy/scope"
	LabelSandboxPolicyOwnerHash = "agentapi.proxy/owner-hash"
	LabelSandboxPolicyTeamHash  = "agentapi.proxy/team-hash"

	AnnotationSandboxPolicyOwnerID = "agentapi.proxy/owner-id"
	AnnotationSandboxPolicyTeamID  = "agentapi.proxy/team-id"

	ConfigMapKeySandboxPolicy    = "sandbox-policy.json"
	SandboxPolicyConfigMapPrefix = "agentapi-sandbox-policy-"
)

type sandboxPolicyJSON struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	AllowedDomains []string  `json:"allowed_domains,omitempty"`
	DeniedDomains  []string  `json:"denied_domains,omitempty"`
	CountMode      bool      `json:"count_mode,omitempty"`
	Scope          string    `json:"scope"`
	OwnerID        string    `json:"owner_id"`
	TeamID         string    `json:"team_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// KubernetesSandboxPolicyRepository implements SandboxPolicyRepository using Kubernetes ConfigMaps.
type KubernetesSandboxPolicyRepository struct {
	client    kubernetes.Interface
	namespace string
}

func NewKubernetesSandboxPolicyRepository(client kubernetes.Interface, namespace string) *KubernetesSandboxPolicyRepository {
	return &KubernetesSandboxPolicyRepository{client: client, namespace: namespace}
}

func (r *KubernetesSandboxPolicyRepository) Create(ctx context.Context, policy *entities.SandboxPolicy) error {
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("invalid sandbox policy: %w", err)
	}

	data, err := r.toJSONBytes(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal sandbox policy: %w", err)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        r.configMapName(policy.ID()),
			Namespace:   r.namespace,
			Labels:      r.buildLabels(policy),
			Annotations: r.buildAnnotations(policy),
		},
		Data: map[string]string{ConfigMapKeySandboxPolicy: string(data)},
	}

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		if k8serrors.IsAlreadyExists(err) {
			return fmt.Errorf("sandbox policy already exists: %s", policy.ID())
		}
		return fmt.Errorf("failed to create sandbox policy ConfigMap: %w", err)
	}
	return nil
}

func (r *KubernetesSandboxPolicyRepository) GetByID(ctx context.Context, id string) (*entities.SandboxPolicy, error) {
	cm, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(id), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, entities.ErrSandboxPolicyNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get sandbox policy ConfigMap: %w", err)
	}
	return r.loadFromConfigMap(cm)
}

func (r *KubernetesSandboxPolicyRepository) List(ctx context.Context, filter repositories.SandboxPolicyFilter) ([]*entities.SandboxPolicy, error) {
	labelSelector := r.buildLabelSelector(filter)

	cmList, err := r.client.CoreV1().ConfigMaps(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list sandbox policy ConfigMaps: %w", err)
	}

	teamIDSet := make(map[string]struct{})
	for _, tid := range filter.TeamIDs {
		teamIDSet[tid] = struct{}{}
	}

	var result []*entities.SandboxPolicy
	for i := range cmList.Items {
		policy, err := r.loadFromConfigMap(&cmList.Items[i])
		if err != nil {
			continue
		}
		if len(filter.TeamIDs) > 0 {
			if _, ok := teamIDSet[policy.TeamID()]; !ok {
				continue
			}
		}
		if filter.Name != "" && policy.Name() != filter.Name {
			continue
		}
		result = append(result, policy)
	}

	if result == nil {
		result = []*entities.SandboxPolicy{}
	}
	return result, nil
}

func (r *KubernetesSandboxPolicyRepository) Update(ctx context.Context, policy *entities.SandboxPolicy) error {
	if err := policy.Validate(); err != nil {
		return fmt.Errorf("invalid sandbox policy: %w", err)
	}

	existing, err := r.client.CoreV1().ConfigMaps(r.namespace).Get(ctx, r.configMapName(policy.ID()), metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return entities.ErrSandboxPolicyNotFound{ID: policy.ID()}
		}
		return fmt.Errorf("failed to get sandbox policy ConfigMap for update: %w", err)
	}

	data, err := r.toJSONBytes(policy)
	if err != nil {
		return fmt.Errorf("failed to marshal sandbox policy: %w", err)
	}

	existing.Labels = r.buildLabels(policy)
	existing.Annotations = r.buildAnnotations(policy)
	if existing.Data == nil {
		existing.Data = make(map[string]string)
	}
	existing.Data[ConfigMapKeySandboxPolicy] = string(data)

	_, err = r.client.CoreV1().ConfigMaps(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update sandbox policy ConfigMap: %w", err)
	}
	return nil
}

func (r *KubernetesSandboxPolicyRepository) Delete(ctx context.Context, id string) error {
	err := r.client.CoreV1().ConfigMaps(r.namespace).Delete(ctx, r.configMapName(id), metav1.DeleteOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return entities.ErrSandboxPolicyNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete sandbox policy ConfigMap: %w", err)
	}
	return nil
}

func (r *KubernetesSandboxPolicyRepository) configMapName(id string) string {
	return SandboxPolicyConfigMapPrefix + id
}

func (r *KubernetesSandboxPolicyRepository) buildLabelSelector(filter repositories.SandboxPolicyFilter) string {
	parts := []string{fmt.Sprintf("%s=%s", LabelSandboxPolicyType, LabelSandboxPolicyTypeValue)}

	if len(filter.TeamIDs) == 0 {
		if filter.Scope != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelSandboxPolicyScope, string(filter.Scope)))
		}
		if filter.OwnerID != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelSandboxPolicyOwnerHash, hashID(filter.OwnerID)))
		}
		if filter.TeamID != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", LabelSandboxPolicyTeamHash, hashID(filter.TeamID)))
		}
	} else if filter.Scope != "" {
		parts = append(parts, fmt.Sprintf("%s=%s", LabelSandboxPolicyScope, string(filter.Scope)))
	}

	return strings.Join(parts, ",")
}

func (r *KubernetesSandboxPolicyRepository) buildLabels(p *entities.SandboxPolicy) map[string]string {
	labels := map[string]string{
		LabelSandboxPolicyType:      LabelSandboxPolicyTypeValue,
		LabelSandboxPolicyScope:     string(p.Scope()),
		LabelSandboxPolicyOwnerHash: hashID(p.OwnerID()),
	}
	if p.Scope() == entities.ScopeTeam && p.TeamID() != "" {
		labels[LabelSandboxPolicyTeamHash] = hashID(p.TeamID())
	}
	return labels
}

func (r *KubernetesSandboxPolicyRepository) buildAnnotations(p *entities.SandboxPolicy) map[string]string {
	annotations := map[string]string{
		AnnotationSandboxPolicyOwnerID: p.OwnerID(),
	}
	if p.TeamID() != "" {
		annotations[AnnotationSandboxPolicyTeamID] = p.TeamID()
	}
	return annotations
}

func (r *KubernetesSandboxPolicyRepository) toJSONBytes(p *entities.SandboxPolicy) ([]byte, error) {
	pj := &sandboxPolicyJSON{
		ID:             p.ID(),
		Name:           p.Name(),
		Description:    p.Description(),
		AllowedDomains: p.AllowedDomains(),
		DeniedDomains:  p.DeniedDomains(),
		CountMode:      p.CountMode(),
		Scope:          string(p.Scope()),
		OwnerID:        p.OwnerID(),
		TeamID:         p.TeamID(),
		CreatedAt:      p.CreatedAt(),
		UpdatedAt:      p.UpdatedAt(),
	}
	return json.Marshal(pj)
}

func (r *KubernetesSandboxPolicyRepository) loadFromConfigMap(cm *corev1.ConfigMap) (*entities.SandboxPolicy, error) {
	rawJSON, ok := cm.Data[ConfigMapKeySandboxPolicy]
	if !ok {
		return nil, fmt.Errorf("ConfigMap %s is missing sandbox-policy.json data", cm.Name)
	}

	var pj sandboxPolicyJSON
	if err := json.Unmarshal([]byte(rawJSON), &pj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal sandbox policy JSON from ConfigMap %s: %w", cm.Name, err)
	}

	if ownerID, ok := cm.Annotations[AnnotationSandboxPolicyOwnerID]; ok && ownerID != "" {
		pj.OwnerID = ownerID
	}
	if teamID, ok := cm.Annotations[AnnotationSandboxPolicyTeamID]; ok && teamID != "" {
		pj.TeamID = teamID
	}

	p := entities.NewSandboxPolicy(
		pj.ID,
		pj.Name,
		pj.Description,
		pj.AllowedDomains,
		pj.DeniedDomains,
		entities.ResourceScope(pj.Scope),
		pj.OwnerID,
		pj.TeamID,
	)
	p.SetCountMode(pj.CountMode)
	p.SetCreatedAt(pj.CreatedAt)
	p.SetUpdatedAt(pj.UpdatedAt)

	return p, nil
}
