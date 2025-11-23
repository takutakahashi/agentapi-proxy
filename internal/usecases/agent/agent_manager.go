package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

type AgentManager struct {
	agentRepo           repositories.AgentRepository
	sessionRepo         repositories.SessionRepository
	k8sService          services.KubernetesService
	healthCheckInterval time.Duration
}

func NewAgentManager(
	agentRepo repositories.AgentRepository,
	sessionRepo repositories.SessionRepository,
	k8sService services.KubernetesService,
) *AgentManager {
	return &AgentManager{
		agentRepo:           agentRepo,
		sessionRepo:         sessionRepo,
		k8sService:          k8sService,
		healthCheckInterval: 30 * time.Second,
	}
}

func (m *AgentManager) CreateAgent(ctx context.Context, sessionID entities.SessionID) (*entities.Agent, error) {
	session, err := m.sessionRepo.FindByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	if !session.IsActive() {
		return nil, fmt.Errorf("session is not active")
	}

	podName, err := m.k8sService.CreateAgentPod(ctx, string(sessionID))
	if err != nil {
		return nil, fmt.Errorf("failed to create agent pod: %w", err)
	}

	agent := entities.NewAgent(sessionID, podName)
	if err := m.agentRepo.Save(ctx, agent); err != nil {
		_ = m.k8sService.DeletePod(ctx, podName)
		return nil, fmt.Errorf("failed to save agent: %w", err)
	}

	return agent, nil
}

func (m *AgentManager) StartAgent(ctx context.Context, agentID entities.AgentID) error {
	agent, err := m.agentRepo.FindByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	if agent.Status != entities.AgentStatusPending {
		return fmt.Errorf("agent is not in pending status")
	}

	podStatus, err := m.k8sService.GetPodStatus(ctx, agent.PodName)
	if err != nil {
		return fmt.Errorf("failed to get pod status: %w", err)
	}

	if podStatus != "Running" {
		return fmt.Errorf("pod is not running yet: %s", podStatus)
	}

	agent.Start()
	if err := m.agentRepo.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	return nil
}

func (m *AgentManager) StopAgent(ctx context.Context, agentID entities.AgentID) error {
	agent, err := m.agentRepo.FindByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	if !agent.IsActive() {
		return fmt.Errorf("agent is not active")
	}

	if err := m.k8sService.DeletePod(ctx, agent.PodName); err != nil {
		return fmt.Errorf("failed to delete pod: %w", err)
	}

	agent.Stop()
	if err := m.agentRepo.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	return nil
}

func (m *AgentManager) GetAgentsBySession(ctx context.Context, sessionID entities.SessionID) ([]*entities.Agent, error) {
	return m.agentRepo.FindBySessionID(ctx, sessionID)
}

func (m *AgentManager) HealthCheck(ctx context.Context, agentID entities.AgentID) error {
	agent, err := m.agentRepo.FindByID(ctx, agentID)
	if err != nil {
		return fmt.Errorf("agent not found: %w", err)
	}

	if !agent.IsActive() {
		return fmt.Errorf("agent is not active")
	}

	podStatus, err := m.k8sService.GetPodStatus(ctx, agent.PodName)
	if err != nil {
		agent.Fail()
		_ = m.agentRepo.Update(ctx, agent)
		return fmt.Errorf("failed to get pod status: %w", err)
	}

	if podStatus != "Running" {
		agent.Fail()
		_ = m.agentRepo.Update(ctx, agent)
		return fmt.Errorf("pod is not running: %s", podStatus)
	}

	agent.UpdatePing()
	if err := m.agentRepo.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	return nil
}

func (m *AgentManager) CleanupInactiveAgents(ctx context.Context) error {
	agents, err := m.agentRepo.FindAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all agents: %w", err)
	}

	for _, agent := range agents {
		if !agent.IsActive() {
			continue
		}

		if !agent.IsHealthy(2 * m.healthCheckInterval) {
			_ = m.k8sService.DeletePod(ctx, agent.PodName)
			agent.Fail()
			_ = m.agentRepo.Update(ctx, agent)
		}
	}

	return nil
}

func (m *AgentManager) ScaleAgents(ctx context.Context, sessionID entities.SessionID, targetCount int) error {
	if targetCount < 0 || targetCount > 10 {
		return fmt.Errorf("invalid target count: %d", targetCount)
	}

	agents, err := m.agentRepo.FindBySessionID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get agents: %w", err)
	}

	activeCount := 0
	for _, agent := range agents {
		if agent.IsActive() {
			activeCount++
		}
	}

	if activeCount == targetCount {
		return nil
	}

	if activeCount > targetCount {
		toStop := activeCount - targetCount
		for _, agent := range agents {
			if toStop == 0 {
				break
			}
			if agent.IsActive() {
				if err := m.StopAgent(ctx, agent.ID); err != nil {
					return fmt.Errorf("failed to stop agent: %w", err)
				}
				toStop--
			}
		}
	} else {
		toCreate := targetCount - activeCount
		for i := 0; i < toCreate; i++ {
			if _, err := m.CreateAgent(ctx, sessionID); err != nil {
				return fmt.Errorf("failed to create agent: %w", err)
			}
		}
	}

	return nil
}

func (m *AgentManager) GetAgent(ctx context.Context, agentID entities.AgentID) (*entities.Agent, error) {
	return m.agentRepo.FindByID(ctx, agentID)
}
