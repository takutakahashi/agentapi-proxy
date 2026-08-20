package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNativeSandboxDefinitions(t *testing.T) {
	root := t.TempDir()
	hostHome := filepath.Join(root, "home")
	sessionsRoot := filepath.Join(hostHome, "native", "sessions")
	sessionRoot := filepath.Join(sessionsRoot, "one")
	binaryPath := filepath.Join(hostHome, "native", "bin", "agentapi-proxy")
	for _, path := range []string{hostHome, sessionsRoot, sessionRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	definitions, err := nativeSandboxDefinitions(sessionRoot, hostHome, sessionsRoot, binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalHostHome, _ := filepath.EvalSymlinks(hostHome)
	canonicalSessionsRoot, _ := filepath.EvalSymlinks(sessionsRoot)
	canonicalSessionRoot, _ := filepath.EvalSymlinks(sessionRoot)
	canonicalBinaryPath, _ := filepath.EvalSymlinks(binaryPath)
	want := []string{
		"-DSESSION_ROOT=" + canonicalSessionRoot,
		"-DHOST_HOME=" + canonicalHostHome,
		"-DSESSIONS_ROOT=" + canonicalSessionsRoot,
		"-DBINARY_PATH=" + canonicalBinaryPath,
	}
	if len(definitions) != len(want) {
		t.Fatalf("definitions = %#v", definitions)
	}
	for i := range want {
		if definitions[i] != want[i] {
			t.Fatalf("definitions[%d] = %q, want %q", i, definitions[i], want[i])
		}
	}
}

func TestNativeSandboxDefinitionsRejectRelativePath(t *testing.T) {
	if _, err := nativeSandboxDefinitions("relative", "also-relative", "sessions", "binary"); err == nil {
		t.Fatal("expected relative path to be rejected")
	}
}

func TestNativeSandboxDefinitionsRejectSessionOutsideState(t *testing.T) {
	root := t.TempDir()
	hostHome := filepath.Join(root, "home")
	sessionsRoot := filepath.Join(root, "sessions")
	sessionRoot := filepath.Join(root, "outside")
	binaryPath := filepath.Join(root, "binary")
	for _, path := range []string{hostHome, sessionsRoot, sessionRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeSandboxDefinitions(sessionRoot, hostHome, sessionsRoot, binaryPath); err == nil {
		t.Fatal("expected session root outside sessions directory to be rejected")
	}
}

func TestNativeProvisionerCommandWithoutSandbox(t *testing.T) {
	m := &NativeSessionManager{binaryPath: "/opt/agentapi-proxy"}
	cmd, err := m.newProvisionerCommand(context.Background(), "/state/sessions/one", filepath.Join("/state/sessions/one", "runtime"), 4321)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/opt/agentapi-proxy" {
		t.Fatalf("command path = %q", cmd.Path)
	}
}

func TestNativeFilesystemSandboxPolicyOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sandbox-exec is only available on macOS")
	}
	root := t.TempDir()
	hostHome := filepath.Join(root, "home")
	sessionsRoot := filepath.Join(hostHome, "native", "sessions")
	sessionRoot := filepath.Join(sessionsRoot, "one")
	runtimeDir := filepath.Join(sessionRoot, "runtime")
	binaryPath := filepath.Join(hostHome, "native", "bin", "agentapi-proxy")
	for _, path := range []string{hostHome, sessionsRoot, sessionRoot, runtimeDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(hostHome, "secret")
	allowedPath := filepath.Join(sessionRoot, "allowed")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowedPath, []byte("allowed"), 0o600); err != nil {
		t.Fatal(err)
	}
	profilePath := filepath.Join(runtimeDir, "filesystem-sandbox.sb")
	if err := os.WriteFile(profilePath, []byte(nativeFilesystemSandboxPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	definitions, err := nativeSandboxDefinitions(sessionRoot, hostHome, sessionsRoot, binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-f", profilePath}, definitions...)
	if output, err := exec.Command(nativeSandboxExecPath, append(args, "--", "/bin/cat", allowedPath)...).CombinedOutput(); err != nil {
		t.Fatalf("read session file: %v: %s", err, output)
	}
	if output, err := exec.Command(nativeSandboxExecPath, append(args, "--", "/bin/cat", binaryPath)...).CombinedOutput(); err != nil {
		t.Fatalf("read daemon binary: %v: %s", err, output)
	}
	if err := exec.Command(nativeSandboxExecPath, append(args, "--", "/bin/cat", secretPath)...).Run(); err == nil {
		t.Fatal("expected host home file read to be denied")
	}
	if err := exec.Command(nativeSandboxExecPath, append(args, "--", "/usr/bin/touch", binaryPath)...).Run(); err == nil {
		t.Fatal("expected daemon binary write to be denied")
	}
}
