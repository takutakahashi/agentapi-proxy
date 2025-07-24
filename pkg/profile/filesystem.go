package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FilesystemStorage implements profile storage using the local filesystem
type FilesystemStorage struct {
	basePath string
	mu       sync.RWMutex
}

// NewFilesystemStorage creates a new filesystem-based profile storage
func NewFilesystemStorage(basePath string) (*FilesystemStorage, error) {
	if basePath == "" {
		// Use default base path in user's home directory
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		basePath = filepath.Join(homeDir, ".agentapi-proxy", "profiles")
	}

	// Ensure base directory exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create profile directory: %w", err)
	}

	return &FilesystemStorage{
		basePath: basePath,
	}, nil
}

// getProfilePath returns the path to a user's profile file
func (fs *FilesystemStorage) getProfilePath(userID string) string {
	// Sanitize userID to prevent directory traversal
	safeUserID := strings.ReplaceAll(userID, "..", "")
	safeUserID = strings.ReplaceAll(safeUserID, "/", "_")
	safeUserID = strings.ReplaceAll(safeUserID, "\\", "_")

	// Use base path for profile storage
	userDir := filepath.Join(fs.basePath, safeUserID)
	_ = os.MkdirAll(userDir, 0755) // Ensure directory exists
	return filepath.Join(userDir, "profile.json")
}

// Save stores a profile to the filesystem
func (fs *FilesystemStorage) Save(ctx context.Context, profile *Profile) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if profile == nil || profile.UserID == "" {
		return ErrInvalidProfile
	}

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profile: %w", err)
	}

	profilePath := fs.getProfilePath(profile.UserID)

	// Write to temporary file first
	tmpPath := profilePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write profile: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, profilePath); err != nil {
		_ = os.Remove(tmpPath) // Clean up
		return fmt.Errorf("failed to save profile: %w", err)
	}

	return nil
}

// Load retrieves a profile from the filesystem
func (fs *FilesystemStorage) Load(ctx context.Context, userID string) (*Profile, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if userID == "" {
		return nil, ErrInvalidProfile
	}

	profilePath := fs.getProfilePath(userID)

	file, err := os.Open(profilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrProfileNotFound
		}
		return nil, fmt.Errorf("failed to open profile: %w", err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read profile: %w", err)
	}

	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile: %w", err)
	}

	return &profile, nil
}

// Update updates an existing profile
func (fs *FilesystemStorage) Update(ctx context.Context, userID string, update *ProfileUpdate) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if userID == "" || update == nil {
		return ErrInvalidProfile
	}

	// Load existing profile
	profilePath := fs.getProfilePath(userID)

	file, err := os.Open(profilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("failed to open profile: %w", err)
	}

	data, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return fmt.Errorf("failed to read profile: %w", err)
	}

	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return fmt.Errorf("failed to unmarshal profile: %w", err)
	}

	// Apply updates
	profile.Update(update)

	// Save updated profile
	updatedData, err := json.MarshalIndent(&profile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal updated profile: %w", err)
	}

	// Write to temporary file first
	tmpPath := profilePath + ".tmp"
	if err := os.WriteFile(tmpPath, updatedData, 0644); err != nil {
		return fmt.Errorf("failed to write updated profile: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, profilePath); err != nil {
		_ = os.Remove(tmpPath) // Clean up
		return fmt.Errorf("failed to save updated profile: %w", err)
	}

	return nil
}

// Delete removes a profile from the filesystem
func (fs *FilesystemStorage) Delete(ctx context.Context, userID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if userID == "" {
		return ErrInvalidProfile
	}

	profilePath := fs.getProfilePath(userID)

	if err := os.Remove(profilePath); err != nil {
		if os.IsNotExist(err) {
			return ErrProfileNotFound
		}
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	return nil
}

// Exists checks if a profile exists
func (fs *FilesystemStorage) Exists(ctx context.Context, userID string) (bool, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if userID == "" {
		return false, ErrInvalidProfile
	}

	profilePath := fs.getProfilePath(userID)

	_, err := os.Stat(profilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check profile existence: %w", err)
	}

	return true, nil
}

// List returns all profile IDs
func (fs *FilesystemStorage) List(ctx context.Context) ([]string, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var userIDs []string

	// List all directories in base path
	entries, err := os.ReadDir(fs.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list profiles: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if profile.json exists in this directory
		profilePath := filepath.Join(fs.basePath, entry.Name(), "profile.json")
		if _, err := os.Stat(profilePath); err == nil {
			userIDs = append(userIDs, entry.Name())
		}
	}

	return userIDs, nil
}
