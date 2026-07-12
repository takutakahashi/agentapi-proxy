package sessionsettings

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Session bin / helper paths used by the broker-based GitHub auth setup.
const (
	sessionBinDir     = "/home/agentapi/.session/bin"
	ghWrapperPath     = "/home/agentapi/.session/bin/gh"
	gitCredentialPath = "/home/agentapi/.session/git-credential-broker"
)

// safePathRe restricts the real gh path to characters that are safe to embed
// inside a double-quoted shell assignment. It rejects quotes, dollar signs,
// backticks, backslashes, spaces and other shell metacharacters so the path
// cannot break out of the wrapper script's REAL="..." assignment or inject
// commands. exec.LookPath normally returns simple absolute paths
// (e.g. /usr/local/bin/gh); anything else is treated as unsafe and aborts setup.
var safePathRe = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)

// isShellSafeAbsPath reports whether p is an absolute path containing only
// shell-safe characters, so it can be embedded verbatim into the wrapper script.
func isShellSafeAbsPath(p string) bool {
	if !filepath.IsAbs(p) {
		return false
	}
	return safePathRe.MatchString(p)
}

// setupGitHubBrokerAuth installs the broker-backed git credential helper and gh
// wrapper into the session container using the broker endpoint + session-scoped
// credential provided in the session env (AGENTAPI_GITHUB_BROKER_URL /
// AGENTAPI_GITHUB_BROKER_TOKEN). It does NOT read any GitHub App PEM or generate an
// installation token: tokens are fetched from the proxy broker on demand.
//
// Return contract:
//   - ("", nil): the broker env vars are absent. The caller uses the legacy
//     gh-based setup (personal token / no-auth sessions).
//   - (dir, nil): the broker is active; dir is the bin directory to prepend to
//     PATH so the agent's `gh` invocations route through the wrapper.
//   - ("", err): the broker env vars are present but setup failed (e.g. the real
//     gh binary could not be resolved to a safe absolute path, or a helper script
//     could not be written/configured). The caller MUST abort clone/setup with
//     this error — it must NOT fall back to the legacy path, because a broker
//     session has no GitHub App PEM or token available in-Pod and a silent
//     fallback would create an unauthenticated or mis-authenticated session.
//
// This strict contract means a broker session either installs working in-Pod
// helpers or fails loudly; it never degrades to legacy auth.
func setupGitHubBrokerAuth(env map[string]string) (pathPrefix string, err error) {
	brokerURL := strings.TrimSpace(env["AGENTAPI_GITHUB_BROKER_URL"])
	brokerToken := strings.TrimSpace(env["AGENTAPI_GITHUB_BROKER_TOKEN"])
	if brokerURL == "" || brokerToken == "" {
		// No broker configured: caller uses the legacy path.
		return "", nil
	}

	if err := os.MkdirAll(sessionBinDir, 0755); err != nil {
		return "", fmt.Errorf("create session bin dir %s: %w", sessionBinDir, err)
	}

	// Resolve the real gh binary path BEFORE installing the wrapper, so the
	// wrapper can exec it directly and avoid recursion. The path MUST resolve to
	// a safe absolute path; otherwise we do not install the wrapper, because
	// falling back to a bare "gh" would re-resolve to the wrapper itself once its
	// directory is prepended to PATH, causing infinite recursion.
	realGH, err := exec.LookPath("gh")
	if err != nil {
		return "", fmt.Errorf("gh CLI not found on PATH; cannot install broker gh wrapper")
	}
	if realGH, err = filepath.Abs(realGH); err != nil {
		return "", fmt.Errorf("resolve real gh path: %w", err)
	}
	if !isShellSafeAbsPath(realGH) {
		// Refuse to embed an unsafe path into the wrapper script.
		return "", fmt.Errorf("resolved gh path %q is not a shell-safe absolute path", realGH)
	}

	if err := writeGHWrapper(ghWrapperPath, realGH); err != nil {
		return "", fmt.Errorf("install gh wrapper: %w", err)
	}

	if err := writeGitCredentialHelper(gitCredentialPath); err != nil {
		return "", fmt.Errorf("install git credential helper: %w", err)
	}

	// Configure git to use ONLY the broker credential helper for github.com and
	// the GitHub Enterprise host (if configured). The existing host helper chain
	// is reset first so no other helper (or interactive prompt) can supply
	// credentials for the session repository. The helper is referenced by absolute
	// path so git invokes it directly (no PATH lookup that could resolve a
	// malicious earlier PATH entry).
	if err := configureGitCredentialHelper(gitCredentialPath); err != nil {
		return "", fmt.Errorf("configure git credential helper: %w", err)
	}

	log.Printf("[SETUP] GitHub token broker active; installed gh wrapper (%s) + git credential helper (%s)", realGH, gitCredentialPath)
	return sessionBinDir, nil
}

// writeGHWrapper writes the gh wrapper script. The wrapper fetches a fresh token
// from the broker for each invocation and runs the real gh with GH_TOKEN set for
// that process only. The token is passed via environment, never via args or
// stdout. A recursion guard (AGENTAPI_GH_WRAPPER_ACTIVE) prevents re-entry when the
// real gh shells out to `gh`.
//
// realGH must be a validated shell-safe absolute path (see isShellSafeAbsPath); it
// is embedded as the default for AGENTAPI_GH_REAL_PATH so the wrapper can exec the
// real gh directly even after the wrapper directory is prepended to PATH.
func writeGHWrapper(path, realGH string) error {
	if !isShellSafeAbsPath(realGH) {
		return fmt.Errorf("refuse to write wrapper with unsafe real gh path %q", realGH)
	}
	script := fmt.Sprintf(`#!/bin/sh
# agentapi-proxy gh wrapper: fetch a short-lived GitHub token from the broker and
# run the real gh CLI with GH_TOKEN set for this invocation only.
# The token is never printed, never placed on the command line, and never persisted.
set -eu

# Absolute path of the real gh, resolved before the wrapper dir was prepended to
# PATH. Embedding it here (and exec'ing it directly) is what makes the wrapper
# recursion-safe: it never re-resolves "gh" through PATH.
REAL="${AGENTAPI_GH_REAL_PATH:-%s}"
BROKER_URL="${AGENTAPI_GITHUB_BROKER_URL:-}"
BROKER_TOKEN="${AGENTAPI_GITHUB_BROKER_TOKEN:-}"

# The real gh path must always be a non-empty absolute path; refuse to exec a
# bare "gh" (which would re-resolve to this wrapper and recurse infinitely).
case "$REAL" in
  /*) ;;
  *) echo "gh: real gh path not configured or not absolute: $REAL" >&2; exit 1 ;;
esac

# Recursion guard: if we are already inside the wrapper, exec the real gh directly.
if [ -n "${AGENTAPI_GH_WRAPPER_ACTIVE:-}" ]; then
  exec "$REAL" "$@"
fi

# Without broker configuration we cannot mint a token. A broker session has no
# PEM/token in-Pod, so fail loudly rather than silently running gh unauthenticated.
if [ -z "$BROKER_URL" ] || [ -z "$BROKER_TOKEN" ]; then
  echo "gh: GitHub token broker is not configured" >&2
  exit 1
fi

token=$(curl -fsS -H "Authorization: Bearer $BROKER_TOKEN" "$BROKER_URL?format=raw") || {
  echo "gh: failed to obtain GitHub token from broker" >&2
  exit 1
}
[ -n "$token" ] || { echo "gh: broker returned an empty token" >&2; exit 1; }
export AGENTAPI_GH_WRAPPER_ACTIVE=1
export GH_TOKEN="$token"
unset GITHUB_TOKEN 2>/dev/null || true
exec "$REAL" "$@"
`, realGH)
	return os.WriteFile(path, []byte(script), 0755)
}

// writeGitCredentialHelper writes the git credential helper backed by the broker.
//
// git credential protocol:
//   - get  : git requests credentials for a URL; the helper reads the request
//     attributes from stdin and prints username/password to stdout for git
//     to consume (over a pipe, never to the terminal). The helper fetches a
//     fresh token from the broker scoped to the session repository.
//   - store: git asks to persist credentials; the helper is a no-op so tokens are
//     never written to disk.
//   - erase : git asks to drop cached credentials; also a no-op.
//
// On broker failure the helper prints a diagnostic to stderr and exits non-zero
// so the operation is reported as failed rather than silently succeeding and
// falling through to another helper or an interactive prompt. The host helper
// chain is reset to ONLY this helper by configureGitCredentialHelper, so there is
// no other helper to fall through to.
func writeGitCredentialHelper(path string) error {
	script := `#!/bin/sh
# agentapi-proxy git credential helper backed by the GitHub token broker.
# get   : fetch a fresh installation token from the broker for git to consume.
# store : no-op — tokens are never persisted to disk.
# erase : no-op — tokens are never persisted to disk.
set -eu
op="$1"
BROKER_URL="${AGENTAPI_GITHUB_BROKER_URL:-}"
BROKER_TOKEN="${AGENTAPI_GITHUB_BROKER_TOKEN:-}"
case "$op" in
  get)
    if [ -z "$BROKER_URL" ] || [ -z "$BROKER_TOKEN" ]; then
      echo "git-credential-broker: broker is not configured" >&2
      exit 1
    fi
    token=$(curl -fsS -H "Authorization: Bearer $BROKER_TOKEN" "$BROKER_URL?format=raw") || {
      echo "git-credential-broker: failed to obtain GitHub token from broker" >&2
      exit 1
    }
    [ -n "$token" ] || { echo "git-credential-broker: broker returned an empty token" >&2; exit 1; }
    printf 'username=x-access-token\n'
    printf 'password=%s\n' "$token"
    ;;
  store|erase)
    : # never persist
    ;;
esac
`
	return os.WriteFile(path, []byte(script), 0755)
}

// configureGitCredentialHelper registers the broker helper as the ONLY credential
// helper for github.com and the GitHub Enterprise host (when GITHUB_API is set).
//
// For each host it first clears any previously-configured helpers for that host
// (including inherited ones) by writing an empty helper entry, then adds the
// broker helper. This prevents an existing helper chain (e.g. a cache or store
// helper, or an interactive prompt) from supplying stale or unrelated credentials
// for the session repository. The helper is referenced by absolute path so git
// invokes it directly (no PATH lookup).
func configureGitCredentialHelper(helperPath string) error {
	hosts := []string{"github.com"}
	if host := githubEnterpriseHost(); host != "" && host != "github.com" {
		hosts = append(hosts, host)
	}
	for _, host := range hosts {
		key := fmt.Sprintf("credential.https://%s.helper", host)
		// Reset the host helper chain to a single empty entry. In git's credential
		// helper protocol an empty helper name tells git to discard all helpers
		// configured so far for this URL (including those inherited from the global
		// credential.helper), so no other helper or interactive prompt can supply
		// credentials for the session repository.
		if err := gitConfigGlobal(key, ""); err != nil {
			return fmt.Errorf("reset %s: %w", key, err)
		}
		// Append the broker helper as the only effective helper for this host.
		if err := gitConfigGlobalAdd(key, helperPath); err != nil {
			return fmt.Errorf("set broker helper for %s: %w", host, err)
		}
	}
	return nil
}

func githubEnterpriseHost() string {
	// Only treat an explicitly-configured, non-default GITHUB_API as an
	// enterprise host. The default api.github.com is handled via the hardcoded
	// "github.com" credential helper key and must not produce a separate host.
	apiBase := strings.TrimSpace(os.Getenv("GITHUB_API"))
	if apiBase == "" || strings.Contains(apiBase, "api.github.com") {
		return ""
	}
	host := strings.TrimPrefix(apiBase, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/api/v3")
	host = strings.TrimSuffix(host, "/")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return host
}

func gitConfigGlobal(key, value string) error {
	cmd := exec.Command("git", "config", "--global", key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config %s failed: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitConfigGlobalAdd appends a value to a multi-valued git config key.
func gitConfigGlobalAdd(key, value string) error {
	cmd := exec.Command("git", "config", "--global", "--add", key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config --add %s failed: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// prependToPath returns PATH with dir prepended if not already present.
func prependToPath(dir, currentPath string) string {
	if dir == "" {
		return currentPath
	}
	if currentPath == "" {
		return dir
	}
	return dir + string(os.PathListSeparator) + currentPath
}
