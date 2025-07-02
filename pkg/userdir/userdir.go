package userdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// GetUserEnvironment returns environment variables for user-specific directory setup
func (m *Manager) GetUserEnvironment(userID string, baseEnv []string) ([]string, error) {
	if !m.enabled {
		return baseEnv, nil
	}

	// Sanitize user ID for use in directory path
	sanitizedUserID := sanitizeUserID(userID)
	if sanitizedUserID == "" {
		return nil, fmt.Errorf("invalid user ID: %s", userID)
	}

	// Create a copy of the base environment
	env := make([]string, 0, len(baseEnv))
	env = append(env, baseEnv...)

	return env, nil
}

// sanitizeUserID removes potentially dangerous characters from user ID
func sanitizeUserID(userID string) string {
	// Replace dangerous characters
	sanitized := userID
	sanitized = strings.ReplaceAll(sanitized, "/", "_")
	sanitized = strings.ReplaceAll(sanitized, "\\", "_")
	sanitized = strings.ReplaceAll(sanitized, "..", "__")
	sanitized = strings.ReplaceAll(sanitized, " ", "_")

	// Remove leading/trailing dots and dashes
	sanitized = strings.Trim(sanitized, ".-")

	// Special case: if the result is only underscores or empty, return empty
	if sanitized == "" || strings.Trim(sanitized, "_") == "" {
		return ""
	}

	return sanitized
}

// SetupUserHome creates user-specific home directory and returns environment variables
func SetupUserHome(userID string) (map[string]string, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID cannot be empty")
	}

	// Sanitize user ID to prevent directory traversal
	sanitizedUserID := sanitizeUserID(userID)
	if sanitizedUserID == "" {
		return nil, fmt.Errorf("invalid user ID: %s", userID)
	}

	// Create user-specific home directory path
	userHomeDir := filepath.Join("/home/agentapi/myclaudes", sanitizedUserID)

	// Create directory with appropriate permissions if it doesn't exist
	if err := os.MkdirAll(userHomeDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create user home directory %s: %w", userHomeDir, err)
	}

	// Return environment variables map
	return map[string]string{
		"HOME": userHomeDir,
	}, nil
}

// IsEnabled returns whether multiple users mode is enabled
func (m *Manager) IsEnabled() bool {
	return m.enabled
}
