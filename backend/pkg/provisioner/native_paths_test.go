package provisioner

import (
	"path/filepath"
	"testing"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func TestNormalizeNativeSettingsRemapsPathsAndDropsContainerTLS(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	repo := filepath.Join(root, "workdir", "repo")
	t.Setenv("AGENTAPI_NATIVE_SESSION_ROOT", root)
	t.Setenv("AGENTAPI_WORKDIR", filepath.Join(root, "workdir"))
	t.Setenv("AGENTAPI_REPO_DIR", repo)
	t.Setenv("AGENTAPI_PORT", "41000")

	oldRuntimeHome, oldRepo := runtimeHome, workdirRepoPath
	runtimeHome, workdirRepoPath = home, repo
	t.Cleanup(func() {
		runtimeHome, workdirRepoPath = oldRuntimeHome, oldRepo
	})

	settings := &sessionsettings.SessionSettings{
		Env: map[string]string{
			"SSL_CERT_FILE":       "/tmp/scia-ca-bundle.pem",
			"NODE_EXTRA_CA_CERTS": "/etc/scia/ca.pem",
			"HTTPS_PROXY":         "http://127.0.0.1:18081",
		},
		Repository: &sessionsettings.RepositoryConfig{CloneDir: "/home/agentapi/workdir/repo"},
		Files:      []sessionsettings.ManagedFile{{Path: "/home/agentapi/.codex/auth.json"}},
	}

	normalizeNativeSettings(settings)

	if settings.Repository.CloneDir != filepath.Join(home, "workdir", "repo") {
		t.Fatalf("clone dir = %q", settings.Repository.CloneDir)
	}
	if settings.Files[0].Path != filepath.Join(home, ".codex", "auth.json") {
		t.Fatalf("managed file path = %q", settings.Files[0].Path)
	}
	for _, key := range []string{"SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "HTTPS_PROXY"} {
		if _, ok := settings.Env[key]; ok {
			t.Fatalf("container-only environment %s was retained", key)
		}
	}
	if settings.Env["HOME"] != home || settings.Env["AGENTAPI_PORT"] != "41000" {
		t.Fatalf("native environment was not applied: %#v", settings.Env)
	}
}
