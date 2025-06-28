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

// GetUserEnvironment returns environment variables with user-specific HOME set
func (m *Manager) GetUserEnvironment(userID string, baseEnv []string) ([]string, error) {
	if !m.enabled {
		return baseEnv, nil
	}

	userDir, err := m.EnsureUserHomeDir(userID)
	if err != nil {
		return nil, err
	}

	// Create a copy of the base environment
	env := make([]string, 0, len(baseEnv)+1)
	homeSet := false

	// Copy existing environment variables, replacing HOME if it exists
	for _, envVar := range baseEnv {
		if strings.HasPrefix(envVar, "HOME=") {
			env = append(env, fmt.Sprintf("HOME=%s", userDir))
			homeSet = true
		} else {
			env = append(env, envVar)
		}
	}

	// If HOME wasn't in the base environment, add it
	if !homeSet {
		env = append(env, fmt.Sprintf("HOME=%s", userDir))
	}

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

// IsEnabled returns whether multiple users mode is enabled
func (m *Manager) IsEnabled() bool {
	return m.enabled
}
