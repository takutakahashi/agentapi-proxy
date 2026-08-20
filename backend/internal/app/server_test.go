package app

import (
	"testing"
)

func TestExtractRepoFullNameFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL",
			url:      "https://github.com/owner/repo",
			expected: "owner/repo",
		},
		{
			name:     "HTTPS URL with .git",
			url:      "https://github.com/owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "SSH URL",
			url:      "git@github.com:owner/repo.git",
			expected: "owner/repo",
		},
		{
			name:     "Invalid URL format",
			url:      "invalid-url",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, _ := extractRepoFullNameFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
