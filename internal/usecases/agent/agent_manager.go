package agent

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

type AgentManager struct {
	agentRepo           repositories.AgentRepository
	sessionRepo         repositories.SessionRepository
	agentService        services.AgentService      // Legacy agent service (process management)
	k8sService          services.KubernetesService // Provision mode service (Kubernetes)
	config              *config.Config
	healthCheckInterval time.Duration
}

func NewAgentManager(
	agentRepo repositories.AgentRepository,
	sessionRepo repositories.SessionRepository,
	agentService services.AgentService,
	k8sService services.KubernetesService,
	config *config.Config,
) *AgentManager {
	return &AgentManager{
		agentRepo:           agentRepo,
		sessionRepo:         sessionRepo,
		agentService:        agentService,
		k8sService:          k8sService,
		config:              config,
		healthCheckInterval: 30 * time.Second,
	}
}

// isK8sModeEnabled returns true if k8s mode (Kubernetes StatefulSets) is enabled
// By default, local mode (process management) is used when k8s mode is disabled
func (m *AgentManager) isK8sModeEnabled() bool {
	return m.config != nil && m.config.K8sMode.Enabled
}

func (m *AgentManager) CreateAgent(ctx context.Context, sessionID entities.SessionID) (*entities.Agent, error) {
	session, err := m.sessionRepo.FindByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	if !session.IsActive() {
		return nil, fmt.Errorf("session is not active")
	}

	agent := entities.NewAgent(sessionID, "")

	if m.isK8sModeEnabled() {
		return m.createAgentWithK8sMode(ctx, agent, string(sessionID))
	}

	return m.createAgentWithLocalMode(ctx, agent, session)
}

// createAgentWithK8sMode creates an agent using Kubernetes StatefulSets
func (m *AgentManager) createAgentWithK8sMode(ctx context.Context, agent *entities.Agent, sessionID string) (*entities.Agent, error) {
	agentID := string(agent.ID)

	// Create StatefulSet for the agent
	if err := m.k8sService.CreateAgentStatefulSet(ctx, agentID, sessionID); err != nil {
		return nil, fmt.Errorf("failed to create agent statefulset: %w", err)
	}

	// Update agent with pod name
	agent.PodName = fmt.Sprintf("agent-%s-0", agentID)

	if err := m.agentRepo.Save(ctx, agent); err != nil {
		_ = m.k8sService.DeleteStatefulSet(ctx, agentID)
		return nil, fmt.Errorf("failed to save agent: %w", err)
	}

	return agent, nil
}

// createAgentWithLocalMode creates an agent using local process management
func (m *AgentManager) createAgentWithLocalMode(ctx context.Context, agent *entities.Agent, session *entities.Session) (*entities.Agent, error) {
	// Allocate port for agent
	port, err := m.agentService.AllocatePort(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate port: %w", err)
	}

	// Start agent process
	agentConfig := &services.AgentConfig{
		SessionID:   session.ID(),
		UserID:      session.UserID(),
		Port:        entities.Port(port),
		Environment: session.Environment(),
		WorkingDir:  "", // WorkingDir will be set from session if available
		Repository:  session.Repository(),
	}

	processInfo, err := m.agentService.StartAgentWithConfig(ctx, agentConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to start agent process: %w", err)
	}

	// Update agent with process information
	agent.PodName = strconv.Itoa(processInfo.PID()) // Use PID as identifier for legacy mode
	if agent.ResourceInfo == nil {
		agent.ResourceInfo = &entities.AgentResourceInfo{}
	}

	if err := m.agentRepo.Save(ctx, agent); err != nil {
		_ = m.agentService.StopAgent(ctx, processInfo.PID())
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

	if m.isK8sModeEnabled() {
		return m.startAgentWithK8sMode(ctx, agent)
	}

	return m.startAgentWithLocalMode(ctx, agent)
}

// startAgentWithK8sMode starts an agent in k8s mode (Kubernetes)
func (m *AgentManager) startAgentWithK8sMode(ctx context.Context, agent *entities.Agent) error {
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

// startAgentWithLocalMode starts an agent in local mode (process management)
func (m *AgentManager) startAgentWithLocalMode(ctx context.Context, agent *entities.Agent) error {
	// In local mode, the agent is already started when created
	// Just update the status to running
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

	if m.isK8sModeEnabled() {
		return m.stopAgentWithK8sMode(ctx, agent, string(agentID))
	}

	return m.stopAgentWithLocalMode(ctx, agent)
}

// stopAgentWithK8sMode stops an agent in k8s mode (Kubernetes)
func (m *AgentManager) stopAgentWithK8sMode(ctx context.Context, agent *entities.Agent, agentID string) error {
	if err := m.k8sService.DeleteStatefulSet(ctx, agentID); err != nil {
		return fmt.Errorf("failed to delete statefulset: %w", err)
	}

	agent.Stop()
	if err := m.agentRepo.Update(ctx, agent); err != nil {
		return fmt.Errorf("failed to update agent: %w", err)
	}

	return nil
}

// stopAgentWithLocalMode stops an agent in local mode (process management)
func (m *AgentManager) stopAgentWithLocalMode(ctx context.Context, agent *entities.Agent) error {
	// Parse PID from PodName (which stores PID in local mode)
	pid, err := strconv.Atoi(agent.PodName)
	if err != nil {
		return fmt.Errorf("invalid PID in agent data: %w", err)
	}

	if err := m.agentService.StopAgent(ctx, pid); err != nil {
		return fmt.Errorf("failed to stop agent process: %w", err)
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

	if m.isK8sModeEnabled() {
		return m.healthCheckWithK8sMode(ctx, agent)
	}

	return m.healthCheckWithLocalMode(ctx, agent)
}

// healthCheckWithK8sMode performs health check in k8s mode (Kubernetes)
func (m *AgentManager) healthCheckWithK8sMode(ctx context.Context, agent *entities.Agent) error {
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

// healthCheckWithLocalMode performs health check in local mode (process management)
func (m *AgentManager) healthCheckWithLocalMode(ctx context.Context, agent *entities.Agent) error {
	// Parse PID from PodName (which stores PID in local mode)
	pid, err := strconv.Atoi(agent.PodName)
	if err != nil {
		agent.Fail()
		_ = m.agentRepo.Update(ctx, agent)
		return fmt.Errorf("invalid PID in agent data: %w", err)
	}

	// Check if process is still running
	processInfo := entities.NewProcessInfo(pid, time.Now())
	status, err := m.agentService.GetAgentStatus(ctx, processInfo)
	if err != nil {
		agent.Fail()
		_ = m.agentRepo.Update(ctx, agent)
		return fmt.Errorf("failed to get process status: %w", err)
	}

	if status != services.ProcessStatusRunning {
		agent.Fail()
		_ = m.agentRepo.Update(ctx, agent)
		return fmt.Errorf("process is not running: %s", status)
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
