package profile

import (
	"testing"
	"time"
)

func TestNewMemoryStorage(t *testing.T) {
	storage := NewMemoryStorage()
	if storage == nil {
		t.Fatal("NewMemoryStorage returned nil")
	}
}

func TestProfile_Save_Load(t *testing.T) {
	storage := NewMemoryStorage()
	service := NewService(storage)

	// Create a test profile
	req := &CreateProfileRequest{
		Name:        "Test Profile",
		Description: "A test profile",
		Environment: map[string]string{
			"TEST_VAR": "test_value",
		},
		SystemPrompt: "You are a helpful assistant",
		MessageTemplates: []MessageTemplate{
			{
				Name:    "Greeting",
				Content: "Hello, how can I help you?",
			},
		},
	}

	profile, err := service.CreateProfile("user123", req)
	if err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}

	// Verify profile was created
	if profile.ID == "" {
		t.Error("Profile ID should not be empty")
	}
	if profile.UserID != "user123" {
		t.Errorf("Expected UserID 'user123', got '%s'", profile.UserID)
	}
	if profile.Name != "Test Profile" {
		t.Errorf("Expected Name 'Test Profile', got '%s'", profile.Name)
	}

	// Load the profile
	loadedProfile, err := service.GetProfile(profile.ID)
	if err != nil {
		t.Fatalf("Failed to load profile: %v", err)
	}

	if loadedProfile.ID != profile.ID {
		t.Errorf("Profile IDs don't match: expected %s, got %s", profile.ID, loadedProfile.ID)
	}
}

func TestProfile_GetUserProfiles(t *testing.T) {
	storage := NewMemoryStorage()
	service := NewService(storage)

	userID := "user123"

	// Create multiple profiles for the same user
	req1 := &CreateProfileRequest{
		Name:         "Profile 1",
		SystemPrompt: "Assistant 1",
	}
	req2 := &CreateProfileRequest{
		Name:         "Profile 2",
		SystemPrompt: "Assistant 2",
	}

	_, err := service.CreateProfile(userID, req1)
	if err != nil {
		t.Fatalf("Failed to create profile 1: %v", err)
	}

	_, err = service.CreateProfile(userID, req2)
	if err != nil {
		t.Fatalf("Failed to create profile 2: %v", err)
	}

	// Create a profile for a different user
	req3 := &CreateProfileRequest{
		Name:         "Profile 3",
		SystemPrompt: "Assistant 3",
	}
	_, err = service.CreateProfile("user456", req3)
	if err != nil {
		t.Fatalf("Failed to create profile 3: %v", err)
	}

	// Get profiles for user123
	profiles, err := service.GetUserProfiles(userID)
	if err != nil {
		t.Fatalf("Failed to get user profiles: %v", err)
	}

	if len(profiles) != 2 {
		t.Errorf("Expected 2 profiles for user123, got %d", len(profiles))
	}
}

func TestProfile_UpdateProfile(t *testing.T) {
	storage := NewMemoryStorage()
	service := NewService(storage)

	// Create a profile
	req := &CreateProfileRequest{
		Name:        "Original Profile",
		Description: "Original description",
		Environment: map[string]string{
			"VAR1": "value1",
		},
		SystemPrompt: "Original prompt",
	}

	profile, err := service.CreateProfile("user123", req)
	if err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}

	// Update the profile
	newName := "Updated Profile"
	newDescription := "Updated description"
	updateReq := &UpdateProfileRequest{
		Name:        &newName,
		Description: &newDescription,
		Environment: map[string]string{
			"VAR1": "updated_value1",
			"VAR2": "value2",
		},
	}

	updatedProfile, err := service.UpdateProfile(profile.ID, updateReq)
	if err != nil {
		t.Fatalf("Failed to update profile: %v", err)
	}

	if updatedProfile.Name != newName {
		t.Errorf("Expected name '%s', got '%s'", newName, updatedProfile.Name)
	}
	if updatedProfile.Description != newDescription {
		t.Errorf("Expected description '%s', got '%s'", newDescription, updatedProfile.Description)
	}
	if updatedProfile.Environment["VAR1"] != "updated_value1" {
		t.Errorf("Expected VAR1 'updated_value1', got '%s'", updatedProfile.Environment["VAR1"])
	}
	if updatedProfile.Environment["VAR2"] != "value2" {
		t.Errorf("Expected VAR2 'value2', got '%s'", updatedProfile.Environment["VAR2"])
	}
}

func TestProfile_DeleteProfile(t *testing.T) {
	storage := NewMemoryStorage()
	service := NewService(storage)

	// Create a profile
	req := &CreateProfileRequest{
		Name:         "Profile to Delete",
		SystemPrompt: "Test prompt",
	}

	profile, err := service.CreateProfile("user123", req)
	if err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}

	// Verify profile exists
	_, err = service.GetProfile(profile.ID)
	if err != nil {
		t.Fatalf("Profile should exist: %v", err)
	}

	// Delete the profile
	err = service.DeleteProfile(profile.ID)
	if err != nil {
		t.Fatalf("Failed to delete profile: %v", err)
	}

	// Verify profile is deleted
	_, err = service.GetProfile(profile.ID)
	if err == nil {
		t.Error("Profile should be deleted")
	}
}

func TestProfile_AddRepositoryEntry(t *testing.T) {
	storage := NewMemoryStorage()
	service := NewService(storage)

	// Create a profile
	req := &CreateProfileRequest{
		Name:         "Profile with Repo",
		SystemPrompt: "Test prompt",
	}

	profile, err := service.CreateProfile("user123", req)
	if err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}

	// Add repository entry
	repoEntry := RepositoryEntry{
		URL:  "https://github.com/example/repo",
		Name: "example-repo",
		Branch: "main",
	}

	err = service.AddRepositoryEntry(profile.ID, repoEntry)
	if err != nil {
		t.Fatalf("Failed to add repository entry: %v", err)
	}

	// Verify repository was added
	updatedProfile, err := service.GetProfile(profile.ID)
	if err != nil {
		t.Fatalf("Failed to get updated profile: %v", err)
	}

	if len(updatedProfile.RepositoryHistory) != 1 {
		t.Errorf("Expected 1 repository, got %d", len(updatedProfile.RepositoryHistory))
	}

	repo := updatedProfile.RepositoryHistory[0]
	if repo.URL != repoEntry.URL {
		t.Errorf("Expected URL '%s', got '%s'", repoEntry.URL, repo.URL)
	}
	if repo.ID == "" {
		t.Error("Repository ID should be generated")
	}
}

func TestProfile_AddMessageTemplate(t *testing.T) {
	storage := NewMemoryStorage()
	service := NewService(storage)

	// Create a profile
	req := &CreateProfileRequest{
		Name:         "Profile with Template",
		SystemPrompt: "Test prompt",
	}

	profile, err := service.CreateProfile("user123", req)
	if err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}

	// Add message template
	template := MessageTemplate{
		Name:     "Test Template",
		Content:  "This is a test template with {{variable}}",
		Variables: []string{"variable"},
		Category: "test",
	}

	err = service.AddMessageTemplate(profile.ID, template)
	if err != nil {
		t.Fatalf("Failed to add message template: %v", err)
	}

	// Verify template was added
	updatedProfile, err := service.GetProfile(profile.ID)
	if err != nil {
		t.Fatalf("Failed to get updated profile: %v", err)
	}

	if len(updatedProfile.MessageTemplates) != 1 {
		t.Errorf("Expected 1 template, got %d", len(updatedProfile.MessageTemplates))
	}

	tmpl := updatedProfile.MessageTemplates[0]
	if tmpl.Name != template.Name {
		t.Errorf("Expected name '%s', got '%s'", template.Name, tmpl.Name)
	}
	if tmpl.ID == "" {
		t.Error("Template ID should be generated")
	}
}

func TestProfile_MergeEnvironmentVariables(t *testing.T) {
	storage := NewMemoryStorage()
	service := NewService(storage)

	// Create a profile with environment variables
	req := &CreateProfileRequest{
		Name: "Profile with Env",
		Environment: map[string]string{
			"PROFILE_VAR1": "profile_value1",
			"PROFILE_VAR2": "profile_value2",
			"COMMON_VAR":   "profile_common",
		},
		SystemPrompt: "Test prompt",
	}

	profile, err := service.CreateProfile("user123", req)
	if err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}

	// Merge with provided environment variables
	providedEnv := map[string]string{
		"PROVIDED_VAR": "provided_value",
		"COMMON_VAR":   "provided_common", // Should override profile value
	}

	merged, err := service.MergeEnvironmentVariables(profile.ID, providedEnv)
	if err != nil {
		t.Fatalf("Failed to merge environment variables: %v", err)
	}

	// Verify merged environment
	expected := map[string]string{
		"PROFILE_VAR1": "profile_value1",
		"PROFILE_VAR2": "profile_value2",
		"PROVIDED_VAR": "provided_value",
		"COMMON_VAR":   "provided_common", // Should be overridden
	}

	if len(merged) != len(expected) {
		t.Errorf("Expected %d variables, got %d", len(expected), len(merged))
	}

	for key, expectedValue := range expected {
		if actualValue, exists := merged[key]; !exists {
			t.Errorf("Expected variable '%s' to exist", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected '%s'='%s', got '%s'", key, expectedValue, actualValue)
		}
	}
}

func TestProfile_UpdateLastUsed(t *testing.T) {
	storage := NewMemoryStorage()
	service := NewService(storage)

	// Create a profile
	req := &CreateProfileRequest{
		Name:         "Profile to Use",
		SystemPrompt: "Test prompt",
	}

	profile, err := service.CreateProfile("user123", req)
	if err != nil {
		t.Fatalf("Failed to create profile: %v", err)
	}

	// Initially, last used should be nil
	if profile.LastUsedAt != nil {
		t.Error("LastUsedAt should initially be nil")
	}

	// Update last used
	err = service.UpdateLastUsed(profile.ID)
	if err != nil {
		t.Fatalf("Failed to update last used: %v", err)
	}

	// Verify last used was updated
	updatedProfile, err := service.GetProfile(profile.ID)
	if err != nil {
		t.Fatalf("Failed to get updated profile: %v", err)
	}

	if updatedProfile.LastUsedAt == nil {
		t.Error("LastUsedAt should be set after update")
	}

	// Check that the timestamp is recent (within last minute)
	if time.Since(*updatedProfile.LastUsedAt) > time.Minute {
		t.Error("LastUsedAt should be recent")
	}
}