package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestBuildSandboxContainersGeneratesRulesThenRestoresWithIptablesImage(t *testing.T) {
	manager := &KubernetesSessionManager{
		k8sConfig: &config.KubernetesSessionConfig{
			Image:                          "session-image:latest",
			ImagePullPolicy:                "IfNotPresent",
			NetworkFilterImage:             "ghcr.io/takutakahashi/nfa:0.7.0",
			SandboxInitImage:               "gcr.io/istio-release/iptables:latest",
			NetworkFilterCPURequest:        "250m",
			NetworkFilterCPULimit:          "1000m",
			NetworkFilterMemoryRequest:     "256Mi",
			NetworkFilterMemoryLimit:       "512Mi",
			NetworkFilterInitCPURequest:    "50m",
			NetworkFilterInitCPULimit:      "100m",
			NetworkFilterInitMemoryRequest: "32Mi",
			NetworkFilterInitMemoryLimit:   "64Mi",
		},
	}

	initContainers, sidecar, proxyEnvVars := manager.buildSandboxContainers(&entities.SandboxParams{
		Enabled:        true,
		AllowedDomains: []string{"example.com"},
		CountMode:      true,
	})

	assert.Len(t, initContainers, 2)

	generate := initContainers[0]
	assert.Equal(t, "network-filter-generate-iptables", generate.Name)
	assert.Equal(t, "ghcr.io/takutakahashi/nfa:0.7.0", generate.Image)
	assert.Equal(t, []string{"nfa", "setup-iptables", "--output", "/etc/iptables/rules.v4"}, generate.Command)
	assert.Equal(t, corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("50m"),
			corev1.ResourceMemory: resource.MustParse("32Mi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("64Mi"),
		},
	}, generate.Resources)
	assert.Equal(t, []corev1.VolumeMount{{
		Name:      "sandbox-iptables",
		MountPath: "/etc/iptables",
	}}, generate.VolumeMounts)

	restore := initContainers[1]
	assert.Equal(t, "network-filter-setup", restore.Name)
	assert.Equal(t, "gcr.io/istio-release/iptables:latest", restore.Image)
	assert.Equal(t, []string{"iptables-restore", "/etc/iptables/rules.v4"}, restore.Command)
	assert.Equal(t, generate.Resources, restore.Resources)
	assert.Equal(t, []corev1.VolumeMount{{
		Name:      "sandbox-iptables",
		MountPath: "/etc/iptables",
		ReadOnly:  true,
	}}, restore.VolumeMounts)
	assert.Contains(t, restore.SecurityContext.Capabilities.Add, corev1.Capability("NET_ADMIN"))

	assert.NotNil(t, sidecar)
	assert.Equal(t, "network-filter", sidecar.Name)
	assert.NotEmpty(t, sidecar.Resources.Requests)
	assert.NotEmpty(t, sidecar.Resources.Limits)
	assert.Contains(t, proxyEnvVars, corev1.EnvVar{Name: "NETWORK_FILTER_COUNT_MODE", Value: "true"})
}
