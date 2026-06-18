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
)

func newWorkloadTestManager(t *testing.T, pvcEnabled bool) *KubernetesSessionManager {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns"}}
	k8sClient := fake.NewSimpleClientset(ns)
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:     "test-ns",
			Image:         "test-image:latest",
			BasePort:      9000,
			PVCEnabled:    boolPtrForTest(pvcEnabled),
			CPURequest:    "100m",
			CPULimit:      "1",
			MemoryRequest: "128Mi",
			MemoryLimit:   "512Mi",
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

	pods, err := manager.client.CoreV1().Pods("test-ns").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list pods: %v", err)
	}
	if len(pods.Items) != 0 {
		t.Fatalf("Expected no standalone pods, got %d", len(pods.Items))
	}
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
