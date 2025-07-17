package userdir

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Manager handles user-specific directory operations
type Manager struct {
	baseDir string
	enabled bool
}

// NewManager creates a new user directory manager
func NewManager(baseDir string, enabled bool) *Manager {
	return &Manager{
		baseDir: baseDir,
		enabled: enabled,
	}
}

// GetUserHomeDir returns the home directory for a specific user
func (m *Manager) GetUserHomeDir(userID string) (string, error) {
	if !m.enabled {
		return os.Getenv("HOME"), nil
	}

	if userID == "" {
		return "", fmt.Errorf("user ID cannot be empty when multiple users is enabled")
	}

	// Sanitize user ID to prevent directory traversal
	sanitizedUserID := sanitizeUserID(userID)
	if sanitizedUserID == "" {
		return "", fmt.Errorf("invalid user ID: %s", userID)
	}

	userDir := filepath.Join(m.baseDir, "users", sanitizedUserID)
	return userDir, nil
}

// EnsureUserHomeDir creates the user home directory if it doesn't exist
func (m *Manager) EnsureUserHomeDir(userID string) (string, error) {
	userDir, err := m.GetUserHomeDir(userID)
	if err != nil {
		return "", err
	}

	if !m.enabled {
		return userDir, nil
	}

	// Create directory with appropriate permissions
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create user directory %s: %w", userDir, err)
	}

	return userDir, nil
}

// GetUserClaudeDir returns the Claude directory for a specific user
func (m *Manager) GetUserClaudeDir(userID string) (string, error) {
	if !m.enabled {
		// Return default .claude location
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		return filepath.Join(homeDir, ".claude"), nil
	}

	if userID == "" {
		return "", fmt.Errorf("user ID cannot be empty when multiple users is enabled")
	}

	// Sanitize user ID to prevent directory traversal
	sanitizedUserID := sanitizeUserID(userID)
	if sanitizedUserID == "" {
		return "", fmt.Errorf("invalid user ID: %s", userID)
	}

	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/home/agentapi"
	}

	claudeDir := filepath.Join(homeDir, ".claude", sanitizedUserID)
	return claudeDir, nil
}

// EnsureUserClaudeDir creates the user Claude directory if it doesn't exist
func (m *Manager) EnsureUserClaudeDir(userID string) (string, error) {
	claudeDir, err := m.GetUserClaudeDir(userID)
	if err != nil {
		return "", err
	}

	// Create directory with appropriate permissions
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create Claude directory %s: %w", claudeDir, err)
	}

	return claudeDir, nil
}

// sanitizeUserID removes potentially dangerous characters from user ID
func sanitizeUserID(userID string) string {
	// Check for null bytes
	if strings.Contains(userID, "\x00") {
		return ""
	}

	// Check maximum length
	if len(userID) > 64 {
		return ""
	}

	// Normalize Unicode to prevent bypasses
	userID = strings.ToValidUTF8(userID, "_")

	// Check for dangerous patterns
	if strings.Contains(userID, "..") ||
		strings.HasPrefix(userID, ".") ||
		strings.HasSuffix(userID, ".") ||
		strings.Contains(userID, "/") ||
		strings.Contains(userID, "\\") {
		return ""
	}

	// Validate using regex - only allow alphanumeric, dash, underscore
	validPattern := regexp.MustCompile(`^[a-zA-Z0-9\-_]+$`)
	if !validPattern.MatchString(userID) {
		return ""
	}

	// Additional check for control characters
	for _, r := range userID {
		if unicode.IsControl(r) {
			return ""
		}
	}

	// Special case: if the result is empty or only special chars, return empty
	if userID == "" || strings.Trim(userID, "_-") == "" {
		return ""
	}

	// Convert to lowercase for consistency
	return strings.ToLower(userID)
}

// SetupUserHome creates user-specific home directory and returns environment variables
func SetupUserHome(userID string) (map[string]string, error) {
	// If userID is empty, return current HOME environment variable
	if userID == "" {
		return map[string]string{}, nil
	}

	// Sanitize user ID to prevent directory traversal
	sanitizedUserID := sanitizeUserID(userID)
	if sanitizedUserID == "" {
		return nil, fmt.Errorf("invalid user ID: %s", userID)
	}

	// Get base directory from USERHOME_BASEDIR environment variable
	// Default to $HOME/.agentapi-proxy if not set
	baseDir := os.Getenv("USERHOME_BASEDIR")
	if baseDir == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		baseDir = filepath.Join(homeDir, ".agentapi-proxy")
	}

	// Create user-specific home directory path
	userHomeDir := filepath.Join(baseDir, "myclaudes", sanitizedUserID)

	// Create directory with appropriate permissions if it doesn't exist
	if err := os.MkdirAll(userHomeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create user home directory %s: %w", userHomeDir, err)
	}

	// Create .bashrc with mise activation
	bashrcPath := filepath.Join(userHomeDir, ".bashrc")
	bashrcContent := `# Auto-generated .bashrc for agentapi user home
# Activate mise (development tools version manager)
eval "$(/home/agentapi/.local/bin/mise activate bash)"
`

	// Write .bashrc file if it doesn't exist
	if _, err := os.Stat(bashrcPath); os.IsNotExist(err) {
		if err := os.WriteFile(bashrcPath, []byte(bashrcContent), 0644); err != nil {
			return nil, fmt.Errorf("failed to create .bashrc in user home directory %s: %w", userHomeDir, err)
		}
	}

	// Copy CLAUDE.md to user home directory if it doesn't exist
	if err := copyClaudeMdToUserHome(userHomeDir); err != nil {
		// Log the error but don't fail the setup
		fmt.Printf("Warning: Failed to copy CLAUDE.md to user home directory: %v\n", err)
	}

	// Get Git repository information
	env := map[string]string{
		"HOME": userHomeDir,
	}

	// Try to get GITHUB_REPO_FULLNAME from git remote
	if repoFullName := getGitHubRepoFullName(); repoFullName != "" {
		env["GITHUB_REPO_FULLNAME"] = repoFullName
	}

	return env, nil
}

// IsEnabled returns whether multiple users mode is enabled
func (m *Manager) IsEnabled() bool {
	return m.enabled
}

// copyClaudeMdToUserHome copies CLAUDE.md to the user's home directory
func copyClaudeMdToUserHome(userHomeDir string) error {
	// Path to the original CLAUDE.md file
	originalClaudeMdPath := filepath.Join(userHomeDir, "CLAUDE.md")

	// Path to the .claude directory
	claudeDir := filepath.Join(userHomeDir, ".claude")

	// Create .claude directory if it doesn't exist
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory %s: %w", claudeDir, err)
	}

	// Target path for CLAUDE.md in .claude directory
	targetClaudeMdPath := filepath.Join(claudeDir, "CLAUDE.md")

	// Check if CLAUDE.md already exists in the target location
	if _, err := os.Stat(targetClaudeMdPath); err == nil {
		// File already exists, no need to copy
		return nil
	}

	// Find the source CLAUDE.md file
	sourcePath := ""

	// First try to find CLAUDE.md using environment variable
	if claudeMdPath := os.Getenv("CLAUDE_MD_PATH"); claudeMdPath != "" {
		if _, err := os.Stat(claudeMdPath); err == nil {
			sourcePath = claudeMdPath
		}
	}

	// If not found via environment variable, try the working directory
	if sourcePath == "" {
		if _, err := os.Stat(originalClaudeMdPath); err == nil {
			sourcePath = originalClaudeMdPath
		}
	}

	// If still not found, try the agentapi working directory
	if sourcePath == "" {
		agentapiWorkdir := os.Getenv("AGENTAPI_WORKDIR")
		if agentapiWorkdir != "" {
			agentapiClaudeMdPath := filepath.Join(agentapiWorkdir, "CLAUDE.md")
			if _, err := os.Stat(agentapiClaudeMdPath); err == nil {
				sourcePath = agentapiClaudeMdPath
			}
		}
	}

	// If still not found, try the default location
	if sourcePath == "" {
		defaultClaudeMdPath := "/home/agentapi/workdir/CLAUDE.md"
		if _, err := os.Stat(defaultClaudeMdPath); err == nil {
			sourcePath = defaultClaudeMdPath
		}
	}

	// If CLAUDE.md is not found, return without error
	if sourcePath == "" {
		return nil
	}

	// Read the source file
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read source CLAUDE.md file %s: %w", sourcePath, err)
	}

	// Write to the target location
	if err := os.WriteFile(targetClaudeMdPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write CLAUDE.md to %s: %w", targetClaudeMdPath, err)
	}

	return nil
}

// getGitHubRepoFullName extracts repository full name from git remote
func getGitHubRepoFullName() string {
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
	return extractRepoFullNameFromURL(remoteURL)
}

// extractRepoFullNameFromURL extracts owner/repo from various Git URL formats
func extractRepoFullNameFromURL(url string) string {
	// Handle SSH URLs: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		path := strings.TrimPrefix(url, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		return path
	}

	// Handle HTTPS URLs: https://github.com/owner/repo.git
	if strings.HasPrefix(url, "https://github.com/") {
		path := strings.TrimPrefix(url, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		return path
	}

	// Handle git protocol URLs: git://github.com/owner/repo.git
	if strings.HasPrefix(url, "git://github.com/") {
		path := strings.TrimPrefix(url, "git://github.com/")
		path = strings.TrimSuffix(path, ".git")
		return path
	}

	return ""
}
