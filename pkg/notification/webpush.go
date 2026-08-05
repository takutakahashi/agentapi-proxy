package notification

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SherClockHolmes/webpush-go"
)

// WebPushService handles sending web push notifications
type WebPushService struct {
	vapidPublicKey    string
	vapidPrivateKey   string
	vapidContactEmail string
}

// NewWebPushService creates a new web push service
func NewWebPushService() (*WebPushService, error) {
	vapidPublicKey := os.Getenv("VAPID_PUBLIC_KEY")
	vapidPrivateKey := os.Getenv("VAPID_PRIVATE_KEY")
	vapidContactEmail := os.Getenv("VAPID_CONTACT_EMAIL")

	if vapidPublicKey == "" || vapidPrivateKey == "" || vapidContactEmail == "" {
		return nil, fmt.Errorf("VAPID configuration required: set VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY, and VAPID_CONTACT_EMAIL environment variables")
	}

	return &WebPushService{
		vapidPublicKey:    vapidPublicKey,
		vapidPrivateKey:   vapidPrivateKey,
		vapidContactEmail: vapidContactEmail,
	}, nil
}

// SendNotification sends a push notification to a subscription
func (s *WebPushService) SendNotification(sub Subscription, title, body string, data map[string]interface{}) error {
	// Create notification payload
	payload := map[string]interface{}{
		"title": title,
		"body":  body,
		"icon":  "/icon-192x192.png",
		"data":  data,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create webpush subscription
	webpushSub := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.Keys["p256dh"],
			Auth:   sub.Keys["auth"],
		},
	}

	// Create webpush options
	options := &webpush.Options{
		Subscriber:      s.vapidContactEmail,
		VAPIDPublicKey:  s.vapidPublicKey,
		VAPIDPrivateKey: s.vapidPrivateKey,
		TTL:             86400, // 24 hours
		Urgency:         webpush.UrgencyNormal,
	}

	// Send notification
	resp, err := webpush.SendNotification(payloadBytes, webpushSub, options)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Warning: failed to close response body: %v\n", err)
		}
	}()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("notification rejected with status %d", resp.StatusCode)
	}

	return nil
}

// SendNotificationWithOptions sends a push notification with custom options
func (s *WebPushService) SendNotificationWithOptions(sub Subscription, title, body string, data map[string]interface{}, ttl int, urgency string) error {
	// Create notification payload
	payload := map[string]interface{}{
		"title": title,
		"body":  body,
		"icon":  data["icon"],
		"badge": data["badge"],
		"data":  data,
	}

	// Remove icon and badge from data to avoid duplication
	delete(data, "icon")
	delete(data, "badge")

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create webpush subscription
	webpushSub := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.Keys["p256dh"],
			Auth:   sub.Keys["auth"],
		},
	}

	// Create webpush options
	options := &webpush.Options{
		Subscriber:      s.vapidContactEmail,
		VAPIDPublicKey:  s.vapidPublicKey,
		VAPIDPrivateKey: s.vapidPrivateKey,
		TTL:             ttl,
	}

	// Set urgency
	switch urgency {
	case "low":
		options.Urgency = webpush.UrgencyLow
	case "high":
		options.Urgency = webpush.UrgencyHigh
	default:
		options.Urgency = webpush.UrgencyNormal
	}

	// Send notification
	resp, err := webpush.SendNotification(payloadBytes, webpushSub, options)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Warning: failed to close response body: %v\n", err)
		}
	}()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("notification rejected with status %d", resp.StatusCode)
	}

	return nil
}
