package testutil

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// OAuthTestHelper provides utilities for OAuth testing
type OAuthTestHelper struct {
	t          *testing.T
	mockServer *GitHubMockServer
}

// NewOAuthTestHelper creates a new OAuth test helper
func NewOAuthTestHelper(t *testing.T) *OAuthTestHelper {
	return &OAuthTestHelper{
		t:          t,
		mockServer: NewGitHubMockServer(),
	}
}

// Cleanup cleans up test resources
func (h *OAuthTestHelper) Cleanup() {
	h.mockServer.Close()
}

// MockServer returns the underlying mock server
func (h *OAuthTestHelper) MockServer() *GitHubMockServer {
	return h.mockServer
}

// GenerateTestState generates a valid JWT state for testing
func GenerateTestState(expiresIn time.Duration) (string, error) {
	// Simple base64 encoded state for testing
	return base64.URLEncoding.EncodeToString([]byte("test-state-" + generateNonce())), nil
}

// GenerateExpiredState generates an expired JWT state for testing
func GenerateExpiredState() (string, error) {
	return GenerateTestState(-1 * time.Hour)
}

// GenerateInvalidState generates an invalid state string
func GenerateInvalidState() string {
	return "invalid.jwt.state"
}

// generateNonce generates a random nonce
func generateNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// SimulateOAuthFlow simulates a complete OAuth flow
func (h *OAuthTestHelper) SimulateOAuthFlow(clientID, redirectURI string) (*OAuthFlowResult, error) {
	// Step 1: Generate authorization URL
	authURL := h.mockServer.URL() + "/login/oauth/authorize"
	state, _ := GenerateTestState(5 * time.Minute)

	fullAuthURL := authURL + "?" + url.Values{
		"client_id":    {clientID},
		"redirect_uri": {redirectURI},
		"state":        {state},
		"scope":        {"read:user read:org"},
	}.Encode()

	// Step 2: Simulate user authorization (would happen in browser)
	code := "test-code-" + generateNonce()

	// Step 3: Configure mock to handle token exchange
	h.mockServer.SetAccessToken(code, &TokenResponse{
		AccessToken: "gho_test_" + generateNonce(),
		TokenType:   "bearer",
		Scope:       "read:user read:org",
	})

	return &OAuthFlowResult{
		AuthURL:     fullAuthURL,
		State:       state,
		Code:        code,
		RedirectURI: redirectURI,
	}, nil
}

// OAuthFlowResult contains the result of a simulated OAuth flow
type OAuthFlowResult struct {
	AuthURL     string
	State       string
	Code        string
	RedirectURI string
}

// TestOAuthClient provides a test HTTP client with custom transport
type TestOAuthClient struct {
	*http.Client
	requests []*http.Request
}

// NewTestOAuthClient creates a new test OAuth client
func NewTestOAuthClient() *TestOAuthClient {
	client := &TestOAuthClient{
		requests: make([]*http.Request, 0),
	}

	client.Client = &http.Client{
		Transport: &testTransport{
			client: client,
			base:   http.DefaultTransport,
		},
		Timeout: 5 * time.Second,
	}

	return client
}

// GetRequests returns all captured requests
func (c *TestOAuthClient) GetRequests() []*http.Request {
	return c.requests
}

// testTransport captures requests for inspection
type testTransport struct {
	client *TestOAuthClient
	base   http.RoundTripper
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Capture request
	t.client.requests = append(t.client.requests, req.Clone(context.Background()))

	// Forward to base transport
	return t.base.RoundTrip(req)
}

// AssertOAuthRequest validates OAuth request parameters
func AssertOAuthRequest(t *testing.T, req *http.Request, expectedParams map[string]string) {
	t.Helper()

	query := req.URL.Query()
	for key, expectedValue := range expectedParams {
		actualValue := query.Get(key)
		require.Equal(t, expectedValue, actualValue, "OAuth parameter %s mismatch", key)
	}
}

// MockOAuthProvider creates a mock OAuth provider for testing
type MockOAuthProvider struct {
	AuthURLFunc       func(redirectURI string) (string, string, error)
	ExchangeCodeFunc  func(ctx context.Context, code string) (string, error)
	ValidateStateFunc func(state string) error
}

func (m *MockOAuthProvider) GetAuthorizationURL(redirectURI string) (string, string, error) {
	if m.AuthURLFunc != nil {
		return m.AuthURLFunc(redirectURI)
	}
	state, _ := GenerateTestState(5 * time.Minute)
	return "http://mock.oauth/authorize?state=" + state, state, nil
}

func (m *MockOAuthProvider) ExchangeCode(ctx context.Context, code string) (string, error) {
	if m.ExchangeCodeFunc != nil {
		return m.ExchangeCodeFunc(ctx, code)
	}
	return "mock-access-token", nil
}

func (m *MockOAuthProvider) ValidateState(state string) error {
	if m.ValidateStateFunc != nil {
		return m.ValidateStateFunc(state)
	}
	return nil
}

// TestScenarios provides common OAuth test scenarios
var TestScenarios = struct {
	SuccessfulAuth func(*GitHubMockServer)
	ExpiredToken   func(*GitHubMockServer)
	InvalidToken   func(*GitHubMockServer)
	RateLimited    func(*GitHubMockServer)
	ServerError    func(*GitHubMockServer)
	NetworkTimeout func(*GitHubMockServer)
}{
	SuccessfulAuth: func(m *GitHubMockServer) {
		token := "gho_valid_token"
		m.SetupSuccessfulAuth(token, 12345, "testuser")
		m.SetAccessToken("valid-code", &TokenResponse{
			AccessToken: token,
			TokenType:   "bearer",
			Scope:       "read:user read:org",
		})
	},

	ExpiredToken: func(m *GitHubMockServer) {
		m.SetError("/user", http.StatusUnauthorized)
	},

	InvalidToken: func(m *GitHubMockServer) {
		m.SetAccessToken("invalid-code", &TokenResponse{
			Error: "bad_verification_code",
		})
	},

	RateLimited: func(m *GitHubMockServer) {
		m.SetupRateLimitedResponse()
	},

	ServerError: func(m *GitHubMockServer) {
		m.SetupServerError()
	},

	NetworkTimeout: func(m *GitHubMockServer) {
		m.SetResponseDelay(10 * time.Second)
	},
}
