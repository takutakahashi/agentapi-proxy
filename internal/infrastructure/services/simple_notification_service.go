package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"net/http"
	"strings"
	"time"
)

// SimpleNotificationService implements NotificationService with basic functionality
type SimpleNotificationService struct {
	httpClient *http.Client
}

// NewSimpleNotificationService creates a new SimpleNotificationService
func NewSimpleNotificationService() *SimpleNotificationService {
	return &SimpleNotificationService{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SendNotification sends a notification to a specific subscription
func (s *SimpleNotificationService) SendNotification(ctx context.Context, notification *entities.Notification, subscription *entities.Subscription) error {
	if notification == nil {
		return errors.New("notification cannot be nil")
	}

	if subscription == nil {
		return errors.New("subscription cannot be nil")
	}

	if !subscription.IsActive() {
		return errors.New("subscription is not active")
	}

	switch subscription.Type() {
	case entities.SubscriptionTypeWebPush:
		return s.sendWebPushNotification(ctx, notification, subscription)
	case entities.SubscriptionTypeWebhook:
		return s.sendWebhookNotification(ctx, notification, subscription)
	case entities.SubscriptionTypeEmail:
		return s.sendEmailNotification(ctx, notification, subscription)
	default:
		return fmt.Errorf("unsupported subscription type: %s", subscription.Type())
	}
}

// SendBulkNotifications sends notifications to multiple subscriptions
func (s *SimpleNotificationService) SendBulkNotifications(ctx context.Context, notification *entities.Notification, subscriptions []*entities.Subscription) ([]*services.NotificationResult, error) {
	results := make([]*services.NotificationResult, len(subscriptions))

	for i, subscription := range subscriptions {
		result := &services.NotificationResult{
			SubscriptionID: subscription.ID(),
			Success:        false,
		}

		err := s.SendNotification(ctx, notification, subscription)
		if err != nil {
			result.Error = err
		} else {
			result.Success = true
			deliveredAt := time.Now().Format(time.RFC3339)
			result.DeliveredAt = &deliveredAt
		}

		results[i] = result
	}

	return results, nil
}

// ValidateSubscription validates a push notification subscription
func (s *SimpleNotificationService) ValidateSubscription(ctx context.Context, subscription *entities.Subscription) error {
	if subscription == nil {
		return errors.New("subscription cannot be nil")
	}

	if subscription.Endpoint() == "" {
		return errors.New("subscription endpoint cannot be empty")
	}

	switch subscription.Type() {
	case entities.SubscriptionTypeWebPush:
		return s.validateWebPushSubscription(subscription)
	case entities.SubscriptionTypeWebhook:
		return s.validateWebhookSubscription(subscription)
	case entities.SubscriptionTypeEmail:
		return s.validateEmailSubscription(subscription)
	default:
		return fmt.Errorf("unsupported subscription type: %s", subscription.Type())
	}
}

// TestNotification sends a test notification to verify the subscription
func (s *SimpleNotificationService) TestNotification(ctx context.Context, subscription *entities.Subscription) error {
	if subscription == nil {
		return errors.New("subscription cannot be nil")
	}

	// Create a test notification
	testNotification := entities.NewNotification(
		entities.NotificationID("test_"+string(subscription.ID())),
		subscription.UserID(),
		subscription.ID(),
		"Test Notification",
		"This is a test notification to verify your subscription.",
		entities.NotificationTypeManual,
	)

	return s.SendNotification(ctx, testNotification, subscription)
}

// sendWebPushNotification sends a web push notification
func (s *SimpleNotificationService) sendWebPushNotification(ctx context.Context, notification *entities.Notification, subscription *entities.Subscription) error {
	// In a real implementation, this would use the Web Push Protocol
	// For now, we'll simulate the behavior

	endpoint := subscription.Endpoint()
	if !strings.HasPrefix(endpoint, "https://") {
		return errors.New("web push endpoint must use HTTPS")
	}

	// Simulate sending to the push service
	fmt.Printf("Sending web push notification to %s: %s\n", endpoint, notification.Title())

	return nil
}

// sendWebhookNotification sends a webhook notification
func (s *SimpleNotificationService) sendWebhookNotification(ctx context.Context, notification *entities.Notification, subscription *entities.Subscription) error {
	endpoint := subscription.Endpoint()

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(s.buildWebhookPayload(notification)))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "agentapi-proxy/1.0")

	// Add webhook authentication if available
	keys := subscription.Keys()
	if secret, exists := keys["secret"]; exists {
		req.Header.Set("X-Webhook-Secret", secret)
	}

	// Send request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // ignore error
	}()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned error status: %d", resp.StatusCode)
	}

	return nil
}

// sendEmailNotification sends an email notification
func (s *SimpleNotificationService) sendEmailNotification(ctx context.Context, notification *entities.Notification, subscription *entities.Subscription) error {
	// In a real implementation, this would integrate with an email service like SendGrid, SES, etc.
	// For now, we'll simulate the behavior

	email := subscription.Endpoint()
	if !strings.Contains(email, "@") {
		return errors.New("invalid email address")
	}

	fmt.Printf("Sending email notification to %s: %s\n", email, notification.Title())

	return nil
}

// validateWebPushSubscription validates a web push subscription
func (s *SimpleNotificationService) validateWebPushSubscription(subscription *entities.Subscription) error {
	endpoint := subscription.Endpoint()

	if !strings.HasPrefix(endpoint, "https://") {
		return errors.New("web push endpoint must use HTTPS")
	}

	keys := subscription.Keys()
	if _, exists := keys["p256dh"]; !exists {
		return errors.New("web push subscription missing p256dh key")
	}

	if _, exists := keys["auth"]; !exists {
		return errors.New("web push subscription missing auth key")
	}

	return nil
}

// validateWebhookSubscription validates a webhook subscription
func (s *SimpleNotificationService) validateWebhookSubscription(subscription *entities.Subscription) error {
	endpoint := subscription.Endpoint()

	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return errors.New("webhook endpoint must be a valid HTTP/HTTPS URL")
	}

	// Test connectivity
	req, err := http.NewRequest("HEAD", endpoint, nil)
	if err != nil {
		return fmt.Errorf("invalid webhook endpoint: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req = req.WithContext(ctx)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook endpoint unreachable: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() // ignore error
	}()

	return nil
}

// validateEmailSubscription validates an email subscription
func (s *SimpleNotificationService) validateEmailSubscription(subscription *entities.Subscription) error {
	email := subscription.Endpoint()

	if !strings.Contains(email, "@") {
		return errors.New("invalid email address format")
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("invalid email address format")
	}

	return nil
}

// buildWebhookPayload builds the JSON payload for webhook notifications
func (s *SimpleNotificationService) buildWebhookPayload(notification *entities.Notification) string {
	// Simple JSON payload - in a real implementation, use proper JSON marshaling
	payload := fmt.Sprintf(`{
		"id": "%s",
		"user_id": "%s",
		"title": "%s",
		"body": "%s",
		"created_at": "%s"`,
		notification.ID(),
		notification.UserID(),
		notification.Title(),
		notification.Body(),
		notification.CreatedAt().Format(time.RFC3339),
	)

	if url := notification.URL(); url != nil {
		payload += fmt.Sprintf(`,
		"url": "%s"`, *url)
	}

	if iconURL := notification.IconURL(); iconURL != nil {
		payload += fmt.Sprintf(`,
		"icon_url": "%s"`, *iconURL)
	}

	payload += "}"

	return payload
}
