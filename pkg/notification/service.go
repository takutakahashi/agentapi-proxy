package notification

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// Service provides notification functionality
type Service struct {
	storage Storage
	webpush *WebPushService
}

// NewService creates a new notification service
func NewService(baseDir string) (*Service, error) {
	storage := NewJSONLStorage(baseDir)

	// WebPush service is optional - notifications can be stored without sending
	webpush, _ := NewWebPushService()

	return &Service{
		storage: storage,
		webpush: webpush,
	}, nil
}

// Subscribe creates a new push notification subscription
func (s *Service) Subscribe(user *entities.User, endpoint string, keys map[string]string, deviceInfo *DeviceInfo) (*Subscription, error) {
	// Get username from GitHub user info if available, otherwise use UserID
	username := string(user.ID())
	if user.UserType() == entities.UserTypeGitHub && user.GitHubInfo() != nil {
		username = user.GitHubInfo().Login()
	}

	now := time.Now()
	sub := Subscription{
		UserID:            string(user.ID()),
		UserType:          string(user.UserType()),
		Username:          username,
		Endpoint:          endpoint,
		Keys:              keys,
		SessionIDs:        []string{}, // Empty means all sessions
		NotificationTypes: []string{"message", "status_change", "session_update", "error"},
		DeviceInfo:        deviceInfo,
		CreatedAt:         now,
		UpdatedAt:         now,
		LastUsed:          now,
		Active:            true,
	}

	err := s.storage.AddSubscription(string(user.ID()), sub)
	if err != nil {
		return nil, fmt.Errorf("failed to add subscription: %w", err)
	}

	return &sub, nil
}

// GetSubscriptions returns all active subscriptions for a user
func (s *Service) GetSubscriptions(userID string) ([]Subscription, error) {
	return s.storage.GetSubscriptions(userID)
}

// DeleteSubscription removes a subscription by endpoint
func (s *Service) DeleteSubscription(userID string, endpoint string) error {
	return s.storage.DeleteSubscription(userID, endpoint)
}

// SendNotificationToUser sends a notification to all subscriptions of a user
func (s *Service) SendNotificationToUser(userID string, title, body, notificationType string, data map[string]interface{}) error {
	if s.webpush == nil {
		return fmt.Errorf("web push service not configured")
	}

	subscriptions, err := s.storage.GetSubscriptions(userID)
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	var lastError error
	successCount := 0

	for _, sub := range subscriptions {
		// Check if subscription wants this notification type
		if !s.shouldSendNotification(sub, notificationType, data) {
			continue
		}

		// Send notification
		err := s.webpush.SendNotification(sub, title, body, data)

		// Record history
		history := NotificationHistory{
			UserID:         userID,
			SubscriptionID: sub.ID,
			Title:          title,
			Body:           body,
			Type:           notificationType,
			SessionID:      getSessionIDFromData(data),
			Data:           data,
			SentAt:         time.Now(),
			Delivered:      err == nil,
		}

		if err != nil {
			errMsg := err.Error()
			history.ErrorMessage = &errMsg
			lastError = err
		} else {
			successCount++
		}

		// Save to history
		if histErr := s.storage.AddNotificationHistory(userID, history); histErr != nil {
			// Log but don't fail the notification send
			fmt.Printf("Failed to save notification history: %v\n", histErr)
		}
	}

	if successCount == 0 && lastError != nil {
		return fmt.Errorf("failed to send any notifications: %w", lastError)
	}

	return nil
}

// SendNotificationToSession sends a notification to all users subscribed to a session
func (s *Service) SendNotificationToSession(sessionID string, title, body, notificationType string, data map[string]interface{}) error {
	if s.webpush == nil {
		return fmt.Errorf("web push service not configured")
	}

	// Add session ID to data
	if data == nil {
		data = make(map[string]interface{})
	}
	data["session_id"] = sessionID

	subscriptions, err := s.storage.GetAllSubscriptions()
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	var lastError error
	successCount := 0

	for _, sub := range subscriptions {
		// Check if user is subscribed to this session
		if !s.isSubscribedToSession(sub, sessionID) {
			continue
		}

		// Check if subscription wants this notification type
		if !s.shouldSendNotification(sub, notificationType, data) {
			continue
		}

		// Send notification
		err := s.webpush.SendNotification(sub, title, body, data)

		// Record history
		history := NotificationHistory{
			UserID:         sub.UserID,
			SubscriptionID: sub.ID,
			Title:          title,
			Body:           body,
			Type:           notificationType,
			SessionID:      sessionID,
			Data:           data,
			SentAt:         time.Now(),
			Delivered:      err == nil,
		}

		if err != nil {
			errMsg := err.Error()
			history.ErrorMessage = &errMsg
			lastError = err
		} else {
			successCount++
		}

		// Save to history
		if histErr := s.storage.AddNotificationHistory(sub.UserID, history); histErr != nil {
			// Log but don't fail the notification send
			fmt.Printf("Failed to save notification history: %v\n", histErr)
		}
	}

	if successCount == 0 && lastError != nil {
		return fmt.Errorf("failed to send any notifications: %w", lastError)
	}

	return nil
}

// ProcessWebhook handles incoming webhooks from agentapi
func (s *Service) ProcessWebhook(webhook WebhookRequest) error {
	// Map event types to notification parameters
	var title, body, notificationType string
	data := webhook.Data
	if data == nil {
		data = make(map[string]interface{})
	}
	data["url"] = fmt.Sprintf("/session/%s", webhook.SessionID)

	switch webhook.EventType {
	case "message_received":
		title = "新しいメッセージ"
		body = "Claude からの返答が到着しました"
		notificationType = "message"
	case "status_change":
		status, _ := data["status"].(string)
		if status == "running" {
			title = "ステータス変更"
			body = "エージェントが応答中です"
		} else {
			title = "ステータス変更"
			body = "エージェントの応答が完了しました"
		}
		notificationType = "status_change"
	case "session_update":
		title = "セッション更新"
		body = "セッションが更新されました"
		notificationType = "session_update"
	case "error":
		title = "エラー発生"
		body = "セッションでエラーが発生しました"
		notificationType = "error"
	default:
		// Unknown event type, skip
		return nil
	}

	// Send notification to all users subscribed to this session
	return s.SendNotificationToSession(webhook.SessionID, title, body, notificationType, data)
}

// GetNotificationHistory retrieves notification history for a user
func (s *Service) GetNotificationHistory(userID string, limit, offset int, filters map[string]string) (*HistoryResponse, error) {
	notifications, total, err := s.storage.GetNotificationHistory(userID, limit, offset, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification history: %w", err)
	}

	hasMore := (offset + len(notifications)) < total

	return &HistoryResponse{
		Notifications: notifications,
		Total:         total,
		HasMore:       hasMore,
	}, nil
}

// shouldSendNotification checks if a notification should be sent to a subscription
func (s *Service) shouldSendNotification(sub Subscription, notificationType string, data map[string]interface{}) bool {
	// Check if subscription wants this notification type
	if len(sub.NotificationTypes) > 0 {
		found := false
		for _, nt := range sub.NotificationTypes {
			if nt == notificationType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// isSubscribedToSession checks if a subscription is for a specific session
func (s *Service) isSubscribedToSession(sub Subscription, sessionID string) bool {
	// Empty session_ids means subscribed to all sessions
	if len(sub.SessionIDs) == 0 {
		return true
	}

	// Check if the session is in the subscription's session list
	for _, sid := range sub.SessionIDs {
		if sid == sessionID {
			return true
		}
	}

	return false
}

// getSessionIDFromData extracts session ID from notification data
func getSessionIDFromData(data map[string]interface{}) string {
	if data == nil {
		return ""
	}

	if sessionID, ok := data["session_id"].(string); ok {
		return sessionID
	}

	return ""
}

// GetBaseDir returns the base directory for user data
func GetBaseDir() string {
	baseDir := os.Getenv("USERHOME_BASEDIR")
	if baseDir == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		baseDir = filepath.Join(homeDir, ".agentapi-proxy")
	}
	return baseDir
}
