package controllers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

var (
	ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	urlRegex  = regexp.MustCompile(`https://[^\s]+`)
	// codex device codes are formatted as XXXX-XXXXX (4 chars, dash, 4–8 chars).
	codeRegex = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4,8}\b`)
)

// authSession tracks an in-progress codex login --device-auth subprocess.
type authSession struct {
	cmd     *exec.Cmd
	tmpHome string
	mu      sync.Mutex
	status  string // "pending", "authorized", "denied"
}

// CodexDeviceAuthController runs `codex login --device-auth` and proxies
// the result to the credentials store. No OAuth app registration required.
type CodexDeviceAuthController struct {
	repo     repositories.CredentialsRepository
	sessions sync.Map // userID -> *authSession
}

// NewCodexDeviceAuthController creates a new CodexDeviceAuthController.
func NewCodexDeviceAuthController(repo repositories.CredentialsRepository) *CodexDeviceAuthController {
	return &CodexDeviceAuthController{repo: repo}
}

// GetName returns the controller name for logging.
func (c *CodexDeviceAuthController) GetName() string {
	return "CodexDeviceAuthController"
}

// StartDeviceAuthResponse is returned by POST /codex/device-auth.
type StartDeviceAuthResponse struct {
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
}

// PollDeviceAuthResponse is returned by POST /codex/device-auth/token.
type PollDeviceAuthResponse struct {
	// Status is one of "pending", "authorized", "denied".
	Status string `json:"status"`
}

// CodexAuthConfigResponse is returned by GET /codex/device-auth/config.
type CodexAuthConfigResponse struct {
	Configured bool `json:"configured"`
}

// GetConfig handles GET /codex/device-auth/config.
// Returns whether the codex CLI is available for device auth.
func (c *CodexDeviceAuthController) GetConfig(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	_, err := exec.LookPath("codex")
	return ctx.JSON(http.StatusOK, CodexAuthConfigResponse{Configured: err == nil})
}

// StartDeviceAuth handles POST /codex/device-auth.
// Runs `codex login --device-auth` and returns the user_code and verification_uri.
func (c *CodexDeviceAuthController) StartDeviceAuth(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := string(user.ID())

	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "codex CLI not found in PATH")
	}

	// Cancel any existing session for this user before starting a new one.
	c.cancelSession(userID)

	// Use a per-session HOME under the proxy user's home dir (not /tmp) so that
	// codex does not warn about "refusing to create helper binaries under /tmp".
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/home/agentapi"
	}
	tmpBase := filepath.Join(homeDir, ".codex-sessions")
	if err := os.MkdirAll(tmpBase, 0700); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create temp directory")
	}
	tmpHome, err := os.MkdirTemp(tmpBase, "")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create temp directory")
	}

	// Inherit env but override HOME.
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "HOME=") {
			env = append(env, e)
		}
	}
	env = append(env, "HOME="+tmpHome)

	// Merge stdout+stderr into a single pipe for parsing.
	pr, pw := io.Pipe()
	cmd := exec.Command(codexPath, "login", "--device-auth")
	cmd.Env = env
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		_ = os.RemoveAll(tmpHome)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to start codex: %v", err))
	}

	session := &authSession{cmd: cmd, tmpHome: tmpHome, status: "pending"}
	c.sessions.Store(userID, session)

	// exitErrCh receives the process exit status once.
	exitErrCh := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		_ = pw.Close()
		exitErrCh <- err
	}()

	// parseCh receives the URL+code pair extracted from stdout.
	type parseResult struct {
		userCode  string
		verifyURI string
		err       error
	}
	parseCh := make(chan parseResult, 1)

	go func() {
		var userCode, verifyURI string
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			line := ansiRegex.ReplaceAllString(scanner.Text(), "")
			if verifyURI == "" {
				if m := urlRegex.FindString(line); m != "" {
					verifyURI = strings.TrimSpace(m)
				}
			}
			if userCode == "" {
				if m := codeRegex.FindString(line); m != "" {
					userCode = m
				}
			}
			if userCode != "" && verifyURI != "" {
				parseCh <- parseResult{userCode: userCode, verifyURI: verifyURI}
				// Drain remaining output so the process isn't blocked.
				for scanner.Scan() {
				}
				return
			}
		}
		parseCh <- parseResult{err: fmt.Errorf("auth flow ended before providing user code")}
	}()

	// Wait for URL+code to appear, or timeout.
	select {
	case r := <-parseCh:
		if r.err != nil {
			c.cancelSession(userID)
			return echo.NewHTTPError(http.StatusInternalServerError, r.err.Error())
		}
		log.Printf("[CODEX_AUTH] Device auth started for user=%s code=%s", userID, r.userCode)
		go c.watchCompletion(userID, session, exitErrCh)
		return ctx.JSON(http.StatusOK, StartDeviceAuthResponse{
			UserCode:        r.userCode,
			VerificationURI: r.verifyURI,
		})

	case exitErr := <-exitErrCh:
		c.cancelSession(userID)
		msg := "codex auth process exited unexpectedly"
		if exitErr != nil {
			msg = fmt.Sprintf("codex auth failed: %v", exitErr)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, msg)

	case <-time.After(30 * time.Second):
		c.cancelSession(userID)
		return echo.NewHTTPError(http.StatusGatewayTimeout, "Timeout waiting for auth flow to start")
	}
}

// PollDeviceAuth handles POST /codex/device-auth/token.
// Returns the current status for the calling user's in-progress auth session.
func (c *CodexDeviceAuthController) PollDeviceAuth(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	userID := string(user.ID())
	val, ok := c.sessions.Load(userID)
	if !ok {
		return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: "pending"})
	}

	s := val.(*authSession)
	s.mu.Lock()
	status := s.status
	s.mu.Unlock()

	return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: status})
}

// watchCompletion waits for the auth subprocess to exit, then reads the generated
// auth.json and persists it to the credentials repository.
func (c *CodexDeviceAuthController) watchCompletion(userID string, session *authSession, exitErrCh <-chan error) {
	exitErr := <-exitErrCh

	session.mu.Lock()
	defer session.mu.Unlock()

	defer func() {
		go func() {
			time.Sleep(10 * time.Second)
			_ = os.RemoveAll(session.tmpHome)
		}()
	}()

	if exitErr != nil {
		log.Printf("[CODEX_AUTH] Auth process failed for user=%s: %v", userID, exitErr)
		session.status = "denied"
		return
	}

	authJSONPath := filepath.Join(session.tmpHome, ".codex", "auth.json")
	data, err := os.ReadFile(authJSONPath)
	if err != nil {
		log.Printf("[CODEX_AUTH] Failed to read auth.json for user=%s: %v", userID, err)
		session.status = "denied"
		return
	}

	creds := entities.NewCredentials(userID, json.RawMessage(data))
	creds.SetFileType(sessionsettings.FileTypeCodexAuth)

	if err := c.repo.Save(context.Background(), creds); err != nil {
		log.Printf("[CODEX_AUTH] Failed to save credentials for user=%s: %v", userID, err)
		session.status = "denied"
		return
	}

	session.status = "authorized"
	log.Printf("[CODEX_AUTH] Auth completed and credentials saved for user=%s", userID)
}

// cancelSession kills any running auth subprocess for the given user.
func (c *CodexDeviceAuthController) cancelSession(userID string) {
	if val, ok := c.sessions.LoadAndDelete(userID); ok {
		s := val.(*authSession)
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		go func() {
			time.Sleep(time.Second)
			_ = os.RemoveAll(s.tmpHome)
		}()
	}
}
