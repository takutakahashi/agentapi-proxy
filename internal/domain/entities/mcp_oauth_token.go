package entities

import "time"

// MCPOAuthToken stores an OAuth access/refresh token pair for a specific
// user × MCP server combination.
type MCPOAuthToken struct {
	userID       string
	serverName   string
	clientID     string
	clientSecret string
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	tokenType    string
	tokenURL     string
}

// NewMCPOAuthToken creates a new empty token for the given user and server.
func NewMCPOAuthToken(userID, serverName string) *MCPOAuthToken {
	return &MCPOAuthToken{
		userID:     userID,
		serverName: serverName,
		tokenType:  "Bearer",
	}
}

func (t *MCPOAuthToken) UserID() string       { return t.userID }
func (t *MCPOAuthToken) ServerName() string   { return t.serverName }
func (t *MCPOAuthToken) ClientID() string     { return t.clientID }
func (t *MCPOAuthToken) ClientSecret() string { return t.clientSecret }
func (t *MCPOAuthToken) AccessToken() string  { return t.accessToken }
func (t *MCPOAuthToken) RefreshToken() string { return t.refreshToken }
func (t *MCPOAuthToken) ExpiresAt() time.Time { return t.expiresAt }
func (t *MCPOAuthToken) TokenType() string    { return t.tokenType }
func (t *MCPOAuthToken) TokenURL() string     { return t.tokenURL }

func (t *MCPOAuthToken) SetClientID(id string)      { t.clientID = id }
func (t *MCPOAuthToken) SetClientSecret(s string)   { t.clientSecret = s }
func (t *MCPOAuthToken) SetAccessToken(tok string)  { t.accessToken = tok }
func (t *MCPOAuthToken) SetRefreshToken(tok string) { t.refreshToken = tok }
func (t *MCPOAuthToken) SetExpiresAt(t2 time.Time)  { t.expiresAt = t2 }
func (t *MCPOAuthToken) SetTokenType(tt string)     { t.tokenType = tt }
func (t *MCPOAuthToken) SetTokenURL(u string)       { t.tokenURL = u }

// IsExpired returns true when the token expires within the next 60 seconds.
func (t *MCPOAuthToken) IsExpired() bool {
	if t.expiresAt.IsZero() {
		return false
	}
	return t.expiresAt.Before(time.Now().Add(60 * time.Second))
}

// IsEmpty returns true when no access token has been set.
func (t *MCPOAuthToken) IsEmpty() bool {
	return t.accessToken == ""
}

// BearerHeader returns the value suitable for an Authorization header.
func (t *MCPOAuthToken) BearerHeader() string {
	tt := t.tokenType
	if tt == "" {
		tt = "Bearer"
	}
	return tt + " " + t.accessToken
}
