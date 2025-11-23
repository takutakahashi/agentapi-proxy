package repositories

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories/testutils"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKubernetesAgentRepository_Save(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	repo := NewKubernetesAgentRepository(suite.GetClient())
	ctx := context.Background()

	agent := &entities.Agent{
		ID:        entities.AgentID("test-agent-1"),
		SessionID: entities.SessionID("session-1"),
		Status:    entities.AgentStatusRunning,
		PodName:   "test-agent-1-0",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = repo.Save(ctx, agent)
	require.NoError(t, err)

	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-test-agent-1",
	}
	err = suite.GetClient().Get(ctx, key, configMap)
	require.NoError(t, err)

	assert.Equal(t, "agent-test-agent-1", configMap.Name)
	assert.Equal(t, testutils.EnvTestNamespace, configMap.Namespace)
	assert.Equal(t, "agent", configMap.Labels["type"])
	assert.Equal(t, "session-1", configMap.Labels["session-id"])
	assert.Equal(t, "test-agent-1", configMap.Labels["agent-id"])
	assert.Contains(t, configMap.Data, "agent.json")
}

func TestKubernetesAgentRepository_Update(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	repo := NewKubernetesAgentRepository(suite.GetClient())
	ctx := context.Background()

	agent := &entities.Agent{
		ID:        entities.AgentID("test-agent-2"),
		SessionID: entities.SessionID("session-2"),
		Status:    entities.AgentStatusRunning,
		PodName:   "test-agent-2-0",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = repo.Save(ctx, agent)
	require.NoError(t, err)

	agent.Status = entities.AgentStatusStopped
	agent.UpdatedAt = time.Now()

	err = repo.Update(ctx, agent)
	require.NoError(t, err)

	savedAgent, err := repo.FindByID(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, entities.AgentStatusStopped, savedAgent.Status)
}

func TestKubernetesAgentRepository_FindByID(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	repo := NewKubernetesAgentRepository(suite.GetClient())
	ctx := context.Background()

	t.Run("existing agent", func(t *testing.T) {
		agent := &entities.Agent{
			ID:        entities.AgentID("test-agent-3"),
			SessionID: entities.SessionID("session-3"),
			Status:    entities.AgentStatusRunning,
			PodName:   "test-pod",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		err = repo.Save(ctx, agent)
		require.NoError(t, err)

		savedAgent, err := repo.FindByID(ctx, agent.ID)
		require.NoError(t, err)
		assert.Equal(t, agent.ID, savedAgent.ID)
		assert.Equal(t, agent.SessionID, savedAgent.SessionID)
		assert.Equal(t, agent.Status, savedAgent.Status)
	})

	t.Run("non-existing agent", func(t *testing.T) {
		_, err := repo.FindByID(ctx, entities.AgentID("non-existing"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "agent not found")
	})
}

func TestKubernetesAgentRepository_FindBySessionID(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.CleanupTestResources()
		_ = suite.Teardown()
	}()

	repo := NewKubernetesAgentRepository(suite.GetClient())
	ctx := context.Background()

	sessionID := entities.SessionID("session-4")

	agent1 := &entities.Agent{
		ID:        entities.AgentID("test-agent-4a"),
		SessionID: sessionID,
		Status:    entities.AgentStatusRunning,
		PodName:   "test-pod",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	agent2 := &entities.Agent{
		ID:        entities.AgentID("test-agent-4b"),
		SessionID: sessionID,
		Status:    entities.AgentStatusRunning,
		PodName:   "test-pod",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = repo.Save(ctx, agent1)
	require.NoError(t, err)

	err = repo.Save(ctx, agent2)
	require.NoError(t, err)

	agents, err := repo.FindBySessionID(ctx, sessionID)
	require.NoError(t, err)
	assert.Len(t, agents, 2)

	agentIDs := make([]entities.AgentID, len(agents))
	for i, agent := range agents {
		agentIDs[i] = agent.ID
	}
	assert.Contains(t, agentIDs, entities.AgentID("test-agent-4a"))
	assert.Contains(t, agentIDs, entities.AgentID("test-agent-4b"))
}

func TestKubernetesAgentRepository_FindAll(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.CleanupTestResources()
		_ = suite.Teardown()
	}()

	repo := NewKubernetesAgentRepository(suite.GetClient())
	ctx := context.Background()

	initialAgents, err := repo.FindAll(ctx)
	require.NoError(t, err)
	initialCount := len(initialAgents)

	agent1 := &entities.Agent{
		ID:        entities.AgentID("test-agent-5a"),
		SessionID: entities.SessionID("session-5"),
		Status:    entities.AgentStatusRunning,
		PodName:   "test-pod",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	agent2 := &entities.Agent{
		ID:        entities.AgentID("test-agent-5b"),
		SessionID: entities.SessionID("session-5"),
		Status:    entities.AgentStatusRunning,
		PodName:   "test-pod",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = repo.Save(ctx, agent1)
	require.NoError(t, err)

	err = repo.Save(ctx, agent2)
	require.NoError(t, err)

	agents, err := repo.FindAll(ctx)
	require.NoError(t, err)
	assert.Len(t, agents, initialCount+2)
}

func TestKubernetesAgentRepository_Delete(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	repo := NewKubernetesAgentRepository(suite.GetClient())
	ctx := context.Background()

	agent := &entities.Agent{
		ID:        entities.AgentID("test-agent-6"),
		SessionID: entities.SessionID("session-6"),
		Status:    entities.AgentStatusRunning,
		PodName:   "test-pod",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = repo.Save(ctx, agent)
	require.NoError(t, err)

	err = repo.Delete(ctx, agent.ID)
	require.NoError(t, err)

	_, err = repo.FindByID(ctx, agent.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "agent not found")
}

func TestKubernetesAgentRepository_DeleteBySessionID(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.CleanupTestResources()
		_ = suite.Teardown()
	}()

	repo := NewKubernetesAgentRepository(suite.GetClient())
	ctx := context.Background()

	sessionID := entities.SessionID("session-7")

	agent1 := &entities.Agent{
		ID:        entities.AgentID("test-agent-7a"),
		SessionID: sessionID,
		Status:    entities.AgentStatusRunning,
		PodName:   "test-pod",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	agent2 := &entities.Agent{
		ID:        entities.AgentID("test-agent-7b"),
		SessionID: sessionID,
		Status:    entities.AgentStatusRunning,
		PodName:   "test-pod",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = repo.Save(ctx, agent1)
	require.NoError(t, err)

	err = repo.Save(ctx, agent2)
	require.NoError(t, err)

	err = repo.DeleteBySessionID(ctx, sessionID)
	require.NoError(t, err)

	agents, err := repo.FindBySessionID(ctx, sessionID)
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestKubernetesAgentRepository_JSONSerialization(t *testing.T) {
	repo := &KubernetesAgentRepository{}

	agent := &entities.Agent{
		ID:        entities.AgentID("test-agent"),
		SessionID: entities.SessionID("session"),
		Status:    entities.AgentStatusRunning,
		PodName:   "test-pod",
		CreatedAt: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2023, 1, 1, 1, 0, 0, 0, time.UTC),
	}

	jsonData, err := repo.agentToJSON(agent)
	require.NoError(t, err)

	deserializedAgent, err := repo.jsonToAgent(jsonData)
	require.NoError(t, err)

	assert.Equal(t, agent.ID, deserializedAgent.ID)
	assert.Equal(t, agent.SessionID, deserializedAgent.SessionID)
	assert.Equal(t, agent.Status, deserializedAgent.Status)
	assert.Equal(t, agent.PodName, deserializedAgent.PodName)
}

func TestKubernetesAgentRepository_ConfigMapName(t *testing.T) {
	repo := &KubernetesAgentRepository{}

	agentID := entities.AgentID("test-agent")
	configMapName := repo.getConfigMapName(agentID)

	assert.Equal(t, "agent-test-agent", configMapName)
}

func TestKubernetesAgentRepository_ConcurrentOperations(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.CleanupTestResources()
		_ = suite.Teardown()
	}()

	repo := NewKubernetesAgentRepository(suite.GetClient())
	ctx := context.Background()

	sessionID := entities.SessionID("session-concurrent")
	numAgents := 5

	agents := make([]*entities.Agent, numAgents)
	for i := 0; i < numAgents; i++ {
		agents[i] = &entities.Agent{
			ID:        entities.AgentID(fmt.Sprintf("test-agent-concurrent-%d", i)),
			SessionID: sessionID,
			Status:    entities.AgentStatusRunning,
			PodName:   "test-pod",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	done := make(chan error, numAgents)
	for i := 0; i < numAgents; i++ {
		go func(agent *entities.Agent) {
			done <- repo.Save(ctx, agent)
		}(agents[i])
	}

	for i := 0; i < numAgents; i++ {
		err := <-done
		assert.NoError(t, err)
	}

	foundAgents, err := repo.FindBySessionID(ctx, sessionID)
	require.NoError(t, err)
	assert.Len(t, foundAgents, numAgents)
}
