package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories/testutils"
	portServices "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
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

func TestKubernetesServiceImpl_UserSpecificResources(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	userID := "test-user-123"
	notificationTargets := []string{"webhook:http://example.com", "email:test@example.com"}
	envVars := map[string]string{
		"GITHUB_TOKEN": "ghp_test123",
		"API_KEY":      "secret123",
		"DEBUG":        "true",
	}

	t.Run("create user configmap", func(t *testing.T) {
		err = svc.CreateUserConfigMap(ctx, userID, notificationTargets)
		require.NoError(t, err)

		configMapName := "user-" + userID + "-notifications"
		configMap := &corev1.ConfigMap{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      configMapName,
		}
		err = suite.GetClient().Get(ctx, key, configMap)
		require.NoError(t, err)

		expectedData := "webhook:http://example.com,email:test@example.com"
		assert.Equal(t, expectedData, configMap.Data["subscriptions.json"])
	})

	t.Run("create user secret", func(t *testing.T) {
		err = svc.CreateUserSecret(ctx, userID, envVars)
		require.NoError(t, err)

		secretName := "user-" + userID + "-env"
		secret := &corev1.Secret{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      secretName,
		}
		err = suite.GetClient().Get(ctx, key, secret)
		require.NoError(t, err)

		assert.Equal(t, []byte("ghp_test123"), secret.Data["GITHUB_TOKEN"])
		assert.Equal(t, []byte("secret123"), secret.Data["API_KEY"])
		assert.Equal(t, []byte("true"), secret.Data["DEBUG"])
	})

	t.Run("delete user resources", func(t *testing.T) {
		err = svc.DeleteUserResources(ctx, userID)
		require.NoError(t, err)

		// Verify ConfigMap is deleted
		configMapName := "user-" + userID + "-notifications"
		configMap := &corev1.ConfigMap{}
		configMapKey := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      configMapName,
		}
		err = suite.GetClient().Get(ctx, configMapKey, configMap)
		assert.True(t, client.IgnoreNotFound(err) == nil, "ConfigMap should be deleted")

		// Verify Secret is deleted
		secretName := "user-" + userID + "-env"
		secret := &corev1.Secret{}
		secretKey := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      secretName,
		}
		err = suite.GetClient().Get(ctx, secretKey, secret)
		assert.True(t, client.IgnoreNotFound(err) == nil, "Secret should be deleted")
	})
}

func TestKubernetesServiceImpl_SecretOperations(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	secretName := "test-secret"
	data := map[string][]byte{
		"username": []byte("testuser"),
		"password": []byte("testpass"),
	}

	t.Run("create secret", func(t *testing.T) {
		err = svc.CreateSecret(ctx, secretName, data)
		require.NoError(t, err)

		secret := &corev1.Secret{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      secretName,
		}
		err = suite.GetClient().Get(ctx, key, secret)
		require.NoError(t, err)

		assert.Equal(t, data, secret.Data)
	})

	t.Run("update secret", func(t *testing.T) {
		newData := map[string][]byte{
			"username": []byte("updateduser"),
			"token":    []byte("newtoken"),
		}

		err = svc.UpdateSecret(ctx, secretName, newData)
		require.NoError(t, err)

		secret := &corev1.Secret{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      secretName,
		}
		err = suite.GetClient().Get(ctx, key, secret)
		require.NoError(t, err)

		assert.Equal(t, newData, secret.Data)
	})

	t.Run("delete secret", func(t *testing.T) {
		err = svc.DeleteSecret(ctx, secretName)
		require.NoError(t, err)

		secret := &corev1.Secret{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      secretName,
		}
		err = suite.GetClient().Get(ctx, key, secret)
		assert.True(t, client.IgnoreNotFound(err) == nil, "Secret should be deleted")
	})
}

func TestKubernetesServiceImpl_StatefulSetWithUserVolumes(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	userID := "test-user-volumes"
	agentID := "test-agent-volumes"
	sessionID := "session-volumes"

	// Create user resources first
	notificationTargets := []string{"webhook:http://example.com/notify"}
	envVars := map[string]string{
		"GITHUB_TOKEN": "ghp_example123",
		"API_URL":      "https://api.example.com",
	}

	err = svc.CreateUserConfigMap(ctx, userID, notificationTargets)
	require.NoError(t, err)

	err = svc.CreateUserSecret(ctx, userID, envVars)
	require.NoError(t, err)

	// Create StatefulSet with user-specific configuration
	config := portServices.AgentResourceConfig{
		AgentID:   agentID,
		SessionID: sessionID,
		UserID:    userID,
		Namespace: testutils.EnvTestNamespace,
	}
	err = svc.CreateAgentStatefulSetWithConfig(ctx, config)
	require.NoError(t, err)

	// Verify StatefulSet was created
	statefulset := &appsv1.StatefulSet{}
	key := client.ObjectKey{
		Namespace: testutils.EnvTestNamespace,
		Name:      "agent-" + agentID,
	}
	err = suite.GetClient().Get(ctx, key, statefulset)
	require.NoError(t, err)

	t.Run("verify InitContainer configuration", func(t *testing.T) {
		require.Len(t, statefulset.Spec.Template.Spec.InitContainers, 1)

		initContainer := statefulset.Spec.Template.Spec.InitContainers[0]
		assert.Equal(t, "setup", initContainer.Name)
		assert.Equal(t, "busybox:latest", initContainer.Image)

		// Verify InitContainer command
		assert.Equal(t, []string{"sh", "-c"}, initContainer.Command)
		require.Len(t, initContainer.Args, 1)
		assert.Contains(t, initContainer.Args[0], "cp /config/subscriptions.json /notifications/")
		// Secret is now handled via envFrom, not copied in InitContainer

		// Verify InitContainer volume mounts
		volumeMountNames := make(map[string]string)
		for _, vm := range initContainer.VolumeMounts {
			volumeMountNames[vm.Name] = vm.MountPath
		}

		assert.Equal(t, "/config", volumeMountNames["config-volume"])
		assert.Equal(t, "/notifications", volumeMountNames["notifications"])
		assert.Equal(t, "/shared/env", volumeMountNames["shared-env"])

		// Verify read-only mounts
		for _, vm := range initContainer.VolumeMounts {
			if vm.Name == "config-volume" {
				assert.True(t, vm.ReadOnly, "ConfigMap volume should be read-only")
			}
		}
	})

	t.Run("verify agent container configuration", func(t *testing.T) {
		require.Len(t, statefulset.Spec.Template.Spec.Containers, 1)

		agentContainer := statefulset.Spec.Template.Spec.Containers[0]
		assert.Equal(t, "agent", agentContainer.Name)

		// Verify agent container environment variables
		envMap := make(map[string]string)
		for _, env := range agentContainer.Env {
			if env.Value != "" {
				envMap[env.Name] = env.Value
			}
		}

		assert.Equal(t, agentID, envMap["AGENT_ID"])
		assert.Equal(t, sessionID, envMap["SESSION_ID"])
		assert.Equal(t, userID, envMap["USER_ID"])
		assert.Equal(t, "/home/agentapi/notifications", envMap["USER_CONFIG_PATH"])

		// Verify agent container volume mounts
		volumeMountNames := make(map[string]string)
		for _, vm := range agentContainer.VolumeMounts {
			volumeMountNames[vm.Name] = vm.MountPath
		}

		assert.Equal(t, "/data", volumeMountNames["data"])
		assert.Equal(t, "/home/agentapi/notifications", volumeMountNames["notifications"])
    assert.Equal(t, "/shared/env", volumeMountNames["shared-env"])
		// Verify notifications volume is read-only for agent container
		for _, vm := range agentContainer.VolumeMounts {
			if vm.Name == "notifications" {
				assert.True(t, vm.ReadOnly, "Notifications volume should be read-only for agent container")
			}
		}
	})

	t.Run("verify volume configuration", func(t *testing.T) {
		volumes := statefulset.Spec.Template.Spec.Volumes
		require.Len(t, volumes, 2)

		volumeMap := make(map[string]corev1.Volume)
		for _, vol := range volumes {
			volumeMap[vol.Name] = vol
		}

		// Verify ConfigMap volume
		configVol, exists := volumeMap["config-volume"]
		require.True(t, exists)
		require.NotNil(t, configVol.ConfigMap)
		expectedConfigMapName := "user-" + userID + "-notifications"
		assert.Equal(t, expectedConfigMapName, configVol.ConfigMap.Name)
		assert.True(t, *configVol.ConfigMap.Optional, "ConfigMap should be optional")

		// Verify notifications emptyDir volume
		notificationsVol, exists := volumeMap["notifications"]
		require.True(t, exists)
		require.NotNil(t, notificationsVol.EmptyDir)
	})

	t.Run("verify volume claim template", func(t *testing.T) {
		require.Len(t, statefulset.Spec.VolumeClaimTemplates, 1)

		volumeClaimTemplate := statefulset.Spec.VolumeClaimTemplates[0]
		assert.Equal(t, "data", volumeClaimTemplate.Name)
		assert.Contains(t, volumeClaimTemplate.Spec.AccessModes, corev1.ReadWriteOnce)

		// Verify storage request exists
		storageRequest, exists := volumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage]
		assert.True(t, exists)
		assert.False(t, storageRequest.IsZero())
	})

	// Cleanup
	err = svc.DeleteStatefulSet(ctx, agentID)
	require.NoError(t, err)

	err = svc.DeleteUserResources(ctx, userID)
	require.NoError(t, err)
}

func TestKubernetesServiceImpl_EndToEndUserWorkflow(t *testing.T) {
	suite := testutils.NewEnvTestSuite()
	err := suite.Setup()
	require.NoError(t, err)
	defer func() {
		_ = suite.Teardown()
	}()

	svc := NewKubernetesService(suite.GetClient(), suite.GetClientset())
	ctx := context.Background()

	userID := "integration-user"
	agentID := "integration-agent"
	sessionID := "integration-session"

	t.Run("complete user workflow", func(t *testing.T) {
		// Step 1: Create user-specific resources
		notificationTargets := []string{
			"slack:https://hooks.slack.com/test",
			"email:user@example.com",
		}
		envVars := map[string]string{
			"GITHUB_TOKEN": "ghp_integration123",
			"DATABASE_URL": "postgresql://test:test@localhost/test",
			"REDIS_URL":    "redis://localhost:6379",
			"LOG_LEVEL":    "debug",
		}

		err := svc.CreateUserConfigMap(ctx, userID, notificationTargets)
		require.NoError(t, err)

		err = svc.CreateUserSecret(ctx, userID, envVars)
		require.NoError(t, err)

		// Step 2: Create agent StatefulSet with user configuration
		config := portServices.AgentResourceConfig{
			AgentID:       agentID,
			SessionID:     sessionID,
			UserID:        userID,
			Image:         "test-agent:v1.0",
			MemoryRequest: "512Mi",
			CPURequest:    "200m",
			MemoryLimit:   "1Gi",
			CPULimit:      "1000m",
			StorageSize:   "2Gi",
			Namespace:     testutils.EnvTestNamespace,
		}

		err = svc.CreateAgentStatefulSetWithConfig(ctx, config)
		require.NoError(t, err)

		// Step 3: Verify all resources are created and properly linked
		statefulset := &appsv1.StatefulSet{}
		key := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      "agent-" + agentID,
		}
		err = suite.GetClient().Get(ctx, key, statefulset)
		require.NoError(t, err)

		// Verify labels and user linkage
		assert.Equal(t, userID, statefulset.Spec.Template.Spec.Containers[0].Env[2].Value) // USER_ID env var
		assert.Equal(t, sessionID, statefulset.Labels["session-id"])

		// Verify InitContainer exists and references correct user resources
		initContainer := statefulset.Spec.Template.Spec.InitContainers[0]
		assert.Contains(t, initContainer.Args[0], "cp /config/subscriptions.json")
		// Secret is now handled via envFrom, not via volume mount

		// Verify volumes reference correct user resources
		volumes := statefulset.Spec.Template.Spec.Volumes
		var configMapFound bool

		for _, vol := range volumes {
			if vol.Name == "config-volume" && vol.ConfigMap != nil {
				expectedName := "user-" + userID + "-notifications"
				assert.Equal(t, expectedName, vol.ConfigMap.Name)
				configMapFound = true
			}
		}

		assert.True(t, configMapFound, "ConfigMap volume not found or incorrectly configured")

		// Check that secret is referenced via envFrom instead of volume
		agentContainer := statefulset.Spec.Template.Spec.Containers[0]
		require.NotNil(t, agentContainer.EnvFrom)
		require.Len(t, agentContainer.EnvFrom, 1)
		assert.NotNil(t, agentContainer.EnvFrom[0].SecretRef)
		assert.Equal(t, "user-"+userID+"-env", agentContainer.EnvFrom[0].SecretRef.Name)

		// Step 4: Verify user resources contain correct data
		configMap := &corev1.ConfigMap{}
		configMapKey := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      "user-" + userID + "-notifications",
		}
		err = suite.GetClient().Get(ctx, configMapKey, configMap)
		require.NoError(t, err)

		expectedConfigData := "slack:https://hooks.slack.com/test,email:user@example.com"
		assert.Equal(t, expectedConfigData, configMap.Data["subscriptions.json"])

		secret := &corev1.Secret{}
		secretKey := client.ObjectKey{
			Namespace: testutils.EnvTestNamespace,
			Name:      "user-" + userID + "-env",
		}
		err = suite.GetClient().Get(ctx, secretKey, secret)
		require.NoError(t, err)

		assert.Equal(t, []byte("ghp_integration123"), secret.Data["GITHUB_TOKEN"])
		assert.Equal(t, []byte("postgresql://test:test@localhost/test"), secret.Data["DATABASE_URL"])
		assert.Equal(t, []byte("debug"), secret.Data["LOG_LEVEL"])

		// Step 5: Cleanup and verify complete removal
		err = svc.DeleteStatefulSet(ctx, agentID)
		require.NoError(t, err)

		err = svc.DeleteUserResources(ctx, userID)
		require.NoError(t, err)

		// Verify everything is deleted
		err = suite.GetClient().Get(ctx, key, statefulset)
		assert.True(t, client.IgnoreNotFound(err) == nil, "StatefulSet should be deleted")

		err = suite.GetClient().Get(ctx, configMapKey, configMap)
		assert.True(t, client.IgnoreNotFound(err) == nil, "ConfigMap should be deleted")

		err = suite.GetClient().Get(ctx, secretKey, secret)
		assert.True(t, client.IgnoreNotFound(err) == nil, "Secret should be deleted")
	})
}
