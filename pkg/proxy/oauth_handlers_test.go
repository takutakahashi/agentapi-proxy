package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

func setupTestProxyWithOAuth(t *testing.T) (*Proxy, *httptest.Server) {
	// Clear environment variable that might interfere with redirect URI validation
	oldRedirectURIs := os.Getenv("OAUTH_ALLOWED_REDIRECT_URIS")
	_ = os.Unsetenv("OAUTH_ALLOWED_REDIRECT_URIS")
	t.Cleanup(func() {
		if oldRedirectURIs != "" {
			_ = os.Setenv("OAUTH_ALLOWED_REDIRECT_URIS", oldRedirectURIs)
		}
	})

	// Mock GitHub OAuth server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"access_token":"gho_test_token","token_type":"bearer","scope":"read:user,read:org"}`))
		case "/user":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"login":"testuser","id":123456,"email":"test@example.com","name":"Test User"}`))
		case "/user/orgs":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"login":"test-org","id":789}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	cfg := &config.Config{
		StartPort: 9000,
		Auth: config.AuthConfig{
			Enabled: true,
			GitHub: &config.GitHubAuthConfig{
				Enabled:     true,
				BaseURL:     mockServer.URL,
				TokenHeader: "Authorization",
				UserMapping: config.GitHubUserMapping{
					DefaultRole:        "user",
					DefaultPermissions: []string{"read", "session:create", "session:list"},
				},
				OAuth: &config.GitHubOAuthConfig{
					ClientID:     "test-client-id",
					ClientSecret: "test-client-secret",
					Scope:        "read:user read:org",
					BaseURL:      mockServer.URL,
				},
			},
		},
	}

	proxy := NewProxy(cfg, false)
	return proxy, mockServer
}

func TestHandleOAuthLogin(t *testing.T) {
	proxy, mockServer := setupTestProxyWithOAuth(t)
	defer mockServer.Close()

	e := echo.New()

	// Test valid request
	reqBody := OAuthLoginRequest{
		RedirectURI: "http://localhost:3000/callback",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := proxy.handleOAuthLogin(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp OAuthLoginResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.AuthURL)
	assert.NotEmpty(t, resp.State)
	assert.Contains(t, resp.AuthURL, mockServer.URL+"/login/oauth/authorize")
}

func TestHandleOAuthLogin_InvalidRequest(t *testing.T) {
	proxy, mockServer := setupTestProxyWithOAuth(t)
	defer mockServer.Close()

	e := echo.New()

	// Test missing redirect_uri
	reqBody := OAuthLoginRequest{}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := proxy.handleOAuthLogin(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestHandleOAuthCallback(t *testing.T) {
	proxy, mockServer := setupTestProxyWithOAuth(t)
	defer mockServer.Close()

	e := echo.New()

	// First generate a valid state
	_, state, _ := proxy.oauthProvider.GenerateAuthURL("http://localhost:3000/callback")

	// Test valid callback
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=test-code&state="+state, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := proxy.handleOAuthCallback(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp OAuthSessionResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.SessionID)
	assert.Equal(t, "gho_test_token", resp.AccessToken)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.NotNil(t, resp.User)
	assert.Equal(t, "testuser", resp.User.UserID)
}

func TestHandleOAuthCallback_MissingParameters(t *testing.T) {
	proxy, mockServer := setupTestProxyWithOAuth(t)
	defer mockServer.Close()

	e := echo.New()

	// Test missing code
	req := httptest.NewRequest(http.MethodGet, "/oauth/callback?state=test-state", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := proxy.handleOAuthCallback(c)
	assert.Error(t, err)
	httpErr, ok := err.(*echo.HTTPError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, httpErr.Code)
}

func TestHandleOAuthLogout(t *testing.T) {
	proxy, mockServer := setupTestProxyWithOAuth(t)
	defer mockServer.Close()

	e := echo.New()

	// Create a test session
	testSession := &OAuthSession{
		ID: "test-session-id",
		UserContext: &auth.UserContext{
			UserID:      "testuser",
			AccessToken: "gho_test_token",
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	proxy.oauthSessions.Store(testSession.ID, testSession)

	// Test logout with session ID in header
	req := httptest.NewRequest(http.MethodPost, "/oauth/logout", nil)
	req.Header.Set("X-Session-ID", testSession.ID)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := proxy.handleOAuthLogout(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify session was removed
	_, exists := proxy.oauthSessions.Load(testSession.ID)
	assert.False(t, exists)
}

func TestHandleOAuthRefresh(t *testing.T) {
	proxy, mockServer := setupTestProxyWithOAuth(t)
	defer mockServer.Close()

	e := echo.New()

	// Create a test session
	testSession := &OAuthSession{
		ID: "test-session-id",
		UserContext: &auth.UserContext{
			UserID:      "testuser",
			AccessToken: "gho_test_token",
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	proxy.oauthSessions.Store(testSession.ID, testSession)

	// Test refresh
	req := httptest.NewRequest(http.MethodPost, "/oauth/refresh", nil)
	req.Header.Set("X-Session-ID", testSession.ID)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := proxy.handleOAuthRefresh(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp OAuthTokenResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.Equal(t, "gho_test_token", resp.AccessToken)
	assert.True(t, resp.ExpiresAt.After(time.Now().Add(23*time.Hour)))
}

func TestValidateOAuthSession(t *testing.T) {
	proxy, mockServer := setupTestProxyWithOAuth(t)
	defer mockServer.Close()

	e := echo.New()

	// Create a test session
	testSession := &OAuthSession{
		ID: "test-session-id",
		UserContext: &auth.UserContext{
			UserID:      "testuser",
			AccessToken: "gho_test_token",
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	proxy.oauthSessions.Store(testSession.ID, testSession)

	// Test with session ID in header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Session-ID", testSession.ID)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	userContext, err := proxy.validateOAuthSession(c)
	assert.NoError(t, err)
	assert.NotNil(t, userContext)
	assert.Equal(t, "testuser", userContext.UserID)

	// Test with session ID in query parameter
	req2 := httptest.NewRequest(http.MethodGet, "/test?session_id="+testSession.ID, nil)
	rec2 := httptest.NewRecorder()
	c2 := e.NewContext(req2, rec2)

	userContext2, err2 := proxy.validateOAuthSession(c2)
	assert.NoError(t, err2)
	assert.NotNil(t, userContext2)
	assert.Equal(t, "testuser", userContext2.UserID)

	// Test with expired session
	expiredSession := &OAuthSession{
		ID: "expired-session-id",
		UserContext: &auth.UserContext{
			UserID: "expireduser",
		},
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	proxy.oauthSessions.Store(expiredSession.ID, expiredSession)

	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.Header.Set("X-Session-ID", expiredSession.ID)
	rec3 := httptest.NewRecorder()
	c3 := e.NewContext(req3, rec3)

	_, err3 := proxy.validateOAuthSession(c3)
	assert.Error(t, err3)
	assert.Contains(t, err3.Error(), "session expired")
}
