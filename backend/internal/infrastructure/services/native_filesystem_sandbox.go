package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const nativeSandboxExecPath = "/usr/bin/sandbox-exec"

const nativeFilesystemSandboxPolicy = `(version 1)

; Keep macOS and Xcode services available, but prevent the session from reading
; or modifying the host user's files outside its own session root.
(allow default)

(deny file-read*
  (require-all
    (subpath (param "HOST_HOME"))
    (require-not (literal (param "SESSION_ROOT")))
    (require-not (subpath (param "SESSION_ROOT")))
    (require-not (literal (param "BINARY_PATH")))))

(deny file-write*
  (require-all
    (subpath (param "HOST_HOME"))
    (require-not (literal (param "SESSION_ROOT")))
    (require-not (subpath (param "SESSION_ROOT")))))

; Protect sibling native sessions when a custom state directory is outside HOME.
(deny file-read*
  (require-all
    (subpath (param "SESSIONS_ROOT"))
    (require-not (literal (param "SESSION_ROOT")))
    (require-not (subpath (param "SESSION_ROOT")))
    (require-not (literal (param "BINARY_PATH")))))

(deny file-write*
  (require-all
    (subpath (param "SESSIONS_ROOT"))
    (require-not (literal (param "SESSION_ROOT")))
    (require-not (subpath (param "SESSION_ROOT")))))
`

func (m *NativeSessionManager) newProvisionerCommand(ctx context.Context, root, runtimeDir string, provisionerPort int) (*exec.Cmd, error) {
	args := []string{"agent-provisioner", "--port", strconv.Itoa(provisionerPort)}
	if !m.filesystemSandbox {
		return exec.CommandContext(ctx, m.binaryPath, args...), nil
	}

	profilePath := filepath.Join(runtimeDir, "filesystem-sandbox.sb")
	if err := os.WriteFile(profilePath, []byte(nativeFilesystemSandboxPolicy), 0o600); err != nil {
		return nil, fmt.Errorf("write native filesystem sandbox policy: %w", err)
	}
	hostHome, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve host home for native filesystem sandbox: %w", err)
	}
	definitions, err := nativeSandboxDefinitions(root, hostHome, filepath.Join(m.stateDir, "sessions"), m.binaryPath)
	if err != nil {
		return nil, err
	}
	if err := validateNativeSandboxProfile(profilePath, definitions); err != nil {
		return nil, err
	}

	sandboxArgs := []string{"-f", profilePath}
	sandboxArgs = append(sandboxArgs, definitions...)
	sandboxArgs = append(sandboxArgs, "--", m.binaryPath)
	sandboxArgs = append(sandboxArgs, args...)
	return exec.CommandContext(ctx, nativeSandboxExecPath, sandboxArgs...), nil
}

func nativeSandboxDefinitions(sessionRoot, hostHome, sessionsRoot, binaryPath string) ([]string, error) {
	paths := map[string]string{
		"SESSION_ROOT":  sessionRoot,
		"HOST_HOME":     hostHome,
		"SESSIONS_ROOT": sessionsRoot,
		"BINARY_PATH":   binaryPath,
	}
	definitions := make([]string, 0, len(paths))
	for _, name := range []string{"SESSION_ROOT", "HOST_HOME", "SESSIONS_ROOT", "BINARY_PATH"} {
		path, err := filepath.EvalSymlinks(filepath.Clean(paths[name]))
		if err != nil {
			return nil, fmt.Errorf("resolve native filesystem sandbox %s: %w", name, err)
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("native filesystem sandbox %s must be absolute: %s", name, path)
		}
		paths[name] = path
		definitions = append(definitions, "-D"+name+"="+path)
	}
	if !strings.HasPrefix(paths["SESSION_ROOT"], paths["SESSIONS_ROOT"]+string(filepath.Separator)) {
		return nil, fmt.Errorf("native filesystem sandbox session root %s is outside %s", paths["SESSION_ROOT"], paths["SESSIONS_ROOT"])
	}
	return definitions, nil
}

func validateNativeSandboxProfile(profilePath string, definitions []string) error {
	args := []string{"-f", profilePath}
	args = append(args, definitions...)
	args = append(args, "--", "/usr/bin/true")
	output, err := exec.Command(nativeSandboxExecPath, args...).CombinedOutput()
	if err != nil {
		message := string(output)
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("validate native filesystem sandbox policy: %w", errors.New(message))
	}
	return nil
}
