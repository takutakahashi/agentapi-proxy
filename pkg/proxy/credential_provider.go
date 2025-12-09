package proxy

// ClaudeCredentials represents Claude authentication credentials
type ClaudeCredentials struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    string // epoch milliseconds as string
}

// CredentialProvider is an interface for loading Claude credentials from various sources
type CredentialProvider interface {
	// Name returns the provider name for logging purposes
	Name() string

	// Load attempts to load credentials from this provider for the specified user
	// userID is used to locate user-specific credential files
	// Returns nil, nil if credentials are not available (not an error)
	// Returns nil, error if there was an error loading credentials
	Load(userID string) (*ClaudeCredentials, error)
}
