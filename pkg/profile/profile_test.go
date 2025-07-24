package profile

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewProfile(t *testing.T) {
	userID := "test-user-123"
	profile := NewProfile(userID)

	if profile.UserID != userID {
		t.Errorf("Expected UserID %s, got %s", userID, profile.UserID)
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

	if profile.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be set")
	}

	if profile.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be set")
	}
}

func TestProfileUpdate(t *testing.T) {
	profile := NewProfile("test-user")
	originalUpdatedAt := profile.UpdatedAt

	// Sleep a bit to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	update := &ProfileUpdate{
		Username:    "newusername",
		Email:       "new@example.com",
		DisplayName: "New Display Name",
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

	profile.Update(update)

	if profile.Username != "newusername" {
		t.Errorf("Expected Username to be updated to 'newusername', got %s", profile.Username)
	}

	if profile.Email != "new@example.com" {
		t.Errorf("Expected Email to be updated to 'new@example.com', got %s", profile.Email)
	}

	if profile.DisplayName != "New Display Name" {
		t.Errorf("Expected DisplayName to be updated to 'New Display Name', got %s", profile.DisplayName)
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

func TestProfileJSONMarshaling(t *testing.T) {
	profile := NewProfile("test-user")
	profile.Username = "testuser"
	profile.Email = "test@example.com"
	profile.DisplayName = "Test User"
	profile.Preferences["theme"] = "dark"
	profile.Settings["timeout"] = 300
	profile.Metadata["role"] = "user"

	now := time.Now()
	profile.LastLoginAt = &now

	// Marshal to JSON
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("Failed to marshal profile: %v", err)
	}

	// Unmarshal back
	var unmarshaled Profile
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal profile: %v", err)
	}

	// Verify fields
	if unmarshaled.UserID != profile.UserID {
		t.Errorf("UserID mismatch: expected %s, got %s", profile.UserID, unmarshaled.UserID)
	}

	if unmarshaled.Username != profile.Username {
		t.Errorf("Username mismatch: expected %s, got %s", profile.Username, unmarshaled.Username)
	}

	if unmarshaled.Email != profile.Email {
		t.Errorf("Email mismatch: expected %s, got %s", profile.Email, unmarshaled.Email)
	}

	if unmarshaled.DisplayName != profile.DisplayName {
		t.Errorf("DisplayName mismatch: expected %s, got %s", profile.DisplayName, unmarshaled.DisplayName)
	}

	if unmarshaled.Preferences["theme"] != "dark" {
		t.Errorf("Theme preference mismatch: expected 'dark', got %v", unmarshaled.Preferences["theme"])
	}

	if unmarshaled.Settings["timeout"] != float64(300) { // JSON unmarshaling converts numbers to float64
		t.Errorf("Timeout setting mismatch: expected 300, got %v", unmarshaled.Settings["timeout"])
	}

	if unmarshaled.Metadata["role"] != "user" {
		t.Errorf("Role metadata mismatch: expected 'user', got %v", unmarshaled.Metadata["role"])
	}

	// Check timestamps (allow small difference due to serialization precision)
	if abs(unmarshaled.CreatedAt.Sub(profile.CreatedAt)) > time.Second {
		t.Errorf("CreatedAt mismatch: expected %v, got %v", profile.CreatedAt, unmarshaled.CreatedAt)
	}

	if abs(unmarshaled.UpdatedAt.Sub(profile.UpdatedAt)) > time.Second {
		t.Errorf("UpdatedAt mismatch: expected %v, got %v", profile.UpdatedAt, unmarshaled.UpdatedAt)
	}

	if unmarshaled.LastLoginAt == nil || abs(unmarshaled.LastLoginAt.Sub(*profile.LastLoginAt)) > time.Second {
		t.Errorf("LastLoginAt mismatch: expected %v, got %v", profile.LastLoginAt, unmarshaled.LastLoginAt)
	}
}

func TestProfileUpdatePartial(t *testing.T) {
	profile := NewProfile("test-user")
	profile.Username = "original"
	profile.Email = "original@example.com"
	profile.Preferences["theme"] = "light"
	profile.Settings["timeout"] = 100

	// Partial update - only update username and add a new preference
	update := &ProfileUpdate{
		Username: "updated",
		Preferences: map[string]interface{}{
			"lang": "en",
		},
	}

	profile.Update(update)

	// Check updated field
	if profile.Username != "updated" {
		t.Errorf("Expected Username to be updated to 'updated', got %s", profile.Username)
	}

	// Check unchanged field
	if profile.Email != "original@example.com" {
		t.Errorf("Expected Email to remain 'original@example.com', got %s", profile.Email)
	}

	// Check that existing preference is preserved
	if profile.Preferences["theme"] != "light" {
		t.Errorf("Expected existing theme preference to be preserved as 'light', got %v", profile.Preferences["theme"])
	}

	// Check that new preference is added
	if profile.Preferences["lang"] != "en" {
		t.Errorf("Expected new lang preference to be 'en', got %v", profile.Preferences["lang"])
	}

	// Check that existing setting is preserved
	if profile.Settings["timeout"] != 100 {
		t.Errorf("Expected existing timeout setting to be preserved as 100, got %v", profile.Settings["timeout"])
	}
}

// Helper function to calculate absolute duration
func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
