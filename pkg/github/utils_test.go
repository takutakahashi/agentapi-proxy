package github

import (
	"os"
	"testing"
)

func TestParseRepositoryURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "SSH format with github.com",
			url:      "git@github.com:owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "SSH format without .git",
			url:      "git@github.com:owner/repo",
			expected: "owner/repo",
		},
		{
			name:     "HTTPS format with github.com",
			url:      "https://github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "HTTPS format without .git",
			url:      "https://github.com/owner/repo",
			expected: "owner/repo",
		},
		{
			name:     "HTTPS format with token",
			url:      "https://token@github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "HTTP format",
			url:      "http://github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "Git protocol format",
			url:      "git://github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "Enterprise SSH format",
			url:      "git@github.enterprise.com:owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "Enterprise HTTPS format",
			url:      "https://github.enterprise.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "Invalid format",
			url:      "invalid-url",
			expected: "",
		},
		{
			name:     "Empty URL",
			url:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseRepositoryURL(tt.url)
			if result != tt.expected {
				t.Errorf("ParseRepositoryURL(%q) = %q, expected %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestGetAPIBase(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "Default GitHub API",
			envValue: "",
			expected: "https://api.github.com",
		},
		{
			name:     "Custom GitHub Enterprise API",
			envValue: "https://github.enterprise.com/api/v3",
			expected: "https://github.enterprise.com/api/v3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env value
			originalValue := os.Getenv("GITHUB_API")
			defer func() {
				if originalValue != "" {
					_ = os.Setenv("GITHUB_API", originalValue)
				} else {
					_ = os.Unsetenv("GITHUB_API")
				}
			}()

			// Set test env value
			if tt.envValue != "" {
				_ = os.Setenv("GITHUB_API", tt.envValue)
			} else {
				_ = os.Unsetenv("GITHUB_API")
			}

			result := GetAPIBase()
			if result != tt.expected {
				t.Errorf("GetAPIBase() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestGetGitHubURL(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected string
	}{
		{
			name:     "Default GitHub URL",
			envValue: "",
			expected: "https://github.com",
		},
		{
			name:     "Custom GitHub Enterprise URL",
			envValue: "https://github.enterprise.com",
			expected: "https://github.enterprise.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env value
			originalValue := os.Getenv("GITHUB_URL")
			defer func() {
				if originalValue != "" {
					_ = os.Setenv("GITHUB_URL", originalValue)
				} else {
					_ = os.Unsetenv("GITHUB_URL")
				}
			}()

			// Set test env value
			if tt.envValue != "" {
				_ = os.Setenv("GITHUB_URL", tt.envValue)
			} else {
				_ = os.Unsetenv("GITHUB_URL")
			}

			result := GetGitHubURL()
			if result != tt.expected {
				t.Errorf("GetGitHubURL() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestExtractHostname(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL",
			url:      "https://github.com",
			expected: "github.com",
		},
		{
			name:     "HTTPS URL with path",
			url:      "https://github.com/path/to/something",
			expected: "github.com",
		},
		{
			name:     "HTTP URL",
			url:      "http://github.enterprise.com",
			expected: "github.enterprise.com",
		},
		{
			name:     "URL without protocol",
			url:      "github.com",
			expected: "github.com",
		},
		{
			name:     "Enterprise URL",
			url:      "https://github.enterprise.com/api/v3",
			expected: "github.enterprise.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractHostname(tt.url)
			if result != tt.expected {
				t.Errorf("ExtractHostname(%q) = %q, expected %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestCreateAuthenticatedURL(t *testing.T) {
	tests := []struct {
		name      string
		repoURL   string
		token     string
		githubURL string
		expected  string
		expectErr bool
	}{
		{
			name:      "HTTPS repo URL",
			repoURL:   "https://github.com/owner/repo",
			token:     "token123",
			githubURL: "https://github.com",
			expected:  "https://token123@github.com/owner/repo",
			expectErr: false,
		},
		{
			name:      "SSH repo URL",
			repoURL:   "git@github.com:owner/repo.git",
			token:     "token123",
			githubURL: "https://github.com",
			expected:  "https://token123@github.com/owner/repo.git",
			expectErr: false,
		},
		{
			name:      "Unsupported format",
			repoURL:   "ftp://example.com/repo",
			token:     "token123",
			githubURL: "https://github.com",
			expected:  "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set GitHub URL environment variable
			originalValue := os.Getenv("GITHUB_URL")
			defer func() {
				if originalValue != "" {
					_ = os.Setenv("GITHUB_URL", originalValue)
				} else {
					_ = os.Unsetenv("GITHUB_URL")
				}
			}()

			if tt.githubURL != "" {
				_ = os.Setenv("GITHUB_URL", tt.githubURL)
			} else {
				_ = os.Unsetenv("GITHUB_URL")
			}

			result, err := CreateAuthenticatedURL(tt.repoURL, tt.token)
			if tt.expectErr {
				if err == nil {
					t.Errorf("CreateAuthenticatedURL(%q, %q) expected error, got nil", tt.repoURL, tt.token)
				}
			} else {
				if err != nil {
					t.Errorf("CreateAuthenticatedURL(%q, %q) unexpected error: %v", tt.repoURL, tt.token, err)
				}
				if result != tt.expected {
					t.Errorf("CreateAuthenticatedURL(%q, %q) = %q, expected %q", tt.repoURL, tt.token, result, tt.expected)
				}
			}
		})
	}
}
