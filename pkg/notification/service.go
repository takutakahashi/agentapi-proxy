package notification

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// Service provides notification functionality
type Service struct {
	storage            Storage
	webpush            *WebPushService
	slack              *SlackService            // Optional, for Slack DM notifications
	secretSyncer       SubscriptionSecretSyncer // Optional, for syncing subscriptions to K8s Secrets (legacy)
	subscriptionReader SubscriptionReader       // Optional, for reading subscriptions from K8s Secrets
	subscriptionWriter SubscriptionWriter       // Optional, for writing subscriptions directly to K8s Secrets
}

// NewService creates a new notification service
func NewService(baseDir string) (*Service, error) {
	storage := NewJSONLStorage(baseDir)

	// WebPush service is optional - notifications can be stored without sending
	webpush, _ := NewWebPushService()

	// Slack service is optional - only available when SLACK_BOT_TOKEN is set
	slackSvc, _ := NewSlackService()

	return &Service{
		storage: storage,
		webpush: webpush,
		slack:   slackSvc,
	}, nil
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

// SetSubscriptionWriter sets the subscription writer for direct K8s Secret writes.
// When set, all subscription mutations bypass local file storage entirely.
func (s *Service) SetSubscriptionWriter(writer SubscriptionWriter) {
	s.subscriptionWriter = writer
}

// readCurrentSubscriptions returns the authoritative subscription list for a user.
// In k8s mode (subscriptionWriter set) it reads from K8s Secret; otherwise from local storage.
func (s *Service) readCurrentSubscriptions(userID string) ([]Subscription, error) {
	if s.subscriptionWriter != nil && s.subscriptionReader != nil {
		return s.subscriptionReader.GetSubscriptions(userID)
	}
	return s.storage.GetSubscriptions(userID)
}

// persistSubscriptions writes the updated subscription list.
// In k8s mode (subscriptionWriter set) it writes directly to K8s Secret; otherwise it replaces
// each entry in local storage and calls Sync.
func (s *Service) persistSubscriptions(userID string, subs []Subscription) error {
	if s.subscriptionWriter != nil {
		return s.subscriptionWriter.UpdateSubscriptions(userID, subs)
	}
	// Local-storage path: rebuild from scratch then sync.
	// Clear existing entries for this user by deleting each known endpoint.
	existing, _ := s.storage.GetSubscriptions(userID)
	for _, sub := range existing {
		_ = s.storage.DeleteSubscription(userID, sub.Endpoint)
	}
	for _, sub := range subs {
		if err := s.storage.AddSubscription(userID, sub); err != nil {
			return err
		}
	}
	if s.secretSyncer != nil {
		if syncErr := s.secretSyncer.Sync(userID); syncErr != nil {
			log.Printf("[NOTIFICATION_SERVICE] Warning: failed to sync subscription secret: %v", syncErr)
		}
	}
	return nil
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
	newSub := Subscription{
		UserID:            string(user.ID()),
		UserType:          string(user.UserType()),
		Username:          username,
		Endpoint:          endpoint,
		Keys:              keys,
		SessionIDs:        []string{},
		NotificationTypes: []string{"message", "status_change", "session_update", "error"},
		DeviceInfo:        deviceInfo,
		CreatedAt:         now,
		UpdatedAt:         now,
		LastUsed:          now,
		Active:            true,
	}

	userID := string(user.ID())
	current, _ := s.readCurrentSubscriptions(userID)

	// Replace existing sub with same endpoint, or append.
	updated := make([]Subscription, 0, len(current)+1)
	replaced := false
	for _, sub := range current {
		if sub.Endpoint == endpoint {
			updated = append(updated, newSub)
			replaced = true
		} else {
			updated = append(updated, sub)
		}
	}
	if !replaced {
		updated = append(updated, newSub)
	}

	if err := s.persistSubscriptions(userID, updated); err != nil {
		return nil, fmt.Errorf("failed to save subscription: %w", err)
	}

	return &newSub, nil
}

// SubscribeSlack creates or updates a Slack DM subscription for a user
func (s *Service) SubscribeSlack(user *entities.User, slackUserID string) (*Subscription, error) {
	if slackUserID == "" {
		return nil, fmt.Errorf("slack user ID is required")
	}

	username := string(user.ID())
	if user.UserType() == entities.UserTypeGitHub && user.GitHubInfo() != nil {
		username = user.GitHubInfo().Login()
	}

	userID := string(user.ID())
	current, _ := s.readCurrentSubscriptions(userID)

	now := time.Now()
	newSub := Subscription{
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

	// Remove existing Slack subs and add the new one.
	updated := make([]Subscription, 0, len(current)+1)
	for _, sub := range current {
		if sub.Type != SubscriptionTypeSlack {
			updated = append(updated, sub)
		}
	}
	updated = append(updated, newSub)

	if err := s.persistSubscriptions(userID, updated); err != nil {
		return nil, fmt.Errorf("failed to save Slack subscription: %w", err)
	}

	return &newSub, nil
}

// DeleteSlackSubscription removes the Slack subscription for a user
func (s *Service) DeleteSlackSubscription(userID string) error {
	current, err := s.readCurrentSubscriptions(userID)
	if err != nil {
		return err
	}
	updated := make([]Subscription, 0, len(current))
	for _, sub := range current {
		if sub.Type != SubscriptionTypeSlack {
			updated = append(updated, sub)
		}
	}
	return s.persistSubscriptions(userID, updated)
}

// GetSubscriptions returns all active subscriptions for a user
func (s *Service) GetSubscriptions(userID string) ([]Subscription, error) {
	return s.storage.GetSubscriptions(userID)
}

// DeleteSubscription removes a subscription by endpoint
func (s *Service) DeleteSubscription(userID string, endpoint string) error {
	current, err := s.readCurrentSubscriptions(userID)
	if err != nil {
		return err
	}
	updated := make([]Subscription, 0, len(current))
	found := false
	for _, sub := range current {
		if sub.Endpoint == endpoint {
			found = true
		} else {
			updated = append(updated, sub)
		}
	}
	if !found {
		return fmt.Errorf("subscription not found: %s", endpoint)
	}
	return s.persistSubscriptions(userID, updated)
}

// SetSubscriptionTypeActive sets the Active field for all subscriptions of a given type for a user.
// This is used to enable/disable a notification channel without deleting the subscription data.
func (s *Service) SetSubscriptionTypeActive(userID, subType string, active bool) error {
	current, err := s.readCurrentSubscriptions(userID)
	if err != nil {
		return err
	}

	updated := make([]Subscription, 0, len(current))
	for _, sub := range current {
		t := sub.Type
		if t == "" {
			t = SubscriptionTypeWebPush // backward compat
		}
		if t == subType {
			sub.Active = active
		}
		updated = append(updated, sub)
	}

	return s.persistSubscriptions(userID, updated)
}

// SendNotificationToUser sends a notification to all subscriptions of a user
func (s *Service) SendNotificationToUser(userID string, title, body, notificationType string, data map[string]interface{}) error {
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
				initialMessage := ""
				if im, ok := data["initial_message"].(string); ok {
					initialMessage = im
				}
				sendErr = s.slack.SendDM(sub.Endpoint, title, body, url, initialMessage)
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
				initialMessage := ""
				if im, ok := data["initial_message"].(string); ok {
					initialMessage = im
				}
				sendErr = s.slack.SendDM(sub.Endpoint, title, body, url, initialMessage)
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
	// Set URL only if not already provided (e.g., enriched by the webhook handler)
	if _, exists := data["url"]; !exists {
		if baseURL := os.Getenv("NOTIFICATION_BASE_URL"); baseURL != "" {
			data["url"] = baseURL + "/sessions/" + webhook.SessionID
		} else {
			data["url"] = fmt.Sprintf("/sessions/%s", webhook.SessionID)
		}
	}

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
	if req.InitialMessage != "" {
		data["initial_message"] = req.InitialMessage
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
