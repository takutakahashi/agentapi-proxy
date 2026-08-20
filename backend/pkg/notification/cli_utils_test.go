package notification

import (
	"testing"
	"time"
)

func TestNewCLIUtils(t *testing.T) {
	utils := NewCLIUtils()
	if utils == nil {
		t.Error("NewCLIUtils() returned nil")
	}
}

func TestCLIUtils_matchesFilter(t *testing.T) {
	utils := NewCLIUtils()

	sub := Subscription{
		ID:         "sub1",
		UserID:     "user123",
		UserType:   "github",
		Username:   "testuser",
		SessionIDs: []string{"session1", "session2"},
		Active:     true,
	}

	tests := []struct {
		name      string
		userID    string
		userType  string
		username  string
		sessionID string
		expected  bool
	}{
		{
			name:     "Match all criteria",
			userID:   "user123",
			userType: "github",
			username: "testuser",
			expected: true,
		},
		{
			name:     "Match userID only",
			userID:   "user123",
			expected: true,
		},
		{
			name:     "Match userType only",
			userType: "github",
			expected: true,
		},
		{
			name:     "Match username only",
			username: "testuser",
			expected: true,
		},
		{
			name:      "Match sessionID in list",
			sessionID: "session1",
			expected:  true,
		},
		{
			name:      "Match sessionID not in list",
			sessionID: "session3",
			expected:  false,
		},
		{
			name:     "No match userID",
			userID:   "user456",
			expected: false,
		},
		{
			name:     "No match userType",
			userType: "api_key",
			expected: false,
		},
		{
			name:     "No match username",
			username: "otheruser",
			expected: false,
		},
		{
			name:     "No criteria (match all)",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.matchesFilter(sub, tt.userID, tt.userType, tt.username, tt.sessionID)
			if result != tt.expected {
				t.Errorf("matchesFilter() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCLIUtils_matchesFilter_EmptySessionIDs(t *testing.T) {
	utils := NewCLIUtils()

	// Subscription with empty SessionIDs (subscribed to all sessions)
	sub := Subscription{
		ID:         "sub1",
		UserID:     "user123",
		UserType:   "github",
		Username:   "testuser",
		SessionIDs: []string{}, // Empty means subscribed to all sessions
		Active:     true,
	}

	result := utils.matchesFilter(sub, "", "", "", "any_session")
	if !result {
		t.Error("matchesFilter() should return true for empty SessionIDs (subscribed to all)")
	}
}

func TestCLIUtils_generateNotificationID(t *testing.T) {
	utils := NewCLIUtils()

	id1 := utils.generateNotificationID()
	if id1 == "" {
		t.Error("generateNotificationID() returned empty string")
	}

	// Add small delay to ensure different timestamps
	time.Sleep(1 * time.Millisecond)
	id2 := utils.generateNotificationID()

	if id1 == id2 {
		t.Errorf("generateNotificationID() should generate unique IDs, got: %s == %s", id1, id2)
	}

	// Check format (should start with "notif_")
	if len(id1) < 6 || id1[:6] != "notif_" {
		t.Errorf("generateNotificationID() = %q, expected format 'notif_*'", id1)
	}
}

func TestSubscription(t *testing.T) {
	// Test Subscription struct creation
	now := time.Now()
	sub := Subscription{
		ID:                "test-id",
		UserID:            "user123",
		UserType:          "github",
		Username:          "testuser",
		Endpoint:          "https://fcm.googleapis.com/endpoint",
		Keys:              map[string]string{"p256dh": "key1", "auth": "key2"},
		SessionIDs:        []string{"session1", "session2"},
		NotificationTypes: []string{"message", "status"},
		CreatedAt:         now,
		Active:            true,
	}

	if sub.ID != "test-id" {
		t.Errorf("Expected ID to be 'test-id', got %q", sub.ID)
	}

	if sub.UserID != "user123" {
		t.Errorf("Expected UserID to be 'user123', got %q", sub.UserID)
	}

	if !sub.Active {
		t.Error("Expected Active to be true")
	}

	if len(sub.SessionIDs) != 2 {
		t.Errorf("Expected 2 SessionIDs, got %d", len(sub.SessionIDs))
	}
}

func TestNotificationHistory(t *testing.T) {
	// Test NotificationHistory struct creation
	now := time.Now()
	errorMsg := "test error"

	history := NotificationHistory{
		ID:             "hist-id",
		UserID:         "user123",
		SubscriptionID: "sub-id",
		Title:          "Test Title",
		Body:           "Test Body",
		Type:           "manual",
		SessionID:      "session1",
		Data:           map[string]interface{}{"url": "https://example.com"},
		SentAt:         now,
		Delivered:      true,
		Clicked:        false,
		ErrorMessage:   &errorMsg,
	}

	if history.ID != "hist-id" {
		t.Errorf("Expected ID to be 'hist-id', got %q", history.ID)
	}

	if history.Title != "Test Title" {
		t.Errorf("Expected Title to be 'Test Title', got %q", history.Title)
	}

	if !history.Delivered {
		t.Error("Expected Delivered to be true")
	}

	if history.ErrorMessage == nil || *history.ErrorMessage != "test error" {
		t.Error("Expected ErrorMessage to be set correctly")
	}

	if url, ok := history.Data["url"].(string); !ok || url != "https://example.com" {
		t.Error("Expected Data to contain correct URL")
	}
}

func TestNotificationResult(t *testing.T) {
	// Test NotificationResult struct creation
	sub := Subscription{
		ID:     "sub-id",
		UserID: "user123",
	}

	result := NotificationResult{
		Subscription: sub,
		Error:        nil,
	}

	if result.Subscription.ID != "sub-id" {
		t.Error("Expected Subscription to be set correctly")
	}

	if result.Error != nil {
		t.Error("Expected Error to be nil")
	}
}
