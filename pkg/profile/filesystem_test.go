package profile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemStorageBasicOperations(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "profile-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create filesystem storage
	storage, err := NewFilesystemStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create filesystem storage: %v", err)
	}

	ctx := context.Background()
	userID := "test-user-123"

	// Test Exists - should return false initially
	exists, err := storage.Exists(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to check existence: %v", err)
	}
	if exists {
		t.Error("Profile should not exist initially")
	}

	// Test Load - should return ErrProfileNotFound
	_, err = storage.Load(ctx, userID)
	if err != ErrProfileNotFound {
		t.Errorf("Expected ErrProfileNotFound, got %v", err)
	}

	// Create and save user profiles with a default profile
	userProfiles := NewUserProfiles(userID)
	userProfiles.Username = "testuser"
	userProfiles.Email = "test@example.com"

	// Add a default profile
	defaultProfile := NewProfileConfig("default")
	defaultProfile.IsDefault = true
	defaultProfile.Preferences["theme"] = "dark"
	userProfiles.Profiles = append(userProfiles.Profiles, *defaultProfile)

	err = storage.Save(ctx, userProfiles)
	if err != nil {
		t.Fatalf("Failed to save profile: %v", err)
	}

	// Test Exists - should return true now
	exists, err = storage.Exists(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to check existence: %v", err)
	}
	if !exists {
		t.Error("Profile should exist after saving")
	}

	// Test Load - should return the user profiles
	loadedUserProfiles, err := storage.Load(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to load profile: %v", err)
	}

	if loadedUserProfiles.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, loadedUserProfiles.UserID)
	}

	if loadedUserProfiles.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %s", loadedUserProfiles.Username)
	}

	if loadedUserProfiles.Email != "test@example.com" {
		t.Errorf("Expected Email 'test@example.com', got %s", loadedUserProfiles.Email)
	}

	// Check default profile preferences
	if len(loadedUserProfiles.Profiles) == 0 {
		t.Error("Expected at least one profile")
	} else {
		defaultProf := loadedUserProfiles.Profiles[0]
		if defaultProf.Preferences["theme"] != "dark" {
			t.Errorf("Expected theme 'dark', got %v", defaultProf.Preferences["theme"])
		}
	}

	// Test Update
	update := &UserProfilesUpdate{
		DisplayName: "Test User",
	}

	err = storage.Update(ctx, userID, update)
	if err != nil {
		t.Fatalf("Failed to update profile: %v", err)
	}

	// Load and verify update
	updatedUserProfiles, err := storage.Load(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to load updated user profiles: %v", err)
	}

	if updatedUserProfiles.DisplayName != "Test User" {
		t.Errorf("Expected DisplayName 'Test User', got %s", updatedUserProfiles.DisplayName)
	}

	// Original profile preferences should still exist
	if len(updatedUserProfiles.Profiles) == 0 {
		t.Error("Expected at least one profile")
	} else {
		defaultProf := updatedUserProfiles.Profiles[0]
		if defaultProf.Preferences["theme"] != "dark" {
			t.Errorf("Expected theme 'dark' to be preserved, got %v", defaultProf.Preferences["theme"])
		}
	}

	// Test List
	userIDs, err := storage.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list profiles: %v", err)
	}

	if len(userIDs) != 1 {
		t.Errorf("Expected 1 profile, got %d", len(userIDs))
	}

	if userIDs[0] != userID {
		t.Errorf("Expected userID %s in list, got %s", userID, userIDs[0])
	}

	// Test Delete
	err = storage.Delete(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to delete profile: %v", err)
	}

	// Test Exists - should return false after deletion
	exists, err = storage.Exists(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to check existence after deletion: %v", err)
	}
	if exists {
		t.Error("Profile should not exist after deletion")
	}

	// Test Load - should return ErrProfileNotFound again
	_, err = storage.Load(ctx, userID)
	if err != ErrProfileNotFound {
		t.Errorf("Expected ErrProfileNotFound after deletion, got %v", err)
	}
}

func TestFilesystemStorageInvalidInputs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	storage, err := NewFilesystemStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create filesystem storage: %v", err)
	}

	ctx := context.Background()

	// Test Save with nil profile
	err = storage.Save(ctx, nil)
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for nil profile, got %v", err)
	}

	// Test Save with empty UserID
	userProfiles := &UserProfiles{}
	err = storage.Save(ctx, userProfiles)
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test Load with empty UserID
	_, err = storage.Load(ctx, "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test Update with empty UserID
	err = storage.Update(ctx, "", &UserProfilesUpdate{})
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test Update with nil update
	err = storage.Update(ctx, "user123", nil)
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for nil update, got %v", err)
	}

	// Test Delete with empty UserID
	err = storage.Delete(ctx, "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test Exists with empty UserID
	_, err = storage.Exists(ctx, "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}
}

func TestFilesystemStorageDefaultPath(t *testing.T) {
	// Test creating storage with empty path (should use default)
	storage, err := NewFilesystemStorage("")
	if err != nil {
		t.Fatalf("Failed to create filesystem storage with default path: %v", err)
	}

	// Verify storage was created (we can't easily test the exact path without
	// accessing internal fields, but we can test basic functionality)
	ctx := context.Background()
	exists, err := storage.Exists(ctx, "test-user")
	if err != nil {
		t.Fatalf("Failed to check existence with default path: %v", err)
	}

	// Should return false (profile doesn't exist)
	if exists {
		t.Error("Profile should not exist")
	}
}

func TestFilesystemStorageFileOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	storage, err := NewFilesystemStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create filesystem storage: %v", err)
	}

	ctx := context.Background()
	userID := "test-user-123"

	// Create and save user profiles
	userProfiles := NewUserProfiles(userID)
	userProfiles.Username = "testuser"

	err = storage.Save(ctx, userProfiles)
	if err != nil {
		t.Fatalf("Failed to save profile: %v", err)
	}

	// Verify file was created
	profilePath := filepath.Join(tmpDir, userID, "profile.json")
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		t.Error("Profile file should exist")
	}

	// Delete profile
	err = storage.Delete(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to delete profile: %v", err)
	}

	// Verify file was deleted
	if _, err := os.Stat(profilePath); !os.IsNotExist(err) {
		t.Error("Profile file should be deleted")
	}
}
