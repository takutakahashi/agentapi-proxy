package services

import (
	"context"
	"strings"
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
	assert.Contains(t, sidecar.Env, corev1.EnvVar{Name: "NETWORK_FILTER_COUNT_MODE", Value: "true"})
	assert.Contains(t, proxyEnvVars, corev1.EnvVar{Name: "HTTP_PROXY", Value: "http://127.0.0.1:3128"})
}

func TestResolveSandboxParamsPreservesSessionCountModeWithoutPolicy(t *testing.T) {
	manager := &KubernetesSessionManager{}
	req := &entities.RunServerRequest{
		Sandbox: &entities.SandboxParams{
			Enabled:   true,
			CountMode: true,
		},
	}

	effective := manager.resolveSandboxParams(context.Background(), req)

	assert.NotNil(t, effective)
	assert.True(t, effective.Enabled)
	assert.True(t, effective.CountMode)
}

func TestBuildDeploymentAddsSciaSidecarAndChainsThroughNFA(t *testing.T) {
	manager := &KubernetesSessionManager{
		config: &config.Config{
			Scia: config.SciaConfig{
				Enabled:                   true,
				SessionSidecarEnabled:     true,
				SessionSidecarImage:       "ghcr.io/takutakahashi/scia:0.17.0",
				SessionSidecarConfigImage: "busybox:1.36",
				SessionSidecarPort:        18081,
				Credential:                "takutakahashi.google",
				UserNamespace:             "takutakahashi",
				NoProxy:                   ".svc,.cluster.local",
				GoogleHosts:               []string{"www.googleapis.com"},
				GooglePaths:               []string{"/calendar/v3/*"},
				TodoistCredential:         "takutakahashi.todoist",
				TodoistHosts:              []string{"api.todoist.com"},
				TodoistPaths:              []string{"/api/v1/*"},
			},
		},
		k8sConfig: &config.KubernetesSessionConfig{
			Namespace:                      "test-ns",
			Image:                          "session-image:latest",
			ImagePullPolicy:                "IfNotPresent",
			BasePort:                       9000,
			CPURequest:                     "100m",
			CPULimit:                       "1",
			MemoryRequest:                  "128Mi",
			MemoryLimit:                    "512Mi",
			NetworkFilterImage:             "ghcr.io/takutakahashi/nfa:0.7.0",
			SandboxInitImage:               "gcr.io/istio-release/iptables:latest",
			NetworkFilterInitMemoryRequest: "32Mi",
			NetworkFilterInitMemoryLimit:   "64Mi",
		},
		namespace: "test-ns",
	}
	session := NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "takutakahashi"},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)
	req := &entities.RunServerRequest{
		UserID: "takutakahashi",
		Sandbox: &entities.SandboxParams{
			Enabled:        true,
			AllowedDomains: []string{"www.googleapis.com"},
		},
	}

	deployment, err := manager.buildDeployment(context.Background(), session, req)
	assert.NoError(t, err)
	podSpec := deployment.Spec.Template.Spec

	var configInit *corev1.Container
	for i := range podSpec.InitContainers {
		if podSpec.InitContainers[i].Name == "scia-config" {
			configInit = &podSpec.InitContainers[i]
			break
		}
	}
	if assert.NotNil(t, configInit) {
		script := strings.Join(configInit.Command, " ")
		if len(configInit.Command) >= 3 {
			script = configInit.Command[2]
		}
		assert.Contains(t, script, "mode: proxy")
		assert.Contains(t, script, `url: "http://127.0.0.1:3128"`)
		assert.Contains(t, script, "  integrations:\n")
		assert.Contains(t, script, "    google:\n")
		assert.Contains(t, script, "    todoist:\n")
		assert.Contains(t, script, `        - "www.googleapis.com"`)
		assert.Contains(t, script, `        - "api.todoist.com"`)
		assert.Contains(t, script, `dynamicUsers: true`)
		assert.Contains(t, script, `dynamicUserSecretNamePrefix: "scia-oauth-"`)
		assert.NotContains(t, script, `secretName: "scia-oauth-takutakahashi"`)
		assert.NotContains(t, script, `scia-google-oauth`)
		assert.NotContains(t, script, `clientSecretRef`)
		assert.Contains(t, script, `token_broker_url: "http://scia-oauth.test-ns.svc.cluster.local:8081/oauth/google/token"`)
		assert.Contains(t, script, `token_broker_url: "http://scia-oauth.test-ns.svc.cluster.local:8081/oauth/todoist/token"`)
		assert.Contains(t, script, `- "takutakahashi.google"`)
		assert.Contains(t, script, `- "takutakahashi.todoist"`)
	}

	main := podSpec.Containers[0]
	assert.NotContains(t, main.Env, corev1.EnvVar{Name: "HTTP_PROXY", Value: "http://127.0.0.1:18081"})
	assert.NotContains(t, main.Env, corev1.EnvVar{Name: "HTTPS_PROXY", Value: "http://127.0.0.1:18081"})
	assert.NotContains(t, main.Env, corev1.EnvVar{Name: "SSL_CERT_FILE", Value: sciaCABundlePath})
	assert.Contains(t, main.VolumeMounts, corev1.VolumeMount{Name: "scia-mitm-ca", MountPath: "/etc/scia/mitm", ReadOnly: true})

	env := map[string]string{"AGENTAPI_USER_ID": "takutakahashi"}
	manager.injectSciaProxyEnv(env, req)
	assert.Equal(t, "http://127.0.0.1:18081", env["HTTP_PROXY"])
	assert.Equal(t, "http://127.0.0.1:18081", env["HTTPS_PROXY"])
	assert.Equal(t, sciaCABundlePath, env["SSL_CERT_FILE"])
	assert.Equal(t, "takutakahashi.google", env["AGENTAPI_SCIA_GOOGLE_CREDENTIAL"])
	assert.Equal(t, "takutakahashi.todoist", env["AGENTAPI_SCIA_TODOIST_CREDENTIAL"])
	assert.NotContains(t, env["NO_PROXY"], "api.openai.com")
	assert.NotContains(t, env["no_proxy"], "api.openai.com")

	var foundScia bool
	var foundNFA bool
	for _, container := range podSpec.Containers {
		if container.Name == "scia-proxy" {
			foundScia = true
			assert.Equal(t, []string{"-config", "/etc/scia-config/config.yaml"}, container.Args)
			assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: "scia-config", MountPath: "/etc/scia-config", ReadOnly: true})
			assert.Contains(t, container.VolumeMounts, corev1.VolumeMount{Name: "scia-mitm-ca", MountPath: "/etc/scia/mitm"})
		}
		if container.Name == "network-filter" {
			foundNFA = true
		}
	}
	assert.True(t, foundScia)
	assert.True(t, foundNFA)
}

func TestBuildDeploymentAddsSciaSidecarWhenAuthProxyDisabled(t *testing.T) {
	manager := newSciaSidecarTestManager(true)
	session := newSciaSidecarTestSession(t, manager)
	req := &entities.RunServerRequest{
		UserID:    "takutakahashi",
		AuthProxy: boolPtr(false),
	}

	deployment, err := manager.buildDeployment(context.Background(), session, req)
	assert.NoError(t, err)
	podSpec := deployment.Spec.Template.Spec

	assert.Contains(t, containerNames(podSpec.InitContainers), "scia-config")
	assert.Contains(t, containerNames(podSpec.Containers), "scia-proxy")
	assert.Contains(t, volumeNames(podSpec.Volumes), "scia-config")
	assert.Contains(t, volumeNames(podSpec.Volumes), "scia-mitm-ca")
	assert.Contains(t, volumeMountNames(podSpec.Containers[0].VolumeMounts), "scia-mitm-ca")

	env := map[string]string{"AGENTAPI_USER_ID": "takutakahashi"}
	manager.injectSciaProxyEnv(env, req)
	assert.Equal(t, "http://127.0.0.1:18081", env["HTTP_PROXY"])
	assert.Equal(t, "http://127.0.0.1:18081", env["HTTPS_PROXY"])
	assert.Equal(t, sciaCABundlePath, env["SSL_CERT_FILE"])
}

func TestBuildDeploymentAddsSciaSidecarWhenAuthProxyEnabled(t *testing.T) {
	manager := newSciaSidecarTestManager(false)
	session := newSciaSidecarTestSession(t, manager)
	req := &entities.RunServerRequest{
		UserID:    "takutakahashi",
		AuthProxy: boolPtr(true),
	}

	deployment, err := manager.buildDeployment(context.Background(), session, req)
	assert.NoError(t, err)
	podSpec := deployment.Spec.Template.Spec

	assert.Contains(t, containerNames(podSpec.InitContainers), "scia-config")
	assert.Contains(t, containerNames(podSpec.Containers), "scia-proxy")
	assert.Contains(t, volumeNames(podSpec.Volumes), "scia-config")
	assert.Contains(t, volumeNames(podSpec.Volumes), "scia-mitm-ca")
	assert.Contains(t, volumeMountNames(podSpec.Containers[0].VolumeMounts), "scia-mitm-ca")

	env := map[string]string{"AGENTAPI_USER_ID": "takutakahashi"}
	manager.injectSciaProxyEnv(env, req)
	assert.Equal(t, "http://127.0.0.1:18081", env["HTTP_PROXY"])
	assert.Equal(t, sciaCABundlePath, env["SSL_CERT_FILE"])
}

func newSciaSidecarTestManager(sessionSidecarEnabled bool) *KubernetesSessionManager {
	return &KubernetesSessionManager{
		config: &config.Config{
			Scia: config.SciaConfig{
				Enabled:                   true,
				SessionSidecarEnabled:     sessionSidecarEnabled,
				SessionSidecarImage:       "ghcr.io/takutakahashi/scia:0.17.0",
				SessionSidecarConfigImage: "busybox:1.36",
				SessionSidecarPort:        18081,
				Credential:                "takutakahashi.google",
				UserNamespace:             "takutakahashi",
				GoogleHosts:               []string{"www.googleapis.com"},
				GooglePaths:               []string{"/calendar/v3/*"},
				TodoistCredential:         "takutakahashi.todoist",
				TodoistHosts:              []string{"api.todoist.com"},
				TodoistPaths:              []string{"/api/v1/*"},
			},
		},
		k8sConfig: &config.KubernetesSessionConfig{
			Namespace:                      "test-ns",
			Image:                          "session-image:latest",
			ImagePullPolicy:                "IfNotPresent",
			BasePort:                       9000,
			CPURequest:                     "100m",
			CPULimit:                       "1",
			MemoryRequest:                  "128Mi",
			MemoryLimit:                    "512Mi",
			NetworkFilterImage:             "ghcr.io/takutakahashi/nfa:0.7.0",
			SandboxInitImage:               "gcr.io/istio-release/iptables:latest",
			NetworkFilterInitMemoryRequest: "32Mi",
			NetworkFilterInitMemoryLimit:   "64Mi",
		},
		namespace: "test-ns",
	}
}

func newSciaSidecarTestSession(t *testing.T, manager *KubernetesSessionManager) *KubernetesSession {
	t.Helper()
	return NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "takutakahashi"},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		manager.namespace,
		9000,
		nil,
		nil,
	)
}

func containerNames(containers []corev1.Container) []string {
	names := make([]string, 0, len(containers))
	for _, container := range containers {
		names = append(names, container.Name)
	}
	return names
}

func volumeNames(volumes []corev1.Volume) []string {
	names := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		names = append(names, volume.Name)
	}
	return names
}

func volumeMountNames(mounts []corev1.VolumeMount) []string {
	names := make([]string, 0, len(mounts))
	for _, mount := range mounts {
		names = append(names, mount.Name)
	}
	return names
}
