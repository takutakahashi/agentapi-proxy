package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionStateCWDPrefersCloneDirectory(t *testing.T) {
	hookCWD := t.TempDir()
	cloneDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(cloneDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(hookCWD)
	t.Setenv("AGENTAPI_CLONE_DIR", cloneDir)

	got, err := sessionStateCWD()
	if err != nil {
		t.Fatal(err)
	}
	if got != cloneDir {
		t.Fatalf("sessionStateCWD() = %q, want %q", got, cloneDir)
	}
}

func TestSessionStateCWDFallsBackOutsideGitClone(t *testing.T) {
	hookCWD := t.TempDir()
	t.Chdir(hookCWD)
	t.Setenv("AGENTAPI_CLONE_DIR", t.TempDir())

	got, err := sessionStateCWD()
	if err != nil {
		t.Fatal(err)
	}
	if got != hookCWD {
		t.Fatalf("sessionStateCWD() = %q, want %q", got, hookCWD)
	}
}
