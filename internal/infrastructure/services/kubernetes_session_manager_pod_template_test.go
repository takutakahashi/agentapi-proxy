package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func TestBuildDeploymentMergesSessionPodTemplateFile(t *testing.T) {
	templateFile := filepath.Join(t.TempDir(), "session-pod-template.yaml")
	if err := os.WriteFile(templateFile, []byte(`
metadata:
  annotations:
    example.com/template: "enabled"
  labels:
    workload-type: agent-session
spec:
  serviceAccountName: overridden-service-account
  restartPolicy: Never
  priorityClassName: agent-session
  volumes:
    - name: extra-config
      configMap:
        name: extra-config
  containers:
    - name: agentapi
      command: ["bad-command"]
      args: ["bad-arg"]
      env:
        - name: EXTRA_ENV
          value: enabled
      resources:
        requests:
          cpu: "250m"
          memory: "256Mi"
        limits:
          cpu: "1500m"
          memory: "2Gi"
      volumeMounts:
        - name: extra-config
          mountPath: /etc/extra-config
          readOnly: true
    - name: observer
      image: busybox:1.36
      command: ["sleep", "3600"]
`), 0o600); err != nil {
		t.Fatalf("failed to write pod template: %v", err)
	}

	manager := newPodTemplateTestManager(templateFile)
	session := newPodTemplateTestSession()

	deployment, err := manager.buildDeployment(context.Background(), session, session.Request())
	assert.NoError(t, err)

	template := deployment.Spec.Template
	assert.Equal(t, "enabled", template.Annotations["example.com/template"])
	assert.Equal(t, "agent-session", template.Labels["workload-type"])
	assert.Equal(t, "test-session", template.Labels["agentapi.proxy/session-id"])
	assert.Equal(t, "agentapi-proxy", template.Labels["app.kubernetes.io/managed-by"])
	assert.Equal(t, "agentapi-proxy-session", template.Spec.ServiceAccountName)
	assert.Equal(t, corev1.RestartPolicyAlways, template.Spec.RestartPolicy)
	assert.Equal(t, "agent-session", template.Spec.PriorityClassName)
	assert.Contains(t, volumeNames(template.Spec.Volumes), "extra-config")

	main := template.Spec.Containers[0]
	assert.Equal(t, "agentapi", main.Name)
	assert.Equal(t, []string{"agentapi-proxy"}, main.Command)
	assert.Equal(t, []string{"agent-provisioner"}, main.Args)
	assert.Contains(t, main.Env, corev1.EnvVar{Name: "EXTRA_ENV", Value: "enabled"})
	assert.Contains(t, main.VolumeMounts, corev1.VolumeMount{Name: "extra-config", MountPath: "/etc/extra-config", ReadOnly: true})
	assert.Equal(t, resource.MustParse("250m"), main.Resources.Requests[corev1.ResourceCPU])
	assert.Equal(t, resource.MustParse("1500m"), main.Resources.Limits[corev1.ResourceCPU])
	assert.Contains(t, containerNames(template.Spec.Containers), "observer")
}

func TestBuildDeploymentFailsOnInvalidSessionPodTemplateFile(t *testing.T) {
	templateFile := filepath.Join(t.TempDir(), "session-pod-template.yaml")
	if err := os.WriteFile(templateFile, []byte("metadata:\n  annotations: ["), 0o600); err != nil {
		t.Fatalf("failed to write pod template: %v", err)
	}

	manager := newPodTemplateTestManager(templateFile)
	session := newPodTemplateTestSession()

	_, err := manager.buildDeployment(context.Background(), session, session.Request())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse session pod template file")
}

func TestBuildDeploymentIgnoresMissingSessionPodTemplateFile(t *testing.T) {
	manager := newPodTemplateTestManager(filepath.Join(t.TempDir(), "missing.yaml"))
	session := newPodTemplateTestSession()

	deployment, err := manager.buildDeployment(context.Background(), session, session.Request())
	assert.NoError(t, err)
	assert.Equal(t, "agentapi-proxy-session", deployment.Spec.Template.Spec.ServiceAccountName)
}

func TestCountStockSessionsPurgesStalePodTemplateHash(t *testing.T) {
	templateFile := filepath.Join(t.TempDir(), "session-pod-template.yaml")
	if err := os.WriteFile(templateFile, []byte(`
metadata:
  annotations:
    example.com/template: "old"
`), 0o600); err != nil {
		t.Fatalf("failed to write pod template: %v", err)
	}

	manager := newPodTemplateTestManager(templateFile)
	manager.client = fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}})
	ctx := context.Background()

	oldHash, err := manager.currentStockPodTemplateHash(ctx, false)
	assert.NoError(t, err)

	if err := os.WriteFile(templateFile, []byte(`
metadata:
  annotations:
    example.com/template: "new"
`), 0o600); err != nil {
		t.Fatalf("failed to update pod template: %v", err)
	}
	newHash, err := manager.currentStockPodTemplateHash(ctx, false)
	assert.NoError(t, err)
	assert.NotEqual(t, oldHash, newHash)

	staleService := stockServiceForPodTemplateTest("stale-stock", oldHash)
	currentService := stockServiceForPodTemplateTest("current-stock", newHash)
	_, err = manager.client.CoreV1().Services("test-ns").Create(ctx, staleService, metav1.CreateOptions{})
	assert.NoError(t, err)
	_, err = manager.client.CoreV1().Services("test-ns").Create(ctx, currentService, metav1.CreateOptions{})
	assert.NoError(t, err)

	count, err := manager.CountStockSessions(ctx, false)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	_, err = manager.client.CoreV1().Services("test-ns").Get(ctx, staleService.Name, metav1.GetOptions{})
	assert.True(t, k8serrors.IsNotFound(err), "stale stock service should be purged")
	_, err = manager.client.CoreV1().Services("test-ns").Get(ctx, currentService.Name, metav1.GetOptions{})
	assert.NoError(t, err)
}

func newPodTemplateTestManager(templateFile string) *KubernetesSessionManager {
	return &KubernetesSessionManager{
		config: &config.Config{},
		k8sConfig: &config.KubernetesSessionConfig{
			Namespace:              "test-ns",
			Image:                  "session-image:latest",
			ImagePullPolicy:        "IfNotPresent",
			BasePort:               9000,
			CPURequest:             "100m",
			CPULimit:               "1",
			MemoryRequest:          "128Mi",
			MemoryLimit:            "512Mi",
			SessionPodTemplateFile: templateFile,
		},
		namespace: "test-ns",
	}
}

func newPodTemplateTestSession() *KubernetesSession {
	return NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "test-user"},
		"agentapi-session-test-session",
		"agentapi-session-test-session-svc",
		"agentapi-session-test-session-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)
}

func stockServiceForPodTemplateTest(sessionID string, podTemplateHash string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentapi-session-" + sessionID + "-svc",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":            "agentapi-session",
				"app.kubernetes.io/managed-by":      "agentapi-proxy",
				"agentapi.proxy/session-id":         sessionID,
				"agentapi.proxy/stock":              "true",
				"agentapi.proxy/capability-sandbox": "true",
				"agentapi.proxy/capability-dind":    "false",
				stockPodTemplateHashLabel:           podTemplateHash,
			},
		},
	}
}
