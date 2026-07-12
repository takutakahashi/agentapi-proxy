package services

import (
	"context"
	"strings"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// githubAppForbiddenEnvKeys are the GitHub App configuration / private-key env
// vars that must never appear in a session Pod spec (container env, envFrom, or
// volumes). They live only on the proxy Deployment.
var githubAppForbiddenEnvKeys = []string{
	"GITHUB_APP_ID",
	"GITHUB_INSTALLATION_ID",
	"GITHUB_APP_PEM",
	"GITHUB_APP_PEM_PATH",
	"REPOSITORY_RESTRICTION",
}

// TestBuildDeployment_PodSpecHasNoGitHubAppCredentials verifies that the session
// Pod manifest produced by buildDeployment never carries GitHub App credentials:
//   - no container env var named GITHUB_APP_ID / GITHUB_INSTALLATION_ID /
//     GITHUB_APP_PEM / GITHUB_APP_PEM_PATH / REPOSITORY_RESTRICTION,
//   - no EnvFromSource referencing the GitHub App auth Secret (only the non-auth
//     GitHub config Secret for GITHUB_API/GITHUB_URL may be mounted),
//   - no Volume / VolumeMount for a GitHub App private key.
//
// This is the negative test complementing the settings-level checks: even if a
// misconfigured cluster puts App keys into the auth Secret, the Pod manifest must
// not surface them.
func TestBuildDeployment_PodSpecHasNoGitHubAppCredentials(t *testing.T) {
	manager := &KubernetesSessionManager{
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
			GitHubSecretName:       "agentapi-proxy-github-session", // auth configured
			GitHubConfigSecretName: "agentapi-proxy-github-config",  // GITHUB_API/GITHUB_URL only
		},
		namespace: "test-ns",
	}
	session := NewKubernetesSession(
		"sess-no-appcreds",
		&entities.RunServerRequest{
			UserID:   "u",
			RepoInfo: &entities.RepositoryInfo{FullName: "octo/repo"},
		},
		"deploy", "svc", "pvc", "test-ns", 9000, nil, nil,
	)

	deployment, err := manager.buildDeployment(context.Background(), session, session.Request())
	if err != nil {
		t.Fatalf("buildDeployment() error = %v", err)
	}
	podSpec := deployment.Spec.Template.Spec

	// 1. No container env var carries GitHub App configuration / PEM.
	for _, c := range podSpec.Containers {
		for _, e := range c.Env {
			for _, k := range githubAppForbiddenEnvKeys {
				if e.Name == k {
					t.Fatalf("container %q env must not include %q (value=%q)", c.Name, k, e.Value)
				}
			}
		}
	}
	for _, c := range podSpec.InitContainers {
		for _, e := range c.Env {
			for _, k := range githubAppForbiddenEnvKeys {
				if e.Name == k {
					t.Fatalf("init container %q env must not include %q (value=%q)", c.Name, k, e.Value)
				}
			}
		}
	}

	// 2. No EnvFromSource may reference the GitHub App auth Secret. Only the
	//    non-auth GitHub config Secret (GITHUB_API/GITHUB_URL) is permitted.
	authSecret := manager.k8sConfig.GitHubSecretName
	for _, c := range podSpec.Containers {
		for _, ef := range c.EnvFrom {
			if ef.SecretRef != nil && ef.SecretRef.Name == authSecret {
				t.Fatalf("container %q envFrom must not reference the GitHub App auth Secret %q", c.Name, authSecret)
			}
		}
	}

	// 3. No volume / volumeMount references a GitHub App private key secret.
	for _, v := range podSpec.Volumes {
		if v.Secret != nil && strings.Contains(strings.ToLower(v.Name), "github-app") {
			t.Fatalf("pod spec must not mount a GitHub App private key volume: %+v", v)
		}
		if v.Secret != nil && v.Secret.SecretName == authSecret {
			t.Fatalf("pod spec must not mount the GitHub App auth Secret as a volume: %+v", v)
		}
	}
	for _, c := range podSpec.Containers {
		for _, vm := range c.VolumeMounts {
			if strings.Contains(strings.ToLower(vm.Name), "github-app") {
				t.Fatalf("container %q must not mount a GitHub App private key volume: %+v", c.Name, vm)
			}
		}
	}

	// 4. The broker env (when present) is delivered via the provision request, not
	//    via the Pod spec container env; verify buildEnvVars does not emit App keys.
	envVars := manager.buildEnvVars(session, session.Request())
	for _, e := range envVars {
		for _, k := range githubAppForbiddenEnvKeys {
			if e.Name == k {
				t.Fatalf("buildEnvVars must not emit %q (value=%q)", k, e.Value)
			}
		}
	}
}
