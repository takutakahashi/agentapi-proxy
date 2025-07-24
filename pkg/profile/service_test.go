package profile

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestServiceBasicOperations(t *testing.T) {
	// Create temporary directory for filesystem storage
	tmpDir, err := os.MkdirTemp("", "profile-service-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create filesystem storage
	storage, err := NewFilesystemStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create filesystem storage: %v", err)
	}

	// Create service
	service := NewService(storage)

	ctx := context.Background()
	userID := "test-user-123"

	// Test ProfileExists - should return false initially
	exists, err := service.ProfileExists(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to check profile existence: %v", err)
	}
	if exists {
		t.Error("Profile should not exist initially")
	}

	// Test GetUserProfiles - should return ErrProfileNotFound
	_, err = service.GetUserProfiles(ctx, userID)
	if err == nil || !strings.Contains(err.Error(), "profile not found") {
		t.Errorf("Expected error containing 'profile not found', got %v", err)
	}

	// Test CreateUserProfiles
	userProfiles, err := service.CreateUserProfiles(ctx, userID, "testuser", "test@example.com", "Test User", "default")
	if err != nil {
		t.Fatalf("Failed to create user profiles: %v", err)
	}

	if userProfiles.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, userProfiles.UserID)
	}

	if userProfiles.Username != "testuser" {
		t.Errorf("Expected Username 'testuser', got %s", userProfiles.Username)
	}

	if userProfiles.Email != "test@example.com" {
		t.Errorf("Expected Email 'test@example.com', got %s", userProfiles.Email)
	}

	if userProfiles.DisplayName != "Test User" {
		t.Errorf("Expected DisplayName 'Test User', got %s", userProfiles.DisplayName)
	}

	// Should have one default profile
	if len(userProfiles.Profiles) != 1 {
		t.Errorf("Expected 1 profile, got %d", len(userProfiles.Profiles))
	} else {
		if userProfiles.Profiles[0].Name != "default" {
			t.Errorf("Expected profile name 'default', got %s", userProfiles.Profiles[0].Name)
		}
		if !userProfiles.Profiles[0].IsDefault {
			t.Error("Expected profile to be default")
		}
	}

	// Test ProfileExists - should return true now
	exists, err = service.ProfileExists(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to check profile existence: %v", err)
	}
	if !exists {
		t.Error("Profile should exist after creation")
	}

	// Test GetUserProfiles - should return the user profiles
	retrievedUserProfiles, err := service.GetUserProfiles(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to get user profiles: %v", err)
	}

	if retrievedUserProfiles.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, retrievedUserProfiles.UserID)
	}

	// Test CreateUserProfiles again - should fail with duplicate
	_, err = service.CreateUserProfiles(ctx, userID, "testuser", "test@example.com", "Test User", "default")
	if err == nil {
		t.Error("Expected error when creating duplicate user profiles")
	}

	// Test UpdateUserProfiles
	update := &UserProfilesUpdate{
		DisplayName: "Updated Test User",
	}

	updatedUserProfiles, err := service.UpdateUserProfiles(ctx, userID, update)
	if err != nil {
		t.Fatalf("Failed to update user profiles: %v", err)
	}

	if updatedUserProfiles.DisplayName != "Updated Test User" {
		t.Errorf("Expected DisplayName 'Updated Test User', got %s", updatedUserProfiles.DisplayName)
	}

	// Test CreateProfile - add another profile
	newProfile, err := service.CreateProfile(ctx, userID, "work")
	if err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}

	if newProfile.Name != "work" {
		t.Errorf("Expected profile name 'work', got %s", newProfile.Name)
	}

	// Test GetProfile
	retrievedProfile, err := service.GetProfile(ctx, userID, "work")
	if err != nil {
		t.Fatalf("Failed to get profile: %v", err)
	}

	if retrievedProfile.Name != "work" {
		t.Errorf("Expected profile name 'work', got %s", retrievedProfile.Name)
	}

	// Test ListProfiles
	profileNames, err := service.ListProfiles(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to list profiles: %v", err)
	}

	if len(profileNames) != 2 {
		t.Errorf("Expected 2 profiles, got %d", len(profileNames))
	}

	// Test ListAllUsers
	users, err := service.ListAllUsers(ctx)
	if err != nil {
		t.Fatalf("Failed to list all users: %v", err)
	}

	if len(users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(users))
	}

	if users[0] != userID {
		t.Errorf("Expected user %s, got %s", userID, users[0])
	}

	// Test UpdateProfile
	profileUpdate := &ProfileConfigUpdate{
		Description: "Work profile",
		APIEndpoint: "https://api.work.com",
		Preferences: map[string]interface{}{
			"theme": "dark",
		},
	}

	updatedProfile, err := service.UpdateProfile(ctx, userID, "work", profileUpdate)
	if err != nil {
		t.Fatalf("Failed to update profile: %v", err)
	}

	if updatedProfile.Description != "Work profile" {
		t.Errorf("Expected description 'Work profile', got %s", updatedProfile.Description)
	}

	if updatedProfile.Preferences["theme"] != "dark" {
		t.Errorf("Expected theme 'dark', got %v", updatedProfile.Preferences["theme"])
	}

	// Test SetDefaultProfile
	err = service.SetDefaultProfile(ctx, userID, "work")
	if err != nil {
		t.Fatalf("Failed to set default profile: %v", err)
	}

	// Test GetDefaultProfile
	defaultProfile, err := service.GetDefaultProfile(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to get default profile: %v", err)
	}

	if defaultProfile.Name != "work" {
		t.Errorf("Expected default profile 'work', got %s", defaultProfile.Name)
	}

	// Test DeleteProfile
	err = service.DeleteProfile(ctx, userID, "work")
	if err != nil {
		t.Fatalf("Failed to delete profile: %v", err)
	}

	// Should only have default profile left
	profileNames, err = service.ListProfiles(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to list profiles after deletion: %v", err)
	}

	if len(profileNames) != 1 {
		t.Errorf("Expected 1 profile after deletion, got %d", len(profileNames))
	}

	// Test DeleteUserProfiles
	err = service.DeleteUserProfiles(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to delete user profiles: %v", err)
	}

	// Test ProfileExists - should return false after deletion
	exists, err = service.ProfileExists(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to check profile existence: %v", err)
	}
	if exists {
		t.Error("Profile should not exist after deletion")
	}
}

func TestServicePreferenceOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-service-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	storage, err := NewFilesystemStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create filesystem storage: %v", err)
	}

	service := NewService(storage)
	ctx := context.Background()
	userID := "test-user-123"

	// Create user profiles first
	_, err = service.CreateUserProfiles(ctx, userID, "testuser", "test@example.com", "Test User", "default")
	if err != nil {
		t.Fatalf("Failed to create user profiles: %v", err)
	}

	// Test SetPreference
	err = service.SetPreference(ctx, userID, "theme", "dark")
	if err != nil {
		t.Fatalf("Failed to set preference: %v", err)
	}

	// Test GetPreference
	value, err := service.GetPreference(ctx, userID, "theme")
	if err != nil {
		t.Fatalf("Failed to get preference: %v", err)
	}

	if value != "dark" {
		t.Errorf("Expected preference value 'dark', got %v", value)
	}

	// Test GetPreference for non-existent key
	_, err = service.GetPreference(ctx, userID, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent preference")
	}

	// Test SetPreference with complex value
	complexValue := map[string]interface{}{
		"nested": "value",
		"number": 42,
	}

	err = service.SetPreference(ctx, userID, "complex", complexValue)
	if err != nil {
		t.Fatalf("Failed to set complex preference: %v", err)
	}

	// Retrieve and verify complex value
	retrievedValue, err := service.GetPreference(ctx, userID, "complex")
	if err != nil {
		t.Fatalf("Failed to get complex preference: %v", err)
	}

	complexMap, ok := retrievedValue.(map[string]interface{})
	if !ok {
		t.Errorf("Expected complex preference to be a map, got %T", retrievedValue)
	} else {
		if complexMap["nested"] != "value" {
			t.Errorf("Expected nested value 'value', got %v", complexMap["nested"])
		}
	}
}

func TestServiceSettingOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-service-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	storage, err := NewFilesystemStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create filesystem storage: %v", err)
	}

	service := NewService(storage)
	ctx := context.Background()
	userID := "test-user-123"

	// Create user profiles first
	_, err = service.CreateUserProfiles(ctx, userID, "testuser", "test@example.com", "Test User", "default")
	if err != nil {
		t.Fatalf("Failed to create user profiles: %v", err)
	}

	// Test SetSetting
	err = service.SetSetting(ctx, userID, "timeout", 300)
	if err != nil {
		t.Fatalf("Failed to set setting: %v", err)
	}

	// Test GetSetting
	value, err := service.GetSetting(ctx, userID, "timeout")
	if err != nil {
		t.Fatalf("Failed to get setting: %v", err)
	}

	// Note: JSON unmarshaling converts numbers to float64
	if value != float64(300) {
		t.Errorf("Expected setting value 300, got %v", value)
	}

	// Test GetSetting for non-existent key
	_, err = service.GetSetting(ctx, userID, "nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent setting")
	}
}

func TestServiceUpdateLastLogin(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-service-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	storage, err := NewFilesystemStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create filesystem storage: %v", err)
	}

	service := NewService(storage)
	ctx := context.Background()
	userID := "test-user-123"

	// Test UpdateLastLogin for non-existent profile - should create user profiles
	err = service.UpdateLastLogin(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to update last login: %v", err)
	}

	// Verify user profiles were created
	userProfiles, err := service.GetUserProfiles(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to get user profiles after updating last login: %v", err)
	}

	if userProfiles.LastLoginAt == nil {
		t.Error("Expected LastLoginAt to be set")
	}

	// Test UpdateLastLogin for existing profile
	firstLoginTime := *userProfiles.LastLoginAt

	// Sleep a bit to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	err = service.UpdateLastLogin(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to update last login again: %v", err)
	}

	// Verify last login was updated
	updatedUserProfiles, err := service.GetUserProfiles(ctx, userID)
	if err != nil {
		t.Fatalf("Failed to get updated user profiles: %v", err)
	}

	if updatedUserProfiles.LastLoginAt == nil {
		t.Error("Expected LastLoginAt to be set after update")
	} else if updatedUserProfiles.LastLoginAt.Before(firstLoginTime) {
		t.Errorf("Expected LastLoginAt to be updated to a later time. First: %v, Updated: %v", firstLoginTime, *updatedUserProfiles.LastLoginAt)
	}
}

func TestServiceInvalidInputs(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "profile-service-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	storage, err := NewFilesystemStorage(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create filesystem storage: %v", err)
	}

	service := NewService(storage)
	ctx := context.Background()

	// Test CreateUserProfiles with empty UserID
	_, err = service.CreateUserProfiles(ctx, "", "user", "email", "name", "default")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test GetUserProfiles with empty UserID
	_, err = service.GetUserProfiles(ctx, "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test UpdateUserProfiles with empty UserID
	_, err = service.UpdateUserProfiles(ctx, "", &UserProfilesUpdate{})
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test UpdateUserProfiles with nil update
	_, err = service.UpdateUserProfiles(ctx, "user123", nil)
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for nil update, got %v", err)
	}

	// Test CreateProfile with empty UserID or profile name
	_, err = service.CreateProfile(ctx, "", "profile")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	_, err = service.CreateProfile(ctx, "user123", "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty profile name, got %v", err)
	}

	// Test GetProfile with empty UserID or profile name
	_, err = service.GetProfile(ctx, "", "profile")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	_, err = service.GetProfile(ctx, "user123", "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty profile name, got %v", err)
	}

	// Test UpdateProfile with empty UserID or profile name
	_, err = service.UpdateProfile(ctx, "", "profile", &ProfileConfigUpdate{})
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	_, err = service.UpdateProfile(ctx, "user123", "", &ProfileConfigUpdate{})
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty profile name, got %v", err)
	}

	_, err = service.UpdateProfile(ctx, "user123", "profile", nil)
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for nil update, got %v", err)
	}

	// Test DeleteProfile with empty UserID or profile name
	err = service.DeleteProfile(ctx, "", "profile")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	err = service.DeleteProfile(ctx, "user123", "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty profile name, got %v", err)
	}

	// Test DeleteUserProfiles with empty UserID
	err = service.DeleteUserProfiles(ctx, "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test ProfileExists with empty UserID
	_, err = service.ProfileExists(ctx, "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	// Test SetPreference with empty UserID or key
	err = service.SetPreference(ctx, "", "key", "value")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	err = service.SetPreference(ctx, "user123", "", "value")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty key, got %v", err)
	}

	// Test GetPreference with empty UserID or key
	_, err = service.GetPreference(ctx, "", "key")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	_, err = service.GetPreference(ctx, "user123", "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty key, got %v", err)
	}

	// Test SetSetting with empty UserID or key
	err = service.SetSetting(ctx, "", "key", "value")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	err = service.SetSetting(ctx, "user123", "", "value")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty key, got %v", err)
	}

	// Test GetSetting with empty UserID or key
	_, err = service.GetSetting(ctx, "", "key")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}

	_, err = service.GetSetting(ctx, "user123", "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty key, got %v", err)
	}

	// Test UpdateLastLogin with empty UserID
	err = service.UpdateLastLogin(ctx, "")
	if err != ErrInvalidProfile {
		t.Errorf("Expected ErrInvalidProfile for empty UserID, got %v", err)
	}
}
