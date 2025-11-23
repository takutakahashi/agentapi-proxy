package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNewAgentResourceBuilder(t *testing.T) {
	tests := []struct {
		name     string
		config   AgentResourceConfig
		expected AgentResourceConfig
	}{
		{
			name: "with all config provided",
			config: AgentResourceConfig{
				AgentID:       "test-agent",
				SessionID:     "test-session",
				Image:         "custom:v1.0",
				MemoryRequest: "512Mi",
				CPURequest:    "200m",
				MemoryLimit:   "1Gi",
				CPULimit:      "1000m",
				StorageSize:   "2Gi",
				Namespace:     "custom-namespace",
			},
			expected: AgentResourceConfig{
				AgentID:       "test-agent",
				SessionID:     "test-session",
				Image:         "custom:v1.0",
				MemoryRequest: "512Mi",
				CPURequest:    "200m",
				MemoryLimit:   "1Gi",
				CPULimit:      "1000m",
				StorageSize:   "2Gi",
				Namespace:     "custom-namespace",
			},
		},
		{
			name: "with defaults applied",
			config: AgentResourceConfig{
				AgentID:   "test-agent",
				SessionID: "test-session",
			},
			expected: AgentResourceConfig{
				AgentID:       "test-agent",
				SessionID:     "test-session",
				Image:         "agentapi-proxy:latest",
				MemoryRequest: "256Mi",
				CPURequest:    "100m",
				MemoryLimit:   "512Mi",
				CPULimit:      "500m",
				StorageSize:   "1Gi",
				Namespace:     "agentapi-proxy",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewAgentResourceBuilder(tt.config)
			assert.Equal(t, tt.expected, builder.config)
		})
	}
}

func TestAgentResourceBuilder_BuildHeadlessService(t *testing.T) {
	config := AgentResourceConfig{
		AgentID:   "test-agent",
		SessionID: "test-session",
		Namespace: "test-namespace",
	}
	builder := NewAgentResourceBuilder(config)

	service := builder.BuildHeadlessService()

	assert.NotNil(t, service)
	assert.Equal(t, "agent-test-agent-headless", service.Name)
	assert.Equal(t, "test-namespace", service.Namespace)
	assert.Equal(t, "None", service.Spec.ClusterIP)

	expectedLabels := map[string]string{
		"app":        "agentapi-proxy",
		"component":  "agent",
		"agent-id":   "test-agent",
		"session-id": "test-session",
	}
	assert.Equal(t, expectedLabels, service.Labels)

	expectedSelector := map[string]string{
		"app":       "agentapi-proxy",
		"component": "agent",
		"agent-id":  "test-agent",
	}
	assert.Equal(t, expectedSelector, service.Spec.Selector)

	require.Len(t, service.Spec.Ports, 1)
	port := service.Spec.Ports[0]
	assert.Equal(t, int32(8080), port.Port)
	assert.Equal(t, "http", port.Name)
}

func TestAgentResourceBuilder_BuildStatefulSet(t *testing.T) {
	config := AgentResourceConfig{
		AgentID:       "test-agent",
		SessionID:     "test-session",
		Image:         "test:latest",
		MemoryRequest: "256Mi",
		CPURequest:    "100m",
		MemoryLimit:   "512Mi",
		CPULimit:      "500m",
		StorageSize:   "1Gi",
		Namespace:     "test-namespace",
	}
	builder := NewAgentResourceBuilder(config)

	statefulset := builder.BuildStatefulSet()

	assert.NotNil(t, statefulset)
	assert.Equal(t, "agent-test-agent", statefulset.Name)
	assert.Equal(t, "test-namespace", statefulset.Namespace)
	assert.Equal(t, "agent-test-agent-headless", statefulset.Spec.ServiceName)

	assert.NotNil(t, statefulset.Spec.Replicas)
	assert.Equal(t, int32(1), *statefulset.Spec.Replicas)

	expectedLabels := map[string]string{
		"app":        "agentapi-proxy",
		"component":  "agent",
		"agent-id":   "test-agent",
		"session-id": "test-session",
	}
	assert.Equal(t, expectedLabels, statefulset.Labels)
	assert.Equal(t, expectedLabels, statefulset.Spec.Template.Labels)

	expectedSelector := map[string]string{
		"app":       "agentapi-proxy",
		"component": "agent",
		"agent-id":  "test-agent",
	}
	assert.Equal(t, expectedSelector, statefulset.Spec.Selector.MatchLabels)

	require.Len(t, statefulset.Spec.Template.Spec.Containers, 1)
	container := statefulset.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "agent", container.Name)
	assert.Equal(t, "test:latest", container.Image)

	require.Len(t, container.Ports, 1)
	assert.Equal(t, int32(8080), container.Ports[0].ContainerPort)
	assert.Equal(t, "http", container.Ports[0].Name)

	expectedEnvs := map[string]string{
		"AGENT_ID":         "test-agent",
		"SESSION_ID":       "test-session",
		"SESSION_PROVIDER": "kubernetes",
		"K8S_NAMESPACE":    "test-namespace",
	}

	for _, env := range container.Env {
		if expectedValue, exists := expectedEnvs[env.Name]; exists {
			assert.Equal(t, expectedValue, env.Value)
		}
	}

	require.Len(t, container.VolumeMounts, 1)
	volumeMount := container.VolumeMounts[0]
	assert.Equal(t, "data", volumeMount.Name)
	assert.Equal(t, "/data", volumeMount.MountPath)

	cpuRequest, _ := resource.ParseQuantity("100m")
	memoryRequest, _ := resource.ParseQuantity("256Mi")
	cpuLimit, _ := resource.ParseQuantity("500m")
	memoryLimit, _ := resource.ParseQuantity("512Mi")

	assert.Equal(t, cpuRequest, container.Resources.Requests[corev1.ResourceCPU])
	assert.Equal(t, memoryRequest, container.Resources.Requests[corev1.ResourceMemory])
	assert.Equal(t, cpuLimit, container.Resources.Limits[corev1.ResourceCPU])
	assert.Equal(t, memoryLimit, container.Resources.Limits[corev1.ResourceMemory])

	assert.NotNil(t, container.LivenessProbe)
	assert.NotNil(t, container.LivenessProbe.HTTPGet)
	assert.Equal(t, "/health", container.LivenessProbe.HTTPGet.Path)
	assert.Equal(t, int32(30), container.LivenessProbe.InitialDelaySeconds)

	assert.NotNil(t, container.ReadinessProbe)
	assert.NotNil(t, container.ReadinessProbe.HTTPGet)
	assert.Equal(t, "/ready", container.ReadinessProbe.HTTPGet.Path)
	assert.Equal(t, int32(5), container.ReadinessProbe.InitialDelaySeconds)

	require.Len(t, statefulset.Spec.VolumeClaimTemplates, 1)
	volumeClaimTemplate := statefulset.Spec.VolumeClaimTemplates[0]
	assert.Equal(t, "data", volumeClaimTemplate.Name)

	storageSize, _ := resource.ParseQuantity("1Gi")
	assert.Equal(t, storageSize, volumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage])

	assert.Contains(t, volumeClaimTemplate.Spec.AccessModes, corev1.ReadWriteOnce)
}

func TestAgentResourceBuilder_GetResourceNames(t *testing.T) {
	config := AgentResourceConfig{
		AgentID: "test-agent",
	}
	builder := NewAgentResourceBuilder(config)

	serviceName, statefulsetName := builder.GetResourceNames()

	assert.Equal(t, "agent-test-agent-headless", serviceName)
	assert.Equal(t, "agent-test-agent", statefulsetName)
}

func TestAgentResourceBuilder_StatefulSetValidation(t *testing.T) {
	config := AgentResourceConfig{
		AgentID:   "test-agent",
		SessionID: "test-session",
		Namespace: "test-namespace",
	}
	builder := NewAgentResourceBuilder(config)

	statefulset := builder.BuildStatefulSet()

	container := statefulset.Spec.Template.Spec.Containers[0]

	podNameEnvFound := false
	podNamespaceEnvFound := false
	podIPEnvFound := false

	for _, env := range container.Env {
		switch env.Name {
		case "POD_NAME":
			podNameEnvFound = true
			assert.NotNil(t, env.ValueFrom)
			assert.NotNil(t, env.ValueFrom.FieldRef)
			assert.Equal(t, "metadata.name", env.ValueFrom.FieldRef.FieldPath)
		case "POD_NAMESPACE":
			podNamespaceEnvFound = true
			assert.NotNil(t, env.ValueFrom)
			assert.NotNil(t, env.ValueFrom.FieldRef)
			assert.Equal(t, "metadata.namespace", env.ValueFrom.FieldRef.FieldPath)
		case "POD_IP":
			podIPEnvFound = true
			assert.NotNil(t, env.ValueFrom)
			assert.NotNil(t, env.ValueFrom.FieldRef)
			assert.Equal(t, "status.podIP", env.ValueFrom.FieldRef.FieldPath)
		}
	}

	assert.True(t, podNameEnvFound, "POD_NAME environment variable should be set")
	assert.True(t, podNamespaceEnvFound, "POD_NAMESPACE environment variable should be set")
	assert.True(t, podIPEnvFound, "POD_IP environment variable should be set")

	assert.Equal(t, corev1.RestartPolicyAlways, statefulset.Spec.Template.Spec.RestartPolicy)
}
