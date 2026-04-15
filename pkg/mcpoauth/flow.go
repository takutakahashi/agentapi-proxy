package mcpoauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	callbackPath = "/mcp-oauth/callback"
	clientName   = "agentapi-proxy"
)

// ConnectRequest is the input for beginning an OAuth flow for a specific
// MCP server / user combination.
type ConnectRequest struct {
	UserID       string
	ServerName   string
	MCPServerURL string
	// Optional overrides (used when DCR is not supported).
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// ConnectResult carries the authorization URL that the user must visit.
type ConnectResult struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
}

// Manager orchestrates the full MCP OAuth flow.
type Manager struct {
	httpClient      *http.Client
	tokenRepo       portrepos.MCPOAuthTokenRepository
	stateStore      *StateStore
	callbackBaseURL string // e.g. "https://proxy.example.com"
}

// NewManager creates a Manager. callbackBaseURL must be the externally reachable
// base URL of agentapi-proxy (scheme + host, no trailing slash).
func NewManager(tokenRepo portrepos.MCPOAuthTokenRepository, callbackBaseURL string) *Manager {
	return &Manager{
		httpClient:      &http.Client{Timeout: 15 * time.Second},
		tokenRepo:       tokenRepo,
		stateStore:      NewStateStore(),
		callbackBaseURL: strings.TrimSuffix(callbackBaseURL, "/"),
	}
}

// Connect runs Discovery + optional DCR and returns the authorization URL.
func (m *Manager) Connect(ctx context.Context, req ConnectRequest) (*ConnectResult, error) {
	// 1. Discover OAuth endpoints.
	metadata, err := Discover(ctx, m.httpClient, req.MCPServerURL)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth connect: discovery: %w", err)
	}

	clientID := req.ClientID
	clientSecret := req.ClientSecret
	redirectURI := m.callbackBaseURL + callbackPath

	// 2. DCR when no client_id was supplied and the server supports it.
	if clientID == "" && metadata.SupportsDCR() {
		scope := strings.Join(req.Scopes, " ")
		dcrResp, err := Register(ctx, m.httpClient, metadata.RegistrationEndpoint, DCRRequest{
			ClientName:              clientName,
			RedirectURIs:            []string{redirectURI},
			GrantTypes:              []string{"authorization_code", "refresh_token"},
			ResponseTypes:           []string{"code"},
			TokenEndpointAuthMethod: "none",
			Scope:                   scope,
		})
		if err != nil {
			return nil, fmt.Errorf("mcpoauth connect: DCR: %w", err)
		}
		clientID = dcrResp.ClientID
		clientSecret = dcrResp.ClientSecret
	}

	if clientID == "" {
		return nil, fmt.Errorf("mcpoauth connect: no client_id available (DCR not supported and none provided)")
	}

	// 3. Generate PKCE + state.
	codeVerifier, err := GenerateCodeVerifier()
	if err != nil {
		return nil, err
	}
	state, err := generateState()
	if err != nil {
		return nil, err
	}

	// 4. Store pending state.
	m.stateStore.Store(state, &PendingState{
		State:        state,
		CodeVerifier: codeVerifier,
		UserID:       req.UserID,
		ServerName:   req.ServerName,
		MCPServerURL: req.MCPServerURL,
		RedirectURI:  redirectURI,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     metadata.TokenEndpoint,
		CreatedAt:    time.Now(),
	})

	// 5. Build authorization URL.
	scope := strings.Join(req.Scopes, " ")
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {CodeChallenge(codeVerifier)},
		"code_challenge_method": {"S256"},
	}
	if scope != "" {
		params.Set("scope", scope)
	}
	authURL := metadata.AuthorizationEndpoint + "?" + params.Encode()

	return &ConnectResult{
		AuthorizationURL: authURL,
		State:            state,
	}, nil
}

// HandleCallback receives the authorization code, exchanges it for tokens,
// and persists the result.
func (m *Manager) HandleCallback(ctx context.Context, code, state string) (*entities.MCPOAuthToken, error) {
	pending, ok := m.stateStore.Load(state)
	if !ok {
		return nil, fmt.Errorf("mcpoauth callback: unknown or expired state")
	}

	tr, err := ExchangeCode(ctx, m.httpClient,
		pending.TokenURL,
		pending.ClientID,
		pending.ClientSecret,
		code,
		pending.CodeVerifier,
		pending.RedirectURI,
	)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth callback: exchange code: %w", err)
	}

	token := entities.NewMCPOAuthToken(pending.UserID, pending.ServerName)
	token.SetClientID(pending.ClientID)
	token.SetClientSecret(pending.ClientSecret)
	token.SetAccessToken(tr.AccessToken)
	token.SetRefreshToken(tr.RefreshToken)
	token.SetExpiresAt(ExpiresAt(tr))
	token.SetTokenType(tr.TokenType)
	token.SetTokenURL(pending.TokenURL)

	if err := m.tokenRepo.Save(ctx, token); err != nil {
		return nil, fmt.Errorf("mcpoauth callback: save token: %w", err)
	}
	return token, nil
}

// GetValidToken retrieves a token for the given user+server and refreshes it
// when it is about to expire. Returns nil (no error) when no token exists.
func (m *Manager) GetValidToken(ctx context.Context, userID, serverName string) (*entities.MCPOAuthToken, error) {
	token, err := m.tokenRepo.FindByUserAndServer(ctx, userID, serverName)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: load token: %w", err)
	}
	if token == nil {
		return nil, nil
	}
	if !token.IsExpired() {
		return token, nil
	}

	// Try to refresh.
	if token.RefreshToken() == "" {
		return nil, nil // no refresh token, user must re-authenticate
	}
	tr, err := RefreshToken(ctx, m.httpClient,
		token.TokenURL(),
		token.ClientID(),
		token.ClientSecret(),
		token.RefreshToken(),
	)
	if err != nil {
		return nil, nil // soft fail: user must re-authenticate
	}

	token.SetAccessToken(tr.AccessToken)
	token.SetExpiresAt(ExpiresAt(tr))
	if tr.RefreshToken != "" {
		token.SetRefreshToken(tr.RefreshToken)
	}
	if err := m.tokenRepo.Save(ctx, token); err != nil {
		return nil, fmt.Errorf("mcpoauth: save refreshed token: %w", err)
	}
	return token, nil
}

// generateState creates a cryptographically random state string.
func generateState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
