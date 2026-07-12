package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// newMockContext builds an echo.Context whose path param :sessionId is set, so the
// controller can read c.Param("sessionId") without a real router.
func newMockContext(rec *httptest.ResponseRecorder, r *http.Request) echo.Context {
	e := echo.New()
	c := e.NewContext(r, rec)
	c.SetPath("/internal/sessions/:sessionId/github-token")
	c.SetParamNames("sessionId")
	c.SetParamValues("sess-A")
	return c
}

// fakeBroker implements GitHubTokenBrokerService for tests.
type fakeBroker struct {
	validToken   string // the broker credential that should validate for "sess-A"
	issuedToken  string
	expiresAt    time.Time
	issueErr     error
	lastRequest  string
	validateFunc func(sessionID, token string) bool
}

func (f *fakeBroker) ValidateGitHubBrokerToken(sessionID, token string) bool {
	if f.validateFunc != nil {
		return f.validateFunc(sessionID, token)
	}
	return f.validToken != "" && sessionID == "sess-A" && token == f.validToken
}

func (f *fakeBroker) IssueGitHubToken(sessionID, requestedRepo string) (string, time.Time, error) {
	f.lastRequest = requestedRepo
	if f.issueErr != nil {
		return "", time.Time{}, f.issueErr
	}
	return f.issuedToken, f.expiresAt, nil
}

func newBrokerRequest(t *testing.T, method, target, token, body string) *http.Request {
	t.Helper()
	var r *http.Request
	var err error
	if body != "" {
		r, err = http.NewRequest(method, target, strings.NewReader(body))
	} else {
		r, err = http.NewRequest(method, target, nil)
	}
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func TestGitHubTokenController_IssueToken_Success(t *testing.T) {
	exp := time.Now().Add(1 * time.Hour).UTC()
	b := &fakeBroker{validToken: "tok-A", issuedToken: "ghs_issued", expiresAt: exp}
	ctl := NewGitHubTokenController(b)

	rec := httptest.NewRecorder()
	c := newMockContext(rec, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token", "tok-A", `{"repository":"octo/repo"}`))

	if err := ctl.IssueToken(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp githubTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v body=%s", err, rec.Body.String())
	}
	if resp.Token != "ghs_issued" {
		t.Fatalf("token = %q", resp.Token)
	}
	if resp.ExpiresAt == "" {
		t.Fatalf("expires_at should be set")
	}
	if b.lastRequest != "octo/repo" {
		t.Fatalf("broker received repo %q, want octo/repo", b.lastRequest)
	}
}

func TestGitHubTokenController_IssueToken_RawFormat(t *testing.T) {
	b := &fakeBroker{validToken: "tok-A", issuedToken: "ghs_raw", expiresAt: time.Now().Add(time.Hour)}
	ctl := NewGitHubTokenController(b)

	rec := httptest.NewRecorder()
	c := newMockContext(rec, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token?format=raw", "tok-A", ""))
	if err := ctl.IssueToken(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ghs_raw" {
		t.Fatalf("raw body = %q, want ghs_raw", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", ct)
	}
}

func TestGitHubTokenController_IssueToken_Unauthorized(t *testing.T) {
	b := &fakeBroker{validToken: "tok-A", issuedToken: "ghs_x"}
	ctl := NewGitHubTokenController(b)

	// No auth header.
	rec := httptest.NewRecorder()
	c := newMockContext(rec, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token", "", ""))
	if err := ctl.IssueToken(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d, want 401", rec.Code)
	}

	// Wrong token (e.g. session B's credential used against session A).
	rec2 := httptest.NewRecorder()
	c2 := newMockContext(rec2, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token", "tok-for-B", ""))
	if err := ctl.IssueToken(c2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: status = %d, want 401", rec2.Code)
	}

	// Body must not contain the token (no leak).
	if strings.Contains(rec2.Body.String(), "ghs_x") {
		t.Fatalf("401 body must not leak token: %s", rec2.Body.String())
	}
}

func TestGitHubTokenController_IssueToken_ScopeMismatch(t *testing.T) {
	b := &fakeBroker{
		validToken: "tok-A",
		issueErr:   errString("repository scope mismatch"),
	}
	ctl := NewGitHubTokenController(b)
	rec := httptest.NewRecorder()
	c := newMockContext(rec, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token", "tok-A", `{"repository":"octo/other"}`))
	if err := ctl.IssueToken(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("scope mismatch: status = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "octo/other") {
		t.Fatalf("403 body must not echo the requested repo")
	}
}

func TestGitHubTokenController_IssueToken_SessionNotFound(t *testing.T) {
	b := &fakeBroker{validToken: "tok-A", issueErr: errString("session not found")}
	ctl := NewGitHubTokenController(b)
	rec := httptest.NewRecorder()
	c := newMockContext(rec, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token", "tok-A", ""))
	if err := ctl.IssueToken(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want 404", rec.Code)
	}
}

func TestGitHubTokenController_IssueToken_IssuanceFailureSanitized(t *testing.T) {
	b := &fakeBroker{
		validToken: "tok-A",
		// An issuance error that would contain secret material if propagated.
		issueErr: errString("failed to issue token: ghs_secret -----BEGIN PRIVATE KEY-----"),
	}
	ctl := NewGitHubTokenController(b)
	rec := httptest.NewRecorder()
	c := newMockContext(rec, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token", "tok-A", ""))
	if err := ctl.IssueToken(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("issuance failure: status = %d, want 502", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "ghs_secret") || strings.Contains(body, "PRIVATE KEY") {
		t.Fatalf("502 body must not leak secret material: %s", body)
	}
}

func TestGitHubTokenController_IssueToken_BrokerUnavailable(t *testing.T) {
	// No broker configured -> controller returns 503.
	ctl := NewGitHubTokenController(nil)
	rec := httptest.NewRecorder()
	c := newMockContext(rec, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token", "tok-A", ""))
	if err := ctl.IssueToken(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil broker: status = %d, want 503", rec.Code)
	}
}

// errString is a simple error wrapping a string.
type errString string

func (e errString) Error() string { return string(e) }

// TestGitHubTokenController_IssueToken_CacheControlNoStore verifies that both the
// JSON and raw responses carry Cache-Control: no-store so the short-lived token
// is never cached/replayed by an intermediary.
func TestGitHubTokenController_IssueToken_CacheControlNoStore(t *testing.T) {
	b := &fakeBroker{validToken: "tok-A", issuedToken: "ghs_issued", expiresAt: time.Now().Add(time.Hour)}
	ctl := NewGitHubTokenController(b)

	// JSON response.
	rec := httptest.NewRecorder()
	c := newMockContext(rec, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token", "tok-A", `{"repository":"octo/repo"}`))
	if err := ctl.IssueToken(c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("JSON response Cache-Control = %q, want no-store", cc)
	}

	// Raw response.
	rec2 := httptest.NewRecorder()
	c2 := newMockContext(rec2, newBrokerRequest(t, http.MethodPost, "/internal/sessions/sess-A/github-token?format=raw", "tok-A", ""))
	if err := ctl.IssueToken(c2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cc := rec2.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("raw response Cache-Control = %q, want no-store", cc)
	}
}
