package services

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func newWorkloadTestManager(t *testing.T, pvcEnabled bool) *KubernetesSessionManager {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}
	k8sClient := fake.NewSimpleClientset(ns)
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:      "test-ns",
			Image:          "test-image:latest",
			BasePort:       9000,
			PVCEnabled:     boolPtrForTest(pvcEnabled),
			CPURequest:     "100m",
			CPULimit:       "1",
			MemoryRequest:  "128Mi",
			MemoryLimit:    "512Mi",
			PVCStorageSize: "1Gi",
		},
	}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	return manager
}

func newWorkloadTestSession() *KubernetesSession {
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

func assertOwnedByService(t *testing.T, obj metav1.Object, serviceName string) {
	t.Helper()
	owners := obj.GetOwnerReferences()
	if len(owners) != 1 {
		t.Fatalf("Expected one owner reference on %s, got %d: %+v", obj.GetName(), len(owners), owners)
	}
	if owners[0].APIVersion != "v1" || owners[0].Kind != "Service" || owners[0].Name != serviceName {
		t.Fatalf("Expected %s to be owned by Service %s, got %+v", obj.GetName(), serviceName, owners[0])
	}
}

func findVolumeMount(t *testing.T, mounts []corev1.VolumeMount, name string) corev1.VolumeMount {
	t.Helper()
	for _, mount := range mounts {
		if mount.Name == name {
			return mount
		}
	}
	t.Fatalf("Expected volume mount %q, got %+v", name, mounts)
	return corev1.VolumeMount{}
}

func findVolume(t *testing.T, volumes []corev1.Volume, name string) corev1.Volume {
	t.Helper()
	for _, volume := range volumes {
		if volume.Name == name {
			return volume
		}
	}
	t.Fatalf("Expected volume %q, got %+v", name, volumes)
	return corev1.Volume{}
}

func findContainer(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()
	for _, container := range containers {
		if container.Name == name {
			return container
		}
	}
	t.Fatalf("Expected container %q, got %+v", name, containers)
	return corev1.Container{}
}

func TestCreateSessionWorkloadWithoutPVCUsesPodRestartPolicyNever(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	session := newWorkloadTestSession()

	if err := manager.createSessionWorkload(context.Background(), session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	pod, err := manager.client.CoreV1().Pods("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected pod to be created: %v", err)
	}
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Fatalf("Expected restartPolicy Never, got %s", pod.Spec.RestartPolicy)
	}
	if pod.Spec.Volumes[0].PersistentVolumeClaim != nil {
		t.Fatal("Expected workdir volume to avoid PVC when PVC is disabled")
	}
	if pod.Spec.Volumes[0].EmptyDir == nil {
		t.Fatal("Expected workdir volume to use EmptyDir when PVC is disabled")
	}
	workdirMount := findVolumeMount(t, pod.Spec.Containers[0].VolumeMounts, "workdir")
	if workdirMount.MountPath != "/home/agentapi/workdir" {
		t.Fatalf("Expected ephemeral workdir mount, got %s", workdirMount.MountPath)
	}
	dotClaudeMount := findVolumeMount(t, pod.Spec.Containers[0].VolumeMounts, "dot-claude")
	if dotClaudeMount.MountPath != "/home/agentapi/.claude" {
		t.Fatalf("Expected ephemeral Claude config mount, got %s", dotClaudeMount.MountPath)
	}
	if findVolume(t, pod.Spec.Volumes, "dot-claude").EmptyDir == nil {
		t.Fatal("Expected dot-claude volume to use EmptyDir when PVC is disabled")
	}

	deployments, err := manager.client.AppsV1().Deployments("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list deployments: %v", err)
	}
	if len(deployments.Items) != 0 {
		t.Fatalf("Expected no deployments, got %d", len(deployments.Items))
	}
}

func TestCreateSessionWorkloadWithPVCUsesDeploymentRestartPolicyAlways(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	session := newWorkloadTestSession()
	session.Request().Docker = &entities.DockerParams{Enabled: true}

	if err := manager.createSessionWorkload(context.Background(), session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	deployment, err := manager.client.AppsV1().Deployments("test-ns").Get(context.Background(), session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected deployment to be created: %v", err)
	}
	if deployment.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		t.Fatalf("Expected restartPolicy Always, got %s", deployment.Spec.Template.Spec.RestartPolicy)
	}
	if deployment.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim == nil {
		t.Fatal("Expected workdir volume to mount PVC when PVC is enabled")
	}
	homeMount := findVolumeMount(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts, "workdir")
	if homeMount.MountPath != "/home/agentapi" {
		t.Fatalf("Expected PVC to mount the full agent home, got %s", homeMount.MountPath)
	}
	for _, mount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == "dot-claude" {
			t.Fatal("Expected Claude state to live on the session-home PVC")
		}
	}
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == "dot-claude" {
			t.Fatal("Expected no dot-claude EmptyDir for PVC-backed sessions")
		}
	}
	if len(deployment.Spec.Template.Spec.InitContainers) == 0 {
		t.Fatal("Expected session-home init container")
	}
	homeInit := deployment.Spec.Template.Spec.InitContainers[0]
	if homeInit.Name != "session-home-init" {
		t.Fatalf("Expected session-home init container first, got %s", homeInit.Name)
	}
	initMount := findVolumeMount(t, homeInit.VolumeMounts, "workdir")
	if initMount.MountPath != "/home/agentapi" {
		t.Fatalf("Expected init container to mount session home, got %s", initMount.MountPath)
	}
	dind := findContainer(t, deployment.Spec.Template.Spec.Containers, "docker-dind")
	dindWorkdirMount := findVolumeMount(t, dind.VolumeMounts, "workdir")
	if dindWorkdirMount.MountPath != "/home/agentapi/workdir" || dindWorkdirMount.SubPath != "workdir" {
		t.Fatalf("Expected DinD to share the persisted workdir subPath, got %+v", dindWorkdirMount)
	}

	pods, err := manager.client.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Fatalf("Expected no standalone pods, got %d", len(pods.Items))
	}
}

func TestSessionResourcesUseServiceOwnerReferenceWithoutPVC(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	session := newWorkloadTestSession()
	ctx := context.Background()

	if err := manager.createService(ctx, session); err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	if err := manager.createSessionWorkload(ctx, session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}
	if err := manager.createWebhookPayloadSecret(ctx, session, []byte(`{"ok":true}`)); err != nil {
		t.Fatalf("Failed to create webhook payload secret: %v", err)
	}
	if err := manager.createOneshotSettingsSecret(ctx, session); err != nil {
		t.Fatalf("Failed to create oneshot settings secret: %v", err)
	}
	settings := &sessionsettings.SessionSettings{}
	session.SetProvisionSettings(settings)
	if err := manager.CreateProvisionRequest(ctx, session); err != nil {
		t.Fatalf("Failed to create provision request: %v", err)
	}
	if err := manager.createSessionSettingsSecretFromSettings(ctx, session, session.Request(), settings); err != nil {
		t.Fatalf("Failed to create session settings secret: %v", err)
	}

	pod, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected pod to be created: %v", err)
	}
	assertOwnedByService(t, pod, session.ServiceName())

	for _, secretName := range []string{
		session.ServiceName() + "-webhook-payload",
		session.ServiceName() + "-oneshot-settings",
		"agentapi-provision-request-" + session.ID(),
		"agentapi-session-" + session.ID() + "-settings",
	} {
		secret, err := manager.client.CoreV1().Secrets("test-ns").Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("Expected secret %s to be created: %v", secretName, err)
		}
		assertOwnedByService(t, secret, session.ServiceName())
	}
}

func TestSessionResourcesUseServiceOwnerReferenceWithPVC(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	session := newWorkloadTestSession()
	ctx := context.Background()

	if err := manager.createService(ctx, session); err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	if err := manager.createPVC(ctx, session); err != nil {
		t.Fatalf("Failed to create PVC: %v", err)
	}
	if err := manager.createSessionWorkload(ctx, session, session.Request()); err != nil {
		t.Fatalf("Failed to create workload: %v", err)
	}

	pvc, err := manager.client.CoreV1().PersistentVolumeClaims("test-ns").Get(ctx, session.PVCName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected PVC to be created: %v", err)
	}
	assertOwnedByService(t, pvc, session.ServiceName())

	deployment, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, session.DeploymentName(), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected deployment to be created: %v", err)
	}
	assertOwnedByService(t, deployment, session.ServiceName())
}

func TestPurgeStockSessionsDeletesMixedWorkloadKindsAndPVC(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	ctx := context.Background()
	sessionID := "stock-session"
	name := "agentapi-session-" + sessionID
	orphanSessionID := "orphan-stock-session"
	orphanName := "agentapi-session-" + orphanSessionID
	labels := map[string]string{
		"app.kubernetes.io/managed-by":      "agentapi-proxy",
		"agentapi.proxy/stock":              "true",
		"agentapi.proxy/session-id":         sessionID,
		"agentapi.proxy/capability-sandbox": "false",
		"agentapi.proxy/capability-dind":    "false",
	}

	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-svc",
			Namespace: "test-ns",
			Labels:    labels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stock service: %v", err)
	}
	_, err = manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    labels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create legacy stock deployment: %v", err)
	}
	_, err = manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    labels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stock pod: %v", err)
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims("test-ns").Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-pvc",
			Namespace: "test-ns",
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create legacy stock PVC: %v", err)
	}
	orphanLabels := map[string]string{
		"app.kubernetes.io/managed-by":      "agentapi-proxy",
		"agentapi.proxy/stock":              "true",
		"agentapi.proxy/session-id":         orphanSessionID,
		"agentapi.proxy/capability-sandbox": "false",
		"agentapi.proxy/capability-dind":    "false",
	}
	_, err = manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orphanName,
			Namespace: "test-ns",
			Labels:    orphanLabels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create orphaned stock deployment: %v", err)
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims("test-ns").Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orphanName + "-pvc",
			Namespace: "test-ns",
			Labels:    orphanLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create orphaned stock PVC: %v", err)
	}

	if err := manager.PurgeStockSessions(ctx); err != nil {
		t.Fatalf("PurgeStockSessions failed: %v", err)
	}

	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected service to be deleted, got err=%v", err)
	}
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, name, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected deployment to be deleted, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, name, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected pod to be deleted, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().PersistentVolumeClaims("test-ns").Get(ctx, name+"-pvc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected PVC to be deleted, got err=%v", err)
	}
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, orphanName, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected orphaned deployment to be deleted, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().PersistentVolumeClaims("test-ns").Get(ctx, orphanName+"-pvc", metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("Expected orphaned PVC to be deleted, got err=%v", err)
	}
}

func TestPurgeStockSessionsKeepsAdoptedSessionWithStaleStockWorkloadLabels(t *testing.T) {
	manager := newWorkloadTestManager(t, true)
	ctx := context.Background()
	sessionID := "adopted-stock-session"
	name := "agentapi-session-" + sessionID

	adoptedServiceLabels := map[string]string{
		"app.kubernetes.io/name":       "agentapi-session",
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/session-id":    sessionID,
		"agentapi.proxy/user-id":       "test-user",
		"agentapi.proxy/scope":         "user",
	}
	staleStockLabels := map[string]string{
		"app.kubernetes.io/name":            "agentapi-session",
		"app.kubernetes.io/managed-by":      "agentapi-proxy",
		"agentapi.proxy/session-id":         sessionID,
		"agentapi.proxy/stock":              "true",
		"agentapi.proxy/capability-sandbox": "false",
		"agentapi.proxy/capability-dind":    "false",
	}

	_, err := manager.client.CoreV1().Services("test-ns").Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-svc",
			Namespace: "test-ns",
			Labels:    adoptedServiceLabels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create adopted service: %v", err)
	}
	_, err = manager.client.AppsV1().Deployments("test-ns").Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    staleStockLabels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stale-labeled deployment: %v", err)
	}
	_, err = manager.client.CoreV1().Pods("test-ns").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test-ns",
			Labels:    staleStockLabels,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stale-labeled pod: %v", err)
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims("test-ns").Create(ctx, &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-pvc",
			Namespace: "test-ns",
			Labels:    staleStockLabels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create stale-labeled PVC: %v", err)
	}

	if err := manager.PurgeStockSessions(ctx); err != nil {
		t.Fatalf("PurgeStockSessions failed: %v", err)
	}

	if _, err := manager.client.CoreV1().Services("test-ns").Get(ctx, name+"-svc", metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected adopted service to remain, got err=%v", err)
	}
	if _, err := manager.client.AppsV1().Deployments("test-ns").Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected stale-labeled deployment to remain, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().Pods("test-ns").Get(ctx, name, metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected stale-labeled pod to remain, got err=%v", err)
	}
	if _, err := manager.client.CoreV1().PersistentVolumeClaims("test-ns").Get(ctx, name+"-pvc", metav1.GetOptions{}); err != nil {
		t.Fatalf("Expected stale-labeled PVC to remain, got err=%v", err)
	}
}
