package services

import "os"

const (
	// Environment variable names for Claude credentials
	EnvClaudeAccessToken  = "CLAUDE_ACCESS_TOKEN"
	EnvClaudeRefreshToken = "CLAUDE_REFRESH_TOKEN"
	EnvClaudeExpiresAt    = "CLAUDE_EXPIRES_AT"
)

// EnvCredentialProvider loads credentials from environment variables
type EnvCredentialProvider struct{}

// NewEnvCredentialProvider creates a new EnvCredentialProvider
func NewEnvCredentialProvider() *EnvCredentialProvider {
	return &EnvCredentialProvider{}
}

// Name returns the provider name
func (p *EnvCredentialProvider) Name() string {
	return "env"
}

// Load attempts to load credentials from environment variables
// userID is ignored for environment variable provider
// Returns nil, nil if CLAUDE_ACCESS_TOKEN is not set
func (p *EnvCredentialProvider) Load(_ string) (*ClaudeCredentials, error) {
	accessToken := os.Getenv(EnvClaudeAccessToken)
	if accessToken == "" {
		return nil, nil
	}

	return &ClaudeCredentials{
		AccessToken:  accessToken,
		RefreshToken: os.Getenv(EnvClaudeRefreshToken),
		ExpiresAt:    os.Getenv(EnvClaudeExpiresAt),
	}, nil
}
