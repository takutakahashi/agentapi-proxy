package github

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ParseRepositoryURL extracts owner/repo from various Git URL formats
func ParseRepositoryURL(url string) string {
	// Handle SSH URLs: git@hostname:owner/repo.git
	if strings.Contains(url, "@") && strings.Contains(url, ":") && !strings.Contains(url, "://") {
		// SSH format: git@hostname:owner/repo.git
		parts := strings.Split(url, ":")
		if len(parts) >= 2 {
			path := strings.Join(parts[1:], ":")
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	// Handle HTTPS URLs: https://hostname/owner/repo.git
	if strings.HasPrefix(url, "https://") {
		// Remove https:// prefix
		withoutProtocol := strings.TrimPrefix(url, "https://")

		// Handle URLs with authentication token: token@hostname/owner/repo.git
		if strings.Contains(withoutProtocol, "@") {
			parts := strings.Split(withoutProtocol, "@")
			if len(parts) >= 2 {
				withoutProtocol = strings.Join(parts[1:], "@")
			}
		}

		// Extract path after hostname: hostname/owner/repo.git -> owner/repo.git
		pathParts := strings.Split(withoutProtocol, "/")
		if len(pathParts) >= 3 {
			// Join owner/repo parts
			path := strings.Join(pathParts[1:], "/")
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	// Handle HTTP URLs: http://hostname/owner/repo.git
	if strings.HasPrefix(url, "http://") {
		// Remove http:// prefix
		withoutProtocol := strings.TrimPrefix(url, "http://")

		// Handle URLs with authentication token: token@hostname/owner/repo.git
		if strings.Contains(withoutProtocol, "@") {
			parts := strings.Split(withoutProtocol, "@")
			if len(parts) >= 2 {
				withoutProtocol = strings.Join(parts[1:], "@")
			}
		}

		// Extract path after hostname: hostname/owner/repo.git -> owner/repo.git
		pathParts := strings.Split(withoutProtocol, "/")
		if len(pathParts) >= 3 {
			// Join owner/repo parts
			path := strings.Join(pathParts[1:], "/")
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	// Handle git protocol URLs: git://hostname/owner/repo.git
	if strings.HasPrefix(url, "git://") {
		// Remove git:// prefix
		withoutProtocol := strings.TrimPrefix(url, "git://")

		// Extract path after hostname: hostname/owner/repo.git -> owner/repo.git
		pathParts := strings.Split(withoutProtocol, "/")
		if len(pathParts) >= 3 {
			// Join owner/repo parts
			path := strings.Join(pathParts[1:], "/")
			path = strings.TrimSuffix(path, ".git")
			return path
		}
	}

	return ""
}

// GetRepositoryFromGitRemote extracts repository full name from git remote
func GetRepositoryFromGitRemote() string {
	// Try to get the current working directory first
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	// Use git to get the remote origin URL
	cmd := exec.Command("git", "config", "--get", "remote.origin.url")
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	remoteURL := strings.TrimSpace(string(output))
	return ParseRepositoryURL(remoteURL)
}

// GetAPIBase returns the GitHub API base URL (supports enterprise)
func GetAPIBase() string {
	apiBase := os.Getenv("GITHUB_API")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	return apiBase
}

// GetGitHubURL returns the GitHub URL (supports enterprise)
func GetGitHubURL() string {
	githubURL := os.Getenv("GITHUB_URL")
	if githubURL == "" {
		githubURL = "https://github.com"
	}
	return githubURL
}

// ExtractHostname extracts hostname from GitHub URL
func ExtractHostname(githubURL string) string {
	// Remove protocol prefix
	hostname := strings.TrimPrefix(githubURL, "https://")
	hostname = strings.TrimPrefix(hostname, "http://")

	// Remove any path components
	if idx := strings.Index(hostname, "/"); idx != -1 {
		hostname = hostname[:idx]
	}

	return hostname
}

// CreateAuthenticatedURL creates an authenticated GitHub URL with token
func CreateAuthenticatedURL(repoURL, token string) (string, error) {
	githubURL := GetGitHubURL()
	githubHost := strings.TrimPrefix(githubURL, "https://")
	githubHost = strings.TrimPrefix(githubHost, "http://")

	// Parse the repository URL and insert the token
	if strings.HasPrefix(repoURL, githubURL+"/") {
		parts := strings.TrimPrefix(repoURL, githubURL+"/")
		return fmt.Sprintf("https://%s@%s/%s", token, githubHost, parts), nil
	} else if strings.HasPrefix(repoURL, "git@"+githubHost+":") {
		parts := strings.TrimPrefix(repoURL, "git@"+githubHost+":")
		parts = strings.TrimSuffix(parts, ".git")
		return fmt.Sprintf("https://%s@%s/%s.git", token, githubHost, parts), nil
	}

	return "", fmt.Errorf("unsupported repository URL format: %s", repoURL)
}
