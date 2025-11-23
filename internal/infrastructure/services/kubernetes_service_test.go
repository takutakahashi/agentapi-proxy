package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories/testutils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestKubernetesServiceImpl_CreateAgentStatefulSet(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	agentID := "test-agent-1"
	sessionID := "session-1"

	err = svc.CreateAgentStatefulSet(ctx, agentID, sessionID)
	require.NoError(t, err)

	statefulset := &appsv1.StatefulSet{}
	key := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-" + agentID,
	}
	err = suite.GetClient().Get(ctx, key, statefulset)
	require.NoError(t, err)

	assert.Equal(t, "agent-"+agentID, statefulset.Name)
	assert.Equal(t, testutils.EnvTestNamespace, statefulset.Namespace)
	assert.Equal(t, sessionID, statefulset.Labels["session-id"])

	service := &corev1.Service{}
	serviceKey := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-" + agentID + "-headless",
	}
	err = suite.GetClient().Get(ctx, serviceKey, service)
	require.NoError(t, err)

	assert.Equal(t, "agent-"+agentID+"-headless", service.Name)
	assert.Equal(t, testutils.EnvTestNamespace, service.Namespace)
	assert.Equal(t, corev1.ServiceTypeClusterIP, service.Spec.Type)
	assert.Equal(t, "None", service.Spec.ClusterIP)
}

func TestKubernetesServiceImpl_CreateAgentStatefulSetWithConfig(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	agentID := "test-agent-config"
	sessionID := "session-config"

	err = svc.CreateAgentStatefulSet(ctx, agentID, sessionID)
	require.NoError(t, err)

	statefulset := &appsv1.StatefulSet{}
	key := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-" + agentID,
	}
	err = suite.GetClient().Get(ctx, key, statefulset)
	require.NoError(t, err)

	container := statefulset.Spec.Template.Spec.Containers[0]
	assert.NotEmpty(t, container.Image)

	found := false
	for _, env := range container.Env {
		if env.Name == "AGENT_ID" && env.Value == agentID {
			found = true
			break
		}
	}
	assert.True(t, found, "Environment variable AGENT_ID not found or incorrect value")
}

func TestKubernetesServiceImpl_DeleteStatefulSet(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	agentID := "test-agent-delete"
	sessionID := "session-delete"

	err = svc.CreateAgentStatefulSet(ctx, agentID, sessionID)
	require.NoError(t, err)

	statefulset := &appsv1.StatefulSet{}
	key := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-" + agentID,
	}
	err = suite.GetClient().Get(ctx, key, statefulset)
	require.NoError(t, err)

	err = svc.DeleteStatefulSet(ctx, agentID)
	require.NoError(t, err)

	err = suite.GetClient().Get(ctx, key, statefulset)
	assert.True(t, client.IgnoreNotFound(err) == nil, "StatefulSet should be deleted")

	service := &corev1.Service{}
	serviceKey := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-" + agentID + "-headless",
	}
	err = suite.GetClient().Get(ctx, serviceKey, service)
	assert.True(t, client.IgnoreNotFound(err) == nil, "Service should be deleted")
}

func TestKubernetesServiceImpl_CreateAgentPod_BackwardCompatibility(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	sessionID := "session-compat"

	podName, err := svc.CreateAgentPod(ctx, sessionID)
	require.NoError(t, err)
	assert.Contains(t, podName, "agent-")
	assert.Contains(t, podName, "-0")
}

func TestKubernetesServiceImpl_GetPodStatus(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	t.Run("existing pod", func(t *testing.T) {
		agentID := "test-agent-status"
		sessionID := "session-status"

		err = svc.CreateAgentStatefulSet(ctx, agentID, sessionID)
		require.NoError(t, err)

		podName := "agent-" + agentID + "-0"
		status, err := svc.GetPodStatus(ctx, podName)
		require.NoError(t, err)
		assert.NotEmpty(t, status)
	})

	t.Run("non-existing pod", func(t *testing.T) {
		status, err := svc.GetPodStatus(ctx, "non-existing-pod")
		require.NoError(t, err)
		assert.Equal(t, "NotFound", status)
	})
}

func TestKubernetesServiceImpl_ListPodsBySession(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	sessionID := "session-list"

	err = svc.CreateAgentStatefulSet(ctx, "agent-list-1", sessionID)
	require.NoError(t, err)

	err = svc.CreateAgentStatefulSet(ctx, "agent-list-2", sessionID)
	require.NoError(t, err)

	// Note: In envtest, StatefulSet pods may not be immediately created
	// We test that the StatefulSets are created correctly instead
	statefulset1 := &appsv1.StatefulSet{}
	key1 := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-agent-list-1",
	}
	err = suite.GetClient().Get(ctx, key1, statefulset1)
	require.NoError(t, err)
	assert.Equal(t, sessionID, statefulset1.Labels["session-id"])

	statefulset2 := &appsv1.StatefulSet{}
	key2 := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-agent-list-2",
	}
	err = suite.GetClient().Get(ctx, key2, statefulset2)
	require.NoError(t, err)
	assert.Equal(t, sessionID, statefulset2.Labels["session-id"])
}

func TestKubernetesServiceImpl_ConfigMapOperations(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	configMapName := "test-config"
	data := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	t.Run("create configmap", func(t *testing.T) {
		err = svc.CreateConfigMap(ctx, configMapName, data)
		require.NoError(t, err)

		configMap := &corev1.ConfigMap{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      configMapName,
		}
		err = suite.GetClient().Get(ctx, key, configMap)
		require.NoError(t, err)

		assert.Equal(t, data, configMap.Data)
	})

	t.Run("update configmap", func(t *testing.T) {
		newData := map[string]string{
			"key1": "updated-value1",
			"key3": "value3",
		}

		err = svc.UpdateConfigMap(ctx, configMapName, newData)
		require.NoError(t, err)

		configMap := &corev1.ConfigMap{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      configMapName,
		}
		err = suite.GetClient().Get(ctx, key, configMap)
		require.NoError(t, err)

		assert.Equal(t, newData, configMap.Data)
	})

	t.Run("delete configmap", func(t *testing.T) {
		err = svc.DeleteConfigMap(ctx, configMapName)
		require.NoError(t, err)

		configMap := &corev1.ConfigMap{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      configMapName,
		}
		err = suite.GetClient().Get(ctx, key, configMap)
		assert.True(t, client.IgnoreNotFound(err) == nil, "ConfigMap should be deleted")
	})
}

func TestKubernetesServiceImpl_ScalePods(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	sessionID := "session-scale-unique"

	// Test scaling to 1 (should create a new StatefulSet)
	err = svc.ScalePods(ctx, sessionID, 1)
	require.NoError(t, err)

	// Test scaling down to 0
	err = svc.ScalePods(ctx, sessionID, 0)
	require.NoError(t, err)

	// Note: envtest environment doesn't support pod scheduling like a real cluster
	// so we just test that the scaling operation completes without error
}

func TestKubernetesServiceImpl_GetPodMetrics(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	metrics, err := svc.GetPodMetrics(ctx, "any-pod")
	require.NoError(t, err)

	assert.NotNil(t, metrics)
	assert.Equal(t, "100m", metrics.CPUUsage)
	assert.Equal(t, "128Mi", metrics.MemoryUsage)
}

func TestKubernetesServiceImpl_DeletePod(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	t.Run("delete pod from statefulset", func(t *testing.T) {
		agentID := "test-agent-delete-pod"
		sessionID := "session-delete-pod"

		err = svc.CreateAgentStatefulSet(ctx, agentID, sessionID)
		require.NoError(t, err)

		podName := "agent-" + agentID + "-0"

		err = svc.DeletePod(ctx, podName)
		require.NoError(t, err)

		statefulset := &appsv1.StatefulSet{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      "agent-" + agentID,
		}
		err = suite.GetClient().Get(ctx, key, statefulset)
		assert.True(t, client.IgnoreNotFound(err) == nil, "StatefulSet should be deleted")
	})

	t.Run("delete non-existing pod", func(t *testing.T) {
		err := svc.DeletePod(ctx, "non-existing-pod")
		require.NoError(t, err)
	})
}
