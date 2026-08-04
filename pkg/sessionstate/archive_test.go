package sessionstate

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackUnpackGitWorkspaceUsesDelta(t *testing.T) {
	srcHome, srcCwd := t.TempDir(), t.TempDir()
	runTestGit(t, srcCwd, "init")
	runTestGit(t, srcCwd, "config", "user.email", "test@example.com")
	runTestGit(t, srcCwd, "config", "user.name", "Test")
	mustWrite(t, filepath.Join(srcCwd, ".gitignore"), "ignored/\n")
	mustWrite(t, filepath.Join(srcCwd, "changed.txt"), "base\n")
	mustWrite(t, filepath.Join(srcCwd, "deleted.txt"), "delete me\n")
	runTestGit(t, srcCwd, "add", ".")
	runTestGit(t, srcCwd, "commit", "-m", "base")
	mustWrite(t, filepath.Join(srcCwd, "changed.txt"), "staged\n")
	runTestGit(t, srcCwd, "add", "changed.txt")
	mustWrite(t, filepath.Join(srcCwd, "changed.txt"), "working\n")
	if err := os.Remove(filepath.Join(srcCwd, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(srcCwd, "new.txt"), "new\n")
	mustWrite(t, filepath.Join(srcCwd, "ignored", "large.bin"), strings.Repeat("x", 1<<20))
	mustWrite(t, filepath.Join(srcCwd, "node_modules", "not-gitignored.bin"), strings.Repeat("y", 1<<20))
	id := "33333333-3333-3333-3333-333333333333"
	mustWrite(t, filepath.Join(srcCwd, ".acp-session-id"), id)
	mustWrite(t, filepath.Join(srcHome, ".claude", "projects", "p", id+".jsonl"), "chat\n")

	var archive bytes.Buffer
	if err := Pack(&archive, "claude-acp", id, srcHome, srcCwd); err != nil {
		t.Fatal(err)
	}
	if archive.Len() > 100<<10 {
		t.Fatalf("delta archive unexpectedly large: %d", archive.Len())
	}
	dstCwd := t.TempDir()
	runTestGit(t, dstCwd, "clone", "--no-local", srcCwd, ".")
	if err := Unpack(&archive, t.TempDir(), dstCwd); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dstCwd, "changed.txt"), "working\n")
	assertFile(t, filepath.Join(dstCwd, "new.txt"), "new\n")
	if _, err := os.Stat(filepath.Join(dstCwd, "deleted.txt")); !os.IsNotExist(err) {
		t.Fatalf("deleted file restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstCwd, "ignored", "large.bin")); !os.IsNotExist(err) {
		t.Fatalf("ignored file restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dstCwd, "node_modules", "not-gitignored.bin")); !os.IsNotExist(err) {
		t.Fatalf("cache file restored: %v", err)
	}
	status := runTestGit(t, dstCwd, "status", "--porcelain")
	if !strings.Contains(status, "MM changed.txt") || !strings.Contains(status, " D deleted.txt") || !strings.Contains(status, "?? new.txt") {
		t.Fatalf("unexpected restored status:\n%s", status)
	}
}

func runTestGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func TestPackUnpackClaude(t *testing.T) {
	srcHome, srcCwd := t.TempDir(), t.TempDir()
	id := "11111111-1111-1111-1111-111111111111"
	mustWrite(t, filepath.Join(srcCwd, ".acp-session-id"), id)
	mustWrite(t, filepath.Join(srcCwd, "src", "main.go"), "package main\n")
	mustWrite(t, filepath.Join(srcCwd, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(srcHome, ".claude", "projects", "project", id+".jsonl"), "main\n")
	mustWrite(t, filepath.Join(srcHome, ".claude", "projects", "project", id, "subagents", "agent-a.jsonl"), "sub\n")
	mustWrite(t, filepath.Join(srcHome, ".claude", "projects", "project", "other.jsonl"), "nope\n")
	var archive bytes.Buffer
	if err := Pack(&archive, "claude-acp", id, srcHome, srcCwd); err != nil {
		t.Fatal(err)
	}
	dstHome, dstCwd := t.TempDir(), t.TempDir()
	if err := Unpack(&archive, dstHome, dstCwd); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dstCwd, ".acp-session-id"), id)
	assertFile(t, filepath.Join(dstCwd, "src", "main.go"), "package main\n")
	assertFile(t, filepath.Join(dstCwd, ".git", "HEAD"), "ref: refs/heads/main\n")
	assertFile(t, filepath.Join(dstHome, ".claude", "projects", "project", id+".jsonl"), "main\n")
	if _, err := os.Stat(filepath.Join(dstHome, ".claude", "projects", "project", "other.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("unrelated transcript restored: %v", err)
	}
}

func TestPackUnpackCodex(t *testing.T) {
	srcHome, srcCwd := t.TempDir(), t.TempDir()
	id := "22222222-2222-2222-2222-222222222222"
	mustWrite(t, filepath.Join(srcCwd, ".acp-session-id"), id)
	executable := filepath.Join(srcCwd, "scripts", "run.sh")
	mustWrite(t, executable, "#!/bin/sh\n")
	if err := os.Chmod(executable, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(".codex", "sessions", "2026", "08", "03", "rollout-x-"+id+".jsonl")
	mustWrite(t, filepath.Join(srcHome, rollout), "rollout\n")
	mustWrite(t, filepath.Join(srcHome, ".codex", "state_5.sqlite"), "db")
	mustWrite(t, filepath.Join(srcHome, ".codex", "state_5.sqlite-wal"), "wal")
	mustWrite(t, filepath.Join(srcHome, ".codex", "session_index.jsonl"), "index")
	var archive bytes.Buffer
	if err := Pack(&archive, "codex-acp", id, srcHome, srcCwd); err != nil {
		t.Fatal(err)
	}
	dstHome, dstCwd := t.TempDir(), t.TempDir()
	if err := Unpack(&archive, dstHome, dstCwd); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(dstHome, rollout), "rollout\n")
	assertFile(t, filepath.Join(dstHome, ".codex", "state_5.sqlite"), "db")
	assertFile(t, filepath.Join(dstHome, ".codex", "state_5.sqlite-wal"), "wal")
	assertFile(t, filepath.Join(dstHome, ".codex", "session_index.jsonl"), "index")
	assertFile(t, filepath.Join(dstCwd, "scripts", "run.sh"), "#!/bin/sh\n")
	info, err := os.Stat(filepath.Join(dstCwd, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("restored executable mode = %v", info.Mode().Perm())
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}
