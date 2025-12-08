package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCredentialProvider_Load(t *testing.T) {
	// Create temp directory
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		fileContent string
		filePath    string
		wantCreds   *ClaudeCredentials
		wantErr     bool
	}{
		{
			name: "valid credentials file",
			fileContent: `{
				"claudeAiOauth": {
					"accessToken": "sk-ant-oat01-test-access-token",
					"refreshToken": "sk-ant-ort01-test-refresh-token",
					"expiresAt": 1765205562255
				}
			}`,
			wantCreds: &ClaudeCredentials{
				AccessToken:  "sk-ant-oat01-test-access-token",
				RefreshToken: "sk-ant-ort01-test-refresh-token",
				ExpiresAt:    "1765205562255",
			},
			wantErr: false,
		},
		{
			name:        "missing access token",
			fileContent: `{"claudeAiOauth": {"refreshToken": "token", "expiresAt": 123}}`,
			wantCreds:   nil,
			wantErr:     false,
		},
		{
			name:        "invalid JSON",
			fileContent: `{invalid json}`,
			wantCreds:   nil,
			wantErr:     true,
		},
		{
			name:      "file does not exist",
			filePath:  filepath.Join(tempDir, "nonexistent", ".credentials.json"),
			wantCreds: nil,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var provider *FileCredentialProvider

			if tt.filePath != "" {
				provider = NewFileCredentialProviderWithPath(tt.filePath)
			} else {
				// Create temp file with content
				filePath := filepath.Join(tempDir, tt.name+".credentials.json")
				if err := os.WriteFile(filePath, []byte(tt.fileContent), 0600); err != nil {
					t.Fatalf("failed to write temp file: %v", err)
				}
				provider = NewFileCredentialProviderWithPath(filePath)
			}

			creds, err := provider.Load()

			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantCreds == nil {
				if creds != nil {
					t.Errorf("Load() got = %v, want nil", creds)
				}
				return
			}

			if creds == nil {
				t.Errorf("Load() got nil, want %v", tt.wantCreds)
				return
			}

			if creds.AccessToken != tt.wantCreds.AccessToken {
				t.Errorf("AccessToken = %v, want %v", creds.AccessToken, tt.wantCreds.AccessToken)
			}
			if creds.RefreshToken != tt.wantCreds.RefreshToken {
				t.Errorf("RefreshToken = %v, want %v", creds.RefreshToken, tt.wantCreds.RefreshToken)
			}
			if creds.ExpiresAt != tt.wantCreds.ExpiresAt {
				t.Errorf("ExpiresAt = %v, want %v", creds.ExpiresAt, tt.wantCreds.ExpiresAt)
			}
		})
	}
}

func TestFileCredentialProvider_Name(t *testing.T) {
	provider := NewFileCredentialProvider()
	if provider.Name() != "file" {
		t.Errorf("Name() = %v, want 'file'", provider.Name())
	}
}

func TestEnvCredentialProvider_Load(t *testing.T) {
	tests := []struct {
		name      string
		envVars   map[string]string
		wantCreds *ClaudeCredentials
	}{
		{
			name: "all env vars set",
			envVars: map[string]string{
				EnvClaudeAccessToken:  "test-access-token",
				EnvClaudeRefreshToken: "test-refresh-token",
				EnvClaudeExpiresAt:    "1234567890",
			},
			wantCreds: &ClaudeCredentials{
				AccessToken:  "test-access-token",
				RefreshToken: "test-refresh-token",
				ExpiresAt:    "1234567890",
			},
		},
		{
			name: "only access token set",
			envVars: map[string]string{
				EnvClaudeAccessToken: "test-access-token",
			},
			wantCreds: &ClaudeCredentials{
				AccessToken:  "test-access-token",
				RefreshToken: "",
				ExpiresAt:    "",
			},
		},
		{
			name:      "no env vars set",
			envVars:   map[string]string{},
			wantCreds: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear and set env vars
			_ = os.Unsetenv(EnvClaudeAccessToken)
			_ = os.Unsetenv(EnvClaudeRefreshToken)
			_ = os.Unsetenv(EnvClaudeExpiresAt)

			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}
			defer func() {
				_ = os.Unsetenv(EnvClaudeAccessToken)
				_ = os.Unsetenv(EnvClaudeRefreshToken)
				_ = os.Unsetenv(EnvClaudeExpiresAt)
			}()

			provider := NewEnvCredentialProvider()
			creds, err := provider.Load()

			if err != nil {
				t.Errorf("Load() unexpected error: %v", err)
				return
			}

			if tt.wantCreds == nil {
				if creds != nil {
					t.Errorf("Load() got = %v, want nil", creds)
				}
				return
			}

			if creds == nil {
				t.Errorf("Load() got nil, want %v", tt.wantCreds)
				return
			}

			if creds.AccessToken != tt.wantCreds.AccessToken {
				t.Errorf("AccessToken = %v, want %v", creds.AccessToken, tt.wantCreds.AccessToken)
			}
			if creds.RefreshToken != tt.wantCreds.RefreshToken {
				t.Errorf("RefreshToken = %v, want %v", creds.RefreshToken, tt.wantCreds.RefreshToken)
			}
			if creds.ExpiresAt != tt.wantCreds.ExpiresAt {
				t.Errorf("ExpiresAt = %v, want %v", creds.ExpiresAt, tt.wantCreds.ExpiresAt)
			}
		})
	}
}

func TestEnvCredentialProvider_Name(t *testing.T) {
	provider := NewEnvCredentialProvider()
	if provider.Name() != "env" {
		t.Errorf("Name() = %v, want 'env'", provider.Name())
	}
}

func TestChainCredentialProvider_Load(t *testing.T) {
	tests := []struct {
		name      string
		providers []CredentialProvider
		wantCreds *ClaudeCredentials
		wantErr   bool
	}{
		{
			name: "first provider succeeds",
			providers: []CredentialProvider{
				&mockCredentialProvider{
					name:  "mock1",
					creds: &ClaudeCredentials{AccessToken: "token1"},
				},
				&mockCredentialProvider{
					name:  "mock2",
					creds: &ClaudeCredentials{AccessToken: "token2"},
				},
			},
			wantCreds: &ClaudeCredentials{AccessToken: "token1"},
			wantErr:   false,
		},
		{
			name: "first returns nil, second succeeds",
			providers: []CredentialProvider{
				&mockCredentialProvider{name: "mock1", creds: nil},
				&mockCredentialProvider{
					name:  "mock2",
					creds: &ClaudeCredentials{AccessToken: "token2"},
				},
			},
			wantCreds: &ClaudeCredentials{AccessToken: "token2"},
			wantErr:   false,
		},
		{
			name: "all providers return nil",
			providers: []CredentialProvider{
				&mockCredentialProvider{name: "mock1", creds: nil},
				&mockCredentialProvider{name: "mock2", creds: nil},
			},
			wantCreds: nil,
			wantErr:   false,
		},
		{
			name:      "empty provider list",
			providers: []CredentialProvider{},
			wantCreds: nil,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewChainCredentialProvider(tt.providers...)
			creds, err := provider.Load()

			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantCreds == nil {
				if creds != nil {
					t.Errorf("Load() got = %v, want nil", creds)
				}
				return
			}

			if creds == nil {
				t.Errorf("Load() got nil, want %v", tt.wantCreds)
				return
			}

			if creds.AccessToken != tt.wantCreds.AccessToken {
				t.Errorf("AccessToken = %v, want %v", creds.AccessToken, tt.wantCreds.AccessToken)
			}
		})
	}
}

func TestChainCredentialProvider_Name(t *testing.T) {
	provider := NewChainCredentialProvider()
	if provider.Name() != "chain" {
		t.Errorf("Name() = %v, want 'chain'", provider.Name())
	}
}

// mockCredentialProvider is a mock implementation of CredentialProvider for testing
type mockCredentialProvider struct {
	name  string
	creds *ClaudeCredentials
	err   error
}

func (m *mockCredentialProvider) Name() string {
	return m.name
}

func (m *mockCredentialProvider) Load() (*ClaudeCredentials, error) {
	return m.creds, m.err
}
