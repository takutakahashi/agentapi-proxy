package repository

import (
	"fmt"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// ExtractInfo extracts repository information from tags.
// The cloneDir parameter is typically the session ID.
func ExtractInfo(tags map[string]string, cloneDir string) (*entities.RepositoryInfo, error) {
	if tags == nil {
		return nil, nil
	}

	repoURL, exists := tags["repository"]
	if !exists || repoURL == "" {
		return nil, nil
	}

	if !IsValidURL(repoURL) {
		return nil, nil
	}

	repoFullName, err := FullNameFromURL(repoURL)
	if err != nil {
		return nil, err
	}

	return &entities.RepositoryInfo{
		FullName: repoFullName,
		CloneDir: cloneDir,
	}, nil
}

// IsValidURL checks for supported GitHub URL or owner/repo formats.
func IsValidURL(repoURL string) bool {
	if strings.HasPrefix(repoURL, "https://github.com/") ||
		strings.HasPrefix(repoURL, "git@github.com:") ||
		strings.HasPrefix(repoURL, "http://github.com/") {
		return true
	}

	parts := strings.Split(repoURL, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// FullNameFromURL extracts the org/repo format from a GitHub repository URL.
func FullNameFromURL(repoURL string) (string, error) {
	var repoPath string

	if strings.HasPrefix(repoURL, "https://github.com/") {
		repoPath = strings.TrimPrefix(repoURL, "https://github.com/")
	} else if strings.HasPrefix(repoURL, "git@github.com:") {
		repoPath = strings.TrimPrefix(repoURL, "git@github.com:")
	} else if strings.HasPrefix(repoURL, "http://github.com/") {
		repoPath = strings.TrimPrefix(repoURL, "http://github.com/")
	} else {
		repoPath = repoURL
	}

	repoPath = strings.TrimSuffix(repoPath, ".git")
	if parts := strings.Split(repoPath, "/"); len(parts) != 2 {
		return "", fmt.Errorf("invalid repository path: %s", repoPath)
	}

	return repoPath, nil
}
