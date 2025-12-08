package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// credentialsFile represents the JSON structure of ~/.claude/.credentials.json
type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

// FileCredentialProvider loads credentials from ~/.claude/.credentials.json
type FileCredentialProvider struct {
	filePath string // optional: override default path
}

// NewFileCredentialProvider creates a new FileCredentialProvider with default path
func NewFileCredentialProvider() *FileCredentialProvider {
	return &FileCredentialProvider{}
}

// NewFileCredentialProviderWithPath creates a new FileCredentialProvider with custom path
func NewFileCredentialProviderWithPath(path string) *FileCredentialProvider {
	return &FileCredentialProvider{filePath: path}
}

// Name returns the provider name
func (p *FileCredentialProvider) Name() string {
	return "file"
}

// Load attempts to load credentials from the file
// Returns nil, nil if the file doesn't exist
// Returns nil, error if there was an error reading or parsing the file
func (p *FileCredentialProvider) Load() (*ClaudeCredentials, error) {
	path := p.filePath
	if path == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(homeDir, ".claude", ".credentials.json")
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	// Parse JSON
	var creds credentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials file: %w", err)
	}

	// Validate required fields
	if creds.ClaudeAiOauth.AccessToken == "" {
		return nil, nil
	}

	return &ClaudeCredentials{
		AccessToken:  creds.ClaudeAiOauth.AccessToken,
		RefreshToken: creds.ClaudeAiOauth.RefreshToken,
		ExpiresAt:    strconv.FormatInt(creds.ClaudeAiOauth.ExpiresAt, 10),
	}, nil
}
