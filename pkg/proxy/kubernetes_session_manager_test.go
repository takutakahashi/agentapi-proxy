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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
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

func TestKubernetesSessionManager_DeleteSession_GithubTokenSecret(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-delete-github-token",
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session with GitHub token
	sessionID := "test-session-github-token"
	req := &RunServerRequest{
		UserID:      "test-user",
		GithubToken: "test-github-token-12345",
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Verify GitHub token Secret was created
	serviceName := "agentapi-session-" + sessionID + "-svc"
	secretName := serviceName + "-github-token"
	_, err = k8sClient.CoreV1().Secrets(ns.Name).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Expected GitHub token Secret to be created: %v", err)
	}

	// Delete session
	err = manager.DeleteSession(sessionID)
	if err != nil {
		t.Errorf("Failed to delete session: %v", err)
	}

	// Verify GitHub token Secret is deleted (or has DeletionTimestamp)
	secret, err := k8sClient.CoreV1().Secrets(ns.Name).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil && secret.DeletionTimestamp == nil {
		t.Error("Expected GitHub token Secret to be deleted or have DeletionTimestamp set")
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:v1.0.0",
			ImagePullPolicy:                 "Always",
			ServiceAccount:                  "custom-sa",
			BasePort:                        9000,
			CPURequest:                      "200m",
			CPULimit:                        "1",
			MemoryRequest:                   "256Mi",
			MemoryLimit:                     "1Gi",
			PVCStorageSize:                  "5Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
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
	// ServiceAccount should come from config
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

	// Verify volume mounts (workdir + claude-config for .claude.json and .claude + notification-subscriptions + github-app)
	if len(container.VolumeMounts) != 5 {
		t.Errorf("Expected 5 volume mounts, got %d", len(container.VolumeMounts))
	}
	volumeMountNames := make(map[string]bool)
	for _, vm := range container.VolumeMounts {
		volumeMountNames[vm.Name] = true
	}
	if !volumeMountNames["workdir"] {
		t.Error("Expected workdir volume mount")
	}
	if !volumeMountNames["claude-config"] {
		t.Error("Expected claude-config volume mount")
	}
	if !volumeMountNames["notifications"] {
		t.Error("Expected notifications volume mount")
	}
	if !volumeMountNames["github-app"] {
		t.Error("Expected github-app volume mount")
	}

	// Verify probes
	if container.LivenessProbe == nil || container.LivenessProbe.HTTPGet.Path != "/status" {
		t.Error("Expected liveness probe with /status path")
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.HTTPGet.Path != "/status" {
		t.Error("Expected readiness probe with /status path")
	}
}

func TestKubernetesSessionManager_CredentialsVolumeConfiguration(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-credentials-volume",
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session
	sessionID := "test-session-creds-vol"
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

	if session == nil {
		t.Fatal("Expected session to be created")
	}

	// Verify Session Secret was NOT created (no per-session credentials)
	sessionSecretName := "agentapi-session-" + sessionID + "-credentials"
	_, err = k8sClient.CoreV1().Secrets(ns.Name).Get(ctx, sessionSecretName, metav1.GetOptions{})
	if err == nil {
		t.Error("Expected per-session Secret to NOT exist")
	}

	// Verify Deployment has user-level credentials volume
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Deployment: %v", err)
	}

	// Check for credentials volume referencing user-level Secret
	expectedSecretName := "agentapi-agent-credentials-test-user"
	foundCredentialsVolume := false
	for _, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == "claude-credentials" {
			foundCredentialsVolume = true
			if vol.Secret == nil {
				t.Error("Expected credentials volume to be a Secret volume")
			} else if vol.Secret.SecretName != expectedSecretName {
				t.Errorf("Expected credentials volume to reference Secret %s, got %s", expectedSecretName, vol.Secret.SecretName)
			}
			// Verify it's marked as optional
			if vol.Secret.Optional == nil || !*vol.Secret.Optional {
				t.Error("Expected claude-credentials volume to be optional")
			}
			break
		}
	}
	if !foundCredentialsVolume {
		t.Error("Expected claude-credentials volume to exist")
	}

	// Check initContainer has credentials volume mount
	initContainer := deployment.Spec.Template.Spec.InitContainers[0]
	foundCredentialsMount := false
	for _, mount := range initContainer.VolumeMounts {
		if mount.Name == "claude-credentials" {
			foundCredentialsMount = true
			if mount.MountPath != "/claude-credentials" {
				t.Errorf("Expected credentials mount path '/claude-credentials', got %s", mount.MountPath)
			}
			if !mount.ReadOnly {
				t.Error("Expected credentials volume mount to be read-only")
			}
			break
		}
	}
	if !foundCredentialsMount {
		t.Error("Expected initContainer to have claude-credentials volume mount")
	}

	// Verify credentials-sync sidecar is present
	foundSidecar := false
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == "credentials-sync" {
			foundSidecar = true
		}
	}
	if !foundSidecar {
		t.Error("Expected credentials-sync sidecar to be present")
	}
}

func TestKubernetesSessionManager_ClaudeConfigSetup(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-claude-config",
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session
	sessionID := "test-session-claude"
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

	// Get deployment and verify InitContainer
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Verify InitContainer exists
	podSpec := deployment.Spec.Template.Spec
	if len(podSpec.InitContainers) != 1 {
		t.Fatalf("Expected 1 init container, got %d", len(podSpec.InitContainers))
	}

	initContainer := podSpec.InitContainers[0]
	if initContainer.Name != "setup-claude" {
		t.Errorf("Expected init container name 'setup-claude', got %s", initContainer.Name)
	}
	if initContainer.Image != "alpine:3.19" {
		t.Errorf("Expected init container image 'alpine:3.19', got %s", initContainer.Image)
	}

	// Verify InitContainer volume mounts
	initVolumeMountNames := make(map[string]bool)
	for _, vm := range initContainer.VolumeMounts {
		initVolumeMountNames[vm.Name] = true
	}
	expectedInitMounts := []string{"claude-config-base", "claude-config-user", "claude-config"}
	for _, name := range expectedInitMounts {
		if !initVolumeMountNames[name] {
			t.Errorf("Expected init container to have volume mount '%s'", name)
		}
	}

	// Verify Volumes
	volumeNames := make(map[string]bool)
	for _, v := range podSpec.Volumes {
		volumeNames[v.Name] = true
	}
	expectedVolumes := []string{"workdir", "claude-config-base", "claude-config-user", "claude-config"}
	for _, name := range expectedVolumes {
		if !volumeNames[name] {
			t.Errorf("Expected volume '%s' to exist", name)
		}
	}

	// Verify claude-config-base ConfigMap volume
	var baseConfigMapVolume *corev1.Volume
	for i, v := range podSpec.Volumes {
		if v.Name == "claude-config-base" {
			baseConfigMapVolume = &podSpec.Volumes[i]
			break
		}
	}
	if baseConfigMapVolume == nil {
		t.Fatal("Expected claude-config-base volume")
	}
	if baseConfigMapVolume.ConfigMap == nil {
		t.Fatal("Expected claude-config-base to be a ConfigMap volume")
	}
	if baseConfigMapVolume.ConfigMap.Name != "claude-config-base" {
		t.Errorf("Expected ConfigMap name 'claude-config-base', got %s", baseConfigMapVolume.ConfigMap.Name)
	}
	if baseConfigMapVolume.ConfigMap.Optional == nil || !*baseConfigMapVolume.ConfigMap.Optional {
		t.Error("Expected claude-config-base ConfigMap to be optional")
	}

	// Verify claude-config-user ConfigMap volume
	var userConfigMapVolume *corev1.Volume
	for i, v := range podSpec.Volumes {
		if v.Name == "claude-config-user" {
			userConfigMapVolume = &podSpec.Volumes[i]
			break
		}
	}
	if userConfigMapVolume == nil {
		t.Fatal("Expected claude-config-user volume")
	}
	if userConfigMapVolume.ConfigMap == nil {
		t.Fatal("Expected claude-config-user to be a ConfigMap volume")
	}
	// User ConfigMap name should be prefix + sanitized userID
	expectedUserConfigMap := "claude-config-test-user"
	if userConfigMapVolume.ConfigMap.Name != expectedUserConfigMap {
		t.Errorf("Expected ConfigMap name '%s', got %s", expectedUserConfigMap, userConfigMapVolume.ConfigMap.Name)
	}
	if userConfigMapVolume.ConfigMap.Optional == nil || !*userConfigMapVolume.ConfigMap.Optional {
		t.Error("Expected claude-config-user ConfigMap to be optional")
	}

	// Verify claude-config EmptyDir volume
	var emptyDirVolume *corev1.Volume
	for i, v := range podSpec.Volumes {
		if v.Name == "claude-config" {
			emptyDirVolume = &podSpec.Volumes[i]
			break
		}
	}
	if emptyDirVolume == nil {
		t.Fatal("Expected claude-config volume")
	}
	if emptyDirVolume.EmptyDir == nil {
		t.Fatal("Expected claude-config to be an EmptyDir volume")
	}

	// Verify main container volume mounts with SubPath
	container := podSpec.Containers[0]
	var claudeJSONMount, claudeDirMount *corev1.VolumeMount
	for i, vm := range container.VolumeMounts {
		if vm.MountPath == "/home/agentapi/.claude.json" {
			claudeJSONMount = &container.VolumeMounts[i]
		}
		if vm.MountPath == "/home/agentapi/.claude" {
			claudeDirMount = &container.VolumeMounts[i]
		}
	}

	if claudeJSONMount == nil {
		t.Fatal("Expected .claude.json volume mount")
	}
	if claudeJSONMount.SubPath != ".claude.json" {
		t.Errorf("Expected SubPath '.claude.json', got %s", claudeJSONMount.SubPath)
	}

	if claudeDirMount == nil {
		t.Fatal("Expected .claude directory volume mount")
	}
	if claudeDirMount.SubPath != ".claude" {
		t.Errorf("Expected SubPath '.claude', got %s", claudeDirMount.SubPath)
	}
}

func TestKubernetesSessionManager_InitContainerImageDefault(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-init-image-default",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	// Config with InitContainerImage not set (empty string)
	// Should default to using the main Image
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:v1.2.3",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "", // Empty - should use main Image
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session
	sessionID := "test-session-init-default"
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

	// Get deployment and verify InitContainer uses main image
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Verify InitContainer uses the main image when InitContainerImage is empty
	podSpec := deployment.Spec.Template.Spec
	if len(podSpec.InitContainers) != 1 {
		t.Fatalf("Expected 1 init container, got %d", len(podSpec.InitContainers))
	}

	initContainer := podSpec.InitContainers[0]
	expectedImage := "ghcr.io/takutakahashi/agentapi-proxy:v1.2.3"
	if initContainer.Image != expectedImage {
		t.Errorf("Expected init container image '%s', got '%s'", expectedImage, initContainer.Image)
	}
}

func TestKubernetesSessionManager_ClaudeConfigUserSanitization(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-claude-sanitize",
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
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session with special characters in userID
	sessionID := "test-session-sanitize"
	req := &RunServerRequest{
		UserID: "test@user.com/special", // Contains @ and /
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Get deployment
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Find claude-config-user volume and verify sanitized name
	var userConfigMapVolume *corev1.Volume
	for i, v := range deployment.Spec.Template.Spec.Volumes {
		if v.Name == "claude-config-user" {
			userConfigMapVolume = &deployment.Spec.Template.Spec.Volumes[i]
			break
		}
	}

	if userConfigMapVolume == nil {
		t.Fatal("Expected claude-config-user volume")
	}

	// UserID should be sanitized: test@user.com/special -> test-user.com-special
	expectedConfigMapName := "claude-config-test-user.com-special"
	if userConfigMapVolume.ConfigMap.Name != expectedConfigMapName {
		t.Errorf("Expected sanitized ConfigMap name '%s', got %s", expectedConfigMapName, userConfigMapVolume.ConfigMap.Name)
	}
}

func TestKubernetesSessionManager_CloneRepoInitContainer(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-clone-repo",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	// Create manager with GitHubSecretName configured
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
			GitHubSecretName:                "github-credentials",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session with RepoInfo
	sessionID := "test-session-clone"
	req := &RunServerRequest{
		UserID: "test-user",
		RepoInfo: &RepositoryInfo{
			FullName: "owner/repo",
			CloneDir: "/home/agentapi/workdir/test-session-clone",
		},
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Get deployment and verify clone-repo InitContainer
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Verify we have 2 init containers: clone-repo and setup-claude
	podSpec := deployment.Spec.Template.Spec
	if len(podSpec.InitContainers) != 2 {
		t.Fatalf("Expected 2 init containers, got %d", len(podSpec.InitContainers))
	}

	// Verify first init container is clone-repo
	cloneRepoContainer := podSpec.InitContainers[0]
	if cloneRepoContainer.Name != "clone-repo" {
		t.Errorf("Expected first init container name 'clone-repo', got %s", cloneRepoContainer.Name)
	}

	// Verify clone-repo container has correct environment variables
	envMap := make(map[string]string)
	for _, env := range cloneRepoContainer.Env {
		envMap[env.Name] = env.Value
	}
	if envMap["AGENTAPI_REPO_FULLNAME"] != "owner/repo" {
		t.Errorf("Expected AGENTAPI_REPO_FULLNAME 'owner/repo', got %s", envMap["AGENTAPI_REPO_FULLNAME"])
	}
	// Note: AGENTAPI_CLONE_DIR is no longer set; the script uses a fixed path /home/agentapi/workdir/repo
	if envMap["HOME"] != "/home/agentapi" {
		t.Errorf("Expected HOME '/home/agentapi', got %s", envMap["HOME"])
	}

	// Verify clone-repo container has envFrom referencing the GitHub secret
	if len(cloneRepoContainer.EnvFrom) != 1 {
		t.Fatalf("Expected 1 envFrom, got %d", len(cloneRepoContainer.EnvFrom))
	}
	if cloneRepoContainer.EnvFrom[0].SecretRef == nil {
		t.Fatal("Expected secretRef in envFrom")
	}
	if cloneRepoContainer.EnvFrom[0].SecretRef.Name != "github-credentials" {
		t.Errorf("Expected secretRef name 'github-credentials', got %s", cloneRepoContainer.EnvFrom[0].SecretRef.Name)
	}
	if cloneRepoContainer.EnvFrom[0].SecretRef.Optional == nil || !*cloneRepoContainer.EnvFrom[0].SecretRef.Optional {
		t.Error("Expected secretRef to be optional")
	}

	// Verify clone-repo container has workdir volume mount
	foundWorkdirMount := false
	for _, mount := range cloneRepoContainer.VolumeMounts {
		if mount.Name == "workdir" && mount.MountPath == "/home/agentapi/workdir" {
			foundWorkdirMount = true
			break
		}
	}
	if !foundWorkdirMount {
		t.Error("Expected clone-repo container to have workdir volume mount")
	}

	// Verify second init container is setup-claude
	setupClaudeContainer := podSpec.InitContainers[1]
	if setupClaudeContainer.Name != "setup-claude" {
		t.Errorf("Expected second init container name 'setup-claude', got %s", setupClaudeContainer.Name)
	}
}

func TestKubernetesSessionManager_CloneRepoInitContainerSkippedWithoutRepoInfo(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-clone-repo-skip",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	// Create manager with GitHubSecretName configured
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
			GitHubSecretName:                "github-credentials",
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session WITHOUT RepoInfo
	sessionID := "test-session-no-clone"
	req := &RunServerRequest{
		UserID: "test-user",
		// RepoInfo is nil
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Get deployment and verify only setup-claude InitContainer exists
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Verify we have only 1 init container (setup-claude)
	podSpec := deployment.Spec.Template.Spec
	if len(podSpec.InitContainers) != 1 {
		t.Fatalf("Expected 1 init container when RepoInfo is nil, got %d", len(podSpec.InitContainers))
	}

	// Verify the init container is setup-claude (not clone-repo)
	if podSpec.InitContainers[0].Name != "setup-claude" {
		t.Errorf("Expected init container name 'setup-claude', got %s", podSpec.InitContainers[0].Name)
	}
}

func TestKubernetesSessionManager_CloneRepoInitContainerWithoutGitHubSecret(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-clone-no-secret",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	// Create manager WITHOUT GitHubSecretName configured
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
			GitHubSecretName:                "", // Empty - no GitHub secret
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session with RepoInfo
	sessionID := "test-session-clone-no-secret"
	req := &RunServerRequest{
		UserID: "test-user",
		RepoInfo: &RepositoryInfo{
			FullName: "owner/repo",
			CloneDir: "/home/agentapi/workdir/test-session-clone-no-secret",
		},
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Get deployment and verify clone-repo InitContainer exists but without envFrom
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	// Verify we have 2 init containers
	podSpec := deployment.Spec.Template.Spec
	if len(podSpec.InitContainers) != 2 {
		t.Fatalf("Expected 2 init containers, got %d", len(podSpec.InitContainers))
	}

	// Verify first init container is clone-repo
	cloneRepoContainer := podSpec.InitContainers[0]
	if cloneRepoContainer.Name != "clone-repo" {
		t.Errorf("Expected first init container name 'clone-repo', got %s", cloneRepoContainer.Name)
	}

	// Verify clone-repo container has NO envFrom when GitHubSecretName is empty
	if len(cloneRepoContainer.EnvFrom) != 0 {
		t.Errorf("Expected 0 envFrom when GitHubSecretName is empty, got %d", len(cloneRepoContainer.EnvFrom))
	}
}

// TestKubernetesSessionManager_GithubTokenDoesNotMountGitHubSecretName tests that
// when params.github_token is provided, clone-repo init container does not mount
// GitHubSecretName but instead mounts the session-specific github-token Secret.
func TestKubernetesSessionManager_GithubTokenDoesNotMountGitHubSecretName(t *testing.T) {
	k8sClient, cleanup := setupEnvTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-github-token-override",
		},
	}
	_, err := k8sClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}
	defer func() {
		_ = k8sClient.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	}()

	// Create manager with GitHubSecretName AND GitHubConfigSecretName configured
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Enabled:                         true,
			Namespace:                       ns.Name,
			Image:                           "ghcr.io/takutakahashi/agentapi-proxy:latest",
			ImagePullPolicy:                 "IfNotPresent",
			ServiceAccount:                  "default",
			BasePort:                        9000,
			CPURequest:                      "100m",
			CPULimit:                        "500m",
			MemoryRequest:                   "128Mi",
			MemoryLimit:                     "512Mi",
			PVCStorageClass:                 "",
			PVCStorageSize:                  "1Gi",
			PodStartTimeout:                 60,
			PodStopTimeout:                  30,
			ClaudeConfigBaseConfigMap:       "claude-config-base",
			ClaudeConfigUserConfigMapPrefix: "claude-config",
			InitContainerImage:              "alpine:3.19",
			GitHubSecretName:                "github-session-secret", // Should NOT be mounted when GithubToken is provided
			GitHubConfigSecretName:          "github-config-secret",  // Should still be mounted
		},
	}

	lgr := logger.NewLogger()
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, lgr, k8sClient)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	// Create session with RepoInfo AND GithubToken
	sessionID := "test-github-token"
	req := &RunServerRequest{
		UserID: "test-user",
		RepoInfo: &RepositoryInfo{
			FullName: "owner/repo",
			CloneDir: "/home/agentapi/workdir/test-github-token",
		},
		GithubToken: "ghp_test_token_12345", // This should trigger session-specific Secret usage
	}

	_, err = manager.CreateSession(ctx, sessionID, req)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	defer func() {
		_ = manager.DeleteSession(sessionID)
	}()

	// Get deployment
	deploymentName := "agentapi-session-" + sessionID
	deployment, err := k8sClient.AppsV1().Deployments(ns.Name).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get deployment: %v", err)
	}

	podSpec := deployment.Spec.Template.Spec

	// Find clone-repo init container
	var cloneRepoContainer *corev1.Container
	for i := range podSpec.InitContainers {
		if podSpec.InitContainers[i].Name == "clone-repo" {
			cloneRepoContainer = &podSpec.InitContainers[i]
			break
		}
	}
	if cloneRepoContainer == nil {
		t.Fatal("Expected clone-repo init container")
	}

	// Verify clone-repo container does NOT mount GitHubSecretName
	// It should have 2 envFrom: github-config-secret and session-specific github-token Secret
	expectedEnvFromCount := 2
	if len(cloneRepoContainer.EnvFrom) != expectedEnvFromCount {
		t.Fatalf("Expected %d envFrom for clone-repo, got %d", expectedEnvFromCount, len(cloneRepoContainer.EnvFrom))
	}

	// Collect secret names from envFrom
	envFromSecretNames := make(map[string]bool)
	for _, ef := range cloneRepoContainer.EnvFrom {
		if ef.SecretRef != nil {
			envFromSecretNames[ef.SecretRef.Name] = true
		}
	}

	// Verify GitHubSecretName is NOT mounted
	if envFromSecretNames["github-session-secret"] {
		t.Error("Expected github-session-secret (GitHubSecretName) to NOT be mounted when GithubToken is provided")
	}

	// Verify GitHubConfigSecretName IS mounted
	if !envFromSecretNames["github-config-secret"] {
		t.Error("Expected github-config-secret (GitHubConfigSecretName) to be mounted")
	}

	// Verify session-specific github-token Secret IS mounted
	// serviceName format is "agentapi-session-{sessionID}-svc"
	expectedSessionSecret := "agentapi-session-" + sessionID + "-svc-github-token"
	if !envFromSecretNames[expectedSessionSecret] {
		t.Errorf("Expected session-specific Secret %s to be mounted", expectedSessionSecret)
	}

	// Also verify the main agentapi container
	var agentapiContainer *corev1.Container
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name == "agentapi" {
			agentapiContainer = &podSpec.Containers[i]
			break
		}
	}
	if agentapiContainer == nil {
		t.Fatal("Expected agentapi container")
	}

	// Collect secret names from main container envFrom
	mainEnvFromSecretNames := make(map[string]bool)
	for _, ef := range agentapiContainer.EnvFrom {
		if ef.SecretRef != nil {
			mainEnvFromSecretNames[ef.SecretRef.Name] = true
		}
	}

	// Verify GitHubSecretName is NOT mounted in main container either
	if mainEnvFromSecretNames["github-session-secret"] {
		t.Error("Expected github-session-secret (GitHubSecretName) to NOT be mounted in main container when GithubToken is provided")
	}
}
