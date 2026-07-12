package services

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

// githubAppSecretKeys are the GitHub App configuration / secret keys that must
// never be exposed to a session Pod.
var githubAppSecretKeys = []string{
	"GITHUB_APP_ID",
	"GITHUB_INSTALLATION_ID",
	"GITHUB_APP_PEM",
	"GITHUB_APP_PEM_PATH",
	"REPOSITORY_RESTRICTION",
}

func newGithubTokenTestManager(t *testing.T, client *fake.Clientset, secretName string) *KubernetesSessionManager {
	t.Helper()
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:        "test-ns",
			Image:            "test-image:latest",
			BasePort:         9000,
			PVCEnabled:       boolPtrForTest(false),
			CPURequest:       "100m",
			CPULimit:         "1",
			MemoryRequest:    "128Mi",
			MemoryLimit:      "512Mi",
			GitHubSecretName: secretName,
		},
	}
	manager, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	manager.namespace = "test-ns"
	return manager
}

func newGithubTokenTestSession() *KubernetesSession {
	return NewKubernetesSession(
		"test-session",
		&entities.RunServerRequest{UserID: "test-user"},
		"test-deploy",
		"agentapi-session-test-svc",
		"test-pvc",
		"test-ns",
		9000,
		nil,
		nil,
	)
}

// TestBuildSessionSettings_GitHubAppUsesBrokerNotToken verifies that when a GitHub App
// auth Secret is configured (no per-request personal token) and the session Pod can
// reach the proxy (useBroker), the Pod receives ONLY a session-scoped broker
// credential + endpoint — never a GITHUB_TOKEN and never GitHub App configuration
// or PEM. The token itself is fetched on demand from the broker at runtime.
func TestBuildSessionSettings_GitHubAppUsesBrokerNotToken(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	manager := newGithubTokenTestManager(t, k8sClient, "agentapi-proxy-github-session")

	// The resolver must NOT be called at build time on the broker path; tokens are
	// minted lazily by the broker.
	resolverCalled := false
	manager.githubTokenResolver = func(repoFullName string) (string, error) {
		resolverCalled = true
		return "ghs_should_not_be_embedded", nil
	}

	session := newGithubTokenTestSession()
	req := &entities.RunServerRequest{
		UserID: "test-user",
		RepoInfo: &entities.RepositoryInfo{
			FullName: "octo/repo",
		},
	}

	settings, err := manager.buildSessionSettings(context.Background(), session, req, nil, true)
	if err != nil {
		t.Fatalf("buildSessionSettings() error = %v", err)
	}
	if resolverCalled {
		t.Fatalf("resolver must not be called at build time on the broker path")
	}
	if _, ok := settings.Env["GITHUB_TOKEN"]; ok {
		t.Fatalf("GITHUB_TOKEN must not be embedded on the broker path")
	}
	brokerURL := settings.Env["AGENTAPI_GITHUB_BROKER_URL"]
	if brokerURL == "" {
		t.Fatalf("AGENTAPI_GITHUB_BROKER_URL must be set on the broker path")
	}
	if !strings.Contains(brokerURL, session.id) {
		t.Fatalf("broker URL %q must contain the session id %q", brokerURL, session.id)
	}
	brokerToken := settings.Env["AGENTAPI_GITHUB_BROKER_TOKEN"]
	if brokerToken == "" {
		t.Fatalf("AGENTAPI_GITHUB_BROKER_TOKEN must be set on the broker path")
	}
	for _, key := range githubAppSecretKeys {
		if _, ok := settings.Env[key]; ok {
			t.Fatalf("session env must not contain GitHub App key %q", key)
		}
	}
	if settings.Github != nil && settings.Github.SecretName != "" {
		t.Fatalf("settings.Github.SecretName must be empty, got %q", settings.Github.SecretName)
	}
}

// TestBuildSessionSettings_PersonalTokenTakesPrecedence verifies that a
// per-request personal token is used directly and no App credentials leak.
func TestBuildSessionSettings_PersonalTokenTakesPrecedence(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	manager := newGithubTokenTestManager(t, k8sClient, "agentapi-proxy-github-session")

	resolverCalled := false
	manager.githubTokenResolver = func(repoFullName string) (string, error) {
		resolverCalled = true
		return "should-not-be-used", nil
	}

	session := newGithubTokenTestSession()
	req := &entities.RunServerRequest{
		UserID:      "test-user",
		GithubToken: "ghp_personaltoken",
	}

	settings, err := manager.buildSessionSettings(context.Background(), session, req, nil, true)
	if err != nil {
		t.Fatalf("buildSessionSettings() error = %v", err)
	}

	if resolverCalled {
		t.Fatalf("server-side resolver must not be called when a personal token is provided")
	}
	if got := settings.Env["GITHUB_TOKEN"]; got != "ghp_personaltoken" {
		t.Fatalf("GITHUB_TOKEN = %q, want ghp_personaltoken", got)
	}
	for _, key := range githubAppSecretKeys {
		if _, ok := settings.Env[key]; ok {
			t.Fatalf("session env must not contain GitHub App key %q", key)
		}
	}
}

// TestBuildSessionSettings_TokenResolveFailureAborts verifies that when GitHub
// authentication is configured but server-side token generation fails, session
// creation is aborted with a clear error (no implicit unauthenticated fallback)
// and the error never contains secret material.
func TestBuildSessionSettings_TokenResolveFailureAborts(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	manager := newGithubTokenTestManager(t, k8sClient, "agentapi-proxy-github-session")
	// The resolver error intentionally embeds real secret material (as could
	// happen if an upstream API echoed a token/PEM back in its error). The test
	// asserts that this never reaches the error returned to callers.
	const secretMarker = "ghs_supersecrettoken"
	manager.githubTokenResolver = func(repoFullName string) (string, error) {
		return "", fmt.Errorf("github api rejected token %s -----BEGIN PRIVATE KEY-----", secretMarker)
	}

	session := newGithubTokenTestSession()
	req := &entities.RunServerRequest{UserID: "test-user"}

	settings, err := manager.buildSessionSettings(context.Background(), session, req, nil, false)
	if err == nil {
		t.Fatalf("expected an error when token resolution fails, got settings=%v", settings)
	}
	if settings != nil {
		t.Fatalf("settings must be nil when token resolution fails, got %v", settings)
	}
	if strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("error must not contain secret material, got: %v", err)
	}
	if strings.Contains(err.Error(), "PRIVATE KEY") {
		t.Fatalf("error must not contain PEM material, got: %v", err)
	}
	if !strings.Contains(err.Error(), session.id) {
		t.Fatalf("error should reference the session ID for diagnosability, got: %v", err)
	}
}

// TestBuildSessionSettings_NoGitHubAuthConfiguredSucceeds verifies that when no
// GitHub authentication is configured (GitHubSecretName empty), session settings
// build succeeds without a token and without any App keys.
func TestBuildSessionSettings_NoGitHubAuthConfiguredSucceeds(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	manager := newGithubTokenTestManager(t, k8sClient, "")
	resolverCalled := false
	manager.githubTokenResolver = func(repoFullName string) (string, error) {
		resolverCalled = true
		return "", fmt.Errorf("should not be called")
	}

	session := newGithubTokenTestSession()
	req := &entities.RunServerRequest{UserID: "test-user"}

	settings, err := manager.buildSessionSettings(context.Background(), session, req, nil, true)
	if err != nil {
		t.Fatalf("buildSessionSettings() error = %v", err)
	}
	if resolverCalled {
		t.Fatalf("resolver must not be called when no GitHub auth is configured")
	}
	if _, ok := settings.Env["GITHUB_TOKEN"]; ok {
		t.Fatalf("GITHUB_TOKEN must not be set when no GitHub auth is configured")
	}
	for _, key := range githubAppSecretKeys {
		if _, ok := settings.Env[key]; ok {
			t.Fatalf("session env must not contain GitHub App key %q", key)
		}
	}
}

// TestBuildSessionSettings_ConfigSecretMergedWithoutAuth verifies that the
// non-authentication GitHub config Secret (GITHUB_API/GITHUB_URL) is still merged
// into the session env, while the auth Secret's App keys are not.
func TestBuildSessionSettings_ConfigSecretMergedWithoutAuth(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	// The auth Secret still exists and (in a misconfigured cluster) may contain
	// App keys; the proxy must never copy those into the Pod env.
	if _, err := k8sClient.CoreV1().Secrets("test-ns").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-proxy-github-session", Namespace: "test-ns"},
		Data: map[string][]byte{
			"GITHUB_APP_ID":          []byte("123"),
			"GITHUB_APP_PEM":         []byte("-----BEGIN PRIVATE KEY-----"),
			"GITHUB_INSTALLATION_ID": []byte("456"),
			"REPOSITORY_RESTRICTION": []byte("true"),
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create auth secret: %v", err)
	}
	if _, err := k8sClient.CoreV1().Secrets("test-ns").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "agentapi-proxy-github-config", Namespace: "test-ns"},
		Data: map[string][]byte{
			"GITHUB_API": []byte("https://ghe.example.com/api/v3"),
			"GITHUB_URL": []byte("https://ghe.example.com"),
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create config secret: %v", err)
	}

	manager := newGithubTokenTestManager(t, k8sClient, "agentapi-proxy-github-session")
	manager.k8sConfig.GitHubConfigSecretName = "agentapi-proxy-github-config"
	manager.githubTokenResolver = func(repoFullName string) (string, error) {
		return "ghs_token", nil
	}

	session := newGithubTokenTestSession()
	req := &entities.RunServerRequest{UserID: "test-user"}

	settings, err := manager.buildSessionSettings(context.Background(), session, req, nil, false)
	if err != nil {
		t.Fatalf("buildSessionSettings() error = %v", err)
	}

	if got := settings.Env["GITHUB_API"]; got != "https://ghe.example.com/api/v3" {
		t.Fatalf("GITHUB_API = %q, want the config secret value", got)
	}
	if got := settings.Env["GITHUB_URL"]; got != "https://ghe.example.com" {
		t.Fatalf("GITHUB_URL = %q, want the config secret value", got)
	}
	for _, key := range githubAppSecretKeys {
		if _, ok := settings.Env[key]; ok {
			t.Fatalf("session env must not contain GitHub App key %q even if present in the auth secret", key)
		}
	}
}
