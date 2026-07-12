package controllers

import (
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// GitHubTokenBrokerService is the port the GitHub token broker controller depends
// on. It is implemented by KubernetesSessionManager.
type GitHubTokenBrokerService interface {
	// ValidateGitHubBrokerToken reports whether token is the valid broker
	// credential for sessionID (constant-time, session-scoped).
	ValidateGitHubBrokerToken(sessionID, token string) bool
	// IssueGitHubToken mints/returns a cached short-lived GitHub installation
	// token for the session, validating repository scope. Errors are secret-free.
	IssueGitHubToken(sessionID, requestedRepo string) (token string, expiresAt time.Time, err error)
}

// GitHubTokenController exposes the session-scoped GitHub token broker endpoint.
// Session Pods call it to obtain short-lived installation tokens on demand instead
// of receiving a long-lived credential at creation time.
type GitHubTokenController struct {
	broker GitHubTokenBrokerService
}

// NewGitHubTokenController creates a controller backed by the given broker service.
func NewGitHubTokenController(broker GitHubTokenBrokerService) *GitHubTokenController {
	return &GitHubTokenController{broker: broker}
}

// githubTokenResponse is the JSON body returned by the broker endpoint.
type githubTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// IssueToken handles POST /internal/sessions/:sessionId/github-token.
//
// Authentication: the caller presents a session-scoped broker credential in the
// Authorization header (Bearer <token>). The credential is bound to the session ID
// in the path, so a token minted for session A cannot be used against session B, and
// the credential is not valid for any other proxy endpoint.
//
// Repository scope: the request body may carry {"repository":"owner/repo"}; when
// present it must match the session's configured repository (the broker enforces
// this). When omitted, the session's repository scope is used.
//
// The token and its expiry are returned as JSON. On error a secret-free message is
// returned with an appropriate status code; token/PEM material never leaks.
func (ctl *GitHubTokenController) IssueToken(c echo.Context) error {
	if ctl == nil || ctl.broker == nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "github token broker is not configured"})
	}

	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "session id is required"})
	}

	// Authenticate the caller with the session-scoped broker credential.
	token := bearerToken(c)
	if token == "" || !ctl.broker.ValidateGitHubBrokerToken(sessionID, token) {
		// 401 (not 403) so a caller cannot distinguish "unknown session" from
		// "wrong token"; no session existence is leaked.
		return c.NoContent(http.StatusUnauthorized)
	}

	// Parse the optional repository scope from the body. Unknown fields are
	// ignored; a missing body is fine.
	var body struct {
		Repository string `json:"repository"`
	}
	_ = c.Bind(&body)
	requestedRepo := strings.TrimSpace(body.Repository)

	issuedToken, expiresAt, err := ctl.broker.IssueGitHubToken(sessionID, requestedRepo)
	if err != nil {
		// Map common failures to status codes without leaking secrets.
		msg := err.Error()
		switch {
		case strings.Contains(msg, "session not found"):
			return c.JSON(http.StatusNotFound, map[string]string{"error": "session not found"})
		case strings.Contains(msg, "repository scope mismatch"):
			return c.JSON(http.StatusForbidden, map[string]string{"error": "repository scope mismatch"})
		case strings.Contains(msg, "not configured"):
			return c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "github token broker unavailable"})
		default:
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to issue github token"})
		}
	}
	// The token is a short-lived secret. Prevent any intermediary from caching it
	// (HTTP caches, proxies, browsers) with Cache-Control: no-store on every token
	// response so the credential is never persisted or replayed from a cache.
	c.Response().Header().Set("Cache-Control", "no-store")

	if issuedToken == "" {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": "failed to issue github token"})
	}

	// format=raw returns the bare token as text/plain for simple in-Pod shell
	// helpers (curl capture). The default is JSON.
	if c.QueryParam("format") == "raw" {
		return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(issuedToken))
	}

	expiresStr := ""
	if !expiresAt.IsZero() {
		expiresStr = expiresAt.UTC().Format(time.RFC3339)
	}
	return c.JSON(http.StatusOK, githubTokenResponse{Token: issuedToken, ExpiresAt: expiresStr})
}

// bearerToken extracts a Bearer token from the Authorization header.
func bearerToken(c echo.Context) string {
	h := c.Request().Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimPrefix(h, prefix)
}
