package proxy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/takutakahashi/agentapi-proxy/pkg/userdir"
)

// FileCredentialProvider loads credentials from user-specific credential files
// When userID is provided, it looks for credentials at:
// $HOME/.agentapi-proxy/myclaudes/[userID]/.claude/.credentials.json
// When userID is empty, it falls back to ~/.claude/.credentials.json
type FileCredentialProvider struct {
	filePath string // optional: override default path (for testing)
}

// NewFileCredentialProvider creates a new FileCredentialProvider with default path
func NewFileCredentialProvider() *FileCredentialProvider {
	return &FileCredentialProvider{}
}

// NewFileCredentialProviderWithPath creates a new FileCredentialProvider with custom path
// This is primarily used for testing
func NewFileCredentialProviderWithPath(path string) *FileCredentialProvider {
	return &FileCredentialProvider{filePath: path}
}

// Name returns the provider name
func (p *FileCredentialProvider) Name() string {
	return "file"
}

// Load attempts to load credentials from the file
// If userID is provided, looks in the user-specific directory
// Returns nil, nil if the file doesn't exist
// Returns nil, error if there was an error reading the file
func (p *FileCredentialProvider) Load(userID string) (*ClaudeCredentials, error) {
	path := p.filePath
	if path == "" {
		var err error
		path, err = p.getCredentialPath(userID)
		if err != nil {
			return nil, err
		}
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	// Read file content directly without parsing
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Return credentials with raw file content
	return &ClaudeCredentials{
		RawJSON: data,
	}, nil
}

// getCredentialPath returns the path to the credentials file for the given user
func (p *FileCredentialProvider) getCredentialPath(userID string) (string, error) {
	if userID != "" {
		// Use user-specific path: $HOME/.agentapi-proxy/myclaudes/[userID]/.claude/.credentials.json
		envMap, err := userdir.SetupUserHome(userID)
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}

		userHome, ok := envMap["HOME"]
		if !ok || userHome == "" {
			return "", fmt.Errorf("failed to get user home directory for user: %s", userID)
		}

		return filepath.Join(userHome, ".claude", ".credentials.json"), nil
	}

	// Fall back to default path: ~/.claude/.credentials.json
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".claude", ".credentials.json"), nil
}
