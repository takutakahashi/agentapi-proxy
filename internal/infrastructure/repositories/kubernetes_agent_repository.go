package repositories

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	agentConfigMapPrefix = "agent-"
	agentNamespace       = "agentapi-proxy"
)

type KubernetesAgentRepository struct {
	client kubernetes.Interface
}

func NewKubernetesAgentRepository(client kubernetes.Interface) repositories.AgentRepository {
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

	_, err = r.client.CoreV1().ConfigMaps(agentNamespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create configmap: %w", err)
	}

	return nil
}

func (r *KubernetesAgentRepository) Update(ctx context.Context, agent *entities.Agent) error {
	data, err := r.agentToJSON(agent)
	if err != nil {
		return fmt.Errorf("failed to marshal agent: %w", err)
	}

	cm, err := r.client.CoreV1().ConfigMaps(agentNamespace).Get(ctx, r.getConfigMapName(agent.ID), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get configmap: %w", err)
	}

	cm.Data["agent.json"] = data
	_, err = r.client.CoreV1().ConfigMaps(agentNamespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update configmap: %w", err)
	}

	return nil
}

func (r *KubernetesAgentRepository) FindByID(ctx context.Context, id entities.AgentID) (*entities.Agent, error) {
	cm, err := r.client.CoreV1().ConfigMaps(agentNamespace).Get(ctx, r.getConfigMapName(id), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("agent not found")
		}
		return nil, fmt.Errorf("failed to get configmap: %w", err)
	}

	data, ok := cm.Data["agent.json"]
	if !ok {
		return nil, fmt.Errorf("agent data not found in configmap")
	}

	return r.jsonToAgent(data)
}

func (r *KubernetesAgentRepository) FindBySessionID(ctx context.Context, sessionID entities.SessionID) ([]*entities.Agent, error) {
	labelSelector := fmt.Sprintf("type=agent,session-id=%s", string(sessionID))
	cmList, err := r.client.CoreV1().ConfigMaps(agentNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list configmaps: %w", err)
	}

	agents := make([]*entities.Agent, 0, len(cmList.Items))
	for _, cm := range cmList.Items {
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
	labelSelector := "type=agent"
	cmList, err := r.client.CoreV1().ConfigMaps(agentNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list configmaps: %w", err)
	}

	agents := make([]*entities.Agent, 0, len(cmList.Items))
	for _, cm := range cmList.Items {
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
	err := r.client.CoreV1().ConfigMaps(agentNamespace).Delete(ctx, r.getConfigMapName(id), metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete configmap: %w", err)
	}
	return nil
}

func (r *KubernetesAgentRepository) DeleteBySessionID(ctx context.Context, sessionID entities.SessionID) error {
	labelSelector := fmt.Sprintf("type=agent,session-id=%s", string(sessionID))
	err := r.client.CoreV1().ConfigMaps(agentNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to delete configmaps: %w", err)
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
