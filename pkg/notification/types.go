package notification

import (
	"time"
)

// SubscribeRequest represents the request body for subscribing to push notifications
type SubscribeRequest struct {
	Endpoint string            `json:"endpoint" validate:"required"`
	Keys     map[string]string `json:"keys" validate:"required"`
}

// SubscribeResponse represents the response for a successful subscription
type SubscribeResponse struct {
	Success        bool   `json:"success"`
	SubscriptionID string `json:"subscription_id"`
}

// Subscription represents a push notification subscription in the system
type Subscription struct {
	ID                string            `json:"id"`
	UserID            string            `json:"user_id"`
	UserType          string            `json:"user_type"`
	Username          string            `json:"username"`
	Endpoint          string            `json:"endpoint"`
	Keys              map[string]string `json:"keys"`
	SessionIDs        []string          `json:"session_ids"`
	NotificationTypes []string          `json:"notification_types"`
	CreatedAt         time.Time         `json:"created_at"`
	Active            bool              `json:"active"`
}

// WebhookRequest represents the webhook payload from agentapi
type WebhookRequest struct {
	SessionID string                 `json:"session_id"`
	UserID    string                 `json:"user_id"`
	EventType string                 `json:"event_type"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// NotificationHistory represents a notification that was sent
type NotificationHistory struct {
	ID             string                 `json:"id"`
	UserID         string                 `json:"user_id"`
	SubscriptionID string                 `json:"subscription_id"`
	Title          string                 `json:"title"`
	Body           string                 `json:"body"`
	Type           string                 `json:"type"`
	SessionID      string                 `json:"session_id"`
	Data           map[string]interface{} `json:"data"`
	SentAt         time.Time              `json:"sent_at"`
	Delivered      bool                   `json:"delivered"`
	Clicked        bool                   `json:"clicked"`
	ErrorMessage   *string                `json:"error_message"`
}

// HistoryResponse represents the response for notification history endpoint
type HistoryResponse struct {
	Notifications []NotificationHistory `json:"notifications"`
	Total         int                   `json:"total"`
	HasMore       bool                  `json:"has_more"`
}

// DeleteSubscriptionRequest represents the request body for deleting a subscription
type DeleteSubscriptionRequest struct {
	Endpoint string `json:"endpoint" validate:"required"`
}
