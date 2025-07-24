package profile

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewUserProfiles(t *testing.T) {
	userID := "test-user-123"
	userProfiles := NewUserProfiles(userID)

	if userProfiles.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, userProfiles.UserID)
	}

	if userProfiles.Profiles == nil {
		t.Error("Expected Profiles to be initialized")
	}

	if userProfiles.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}

	if userProfiles.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be set")
	}
}

func TestNewProfileConfig(t *testing.T) {
	name := "test-profile"
	profile := NewProfileConfig(name)

	if profile.Name != name {
		t.Errorf("Expected Name %s, got %s", name, profile.Name)
	}

	if profile.Preferences == nil {
		t.Error("Expected Preferences to be initialized")
	}

	if profile.Settings == nil {
		t.Error("Expected Settings to be initialized")
	}

	if profile.Metadata == nil {
		t.Error("Expected Metadata to be initialized")
	}

	if profile.PromptTemplates == nil {
		t.Error("Expected PromptTemplates to be initialized")
	}

	if profile.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}

	if profile.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be set")
	}
}

func TestUserProfilesUpdate(t *testing.T) {
	userProfiles := NewUserProfiles("test-user")
	originalUpdatedAt := userProfiles.UpdatedAt

	// Sleep a bit to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	update := &UserProfilesUpdate{
		Username:    "newusername",
		Email:       "new@example.com",
		DisplayName: "New Display Name",
	}

	userProfiles.Update(update)

	if userProfiles.Username != "newusername" {
		t.Errorf("Expected Username to be updated to 'newusername', got %s", userProfiles.Username)
	}

	if userProfiles.Email != "new@example.com" {
		t.Errorf("Expected Email to be updated to 'new@example.com', got %s", userProfiles.Email)
	}

	if userProfiles.DisplayName != "New Display Name" {
		t.Errorf("Expected DisplayName to be updated to 'New Display Name', got %s", userProfiles.DisplayName)
	}

	if !userProfiles.UpdatedAt.After(originalUpdatedAt) {
		t.Error("Expected UpdatedAt to be updated")
	}
}

func TestProfileConfigUpdate(t *testing.T) {
	profile := NewProfileConfig("test-profile")
	originalUpdatedAt := profile.UpdatedAt

	// Sleep a bit to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	update := &ProfileConfigUpdate{
		Description: "Updated description",
		APIEndpoint: "https://api.example.com",
		Preferences: map[string]interface{}{
			"theme": "dark",
			"lang":  "en",
		},
		Settings: map[string]interface{}{
			"timeout": 300,
		},
		Metadata: map[string]string{
			"role": "admin",
		},
	}

	profile.updateFrom(update)

	if profile.Description != "Updated description" {
		t.Errorf("Expected Description to be updated to 'Updated description', got %s", profile.Description)
	}

	if profile.APIEndpoint != "https://api.example.com" {
		t.Errorf("Expected APIEndpoint to be updated to 'https://api.example.com', got %s", profile.APIEndpoint)
	}

	if profile.Preferences["theme"] != "dark" {
		t.Errorf("Expected theme preference to be 'dark', got %v", profile.Preferences["theme"])
	}

	if profile.Settings["timeout"] != 300 {
		t.Errorf("Expected timeout setting to be 300, got %v", profile.Settings["timeout"])
	}

	if profile.Metadata["role"] != "admin" {
		t.Errorf("Expected role metadata to be 'admin', got %v", profile.Metadata["role"])
	}

	if !profile.UpdatedAt.After(originalUpdatedAt) {
		t.Error("Expected UpdatedAt to be updated")
	}
}

func TestUserProfilesJSONMarshaling(t *testing.T) {
	userProfiles := NewUserProfiles("test-user")
	userProfiles.Username = "testuser"
	userProfiles.Email = "test@example.com"
	userProfiles.DisplayName = "Test User"

	// Add a profile with data
	profile := NewProfileConfig("default")
	profile.IsDefault = true
	profile.Description = "Default profile"
	profile.Preferences["theme"] = "dark"
	profile.Settings["timeout"] = 300
	profile.Metadata["role"] = "user"
	userProfiles.Profiles = append(userProfiles.Profiles, *profile)

	now := time.Now()
	userProfiles.LastLoginAt = &now

	// Marshal to JSON
	data, err := json.Marshal(userProfiles)
	if err != nil {
		t.Fatalf("Failed to marshal user profiles: %v", err)
	}

	// Unmarshal back
	var unmarshaled UserProfiles
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal user profiles: %v", err)
	}

	// Verify fields
	if unmarshaled.UserID != userProfiles.UserID {
		t.Errorf("UserID mismatch: expected %s, got %s", userProfiles.UserID, unmarshaled.UserID)
	}

	if unmarshaled.Username != userProfiles.Username {
		t.Errorf("Username mismatch: expected %s, got %s", userProfiles.Username, unmarshaled.Username)
	}

	if unmarshaled.Email != userProfiles.Email {
		t.Errorf("Email mismatch: expected %s, got %s", userProfiles.Email, unmarshaled.Email)
	}

	if unmarshaled.DisplayName != userProfiles.DisplayName {
		t.Errorf("DisplayName mismatch: expected %s, got %s", userProfiles.DisplayName, unmarshaled.DisplayName)
	}

	// Check profiles
	if len(unmarshaled.Profiles) != 1 {
		t.Errorf("Expected 1 profile, got %d", len(unmarshaled.Profiles))
	} else {
		unmarshaledProfile := unmarshaled.Profiles[0]
		if unmarshaledProfile.Name != "default" {
			t.Errorf("Profile name mismatch: expected 'default', got %s", unmarshaledProfile.Name)
		}
		if unmarshaledProfile.Preferences["theme"] != "dark" {
			t.Errorf("Theme preference mismatch: expected 'dark', got %v", unmarshaledProfile.Preferences["theme"])
		}
		if unmarshaledProfile.Settings["timeout"] != float64(300) { // JSON unmarshaling converts numbers to float64
			t.Errorf("Timeout setting mismatch: expected 300, got %v", unmarshaledProfile.Settings["timeout"])
		}
		if unmarshaledProfile.Metadata["role"] != "user" {
			t.Errorf("Role metadata mismatch: expected 'user', got %v", unmarshaledProfile.Metadata["role"])
		}
	}

	// Check timestamps (allow small difference due to serialization precision)
	if abs(unmarshaled.CreatedAt.Sub(userProfiles.CreatedAt)) > time.Second {
		t.Errorf("CreatedAt mismatch: expected %v, got %v", userProfiles.CreatedAt, unmarshaled.CreatedAt)
	}

	if abs(unmarshaled.UpdatedAt.Sub(userProfiles.UpdatedAt)) > time.Second {
		t.Errorf("UpdatedAt mismatch: expected %v, got %v", userProfiles.UpdatedAt, unmarshaled.UpdatedAt)
	}

	if unmarshaled.LastLoginAt == nil || abs(unmarshaled.LastLoginAt.Sub(*userProfiles.LastLoginAt)) > time.Second {
		t.Errorf("LastLoginAt mismatch: expected %v, got %v", userProfiles.LastLoginAt, unmarshaled.LastLoginAt)
	}
}

func TestUserProfilesUpdatePartial(t *testing.T) {
	userProfiles := NewUserProfiles("test-user")
	userProfiles.Username = "original"
	userProfiles.Email = "original@example.com"
	userProfiles.DisplayName = "Original Name"

	// Partial update - only update username
	update := &UserProfilesUpdate{
		Username: "updated",
	}

	userProfiles.Update(update)

	// Check updated field
	if userProfiles.Username != "updated" {
		t.Errorf("Expected Username to be updated to 'updated', got %s", userProfiles.Username)
	}

	// Check unchanged fields
	if userProfiles.Email != "original@example.com" {
		t.Errorf("Expected Email to remain 'original@example.com', got %s", userProfiles.Email)
	}

	if userProfiles.DisplayName != "Original Name" {
		t.Errorf("Expected DisplayName to remain 'Original Name', got %s", userProfiles.DisplayName)
	}
}

func TestUserProfilesProfileOperations(t *testing.T) {
	userProfiles := NewUserProfiles("test-user")

	// Test AddProfile
	profile1 := NewProfileConfig("profile1")
	profile1.Description = "First profile"
	err := userProfiles.AddProfile(profile1)
	if err != nil {
		t.Fatalf("Failed to add first profile: %v", err)
	}

	// First profile should be default
	if !userProfiles.Profiles[0].IsDefault {
		t.Error("First profile should be set as default")
	}

	// Test adding duplicate profile name
	profile1Dup := NewProfileConfig("profile1")
	err = userProfiles.AddProfile(profile1Dup)
	if err == nil {
		t.Error("Expected error when adding duplicate profile name")
	}

	// Test GetProfile
	retrievedProfile, err := userProfiles.GetProfile("profile1")
	if err != nil {
		t.Fatalf("Failed to get profile: %v", err)
	}
	if retrievedProfile.Name != "profile1" {
		t.Errorf("Expected profile name 'profile1', got %s", retrievedProfile.Name)
	}

	// Test GetProfile with non-existent name
	_, err = userProfiles.GetProfile("nonexistent")
	if err == nil {
		t.Error("Expected error when getting non-existent profile")
	}

	// Test ListProfileNames
	names := userProfiles.ListProfileNames()
	if len(names) != 1 || names[0] != "profile1" {
		t.Errorf("Expected profile names ['profile1'], got %v", names)
	}

	// Test GetDefaultProfile
	defaultProfile := userProfiles.GetDefaultProfile()
	if defaultProfile == nil || defaultProfile.Name != "profile1" {
		t.Errorf("Expected default profile 'profile1', got %v", defaultProfile)
	}

	// Add second profile
	profile2 := NewProfileConfig("profile2")
	err = userProfiles.AddProfile(profile2)
	if err != nil {
		t.Fatalf("Failed to add second profile: %v", err)
	}

	// Second profile should not be default
	if userProfiles.Profiles[1].IsDefault {
		t.Error("Second profile should not be default")
	}

	// Test SetDefaultProfile
	err = userProfiles.SetDefaultProfile("profile2")
	if err != nil {
		t.Fatalf("Failed to set default profile: %v", err)
	}

	// Check that profile2 is now default and profile1 is not
	if !userProfiles.Profiles[1].IsDefault {
		t.Error("profile2 should be default now")
	}
	if userProfiles.Profiles[0].IsDefault {
		t.Error("profile1 should not be default now")
	}

	// Test DeleteProfile
	err = userProfiles.DeleteProfile("profile1")
	if err != nil {
		t.Fatalf("Failed to delete profile: %v", err)
	}

	// Should only have profile2 left
	if len(userProfiles.Profiles) != 1 || userProfiles.Profiles[0].Name != "profile2" {
		t.Errorf("Expected only profile2 remaining, got %v", userProfiles.ListProfileNames())
	}

	// Test deleting non-existent profile
	err = userProfiles.DeleteProfile("nonexistent")
	if err == nil {
		t.Error("Expected error when deleting non-existent profile")
	}
}

// Helper function to calculate absolute duration
func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
