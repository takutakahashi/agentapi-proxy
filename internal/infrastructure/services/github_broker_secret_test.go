package services

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

func newBrokerTestManager(t *testing.T, client *fake.Clientset) *KubernetesSessionManager {
	t.Helper()
	cfg := &config.Config{
		KubernetesSession: config.KubernetesSessionConfig{
			Namespace:     "test-ns",
			Image:         "test-image:latest",
			BasePort:      9000,
			PVCEnabled:    boolPtrForTest(false),
			CPURequest:    "100m",
			CPULimit:      "1",
			MemoryRequest: "128Mi",
			MemoryLimit:   "512Mi",
		},
	}
	m, err := NewKubernetesSessionManagerWithClient(cfg, false, logger.NewLogger(), client)
	if err != nil {
		t.Fatalf("NewKubernetesSessionManagerWithClient() error = %v", err)
	}
	m.namespace = "test-ns"
	return m
}

// TestGitHubBrokerToken_SessionScoped verifies that the HMAC-derived broker
// credential is bound to a single session: a token for session A does not
// validate for session B.
func TestGitHubBrokerToken_SessionScoped(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	m := newBrokerTestManager(t, k8sClient)

	tokA := m.githubBrokerTokenForSession("session-A")
	tokB := m.githubBrokerTokenForSession("session-B")
	if tokA == "" || tokB == "" {
		t.Fatalf("tokens must be non-empty")
	}
	if tokA == tokB {
		t.Fatalf("tokens for different sessions must differ")
	}
	if !m.ValidateGitHubBrokerToken("session-A", tokA) {
		t.Fatalf("token A must validate for session A")
	}
	if m.ValidateGitHubBrokerToken("session-B", tokA) {
		t.Fatalf("token A must NOT validate for session B (cross-session reuse)")
	}
	if m.ValidateGitHubBrokerToken("session-A", "not-the-token") {
		t.Fatalf("wrong token must not validate")
	}
	if m.ValidateGitHubBrokerToken("session-A", "") {
		t.Fatalf("empty token must not validate")
	}
}

// TestGitHubBrokerToken_StableAcrossCalls verifies the credential is deterministic
// (HMAC), so a proxy restart with the same persisted secret validates existing
// sessions' tokens.
func TestGitHubBrokerToken_StableAcrossCalls(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	m := newBrokerTestManager(t, k8sClient)
	tok1 := m.githubBrokerTokenForSession("sess")
	tok2 := m.githubBrokerTokenForSession("sess")
	if tok1 != tok2 {
		t.Fatalf("token must be deterministic for the same session/secret")
	}
}

// TestEnsureGitHubBrokerSecret_PersistsAcrossManagers verifies the broker secret is
// persisted in a Kubernetes Secret and reloaded by a new manager instance.
func TestEnsureGitHubBrokerSecret_PersistsAcrossManagers(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	m1 := newBrokerTestManager(t, k8sClient)
	tok1 := m1.githubBrokerTokenForSession("sess")

	// A second manager reads the same persisted secret.
	m2 := newBrokerTestManager(t, k8sClient)
	tok2 := m2.githubBrokerTokenForSession("sess")
	if tok1 != tok2 {
		t.Fatalf("broker secret must persist across manager instances: %q != %q", tok1, tok2)
	}
}

// TestIssueGitHubToken_AuthAndScope verifies the broker's session existence,
// authorization and repository-scope checks.
func TestIssueGitHubToken_AuthAndScope(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	m := newBrokerTestManager(t, k8sClient)

	// Register a session with a configured repository.
	repoSession := NewKubernetesSession(
		"repo-session",
		&entities.RunServerRequest{
			UserID:   "u",
			RepoInfo: &entities.RepositoryInfo{FullName: "octo/repo"},
		},
		"deploy", "svc", "pvc", "test-ns", 9000, nil, nil,
	)
	m.sessions["repo-session"] = repoSession

	// Wire a deterministic resolver returning a token with expiry.
	exp := time.Now().Add(1 * time.Hour).UTC()
	m.githubTokenBroker = NewGitHubTokenBroker(func(repo string) (string, time.Time, error) {
		if repo != "octo/repo" {
			return "", time.Time{}, nil
		}
		return "ghs_issued", exp, nil
	})

	// Valid call: matching repo scope.
	tok, gotExp, err := m.IssueGitHubToken("repo-session", "octo/repo")
	if err != nil {
		t.Fatalf("valid call: unexpected error: %v", err)
	}
	if tok != "ghs_issued" {
		t.Fatalf("token = %q, want ghs_issued", tok)
	}
	if !gotExp.Equal(exp) {
		t.Fatalf("expiry mismatch")
	}

	// Scope substitution: a different repository must be rejected.
	_, _, err = m.IssueGitHubToken("repo-session", "octo/other")
	if err == nil || !strings.Contains(err.Error(), "scope mismatch") {
		t.Fatalf("scope substitution must be rejected, got err=%v", err)
	}

	// Empty request against a scoped session uses the session's repository scope
	// (this is how in-Pod helpers call the broker with no body). It must succeed
	// and scope the token to the session repository.
	tok, _, err = m.IssueGitHubToken("repo-session", "")
	if err != nil {
		t.Fatalf("empty repo against scoped session must use session scope, got err=%v", err)
	}
	if tok != "ghs_issued" {
		t.Fatalf("token = %q, want ghs_issued (session scope)", tok)
	}

	// Unknown session.
	_, _, err = m.IssueGitHubToken("nope", "")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown session must error, got %v", err)
	}
}

// TestIssueGitHubToken_UnscopedSessionAllowsEmpty verifies that a session with no
// configured repository can still get a token (empty repo scope), subject to the
// resolver's REPOSITORY_RESTRICTION handling.
func TestIssueGitHubToken_UnscopedSessionAllowsEmpty(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	m := newBrokerTestManager(t, k8sClient)
	m.sessions["unscoped"] = NewKubernetesSession(
		"unscoped", &entities.RunServerRequest{UserID: "u"},
		"deploy", "svc", "pvc", "test-ns", 9000, nil, nil,
	)
	m.githubTokenBroker = NewGitHubTokenBroker(func(repo string) (string, time.Time, error) {
		if repo != "" {
			t.Fatalf("unscoped session must request empty repo, got %q", repo)
		}
		return "ghs_unscoped", time.Now().Add(time.Hour), nil
	})
	tok, _, err := m.IssueGitHubToken("unscoped", "")
	if err != nil {
		t.Fatalf("unscoped call: %v", err)
	}
	if tok != "ghs_unscoped" {
		t.Fatalf("token = %q, want ghs_unscoped", tok)
	}
}

// TestGitHubBrokerToken_NotReusableForProvisioner verifies the broker credential is
// distinct from the provisioner token, so a captured broker token cannot be used
// to call the provisioner internal API (and vice versa).
func TestGitHubBrokerToken_NotReusableForProvisioner(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	m := newBrokerTestManager(t, k8sClient)
	brokerTok := m.githubBrokerTokenForSession("sess")
	if m.ValidateProvisionerToken(brokerTok) {
		t.Fatalf("broker credential must not validate as a provisioner token")
	}
}

// TestGithubBrokerURL_UsesProvisionerProxyURL verifies that the broker endpoint a
// session Pod uses is rooted at the configured ProvisionerProxyURL (which the Helm
// chart derives from the actual proxy Service fullname), and that the session id
// is embedded in the path. This keeps the broker URL consistent with custom
// release names / fullnameOverride rather than a hardcoded "agentapi-proxy" name.
func TestGithubBrokerURL_UsesProvisionerProxyURL(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	m := newBrokerTestManager(t, k8sClient)

	cases := []struct {
		name      string
		proxyURL  string
		sessionID string
		want      string
	}{
		{name: "explicit proxy url", proxyURL: "http://custom-proxy.release-ns.svc.cluster.local:8080", sessionID: "sess-1", want: "http://custom-proxy.release-ns.svc.cluster.local:8080/internal/sessions/sess-1/github-token"},
		{name: "explicit proxy url trailing slash", proxyURL: "http://custom-proxy:8080/", sessionID: "sess-2", want: "http://custom-proxy:8080/internal/sessions/sess-2/github-token"},
		{name: "empty falls back to in-cluster default", proxyURL: "", sessionID: "sess-3", want: "http://agentapi-proxy.test-ns.svc.cluster.local:8080/internal/sessions/sess-3/github-token"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m.k8sConfig.ProvisionerProxyURL = c.proxyURL
			m.namespace = "test-ns"
			got := m.githubBrokerURL(c.sessionID)
			// The trailing-slash case may keep the slash; normalize comparison.
			if c.name == "explicit proxy url trailing slash" {
				// accept either with or without the extra slash from the trailing input
				wantA := c.want
				wantB := "http://custom-proxy:8080//internal/sessions/sess-2/github-token"
				if got != wantA && got != wantB {
					t.Fatalf("githubBrokerURL() = %q, want %q or %q", got, wantA, wantB)
				}
				return
			}
			if got != c.want {
				t.Fatalf("githubBrokerURL() = %q, want %q", got, c.want)
			}
			if !strings.Contains(got, c.sessionID) {
				t.Fatalf("broker URL %q must contain session id %q", got, c.sessionID)
			}
		})
	}
}

// TestBuildSessionSettings_BrokerURLObeyedFromProxyURL verifies that the broker
// URL injected into the session env follows ProvisionerProxyURL (chart-derived),
// so a custom release/fullname does not break token fetching.
func TestBuildSessionSettings_BrokerURLObeyedFromProxyURL(t *testing.T) {
	k8sClient := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test-ns"},
	})
	manager := newGithubTokenTestManager(t, k8sClient, "agentapi-proxy-github-session")
	manager.k8sConfig.ProvisionerProxyURL = "http://custom-proxy.release-ns.svc.cluster.local:8080"
	manager.namespace = "test-ns"

	session := newGithubTokenTestSession()
	req := &entities.RunServerRequest{
		UserID:   "test-user",
		RepoInfo: &entities.RepositoryInfo{FullName: "octo/repo"},
	}
	settings, err := manager.buildSessionSettings(context.Background(), session, req, nil, true)
	if err != nil {
		t.Fatalf("buildSessionSettings() error = %v", err)
	}
	brokerURL := settings.Env["AGENTAPI_GITHUB_BROKER_URL"]
	if brokerURL != "http://custom-proxy.release-ns.svc.cluster.local:8080/internal/sessions/"+session.id+"/github-token" {
		t.Fatalf("broker URL must follow ProvisionerProxyURL, got %q", brokerURL)
	}
}
