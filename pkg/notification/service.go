package notification

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// NotificationCooldown is the minimum interval between notifications sent to the same user.
const NotificationCooldown = 3 * time.Minute

// Service provides notification functionality
type Service struct {
	storage            Storage
	webpush            *WebPushService
	slack              *SlackService            // Optional, for Slack DM notifications
	secretSyncer       SubscriptionSecretSyncer // Optional, for syncing subscriptions to K8s Secrets
	subscriptionReader SubscriptionReader       // Optional, for reading subscriptions from K8s Secrets
	rateLimitStore     RateLimitStore           // Optional, for per-user notification rate limiting
}

// NewService creates a new notification service
func NewService(baseDir string) (*Service, error) {
	storage := NewJSONLStorage(baseDir)

	// WebPush service is optional - notifications can be stored without sending
	webpush, _ := NewWebPushService()

	// Slack service is optional - only available when SLACK_BOT_TOKEN is set
	slackSvc, _ := NewSlackService()

	return &Service{
		storage:        storage,
		webpush:        webpush,
		slack:          slackSvc,
		rateLimitStore: NewInMemoryRateLimitStore(NotificationCooldown),
	}, nil
}

// SetRateLimitStore replaces the rate limit store (e.g. with a ConfigMap-backed one in k8s mode).
func (s *Service) SetRateLimitStore(store RateLimitStore) {
	s.rateLimitStore = store
}

// SetSecretSyncer sets the secret syncer for syncing subscriptions to K8s Secrets
// This is optional and only used when Kubernetes mode is enabled
func (s *Service) SetSecretSyncer(syncer SubscriptionSecretSyncer) {
	s.secretSyncer = syncer
}

// SetSubscriptionReader sets the subscription reader for reading subscriptions from K8s Secrets.
// When set, SendNotificationToUser and SendNotificationToSession will read subscriptions from
// the external storage (e.g., Kubernetes Secrets) instead of the local file-based storage.
func (s *Service) SetSubscriptionReader(reader SubscriptionReader) {
	s.subscriptionReader = reader
}

// getSubscriptionsForUser returns subscriptions for a user, preferring the external reader if set.
func (s *Service) getSubscriptionsForUser(userID string) ([]Subscription, error) {
	if s.subscriptionReader != nil {
		return s.subscriptionReader.GetSubscriptions(userID)
	}
	return s.storage.GetSubscriptions(userID)
}

// getAllSubscriptions returns all subscriptions, preferring the external reader if set.
func (s *Service) getAllSubscriptions() ([]Subscription, error) {
	if s.subscriptionReader != nil {
		return s.subscriptionReader.GetAllSubscriptions()
	}
	return s.storage.GetAllSubscriptions()
}

// GetStorage returns the storage instance (used by secret syncer)
func (s *Service) GetStorage() Storage {
	return s.storage
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

	// Sync to K8s Secret if syncer is configured
	if s.secretSyncer != nil {
		if syncErr := s.secretSyncer.Sync(string(user.ID())); syncErr != nil {
			// Log warning but don't fail the subscription
			log.Printf("[NOTIFICATION_SERVICE] Warning: failed to sync subscription secret: %v", syncErr)
		}
	}

	return &sub, nil
}

// SubscribeSlack creates or updates a Slack DM subscription for a user
func (s *Service) SubscribeSlack(user *entities.User, slackUserID string) (*Subscription, error) {
	if slackUserID == "" {
		return nil, fmt.Errorf("slack user ID is required")
	}

	// Get username from GitHub user info if available, otherwise use UserID
	username := string(user.ID())
	if user.UserType() == entities.UserTypeGitHub && user.GitHubInfo() != nil {
		username = user.GitHubInfo().Login()
	}

	// Delete existing Slack subscriptions first to avoid duplicates
	userID := string(user.ID())
	existingSubs, err := s.storage.GetSubscriptions(userID)
	if err == nil {
		for _, sub := range existingSubs {
			if sub.Type == SubscriptionTypeSlack {
				_ = s.storage.DeleteSubscription(userID, sub.Endpoint)
			}
		}
	}

	now := time.Now()
	sub := Subscription{
		UserID:            userID,
		UserType:          string(user.UserType()),
		Username:          username,
		Type:              SubscriptionTypeSlack,
		Endpoint:          slackUserID,
		Keys:              map[string]string{},
		SessionIDs:        []string{},
		NotificationTypes: []string{"message", "status_change", "session_update", "error"},
		CreatedAt:         now,
		UpdatedAt:         now,
		LastUsed:          now,
		Active:            true,
	}

	err = s.storage.AddSubscription(userID, sub)
	if err != nil {
		return nil, fmt.Errorf("failed to add Slack subscription: %w", err)
	}

	// Sync to K8s Secret if syncer is configured
	if s.secretSyncer != nil {
		if syncErr := s.secretSyncer.Sync(userID); syncErr != nil {
			log.Printf("[NOTIFICATION_SERVICE] Warning: failed to sync subscription secret: %v", syncErr)
		}
	}

	return &sub, nil
}

// DeleteSlackSubscription removes the Slack subscription for a user
func (s *Service) DeleteSlackSubscription(userID string) error {
	existingSubs, err := s.storage.GetSubscriptions(userID)
	if err != nil {
		return err
	}
	for _, sub := range existingSubs {
		if sub.Type == SubscriptionTypeSlack {
			if err := s.storage.DeleteSubscription(userID, sub.Endpoint); err != nil {
				return err
			}
		}
	}

	if s.secretSyncer != nil {
		if syncErr := s.secretSyncer.Sync(userID); syncErr != nil {
			log.Printf("[NOTIFICATION_SERVICE] Warning: failed to sync subscription secret: %v", syncErr)
		}
	}
	return nil
}

// GetSubscriptions returns all active subscriptions for a user
func (s *Service) GetSubscriptions(userID string) ([]Subscription, error) {
	return s.storage.GetSubscriptions(userID)
}

// DeleteSubscription removes a subscription by endpoint
func (s *Service) DeleteSubscription(userID string, endpoint string) error {
	err := s.storage.DeleteSubscription(userID, endpoint)
	if err != nil {
		return err
	}

	// Sync to K8s Secret if syncer is configured
	if s.secretSyncer != nil {
		if syncErr := s.secretSyncer.Sync(userID); syncErr != nil {
			// Log warning but don't fail the deletion
			log.Printf("[NOTIFICATION_SERVICE] Warning: failed to sync subscription secret: %v", syncErr)
		}
	}

	return nil
}

// SetSubscriptionTypeActive sets the Active field for all subscriptions of a given type for a user.
// This is used to enable/disable a notification channel without deleting the subscription data.
func (s *Service) SetSubscriptionTypeActive(userID, subType string, active bool) error {
	subs, err := s.storage.GetSubscriptions(userID)
	if err != nil {
		return err
	}

	changed := false
	for _, sub := range subs {
		t := sub.Type
		if t == "" {
			t = SubscriptionTypeWebPush // backward compat
		}
		if t == subType {
			if err := s.storage.DeleteSubscription(userID, sub.Endpoint); err != nil {
				return fmt.Errorf("failed to delete subscription for update: %w", err)
			}
			sub.Active = active
			if err := s.storage.AddSubscription(userID, sub); err != nil {
				return fmt.Errorf("failed to re-add subscription after update: %w", err)
			}
			changed = true
		}
	}

	if changed && s.secretSyncer != nil {
		if syncErr := s.secretSyncer.Sync(userID); syncErr != nil {
			log.Printf("[NOTIFICATION_SERVICE] Warning: failed to sync subscription secret: %v", syncErr)
		}
	}
	return nil
}

// SendNotificationToUser sends a notification to all subscriptions of a user
func (s *Service) SendNotificationToUser(userID string, title, body, notificationType string, data map[string]interface{}) error {
	// Rate limiting: skip if a notification was sent recently (cooldown window)
	if s.rateLimitStore != nil && s.rateLimitStore.IsRateLimited(userID) {
		log.Printf("[NOTIFICATION_SERVICE] Rate limited: skipping notification for user %s", userID)
		return nil
	}

	subscriptions, err := s.getSubscriptionsForUser(userID)
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	var lastError error
	successCount := 0

	for _, sub := range subscriptions {
		// Skip inactive subscriptions (channel disabled by user)
		if !sub.Active {
			continue
		}
		// Check if subscription wants this notification type
		if !s.shouldSendNotification(sub, notificationType, data) {
			continue
		}

		var sendErr error
		subType := sub.Type
		if subType == "" {
			subType = SubscriptionTypeWebPush // backward compat
		}

		switch subType {
		case SubscriptionTypeWebPush:
			if s.webpush == nil {
				sendErr = fmt.Errorf("web push service not configured")
			} else {
				sendErr = s.webpush.SendNotification(sub, title, body, data)
			}
		case SubscriptionTypeSlack:
			if s.slack == nil {
				sendErr = fmt.Errorf("slack service not configured")
			} else {
				url := ""
				if u, ok := data["url"].(string); ok {
					url = u
				}
				sendErr = s.slack.SendDM(sub.Endpoint, title, body, url)
			}
		default:
			sendErr = fmt.Errorf("unsupported subscription type: %s", subType)
		}

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
			Delivered:      sendErr == nil,
		}

		if sendErr != nil {
			errMsg := sendErr.Error()
			history.ErrorMessage = &errMsg
			lastError = sendErr
		} else {
			successCount++
		}

		// Save to history
		if histErr := s.storage.AddNotificationHistory(userID, history); histErr != nil {
			// Log but don't fail the notification send
			fmt.Printf("Failed to save notification history: %v\n", histErr)
		}
	}

	if successCount > 0 && s.rateLimitStore != nil {
		if err := s.rateLimitStore.RecordSent(userID); err != nil {
			log.Printf("[NOTIFICATION_SERVICE] Failed to record rate limit for user %s: %v", userID, err)
		}
	}

	if successCount == 0 && lastError != nil {
		return fmt.Errorf("failed to send any notifications: %w", lastError)
	}

	return nil
}

// SendNotificationToSession sends a notification to all users subscribed to a session
func (s *Service) SendNotificationToSession(sessionID string, title, body, notificationType string, data map[string]interface{}) error {
	// Add session ID to data
	if data == nil {
		data = make(map[string]interface{})
	}
	data["session_id"] = sessionID

	subscriptions, err := s.getAllSubscriptions()
	if err != nil {
		return fmt.Errorf("failed to get subscriptions: %w", err)
	}

	var lastError error
	successCount := 0

	// Track which users have been successfully notified in this call to record rate limits at the end.
	sentUsers := make(map[string]struct{})

	for _, sub := range subscriptions {
		// Skip inactive subscriptions (channel disabled by user)
		if !sub.Active {
			continue
		}
		// Check if user is subscribed to this session
		if !s.isSubscribedToSession(sub, sessionID) {
			continue
		}
		// Check if subscription wants this notification type
		if !s.shouldSendNotification(sub, notificationType, data) {
			continue
		}
		// Rate limiting: skip if a notification was sent recently for this user
		if s.rateLimitStore != nil && s.rateLimitStore.IsRateLimited(sub.UserID) {
			log.Printf("[NOTIFICATION_SERVICE] Rate limited: skipping notification for user %s", sub.UserID)
			continue
		}

		var sendErr error
		subType := sub.Type
		if subType == "" {
			subType = SubscriptionTypeWebPush // backward compat
		}

		switch subType {
		case SubscriptionTypeWebPush:
			if s.webpush == nil {
				sendErr = fmt.Errorf("web push service not configured")
			} else {
				sendErr = s.webpush.SendNotification(sub, title, body, data)
			}
		case SubscriptionTypeSlack:
			if s.slack == nil {
				sendErr = fmt.Errorf("slack service not configured")
			} else {
				url := ""
				if u, ok := data["url"].(string); ok {
					url = u
				}
				sendErr = s.slack.SendDM(sub.Endpoint, title, body, url)
			}
		default:
			sendErr = fmt.Errorf("unsupported subscription type: %s", subType)
		}

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
			Delivered:      sendErr == nil,
		}

		if sendErr != nil {
			errMsg := sendErr.Error()
			history.ErrorMessage = &errMsg
			lastError = sendErr
		} else {
			successCount++
			sentUsers[sub.UserID] = struct{}{}
		}

		// Save to history
		if histErr := s.storage.AddNotificationHistory(sub.UserID, history); histErr != nil {
			// Log but don't fail the notification send
			fmt.Printf("Failed to save notification history: %v\n", histErr)
		}
	}

	// Record rate limit timestamps for all users who received a notification.
	if s.rateLimitStore != nil {
		for userID := range sentUsers {
			if err := s.rateLimitStore.RecordSent(userID); err != nil {
				log.Printf("[NOTIFICATION_SERVICE] Failed to record rate limit for user %s: %v", userID, err)
			}
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
	data["url"] = fmt.Sprintf("/sessions/%s", webhook.SessionID)

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
	// "manual" notifications (explicitly triggered via API/CLI) are always delivered
	// regardless of the subscription's notification type filter.
	if notificationType == "manual" {
		return true
	}

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

// SendNotification sends a notification based on a SendNotificationRequest.
// It routes to SendNotificationToSession or SendNotificationToUser depending on which field is set.
func (s *Service) SendNotification(req SendNotificationRequest) (*SendNotificationResponse, error) {
	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if req.Body == "" {
		return nil, fmt.Errorf("body is required")
	}
	if req.SessionID == "" && req.UserID == "" {
		return nil, fmt.Errorf("either session_id or user_id is required")
	}

	data := make(map[string]interface{})
	if req.URL != "" {
		data["url"] = req.URL
	}
	if req.Icon != "" {
		data["icon"] = req.Icon
	}

	var err error
	if req.SessionID != "" {
		err = s.SendNotificationToSession(req.SessionID, req.Title, req.Body, "manual", data)
	} else {
		err = s.SendNotificationToUser(req.UserID, req.Title, req.Body, "manual", data)
	}

	if err != nil {
		return &SendNotificationResponse{Success: false, Message: err.Error()}, err
	}

	return &SendNotificationResponse{Success: true}, nil
}

// GetBaseDir returns the base directory for user data
func GetBaseDir() string {
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		homeDir = "/home/agentapi"
	}
	return filepath.Join(homeDir, ".agentapi-proxy")
}
