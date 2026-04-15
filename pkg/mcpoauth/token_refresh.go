package mcpoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TokenResponse is the OAuth 2.x token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope,omitempty"`
}

// ExchangeCode exchanges an authorization code for tokens using PKCE.
func ExchangeCode(ctx context.Context, client *http.Client, tokenURL, clientID, clientSecret, code, codeVerifier, redirectURI string) (*TokenResponse, error) {
	return callTokenEndpoint(ctx, client, tokenURL, clientID, clientSecret, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	})
}

// RefreshToken uses a refresh_token to obtain a new access_token.
func RefreshToken(ctx context.Context, client *http.Client, tokenURL, clientID, clientSecret, refreshToken string) (*TokenResponse, error) {
	return callTokenEndpoint(ctx, client, tokenURL, clientID, clientSecret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func callTokenEndpoint(ctx context.Context, client *http.Client, tokenURL, clientID, clientSecret string, params url.Values) (*TokenResponse, error) {
	if client == nil {
		client = http.DefaultClient
	}

	params.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Confidential client: send Basic auth.
	if clientSecret != "" {
		req.SetBasicAuth(clientID, clientSecret)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token endpoint %s: %w", tokenURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint %s returned %d", tokenURL, resp.StatusCode)
	}

	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	return &tr, nil
}

// ExpiresAt computes the expiry time from a token response.
// Falls back to 1 hour when ExpiresIn is not set.
func ExpiresAt(tr *TokenResponse) time.Time {
	if tr.ExpiresIn > 0 {
		return time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	return time.Now().Add(time.Hour)
}
