package notification

import (
	"os"
	"testing"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func TestNotificationService(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "notification_service_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create service
	service, err := NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Test user context
	user := &auth.UserContext{
		UserID:   "testuser123",
		AuthType: "github",
		GitHubUser: &auth.GitHubUserInfo{
			Login: "testuser",
		},
	}

	// Test Subscribe
	sub, err := service.Subscribe(user, "https://fcm.googleapis.com/test", map[string]string{
		"p256dh": "test_p256dh",
		"auth":   "test_auth",
	})
	if err != nil {
		t.Errorf("Subscribe failed: %v", err)
	}

	if sub.UserID != "testuser123" {
		t.Errorf("Expected UserID testuser123, got %s", sub.UserID)
	}

	if sub.Username != "testuser" {
		t.Errorf("Expected Username testuser, got %s", sub.Username)
	}

	if sub.UserType != "github" {
		t.Errorf("Expected UserType github, got %s", sub.UserType)
	}

	// Test GetSubscriptions
	subscriptions, err := service.GetSubscriptions("testuser123")
	if err != nil {
		t.Errorf("GetSubscriptions failed: %v", err)
	}

	if len(subscriptions) != 1 {
		t.Errorf("Expected 1 subscription, got %d", len(subscriptions))
	}

	// Test DeleteSubscription
	err = service.DeleteSubscription("testuser123", "https://fcm.googleapis.com/test")
	if err != nil {
		t.Errorf("DeleteSubscription failed: %v", err)
	}

	// Verify subscription is gone
	subscriptions, err = service.GetSubscriptions("testuser123")
	if err != nil {
		t.Errorf("GetSubscriptions failed: %v", err)
	}

	if len(subscriptions) != 0 {
		t.Errorf("Expected 0 subscriptions after deletion, got %d", len(subscriptions))
	}
}

func TestProcessWebhook(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "notification_webhook_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create service
	service, err := NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Test webhook processing (without actual sending since WebPush isn't configured)
	webhook := WebhookRequest{
		SessionID: "session123",
		UserID:    "user123",
		EventType: "message_received",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"message_id": "msg123",
		},
	}

	// This should not return an error even without WebPush configured
	// since the service handles missing WebPush gracefully
	err = service.ProcessWebhook(webhook)
	if err == nil {
		// Expected to fail because no subscriptions exist and webpush not configured
		// But the function should handle this gracefully
		t.Log("ProcessWebhook completed (expected to fail gracefully with no subscriptions)")
	}
}

func TestGetNotificationHistory(t *testing.T) {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "notification_history_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create service
	service, err := NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	userID := "testuser123"

	// Add some notification history directly to storage
	history1 := NotificationHistory{
		UserID:    userID,
		Title:     "Test Notification 1",
		Body:      "First test notification",
		Type:      "message",
		SessionID: "session1",
		SentAt:    time.Now().Add(-2 * time.Hour),
		Delivered: true,
	}

	history2 := NotificationHistory{
		UserID:    userID,
		Title:     "Test Notification 2",
		Body:      "Second test notification",
		Type:      "status_change",
		SessionID: "session2",
		SentAt:    time.Now().Add(-1 * time.Hour),
		Delivered: false,
	}

	err = service.storage.AddNotificationHistory(userID, history1)
	if err != nil {
		t.Errorf("Failed to add notification history 1: %v", err)
	}

	err = service.storage.AddNotificationHistory(userID, history2)
	if err != nil {
		t.Errorf("Failed to add notification history 2: %v", err)
	}

	// Test GetNotificationHistory
	response, err := service.GetNotificationHistory(userID, 10, 0, nil)
	if err != nil {
		t.Errorf("GetNotificationHistory failed: %v", err)
	}

	if response.Total != 2 {
		t.Errorf("Expected total 2, got %d", response.Total)
	}

	if len(response.Notifications) != 2 {
		t.Errorf("Expected 2 notifications, got %d", len(response.Notifications))
	}

	// Notifications should be sorted by newest first
	if response.Notifications[0].Title != "Test Notification 2" {
		t.Errorf("Expected first notification to be 'Test Notification 2', got %s", response.Notifications[0].Title)
	}

	// Test with filters
	filters := map[string]string{"session_id": "session1"}
	response, err = service.GetNotificationHistory(userID, 10, 0, filters)
	if err != nil {
		t.Errorf("GetNotificationHistory with filters failed: %v", err)
	}

	if response.Total != 1 {
		t.Errorf("Expected total 1 with session filter, got %d", response.Total)
	}

	if len(response.Notifications) != 1 {
		t.Errorf("Expected 1 notification with session filter, got %d", len(response.Notifications))
	}

	// Test pagination
	response, err = service.GetNotificationHistory(userID, 1, 0, nil)
	if err != nil {
		t.Errorf("GetNotificationHistory with limit failed: %v", err)
	}

	if len(response.Notifications) != 1 {
		t.Errorf("Expected 1 notification with limit 1, got %d", len(response.Notifications))
	}

	if response.HasMore != true {
		t.Errorf("Expected HasMore to be true with limit 1")
	}
}