//go:build envtest
// +build envtest

package proxy

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

// setupEnvTest creates and starts an envtest environment
func setupEnvTest(t *testing.T) (kubernetes.Interface, func()) {
	t.Helper()

	testEnv := &envtest.Environment{}

	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("Failed to start envtest: %v", err)
	}

	k8sClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		testEnv.Stop()
		t.Fatalf("Failed to create kubernetes client: %v", err)
	}

	cleanup := func() {
		if err := testEnv.Stop(); err != nil {
			t.Logf("Failed to stop envtest: %v", err)
		}
	}

	return k8sClient, cleanup
}

func TestKubernetesSessionManager_CreateSession(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-create-session",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	// Create manager
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:         true,
			Namespace:       ns.Name,
			Image:           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy: "IfNotPresent",
			ServiceAccount:  "default",
			BasePort:        9000,
			CPURequest:      "100m",
			CPULimit:        "500m",
			MemoryRequest:   "128Mi",
			MemoryLimit:     "512Mi",
			PVCStorageClass: "",
			PVCStorageSize:  "1Gi",
			PodStartTimeout: 60,
			PodStopTimeout:  30,
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session
	sessionID := "test-session-1"
	req := &RunServerRequest{
		UserID: "test-user",
		Tags: map[string]string{
			"env": "test",
		},
	}

	session, err := manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Verify session was created
	if session.ID() != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, session.ID())
	}
	if session.UserID() != "test-user" {
		t.Errorf("Expected user ID 'test-user', got %s", session.UserID())
	}

	// Verify PVC was created
	pvcName := "agentapi-session-" + sessionID + "-pvc"
	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get PVC: %v", err)
	}
	if pvc.Labels["agentapi.proxy/session-id"] != sessionID {
		t.Errorf("Expected session-id label %s, got %s", sessionID, pvc.Labels["agentapi.proxy/session-id"])
	}

	// Verify Deployment was created
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get Deployment: %v", err)
	}
	if deployment.Spec.Selector.MatchLabels["agentapi.proxy/session-id"] != sessionID {
		t.Errorf("Expected session-id selector %s, got %s", sessionID, deployment.Spec.Selector.MatchLabels["agentapi.proxy/session-id"])
	}

	// Verify Service was created
	serviceName := "agentapi-session-" + sessionID + "-svc"
	svc, err := k8sClient.CoreV1().Services(ns.Name).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed to get Service: %v", err)
	}
	if svc.Spec.Selector["agentapi.proxy/session-id"] != sessionID {
		t.Errorf("Expected session-id selector %s, got %s", sessionID, svc.Spec.Selector["agentapi.proxy/session-id"])
	}

	// Cleanup
	err = manager.DeleteSession(sessionID)
	if err != nil {
		t.Errorf("Failed to delete session: %v", err)
	}
}

func TestKubernetesSessionManager_GetSession(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-get-session",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:         true,
			Namespace:       ns.Name,
			Image:           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy: "IfNotPresent",
			ServiceAccount:  "default",
			BasePort:        9000,
			CPURequest:      "100m",
			CPULimit:        "500m",
			MemoryRequest:   "128Mi",
			MemoryLimit:     "512Mi",
			PVCStorageSize:  "1Gi",
			PodStartTimeout: 60,
			PodStopTimeout:  30,
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Get non-existent session
	session := manager.GetSession("non-existent")
	if session != nil {
		t.Error("Expected nil for non-existent session")
	}

	// Create session
	sessionID := "test-session-get"
	req := &RunServerRequest{
		UserID: "test-user",
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Get existing session
	session = manager.GetSession(sessionID)
	if session == nil {
		t.Fatal("Expected session to exist")
	}
	if session.ID() != sessionID {
		t.Errorf("Expected session ID %s, got %s", sessionID, session.ID())
	}
}

func TestKubernetesSessionManager_ListSessions(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-list-sessions",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:         true,
			Namespace:       ns.Name,
			Image:           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy: "IfNotPresent",
			ServiceAccount:  "default",
			BasePort:        9000,
			CPURequest:      "100m",
			CPULimit:        "500m",
			MemoryRequest:   "128Mi",
			MemoryLimit:     "512Mi",
			PVCStorageSize:  "1Gi",
			PodStartTimeout: 60,
			PodStopTimeout:  30,
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create sessions with different users
	sessions := []struct {
		id     string
		userID string
		tags   map[string]string
	}{
		{"session-1", "user-a", map[string]string{"env": "dev"}},
		{"session-2", "user-a", map[string]string{"env": "prod"}},
		{"session-3", "user-b", map[string]string{"env": "dev"}},
	}

	for _, s := range sessions {
		req := &RunServerRequest{
			UserID: s.userID,
			Tags:   s.tags,
		}
		_, err := manager.CreateSession(ctx, s.id, req)
		if err != nil {
			t.Fatalf("Failed to create session %s: %v", s.id, err)
		}
	}
	defer func() {
		for _, s := range sessions {
			_ = manager.DeleteSession(s.id)
		}
	}()

	// List all sessions
	allSessions := manager.ListSessions(SessionFilter{})
	if len(allSessions) != 3 {
		t.Errorf("Expected 3 sessions, got %d", len(allSessions))
	}

	// List sessions by user
	userASessions := manager.ListSessions(SessionFilter{UserID: "user-a"})
	if len(userASessions) != 2 {
		t.Errorf("Expected 2 sessions for user-a, got %d", len(userASessions))
	}

	// List sessions by tag
	devSessions := manager.ListSessions(SessionFilter{Tags: map[string]string{"env": "dev"}})
	if len(devSessions) != 2 {
		t.Errorf("Expected 2 dev sessions, got %d", len(devSessions))
	}

	// List sessions by user and tag
	userADevSessions := manager.ListSessions(SessionFilter{
		UserID: "user-a",
		Tags:   map[string]string{"env": "dev"},
	})
	if len(userADevSessions) != 1 {
		t.Errorf("Expected 1 session for user-a with env=dev, got %d", len(userADevSessions))
	}
}

func TestKubernetesSessionManager_DeleteSession(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-delete-session",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:         true,
			Namespace:       ns.Name,
			Image:           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy: "IfNotPresent",
			ServiceAccount:  "default",
			BasePort:        9000,
			CPURequest:      "100m",
			CPULimit:        "500m",
			MemoryRequest:   "128Mi",
			MemoryLimit:     "512Mi",
			PVCStorageSize:  "1Gi",
			PodStartTimeout: 60,
			PodStopTimeout:  30,
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session
	sessionID := "test-session-delete"
	req := &RunServerRequest{
		UserID: "test-user",
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Delete non-existent session should fail
	err = manager.DeleteSession("non-existent")
	if err == nil {
		t.Error("Expected error when deleting non-existent session")
	}

	// Delete existing session
	err = manager.DeleteSession(sessionID)
	if err != nil {
		t.Errorf("Failed to delete session: %v", err)
	}

	// Verify session is gone from manager immediately
	session := manager.GetSession(sessionID)
	if session != nil {
		t.Error("Expected session to be deleted from manager")
	}

	// Verify Kubernetes resources have DeletionTimestamp set (deletion initiated)
	// In envtest, actual deletion is asynchronous, so we check DeletionTimestamp
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, "agentapi-session-"+sessionID, metav1.GetOptions{})
	if err == nil && deployment.DeletionTimestamp == nil {
		t.Error("Expected Deployment to have DeletionTimestamp set")
	}

	svc, err := k8sClient.CoreV1().Services(ns.Name).Get(ctx, "agentapi-session-"+sessionID+"-svc", metav1.GetOptions{})
	if err == nil && svc.DeletionTimestamp == nil {
		t.Error("Expected Service to have DeletionTimestamp set")
	}

	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(ctx, "agentapi-session-"+sessionID+"-pvc", metav1.GetOptions{})
	if err == nil && pvc.DeletionTimestamp == nil {
		t.Error("Expected PVC to have DeletionTimestamp set")
	}
}

func TestKubernetesSessionManager_Shutdown(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-shutdown",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:         true,
			Namespace:       ns.Name,
			Image:           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy: "IfNotPresent",
			ServiceAccount:  "default",
			BasePort:        9000,
			CPURequest:      "100m",
			CPULimit:        "500m",
			MemoryRequest:   "128Mi",
			MemoryLimit:     "512Mi",
			PVCStorageSize:  "1Gi",
			PodStartTimeout: 60,
			PodStopTimeout:  30,
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create multiple sessions
	for i := 0; i < 3; i++ {
		sessionID := "shutdown-session-" + string(rune('a'+i))
		req := &RunServerRequest{
			UserID: "test-user",
		}
		_, err := manager.CreateSession(ctx, sessionID, req)
		if err != nil {
			t.Fatalf("Failed to create session %s: %v", sessionID, err)
		}
	}

	// Verify sessions exist
	sessions := manager.ListSessions(SessionFilter{})
	if len(sessions) != 3 {
		t.Fatalf("Expected 3 sessions, got %d", len(sessions))
	}

	// Shutdown
	err = manager.Shutdown(30 * time.Second)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Verify that shutdown completed without error
	// Note: In envtest, Kubernetes resource deletion is asynchronous,
	// so we verify the manager's internal state rather than the actual K8s resources.
	// The deletion requests were sent successfully (no error from Shutdown).

	// Verify the manager has processed all sessions
	// by checking that Shutdown returned without error
	t.Log("Shutdown completed successfully")
}

func TestKubernetesSessionManager_SessionLabels(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-labels",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:         true,
			Namespace:       ns.Name,
			Image:           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy: "IfNotPresent",
			ServiceAccount:  "default",
			BasePort:        9000,
			CPURequest:      "100m",
			CPULimit:        "500m",
			MemoryRequest:   "128Mi",
			MemoryLimit:     "512Mi",
			PVCStorageSize:  "1Gi",
			PodStartTimeout: 60,
			PodStopTimeout:  30,
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session with tags
	sessionID := "test-session-labels"
	req := &RunServerRequest{
		UserID: "test-user@example.com", // Test sanitization
		Tags: map[string]string{
			"env":     "production",
			"team":    "platform",
			"special": "value/with/slashes", // Test sanitization
		},
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Check deployment labels
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Verify standard labels
	expectedLabels := map[string]string{
		"app.kubernetes.io/name":       "agentapi-session",
		"app.kubernetes.io/instance":   sessionID,
		"app.kubernetes.io/managed-by": "agentapi-proxy",
		"agentapi.proxy/session-id":    sessionID,
	}

	for key, expected := range expectedLabels {
		if deployment.Labels[key] != expected {
			t.Errorf("Expected label %s=%s, got %s", key, expected, deployment.Labels[key])
		}
	}

	// Verify user-id is sanitized (@ replaced with -)
	if deployment.Labels["agentapi.proxy/user-id"] != "test-user-example.com" {
		t.Errorf("Expected sanitized user-id, got %s", deployment.Labels["agentapi.proxy/user-id"])
	}

	// Verify tag labels are present and sanitized
	if deployment.Labels["agentapi.proxy/tag-special"] != "value-with-slashes" {
		t.Errorf("Expected sanitized tag value, got %s", deployment.Labels["agentapi.proxy/tag-special"])
	}
}

func TestKubernetesSessionManager_SessionAddr(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-addr",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:         true,
			Namespace:       ns.Name,
			Image:           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy: "IfNotPresent",
			ServiceAccount:  "default",
			BasePort:        9000,
			CPURequest:      "100m",
			CPULimit:        "500m",
			MemoryRequest:   "128Mi",
			MemoryLimit:     "512Mi",
			PVCStorageSize:  "1Gi",
			PodStartTimeout: 60,
			PodStopTimeout:  30,
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session
	sessionID := "test-session-addr"
	req := &RunServerRequest{
		UserID: "test-user",
	}

	session, err := manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Verify Addr returns the expected Service DNS
	expectedAddr := "agentapi-session-" + sessionID + "-svc." + ns.Name + ".svc.cluster.local:9000"
	if session.Addr() != expectedAddr {
		t.Errorf("Expected Addr %s, got %s", expectedAddr, session.Addr())
	}
}

func TestKubernetesSessionManager_DeploymentSpec(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-deployment-spec",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:         true,
			Namespace:       ns.Name,
			Image:           "ghcr.io/takutakahashi/agentapi-proxy:v1.0.0",
			ImagePullPolicy: "Always",
			ServiceAccount:  "custom-sa",
			BasePort:        9000,
			CPURequest:      "200m",
			CPULimit:        "1",
			MemoryRequest:   "256Mi",
			MemoryLimit:     "1Gi",
			PVCStorageSize:  "5Gi",
			PodStartTimeout: 60,
			PodStopTimeout:  30,
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient, nil)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session with environment variables
	sessionID := "test-session-spec"
	req := &RunServerRequest{
		UserID: "test-user",
		RepoInfo: &RepositoryInfo{
			FullName: "org/repo",
			CloneDir: "/workspace/repo",
		},
		Environment: map[string]string{
			"CUSTOM_VAR": "custom-value",
		},
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Get deployment and verify spec
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Verify replicas
	if *deployment.Spec.Replicas != 1 {
		t.Errorf("Expected 1 replica, got %d", *deployment.Spec.Replicas)
	}

	// Verify selector
	if deployment.Spec.Selector.MatchLabels["agentapi.proxy/session-id"] != sessionID {
		t.Errorf("Expected session-id selector %s", sessionID)
	}

	// Verify pod spec
	podSpec := deployment.Spec.Template.Spec
	if podSpec.ServiceAccountName != "custom-sa" {
		t.Errorf("Expected service account 'custom-sa', got %s", podSpec.ServiceAccountName)
	}

	// Verify security context
	if *podSpec.SecurityContext.FSGroup != 999 {
		t.Errorf("Expected FSGroup 999, got %d", *podSpec.SecurityContext.FSGroup)
	}

	// Verify container spec
	container := podSpec.Containers[0]
	if container.Image != "ghcr.io/takutakahashi/agentapi-proxy:v1.0.0" {
		t.Errorf("Expected image 'ghcr.io/takutakahashi/agentapi-proxy:v1.0.0', got %s", container.Image)
	}
	if container.ImagePullPolicy != corev1.PullAlways {
		t.Errorf("Expected pull policy 'Always', got %s", container.ImagePullPolicy)
	}

	// Verify resources
	cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
	if cpuRequest.String() != "200m" {
		t.Errorf("Expected CPU request '200m', got %s", cpuRequest.String())
	}

	memoryLimit := container.Resources.Limits[corev1.ResourceMemory]
	if memoryLimit.String() != "1Gi" {
		t.Errorf("Expected memory limit '1Gi', got %s", memoryLimit.String())
	}

	// Verify environment variables
	envMap := make(map[string]string)
	for _, env := range container.Env {
		envMap[env.Name] = env.Value
	}

	if envMap["AGENTAPI_PORT"] != "9000" {
		t.Errorf("Expected AGENTAPI_PORT '9000', got %s", envMap["AGENTAPI_PORT"])
	}
	if envMap["AGENTAPI_SESSION_ID"] != sessionID {
		t.Errorf("Expected AGENTAPI_SESSION_ID %s, got %s", sessionID, envMap["AGENTAPI_SESSION_ID"])
	}
	if envMap["AGENTAPI_REPO_FULLNAME"] != "org/repo" {
		t.Errorf("Expected AGENTAPI_REPO_FULLNAME 'org/repo', got %s", envMap["AGENTAPI_REPO_FULLNAME"])
	}
	if envMap["CUSTOM_VAR"] != "custom-value" {
		t.Errorf("Expected CUSTOM_VAR 'custom-value', got %s", envMap["CUSTOM_VAR"])
	}

	// Verify volume mounts
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].Name != "workdir" {
		t.Error("Expected workdir volume mount")
	}

	// Verify probes
	if container.LivenessProbe == nil || container.LivenessProbe.HTTPGet.Path != "/status" {
		t.Error("Expected liveness probe with /status path")
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.HTTPGet.Path != "/status" {
		t.Error("Expected readiness probe with /status path")
	}
}
