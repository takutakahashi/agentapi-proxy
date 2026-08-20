package sessionstate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const maxRestoredFileBytes = 1 << 30

// Pack writes the ACP state and workspace files required to resume a session.
func Pack(w io.Writer, agentType, sessionID, home, cwd string) error {
	if sessionID == "" {
		return fmt.Errorf("ACP session id is empty")
	}
	zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(2))
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	add := func(path, name string) error {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(name)
		h.Mode = int64(info.Mode().Perm())
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if err := add(filepath.Join(cwd, ".acp-session-id"), "cwd/.acp-session-id"); err != nil {
		return err
	}
	gitSnapshot, err := packGitWorkspace(tw, cwd, add)
	if err != nil {
		return err
	}
	// Non-Git workspaces have no reproducible base, so preserve them in full.
	if !gitSnapshot {
		if err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if info.IsDir() || !info.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(cwd, path)
			if err != nil {
				return err
			}
			if rel == ".acp-session-id" {
				return nil
			}
			return add(path, filepath.Join("cwd", rel))
		}); err != nil {
			return err
		}
	}
	var root string
	switch agentType {
	case "claude-acp":
		root = filepath.Join(home, ".claude", "projects")
	case "codex-acp":
		root = filepath.Join(home, ".codex", "sessions")
	default:
		return fmt.Errorf("unsupported ACP agent type %q", agentType)
	}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		name := info.Name()
		include := agentType == "claude-acp" && (name == sessionID+".jsonl" || strings.Contains(filepath.ToSlash(path), "/"+sessionID+"/"))
		include = include || agentType == "codex-acp" && strings.Contains(name, sessionID) && strings.HasSuffix(name, ".jsonl")
		if !include {
			return nil
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		return add(path, filepath.Join("home", rel))
	})
	if err != nil {
		return err
	}
	if agentType == "codex-acp" {
		// Current Codex app-server resolves thread/resume through its local index.
		// Keep the SQLite database and WAL pair together with the rollout.
		codexHome := filepath.Join(home, ".codex")
		entries, readErr := os.ReadDir(codexHome)
		if readErr != nil && !os.IsNotExist(readErr) {
			return readErr
		}
		for _, entry := range entries {
			name := entry.Name()
			if name != "session_index.jsonl" && (!strings.HasPrefix(name, "state_") || !strings.Contains(name, ".sqlite")) {
				continue
			}
			if err := add(filepath.Join(codexHome, name), filepath.Join("home", ".codex", name)); err != nil {
				return err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

type gitManifest struct {
	Version int    `json:"version"`
	Base    string `json:"base"`
	Branch  string `json:"branch,omitempty"`
}

func gitOutput(cwd string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func addBytes(tw *tar.Writer, name string, data []byte, mode int64) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// packGitWorkspace stores only changes relative to Git's content-addressed
// HEAD. The repository clone supplies unchanged objects during restore.
func packGitWorkspace(tw *tar.Writer, cwd string, add func(string, string) error) (bool, error) {
	if info, err := os.Stat(filepath.Join(cwd, ".git")); err != nil || !info.IsDir() {
		return false, nil
	}
	base, err := gitOutput(cwd, "rev-parse", "HEAD")
	if err != nil {
		return false, nil
	}
	branch, _ := gitOutput(cwd, "symbolic-ref", "--quiet", "--short", "HEAD")
	manifest, err := json.Marshal(gitManifest{Version: 1, Base: strings.TrimSpace(string(base)), Branch: strings.TrimSpace(string(branch))})
	if err != nil {
		return false, err
	}
	if err := addBytes(tw, "git/manifest.json", manifest, 0o600); err != nil {
		return false, err
	}
	staged, err := gitOutput(cwd, "diff", "--cached", "--binary", "--full-index", "HEAD")
	if err != nil {
		return false, err
	}
	worktree, err := gitOutput(cwd, "diff", "--binary", "--full-index", "HEAD")
	if err != nil {
		return false, err
	}
	if err := addBytes(tw, "git/staged.patch", staged, 0o600); err != nil {
		return false, err
	}
	if err := addBytes(tw, "git/worktree.patch", worktree, 0o600); err != nil {
		return false, err
	}
	untracked, err := gitOutput(cwd, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return false, err
	}
	for _, raw := range bytes.Split(untracked, []byte{0}) {
		rel := string(raw)
		if rel == "" || rel == ".acp-session-id" || excludedUntrackedPath(rel) {
			continue
		}
		if err := add(filepath.Join(cwd, filepath.FromSlash(rel)), filepath.Join("cwd", filepath.FromSlash(rel))); err != nil {
			return false, err
		}
	}
	return true, nil
}

func excludedUntrackedPath(path string) bool {
	excluded := map[string]bool{
		"node_modules": true, ".cache": true, ".venv": true, "venv": true,
		"dist": true, "build": true, "target": true, ".next": true,
		".turbo": true, ".pytest_cache": true, "__pycache__": true, "coverage": true,
	}
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if excluded[component] {
			return true
		}
	}
	return false
}

// Unpack restores a trusted backend snapshot without permitting path traversal.
func Unpack(r io.Reader, home, cwd string) error {
	buffered := newMagicReader(r)
	var archiveReader io.Reader
	var closeReader io.Closer
	if bytes.Equal(buffered.magic, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(buffered)
		if err != nil {
			return err
		}
		archiveReader, closeReader = gz, gz
	} else {
		zr, err := zstd.NewReader(buffered)
		if err != nil {
			return err
		}
		archiveReader, closeReader = zr, zr.IOReadCloser()
	}
	defer func() { _ = closeReader.Close() }()
	tr := tar.NewReader(archiveReader)
	var manifest *gitManifest
	var stagedPatch, worktreePatch []byte
	for {
		h, err := tr.Next()
		if err == io.EOF {
			if manifest != nil {
				return restoreGitWorkspace(cwd, *manifest, stagedPatch, worktreePatch)
			}
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("unsupported archive entry %q", h.Name)
		}
		clean := filepath.Clean(filepath.FromSlash(h.Name))
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("unsafe archive entry %q", h.Name)
		}
		if clean == filepath.FromSlash("git/manifest.json") || clean == filepath.FromSlash("git/staged.patch") || clean == filepath.FromSlash("git/worktree.patch") {
			data, readErr := io.ReadAll(io.LimitReader(tr, maxRestoredFileBytes+1))
			if readErr != nil || len(data) > maxRestoredFileBytes {
				return fmt.Errorf("invalid git snapshot entry %q", h.Name)
			}
			switch clean {
			case filepath.FromSlash("git/manifest.json"):
				var value gitManifest
				if err := json.Unmarshal(data, &value); err != nil {
					return err
				}
				manifest = &value
			case filepath.FromSlash("git/staged.patch"):
				stagedPatch = data
			case filepath.FromSlash("git/worktree.patch"):
				worktreePatch = data
			}
			continue
		}
		var target string
		switch {
		case strings.HasPrefix(clean, "cwd"+string(filepath.Separator)):
			target = filepath.Join(cwd, strings.TrimPrefix(clean, "cwd"+string(filepath.Separator)))
		case strings.HasPrefix(clean, "home"+string(filepath.Separator)):
			target = filepath.Join(home, strings.TrimPrefix(clean, "home"+string(filepath.Separator)))
		default:
			return fmt.Errorf("unexpected archive entry %q", h.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, io.LimitReader(tr, maxRestoredFileBytes))
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := os.Chmod(target, os.FileMode(h.Mode)&0o777); err != nil {
			return err
		}
	}
}

type magicReader struct {
	magic []byte
	r     io.Reader
}

func newMagicReader(r io.Reader) *magicReader {
	magic := make([]byte, 2)
	n, _ := io.ReadFull(r, magic)
	magic = magic[:n]
	return &magicReader{magic: magic, r: io.MultiReader(bytes.NewReader(magic), r)}
}
func (r *magicReader) Read(p []byte) (int, error) { return r.r.Read(p) }

func restoreGitWorkspace(cwd string, manifest gitManifest, staged, worktree []byte) error {
	if manifest.Version != 1 || manifest.Base == "" {
		return fmt.Errorf("unsupported git snapshot manifest")
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
		return fmt.Errorf("git workspace base is unavailable: %w", err)
	}
	if err := runGit(cwd, nil, "fetch", "--depth=1", "origin", manifest.Base); err != nil {
		// The shallow clone often already contains the exact object.
		if _, checkErr := gitOutput(cwd, "cat-file", "-e", manifest.Base+"^{commit}"); checkErr != nil {
			return err
		}
	}
	if err := runGit(cwd, nil, "checkout", "--force", "--detach", manifest.Base); err != nil {
		return err
	}
	if manifest.Branch != "" {
		if err := runGit(cwd, nil, "check-ref-format", "--branch", manifest.Branch); err != nil {
			return fmt.Errorf("invalid git snapshot branch %q: %w", manifest.Branch, err)
		}
		// Recreate the local branch at the captured commit. This also handles
		// branches whose tip only existed in the suspended workspace.
		if err := runGit(cwd, nil, "checkout", "--force", "-B", manifest.Branch, manifest.Base); err != nil {
			return err
		}
	}
	if len(staged) > 0 {
		if err := runGit(cwd, staged, "apply", "--cached", "--binary", "-"); err != nil {
			return err
		}
	}
	if len(worktree) > 0 {
		if err := runGit(cwd, worktree, "apply", "--binary", "-"); err != nil {
			return err
		}
	}
	return nil
}

func runGit(cwd string, stdin []byte, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
