package notification

import (
	"os"
	"testing"
	"time"
)

func TestJSONLStorage(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "notification_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	storage := NewJSONLStorage(tmpDir)

	// Test AddSubscription
	sub := Subscription{
		UserID:   "user123",
		UserType: "github",
		Username: "testuser",
		Endpoint: "https://fcm.googleapis.com/test",
		Keys: map[string]string{
			"p256dh": "test_p256dh",
			"auth":   "test_auth",
		},
		SessionIDs:        []string{"session1", "session2"},
		NotificationTypes: []string{"message", "status_change"},
		CreatedAt:         time.Now(),
		Active:            true,
	}

	err = storage.AddSubscription("user123", sub)
	if err != nil {
		t.Errorf("AddSubscription failed: %v", err)
	}

	// Test GetSubscriptions
	subscriptions, err := storage.GetSubscriptions("user123")
	if err != nil {
		t.Errorf("GetSubscriptions failed: %v", err)
	}

	if len(subscriptions) != 1 {
		t.Errorf("Expected 1 subscription, got %d", len(subscriptions))
	}

	if subscriptions[0].UserID != "user123" {
		t.Errorf("Expected UserID user123, got %s", subscriptions[0].UserID)
	}

	// Test DeleteSubscription
	err = storage.DeleteSubscription("user123", sub.Endpoint)
	if err != nil {
		t.Errorf("DeleteSubscription failed: %v", err)
	}

	// Verify subscription is inactive
	subscriptions, err = storage.GetSubscriptions("user123")
	if err != nil {
		t.Errorf("GetSubscriptions failed: %v", err)
	}

	if len(subscriptions) != 0 {
		t.Errorf("Expected 0 active subscriptions, got %d", len(subscriptions))
	}

	// Test AddNotificationHistory
	history := NotificationHistory{
		UserID:         "user123",
		SubscriptionID: sub.ID,
		Title:          "Test Notification",
		Body:           "This is a test notification",
		Type:           "message",
		SessionID:      "session1",
		Data:           map[string]interface{}{"url": "/session/session1"},
		SentAt:         time.Now(),
		Delivered:      true,
		Clicked:        false,
	}

	err = storage.AddNotificationHistory("user123", history)
	if err != nil {
		t.Errorf("AddNotificationHistory failed: %v", err)
	}

	// Test GetNotificationHistory
	notifications, total, err := storage.GetNotificationHistory("user123", 10, 0, nil)
	if err != nil {
		t.Errorf("GetNotificationHistory failed: %v", err)
	}

	if len(notifications) != 1 {
		t.Errorf("Expected 1 notification, got %d", len(notifications))
	}

	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}

	if notifications[0].Title != "Test Notification" {
		t.Errorf("Expected title 'Test Notification', got %s", notifications[0].Title)
	}

	// Test filtering by session_id
	filters := map[string]string{"session_id": "session1"}
	notifications, _, err = storage.GetNotificationHistory("user123", 10, 0, filters)
	if err != nil {
		t.Errorf("GetNotificationHistory with filters failed: %v", err)
	}

	if len(notifications) != 1 {
		t.Errorf("Expected 1 notification with session filter, got %d", len(notifications))
	}

	// Test filtering with non-matching session_id
	filters = map[string]string{"session_id": "nonexistent"}
	notifications, _, err = storage.GetNotificationHistory("user123", 10, 0, filters)
	if err != nil {
		t.Errorf("GetNotificationHistory with filters failed: %v", err)
	}

	if len(notifications) != 0 {
		t.Errorf("Expected 0 notifications with non-matching session filter, got %d", len(notifications))
	}
}

func TestGetAllSubscriptions(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "notification_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	storage := NewJSONLStorage(tmpDir)

	// Add subscriptions for different users
	sub1 := Subscription{
		UserID:   "user1",
		UserType: "github",
		Username: "testuser1",
		Endpoint: "https://fcm.googleapis.com/test1",
		Keys: map[string]string{
			"p256dh": "test_p256dh_1",
			"auth":   "test_auth_1",
		},
		Active: true,
	}

	sub2 := Subscription{
		UserID:   "user2",
		UserType: "api_key",
		Username: "testuser2",
		Endpoint: "https://fcm.googleapis.com/test2",
		Keys: map[string]string{
			"p256dh": "test_p256dh_2",
			"auth":   "test_auth_2",
		},
		Active: true,
	}

	err = storage.AddSubscription("user1", sub1)
	if err != nil {
		t.Errorf("AddSubscription for user1 failed: %v", err)
	}

	err = storage.AddSubscription("user2", sub2)
	if err != nil {
		t.Errorf("AddSubscription for user2 failed: %v", err)
	}

	// Get all subscriptions
	allSubs, err := storage.GetAllSubscriptions()
	if err != nil {
		t.Errorf("GetAllSubscriptions failed: %v", err)
	}

	if len(allSubs) != 2 {
		t.Errorf("Expected 2 subscriptions, got %d", len(allSubs))
	}

	// Verify both users' subscriptions are included
	userIDs := make(map[string]bool)
	for _, sub := range allSubs {
		userIDs[sub.UserID] = true
	}

	if !userIDs["user1"] || !userIDs["user2"] {
		t.Errorf("Expected subscriptions for both user1 and user2")
	}
}

func TestRotateNotificationHistory(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "notification_test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	storage := NewJSONLStorage(tmpDir)
	userID := "user123"

	// Add multiple notifications
	for i := 0; i < 10; i++ {
		history := NotificationHistory{
			UserID:    userID,
			Title:     "Notification " + string(rune('0'+i)),
			Body:      "Test notification body",
			Type:      "message",
			SessionID: "session1",
			SentAt:    time.Now().Add(time.Duration(i) * time.Minute),
			Delivered: true,
		}

		err = storage.AddNotificationHistory(userID, history)
		if err != nil {
			t.Errorf("AddNotificationHistory failed: %v", err)
		}
	}

	// Verify we have 10 notifications
	var notifications []NotificationHistory
	_, total, err := storage.GetNotificationHistory(userID, 20, 0, nil)
	if err != nil {
		t.Errorf("GetNotificationHistory failed: %v", err)
	}

	if total != 10 {
		t.Errorf("Expected 10 notifications before rotation, got %d", total)
	}

	// Rotate to keep only 5 most recent
	err = storage.RotateNotificationHistory(userID, 5)
	if err != nil {
		t.Errorf("RotateNotificationHistory failed: %v", err)
	}

	// Verify we now have only 5 notifications
	notifications, total, err = storage.GetNotificationHistory(userID, 20, 0, nil)
	if err != nil {
		t.Errorf("GetNotificationHistory after rotation failed: %v", err)
	}

	if total != 5 {
		t.Errorf("Expected 5 notifications after rotation, got %d", total)
	}

	// Verify the most recent notifications are kept (they should have higher indices)
	if len(notifications) > 0 {
		// The first notification should be the most recent (index 9 in our case)
		if notifications[0].Title != "Notification 9" {
			t.Errorf("Expected most recent notification 'Notification 9', got '%s'", notifications[0].Title)
		}
	}
}
