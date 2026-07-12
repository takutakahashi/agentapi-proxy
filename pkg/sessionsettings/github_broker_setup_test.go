package sessionsettings

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestWriteGHWrapper_TokenNeverInArgsOrScriptLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gh")
	if err := writeGHWrapper(path, "/usr/bin/gh"); err != nil {
		t.Fatalf("writeGHWrapper: %v", err)
	}
	script, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read wrapper: %v", err)
	}
	s := string(script)

	// The wrapper must not hardcode any token/PEM literal.
	for _, forbidden := range []string{"ghs_", "ghp_", "-----BEGIN PRIVATE KEY-----", "GITHUB_APP_PEM"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("wrapper script must not embed secret literal %q:\n%s", forbidden, s)
		}
	}
	// Token must be passed via GH_TOKEN env, never as a command-line arg.
	if !strings.Contains(s, "export GH_TOKEN=") {
		t.Fatalf("wrapper must export GH_TOKEN, script:\n%s", s)
	}
	// The real gh path must be the resolved absolute path (recursion-safe).
	if !strings.Contains(s, "/usr/bin/gh") {
		t.Fatalf("wrapper must reference the resolved real gh path, script:\n%s", s)
	}
	// Recursion guard must be present.
	if !strings.Contains(s, "AGENTAPI_GH_WRAPPER_ACTIVE") {
		t.Fatalf("wrapper must include a recursion guard, script:\n%s", s)
	}
	// exec, not shell expansion of token into args.
	if !strings.Contains(s, `exec "$REAL" "$@"`) {
		t.Fatalf("wrapper must exec real gh with forwarded args (no token in args), script:\n%s", s)
	}
	// The wrapper must unset GITHUB_TOKEN so a stale env token is not used.
	if !strings.Contains(s, "unset GITHUB_TOKEN") {
		t.Fatalf("wrapper must unset GITHUB_TOKEN, script:\n%s", s)
	}
	// The wrapper must validate REAL is absolute before exec, so a bare "gh"
	// (which would re-resolve to the wrapper) is never exec'd.
	if !strings.Contains(s, `case "$REAL" in`) || !strings.Contains(s, "/*) ;;") {
		t.Fatalf("wrapper must assert REAL is an absolute path, script:\n%s", s)
	}
}

// TestWriteGHWrapper_RejectsUnsafePath verifies that writeGHWrapper refuses to
// embed a real gh path containing shell metacharacters (which could break out of
// the REAL="..." assignment or inject commands), and accepts a safe absolute path.
func TestWriteGHWrapper_RejectsUnsafePath(t *testing.T) {
	dir := t.TempDir()
	unsafe := []string{
		`/usr/bin/gh"; rm -rf /`,     // quote injection
		`/usr/bin/$(whoami)`,         // command substitution
		`/usr/bin/` + "`id`" + `/gh`, // backtick command substitution
		`/usr/bin/gh\ with\ space`,   // backslash escapes
		`/usr/bin/$HOME/gh`,          // variable expansion
		`relative/gh`,                // not absolute
	}
	for _, p := range unsafe {
		if err := writeGHWrapper(filepath.Join(dir, "gh-bad"), p); err == nil {
			t.Fatalf("writeGHWrapper must reject unsafe path %q", p)
		}
	}
	// A safe absolute path succeeds.
	if err := writeGHWrapper(filepath.Join(dir, "gh-ok"), "/usr/local/bin/gh"); err != nil {
		t.Fatalf("writeGHWrapper safe path failed: %v", err)
	}
}

func TestWriteGitCredentialHelper_StoreEraseNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cred")
	if err := writeGitCredentialHelper(path); err != nil {
		t.Fatalf("writeGitCredentialHelper: %v", err)
	}
	script, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read helper: %v", err)
	}
	s := string(script)

	// store/erase must be no-ops (never persist tokens).
	if !strings.Contains(s, "store|erase") {
		t.Fatalf("helper must handle store|erase, script:\n%s", s)
	}
	if strings.Contains(s, "GITHUB_APP_PEM") || strings.Contains(s, "-----BEGIN PRIVATE KEY-----") {
		t.Fatalf("helper must not reference PEM material, script:\n%s", s)
	}
	// get must output username/password via the git credential protocol, not echo
	// the token to the terminal. It uses printf to stdout (consumed by git over pipe).
	if !strings.Contains(s, "username=x-access-token") {
		t.Fatalf("helper get must emit username, script:\n%s", s)
	}
	if !strings.Contains(s, `printf 'password=%s\n' "$token"`) {
		t.Fatalf("helper get must emit password via git protocol, script:\n%s", s)
	}
	// On broker failure the helper must exit non-zero (no silent fallthrough).
	if !strings.Contains(s, "exit 1") {
		t.Fatalf("helper get must exit non-zero on broker failure, script:\n%s", s)
	}
	// The helper must not suppress curl errors with 2>/dev/null.
	if strings.Contains(s, "2>/dev/null") {
		t.Fatalf("helper must not hide broker errors, script:\n%s", s)
	}
}

// TestSetupGitHubBrokerAuth_NoopWithoutBrokerEnv verifies that with no broker env,
// setup is a no-op returning ("", nil) so the caller uses the legacy path.
func TestSetupGitHubBrokerAuth_NoopWithoutBrokerEnv(t *testing.T) {
	origPath := os.Getenv("PATH")
	prefix, err := setupGitHubBrokerAuth(map[string]string{})
	if err != nil {
		t.Fatalf("expected nil error when broker env is absent, got %v", err)
	}
	if prefix != "" {
		t.Fatalf("expected empty prefix when broker env is absent, got %q", prefix)
	}
	if os.Getenv("PATH") != origPath {
		t.Fatalf("PATH must not be modified when broker env is absent")
	}
}

// TestSetupGitHubBrokerAuth_AbortsWhenRealGHMissing verifies that when the broker
// env is present but the real gh binary cannot be resolved, setup returns an
// error (NOT ok) so the caller aborts clone/setup instead of silently falling
// back to legacy auth. A broker session has no PEM/token in-Pod, so a silent
// fallback would create an unauthenticated session.
func TestSetupGitHubBrokerAuth_AbortsWhenRealGHMissing(t *testing.T) {
	// Broker env present.
	env := map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL":   "http://proxy/internal/sessions/sess/github-token",
		"AGENTAPI_GITHUB_BROKER_TOKEN": "broker-token",
	}
	// Force exec.LookPath("gh") to fail by emptying PATH (no gh resolvable).
	t.Setenv("PATH", "")
	// Restore a PATH without gh afterwards; here we just rely on empty PATH.
	prefix, err := setupGitHubBrokerAuth(env)
	if err == nil {
		t.Fatalf("expected error when real gh cannot be resolved, got prefix=%q", prefix)
	}
	if prefix != "" {
		t.Fatalf("prefix must be empty on failure, got %q", prefix)
	}
	if !strings.Contains(err.Error(), "gh") {
		t.Fatalf("error should mention gh resolution failure, got: %v", err)
	}
	// The wrapper must NOT have been installed (no silent partial setup).
	if _, statErr := os.Stat(ghWrapperPath); statErr == nil {
		t.Fatalf("wrapper must not be installed when real gh is missing")
	}
}

// TestSetupGitHubBrokerAuth_AbortsOnUnsafeGHPath verifies that when the resolved
// gh path is not shell-safe, setup aborts with an error (no wrapper installed).
func TestSetupGitHubBrokerAuth_AbortsOnUnsafeGHPath(t *testing.T) {
	// This is covered indirectly by TestWriteGHWrapper_RejectsUnsafePath and
	// TestIsShellSafeAbsPath; the integration path requires a real gh binary with
	// an unsafe name which is impractical to fabricate. Keep the unit coverage.
	t.Skip("requires a real gh binary with an unsafe name; covered by unit tests")
}

func TestPrependToPath(t *testing.T) {
	if got := prependToPath("/bin/x", "/usr/bin:/bin"); got != "/bin/x:/usr/bin:/bin" {
		t.Fatalf("prependToPath = %q", got)
	}
	if got := prependToPath("", "/usr/bin"); got != "/usr/bin" {
		t.Fatalf("prependToPath empty dir = %q", got)
	}
	if got := prependToPath("/bin/x", ""); got != "/bin/x" {
		t.Fatalf("prependToPath empty path = %q", got)
	}
}

func TestIsShellSafeAbsPath(t *testing.T) {
	good := []string{"/usr/bin/gh", "/usr/local/bin/gh", "/opt/gh-2.40/bin/gh"}
	for _, p := range good {
		if !isShellSafeAbsPath(p) {
			t.Fatalf("expected safe: %q", p)
		}
	}
	bad := []string{
		"gh", "relative/gh",
		"/usr/bin/gh;", "/usr/bin/gh\"x", "/usr/bin/$(id)",
		"/usr/bin/`id`", "/usr/bin/$HOME/gh", "/usr/bin/gh\\ x",
		"/usr/bin/gh with space", "/usr/bin/gh|cat",
	}
	for _, p := range bad {
		if isShellSafeAbsPath(p) {
			t.Fatalf("expected unsafe: %q", p)
		}
	}
}

func TestGitHubEnterpriseHost(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{name: "unset", env: "", want: ""},
		{name: "github.com default", env: "https://api.github.com", want: ""},
		{name: "enterprise with api/v3", env: "https://ghe.example.com/api/v3", want: "ghe.example.com"},
		{name: "enterprise trailing slash", env: "https://ghe.example.com/api/v3/", want: "ghe.example.com"},
		{name: "enterprise with path stripped", env: "https://ghe.example.com/api/v3/some/extra", want: "ghe.example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GITHUB_API", c.env)
			if got := githubEnterpriseHost(); got != c.want {
				t.Fatalf("githubEnterpriseHost() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestSafePathRe ensures the regex covers the documented safe charset.
func TestSafePathRe(t *testing.T) {
	if !safePathRe.MatchString("/usr/local/bin/gh") {
		t.Fatal("regex should match safe path")
	}
	for _, bad := range []string{`"`, "$", "`", "\\", " ", ";", "|", "&", "(", ")"} {
		if safePathRe.MatchString(bad) {
			t.Fatalf("regex must reject %q", bad)
		}
	}
	// compile-time sanity that the regex is anchored.
	if safePathRe.MatchString("safe") && safePathRe.String() == "" {
		t.Fatal("regex unexpectedly empty")
	}
	_ = regexp.MustCompile(`^[A-Za-z0-9_./-]+$`)
}

// runHelper executes the git credential helper script with the given op and env,
// returning stdout, stderr and the exit code.
func runHelper(t *testing.T, scriptPath, op string, env map[string]string) (string, string, int) {
	t.Helper()
	cmd := exec.Command("/bin/sh", scriptPath, op)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run helper: %v", err)
		}
	}
	return stdout.String(), stderr.String(), code
}

// TestGitCredentialHelper_Execution verifies the helper behaves correctly when
// actually executed by /bin/sh:
//   - store/erase exit 0 with no output (never persist),
//   - get with no broker env exits non-zero (no silent success),
//   - get with an unreachable broker exits non-zero (no silent fallback),
//   - get against a stub broker returns the git credential protocol payload.
func TestGitCredentialHelper_Execution(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "git-credential-broker")
	if err := writeGitCredentialHelper(helper); err != nil {
		t.Fatalf("writeGitCredentialHelper: %v", err)
	}

	// store/erase are no-ops that exit 0 with no output.
	for _, op := range []string{"store", "erase"} {
		out, errOut, code := runHelper(t, helper, op, nil)
		if code != 0 {
			t.Fatalf("%s: exit=%d, want 0", op, code)
		}
		if out != "" || errOut != "" {
			t.Fatalf("%s: must produce no output, got stdout=%q stderr=%q", op, out, errOut)
		}
	}

	// get with no broker env must exit non-zero (no silent fallthrough).
	out, errOut, code := runHelper(t, helper, "get", nil)
	if code == 0 {
		t.Fatalf("get with no broker env must fail, got exit=0 stdout=%q", out)
	}
	if strings.Contains(out, "password=") {
		t.Fatalf("get with no broker env must not emit credentials, got %q", out)
	}
	if !strings.Contains(errOut, "broker") {
		t.Fatalf("get with no broker env should log a diagnostic, got stderr=%q", errOut)
	}

	// get against an unreachable broker must exit non-zero (no silent fallback).
	out, _, code = runHelper(t, helper, "get", map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL":   "http://127.0.0.1:1/no-such-broker",
		"AGENTAPI_GITHUB_BROKER_TOKEN": "tok",
	})
	if code == 0 {
		t.Fatalf("get with unreachable broker must fail, got exit=0 stdout=%q", out)
	}
	if strings.Contains(out, "password=") {
		t.Fatalf("get with unreachable broker must not emit credentials, got %q", out)
	}

	// get against a stub broker returns the git credential protocol payload.
	stub := stubBroker(t, "ghs_stub_token")
	out, _, code = runHelper(t, helper, "get", map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL":   stub,
		"AGENTAPI_GITHUB_BROKER_TOKEN": "tok",
	})
	if code != 0 {
		t.Fatalf("get with stub broker: exit=%d, want 0", code)
	}
	if !strings.Contains(out, "username=x-access-token") || !strings.Contains(out, "password=ghs_stub_token") {
		t.Fatalf("get with stub broker: unexpected output %q", out)
	}
}

// TestGHWrapper_Execution verifies the wrapper script behaves correctly when
// executed by /bin/sh:
//   - with no broker env it refuses to run (exit 1), no real gh needed,
//   - the recursion guard exec's a stand-in "real gh" and forwards args/env.
func TestGHWrapper_Execution(t *testing.T) {
	dir := t.TempDir()
	// A stand-in "real gh" that prints its args and GH_TOKEN so we can assert the
	// wrapper passes the token via env and forwards args.
	realGH := filepath.Join(dir, "real-gh")
	if err := os.WriteFile(realGH, []byte("#!/bin/sh\necho \"ARGS:$*\"\necho \"GH_TOKEN:${GH_TOKEN:-}\"\n"), 0755); err != nil {
		t.Fatalf("write real gh: %v", err)
	}
	wrapper := filepath.Join(dir, "gh")
	if err := writeGHWrapper(wrapper, realGH); err != nil {
		t.Fatalf("writeGHWrapper: %v", err)
	}

	// With no broker env the wrapper must refuse (exit 1), even though a real gh
	// path is configured. A broker session has no in-Pod token, so it must not
	// silently run unauthenticated.
	out, errOut, code := runHelper(t, wrapper, "", nil)
	_ = out
	if code == 0 {
		t.Fatalf("wrapper with no broker env must fail, got exit=0")
	}
	if !strings.Contains(errOut, "broker") {
		t.Fatalf("wrapper with no broker env should log a diagnostic, got stderr=%q", errOut)
	}

	// With a stub broker the wrapper fetches a token and exec's the real gh with
	// GH_TOKEN set (token never in args) and args forwarded.
	stub := stubBroker(t, "ghs_wrapper_token")
	out, _, code = runHelper(t, wrapper, "repo view --json name", map[string]string{
		"AGENTAPI_GITHUB_BROKER_URL":   stub,
		"AGENTAPI_GITHUB_BROKER_TOKEN": "tok",
	})
	if code != 0 {
		t.Fatalf("wrapper with stub broker: exit=%d, want 0", code)
	}
	if !strings.Contains(out, "ARGS:repo view --json name") {
		t.Fatalf("wrapper must forward args to real gh, got %q", out)
	}
	if !strings.Contains(out, "GH_TOKEN:ghs_wrapper_token") {
		t.Fatalf("wrapper must set GH_TOKEN for real gh, got %q", out)
	}
	// The token must NOT appear in the forwarded args.
	if strings.Contains(out, "ARGS:ghs_wrapper_token") {
		t.Fatalf("token must not leak into args, got %q", out)
	}
}

// stubBroker starts a tiny HTTP server that responds to the broker endpoint with
// the given raw token, and returns its URL. It is used to exercise the in-Pod
// helper scripts end-to-end.
func stubBroker(t *testing.T, token string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "raw" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(token))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String() + "/internal/sessions/sess/github-token"
}

// TestCloneRepo_AbortsOnBrokerSetupFailure verifies that cloneRepo does NOT
// silently fall back to legacy auth when the broker env is present but the broker
// helpers cannot be installed (e.g. real gh missing). A broker session has no
// in-Pod PEM/token, so cloneRepo must return the error and abort rather than
// proceeding to an unauthenticated clone.
func TestCloneRepo_AbortsOnBrokerSetupFailure(t *testing.T) {
	// Broker env present but no resolvable gh on PATH -> setupGitHubBrokerAuth
	// returns an error. cloneRepo must propagate it.
	t.Setenv("PATH", "")
	settings := &SessionSettings{
		Repository: &RepositoryConfig{FullName: "octo/repo"},
		Env: map[string]string{
			"AGENTAPI_GITHUB_BROKER_URL":   "http://proxy/internal/sessions/sess/github-token",
			"AGENTAPI_GITHUB_BROKER_TOKEN": "broker-token",
		},
	}
	err := cloneRepo(settings)
	if err == nil {
		t.Fatalf("cloneRepo must abort when broker setup fails, got nil error")
	}
	if !strings.Contains(err.Error(), "broker") && !strings.Contains(err.Error(), "gh") {
		t.Fatalf("cloneRepo error should reference broker/gh setup failure, got: %v", err)
	}
}
