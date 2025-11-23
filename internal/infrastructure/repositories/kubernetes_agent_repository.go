package repositories

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	agentConfigMapPrefix = "agent-"
	agentNamespace       = "agentapi-proxy"
)

type KubernetesAgentRepository struct {
	client client.Client
}

func NewKubernetesAgentRepository(client client.Client) repositories.AgentRepository {
	return &KubernetesAgentRepository{
		client: client,
	}
}

func (r *KubernetesAgentRepository) Save(ctx context.Context, agent *entities.Agent) error {
	data, err := r.agentToJSON(agent)
	if err != nil {
		return fmt.Errorf("failed to marshal agent: %w", err)
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.getConfigMapName(agent.ID),
			Namespace: agentNamespace,
			Labels: map[string]string{
				"type":       "agent",
				"session-id": string(agent.SessionID),
				"agent-id":   string(agent.ID),
			},
		},
		Data: map[string]string{
			"agent.json": data,
		},
	}

	if err := r.client.Create(ctx, configMap); err != nil {
		return fmt.Errorf("failed to create configmap: %w", err)
	}

	return nil
}

func (r *KubernetesAgentRepository) Update(ctx context.Context, agent *entities.Agent) error {
	data, err := r.agentToJSON(agent)
	if err != nil {
		return fmt.Errorf("failed to marshal agent: %w", err)
	}

	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{
		Namespace: agentNamespace,
		Name:      r.getConfigMapName(agent.ID),
	}

	if err := r.client.Get(ctx, key, configMap); err != nil {
		return fmt.Errorf("failed to get configmap: %w", err)
	}

	configMap.Data["agent.json"] = data
	if err := r.client.Update(ctx, configMap); err != nil {
		return fmt.Errorf("failed to update configmap: %w", err)
	}

	return nil
}

func (r *KubernetesAgentRepository) FindByID(ctx context.Context, id entities.AgentID) (*entities.Agent, error) {
	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{
		Namespace: agentNamespace,
		Name:      r.getConfigMapName(id),
	}

	if err := r.client.Get(ctx, key, configMap); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf("failed to get configmap: %w", err)
		}
		return nil, fmt.Errorf("agent not found")
	}

	data, ok := configMap.Data["agent.json"]
	if !ok {
		return nil, fmt.Errorf("agent data not found in configmap")
	}

	return r.jsonToAgent(data)
}

func (r *KubernetesAgentRepository) FindBySessionID(ctx context.Context, sessionID entities.SessionID) ([]*entities.Agent, error) {
	configMapList := &corev1.ConfigMapList{}
	if err := r.client.List(ctx, configMapList,
		client.InNamespace(agentNamespace),
		client.MatchingLabels{
			"type":       "agent",
			"session-id": string(sessionID),
		},
	); err != nil {
		return nil, fmt.Errorf("failed to list configmaps: %w", err)
	}

	agents := make([]*entities.Agent, 0, len(configMapList.Items))
	for _, cm := range configMapList.Items {
		data, ok := cm.Data["agent.json"]
		if !ok {
			continue
		}

		agent, err := r.jsonToAgent(data)
		if err != nil {
			continue
		}

		agents = append(agents, agent)
	}

	return agents, nil
}

func (r *KubernetesAgentRepository) FindAll(ctx context.Context) ([]*entities.Agent, error) {
	configMapList := &corev1.ConfigMapList{}
	if err := r.client.List(ctx, configMapList,
		client.InNamespace(agentNamespace),
		client.MatchingLabels{"type": "agent"},
	); err != nil {
		return nil, fmt.Errorf("failed to list configmaps: %w", err)
	}

	agents := make([]*entities.Agent, 0, len(configMapList.Items))
	for _, cm := range configMapList.Items {
		data, ok := cm.Data["agent.json"]
		if !ok {
			continue
		}

		agent, err := r.jsonToAgent(data)
		if err != nil {
			continue
		}

		agents = append(agents, agent)
	}

	return agents, nil
}

func (r *KubernetesAgentRepository) Delete(ctx context.Context, id entities.AgentID) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.getConfigMapName(id),
			Namespace: agentNamespace,
		},
	}

	if err := r.client.Delete(ctx, configMap); err != nil && client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete configmap: %w", err)
	}
	return nil
}

func (r *KubernetesAgentRepository) DeleteBySessionID(ctx context.Context, sessionID entities.SessionID) error {
	configMapList := &corev1.ConfigMapList{}
	if err := r.client.List(ctx, configMapList,
		client.InNamespace(agentNamespace),
		client.MatchingLabels{
			"type":       "agent",
			"session-id": string(sessionID),
		},
	); err != nil {
		return fmt.Errorf("failed to list configmaps: %w", err)
	}

	for _, cm := range configMapList.Items {
		if err := r.client.Delete(ctx, &cm); err != nil && client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete configmap: %w", err)
		}
	}

	return nil
}

func (r *KubernetesAgentRepository) getConfigMapName(id entities.AgentID) string {
	return fmt.Sprintf("%s%s", agentConfigMapPrefix, string(id))
}

func (r *KubernetesAgentRepository) agentToJSON(agent *entities.Agent) (string, error) {
	data, err := json.Marshal(agent)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *KubernetesAgentRepository) jsonToAgent(data string) (*entities.Agent, error) {
	var agent entities.Agent
	if err := json.Unmarshal([]byte(data), &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}
