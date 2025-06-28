package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// GitHubMockServer provides a configurable mock GitHub API server for testing
type GitHubMockServer struct {
	server *httptest.Server
	mu     sync.RWMutex

	// Configurable responses
	Users         map[string]*GitHubUser
	Teams         map[string][]GitHubTeam
	AccessTokens  map[string]*TokenResponse
	Organizations map[string]*GitHubOrg

	// Error simulation
	ErrorCodes    map[string]int
	ResponseDelay time.Duration

	// Request tracking
	RequestLog []RequestInfo
}

// GitHubUser represents a GitHub user response
type GitHubUser struct {
	Login     string    `json:"login"`
	ID        int64     `json:"id"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// GitHubTeam represents a GitHub team
type GitHubTeam struct {
	Slug         string    `json:"slug"`
	Organization GitHubOrg `json:"organization"`
	Permission   string    `json:"permission,omitempty"`
}

// GitHubOrg represents a GitHub organization
type GitHubOrg struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// TokenResponse represents OAuth token exchange response
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error,omitempty"`
}

// RequestInfo tracks request details
type RequestInfo struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
	Time    time.Time
}

// NewGitHubMockServer creates a new mock GitHub API server
func NewGitHubMockServer() *GitHubMockServer {
	mock := &GitHubMockServer{
		Users:         make(map[string]*GitHubUser),
		Teams:         make(map[string][]GitHubTeam),
		AccessTokens:  make(map[string]*TokenResponse),
		Organizations: make(map[string]*GitHubOrg),
		ErrorCodes:    make(map[string]int),
		RequestLog:    make([]RequestInfo, 0),
	}

	mock.server = httptest.NewServer(http.HandlerFunc(mock.handler))
	return mock
}

// URL returns the mock server URL
func (m *GitHubMockServer) URL() string {
	return m.server.URL
}

// Close shuts down the mock server
func (m *GitHubMockServer) Close() {
	m.server.Close()
}

// SetUser configures a user response for a given token
func (m *GitHubMockServer) SetUser(token string, user *GitHubUser) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Users[token] = user
}

// SetTeams configures team memberships for a given token
func (m *GitHubMockServer) SetTeams(token string, teams []GitHubTeam) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Teams[token] = teams
}

// SetAccessToken configures token exchange response for a given code
func (m *GitHubMockServer) SetAccessToken(code string, response *TokenResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AccessTokens[code] = response
}

// SetError configures an error response for a specific endpoint
func (m *GitHubMockServer) SetError(endpoint string, statusCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ErrorCodes[endpoint] = statusCode
}

// SetResponseDelay configures a delay for all responses
func (m *GitHubMockServer) SetResponseDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResponseDelay = delay
}

// GetRequestLog returns all logged requests
func (m *GitHubMockServer) GetRequestLog() []RequestInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]RequestInfo{}, m.RequestLog...)
}

// ClearRequestLog clears the request log
func (m *GitHubMockServer) ClearRequestLog() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RequestLog = make([]RequestInfo, 0)
}

// handler processes incoming requests
func (m *GitHubMockServer) handler(w http.ResponseWriter, r *http.Request) {
	m.logRequest(r)

	// Apply response delay if configured
	m.mu.RLock()
	delay := m.ResponseDelay
	m.mu.RUnlock()
	if delay > 0 {
		time.Sleep(delay)
	}

	// Check for configured errors
	m.mu.RLock()
	if statusCode, exists := m.ErrorCodes[r.URL.Path]; exists {
		m.mu.RUnlock()
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(map[string]string{
			"message": http.StatusText(statusCode),
		})
		return
	}
	m.mu.RUnlock()

	switch r.URL.Path {
	case "/user":
		m.handleUser(w, r)
	case "/user/teams":
		m.handleUserTeams(w, r)
	case "/login/oauth/access_token":
		m.handleAccessToken(w, r)
	case "/user/orgs":
		m.handleUserOrgs(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Not Found",
		})
	}
}

func (m *GitHubMockServer) handleUser(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r.Header.Get("Authorization"))

	m.mu.RLock()
	user, exists := m.Users[token]
	m.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Bad credentials",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

func (m *GitHubMockServer) handleUserTeams(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r.Header.Get("Authorization"))

	m.mu.RLock()
	teams, exists := m.Teams[token]
	m.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Bad credentials",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(teams)
}

func (m *GitHubMockServer) handleAccessToken(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	code := r.Form.Get("code")

	m.mu.RLock()
	response, exists := m.AccessTokens[code]
	m.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(TokenResponse{
			Error: "bad_verification_code",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (m *GitHubMockServer) handleUserOrgs(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r.Header.Get("Authorization"))

	m.mu.RLock()
	_, exists := m.Users[token]
	orgs := make([]*GitHubOrg, 0)
	for _, org := range m.Organizations {
		orgs = append(orgs, org)
	}
	m.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "Bad credentials",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orgs)
}

func (m *GitHubMockServer) logRequest(r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info := RequestInfo{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Time:    time.Now(),
	}

	// Read body if present
	if r.Body != nil {
		// body, _ := io.ReadAll(r.Body)
		// info.Body = body
		// r.Body = io.NopCloser(bytes.NewReader(body))
	}

	m.RequestLog = append(m.RequestLog, info)
}

func extractToken(authHeader string) string {
	if len(authHeader) > 6 && authHeader[:6] == "token " {
		return authHeader[6:]
	}
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		return authHeader[7:]
	}
	return ""
}

// Preset configurations for common test scenarios

// SetupSuccessfulAuth configures the mock for a successful authentication flow
func (m *GitHubMockServer) SetupSuccessfulAuth(token string, userID int64, username string) {
	m.SetUser(token, &GitHubUser{
		Login: username,
		ID:    userID,
		Email: username + "@example.com",
		Name:  "Test " + username,
	})

	m.SetTeams(token, []GitHubTeam{
		{
			Slug: "developers",
			Organization: GitHubOrg{
				Login: "test-org",
			},
		},
	})
}

// SetupRateLimitedResponse configures the mock to simulate rate limiting
func (m *GitHubMockServer) SetupRateLimitedResponse() {
	m.SetError("/user", http.StatusForbidden)
	m.SetError("/user/teams", http.StatusForbidden)
}

// SetupServerError configures the mock to simulate server errors
func (m *GitHubMockServer) SetupServerError() {
	m.SetError("/user", http.StatusInternalServerError)
	m.SetError("/user/teams", http.StatusInternalServerError)
	m.SetError("/login/oauth/access_token", http.StatusInternalServerError)
}
