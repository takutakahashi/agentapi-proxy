package repositories

import (
	"testing"
)

func TestNormalizeEnterpriseURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "hostname only",
			input:    "github.enterprise.com",
			expected: "github.enterprise.com",
		},
		{
			name:     "https URL",
			input:    "https://github.enterprise.com",
			expected: "github.enterprise.com",
		},
		{
			name:     "http URL",
			input:    "http://github.enterprise.com",
			expected: "github.enterprise.com",
		},
		{
			name:     "https URL with trailing slash",
			input:    "https://github.enterprise.com/",
			expected: "github.enterprise.com",
		},
		{
			name:     "uppercase URL",
			input:    "HTTPS://GitHub.Enterprise.Com",
			expected: "github.enterprise.com",
		},
		{
			name:     "URL with whitespace",
			input:    "  https://github.enterprise.com  ",
			expected: "github.enterprise.com",
		},
		{
			name:     "hostname with port",
			input:    "https://github.enterprise.com:8443",
			expected: "github.enterprise.com:8443",
		},
		{
			name:     "hostname only with port",
			input:    "github.enterprise.com:8443",
			expected: "github.enterprise.com:8443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeEnterpriseURL(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeEnterpriseURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeEnterpriseURL_Matching(t *testing.T) {
	// Test that webhook config URL and GitHub header match after normalization
	tests := []struct {
		name          string
		webhookConfig string // URL set in webhook configuration
		githubHeader  string // X-GitHub-Enterprise-Host header value
		shouldMatch   bool
	}{
		{
			name:          "full URL matches hostname",
			webhookConfig: "https://github.enterprise.com",
			githubHeader:  "github.enterprise.com",
			shouldMatch:   true,
		},
		{
			name:          "both full URLs",
			webhookConfig: "https://github.enterprise.com",
			githubHeader:  "https://github.enterprise.com",
			shouldMatch:   true,
		},
		{
			name:          "both hostnames",
			webhookConfig: "github.enterprise.com",
			githubHeader:  "github.enterprise.com",
			shouldMatch:   true,
		},
		{
			name:          "case insensitive",
			webhookConfig: "https://GitHub.Enterprise.Com",
			githubHeader:  "github.enterprise.com",
			shouldMatch:   true,
		},
		{
			name:          "different hosts should not match",
			webhookConfig: "https://github.enterprise1.com",
			githubHeader:  "github.enterprise2.com",
			shouldMatch:   false,
		},
		{
			name:          "github.com (empty) should not match enterprise",
			webhookConfig: "",
			githubHeader:  "github.enterprise.com",
			shouldMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizedConfig := normalizeEnterpriseURL(tt.webhookConfig)
			normalizedHeader := normalizeEnterpriseURL(tt.githubHeader)
			matched := normalizedConfig == normalizedHeader
			if matched != tt.shouldMatch {
				t.Errorf("normalizeEnterpriseURL(%q) = %q, normalizeEnterpriseURL(%q) = %q, matched = %v, want %v",
					tt.webhookConfig, normalizedConfig, tt.githubHeader, normalizedHeader, matched, tt.shouldMatch)
			}
		})
	}
}
