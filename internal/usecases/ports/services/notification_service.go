package services

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// NotificationService defines the interface for notification services
type NotificationService interface {
	// SendNotification sends a notification to a specific subscription
	SendNotification(ctx context.Context, notification *entities.Notification, subscription *entities.Subscription) error

	// SendBulkNotifications sends notifications to multiple subscriptions
	SendBulkNotifications(ctx context.Context, notification *entities.Notification, subscriptions []*entities.Subscription) ([]*NotificationResult, error)

	// ValidateSubscription validates a push notification subscription
	ValidateSubscription(ctx context.Context, subscription *entities.Subscription) error

	// TestNotification sends a test notification to verify the subscription
	TestNotification(ctx context.Context, subscription *entities.Subscription) error
}

// NotificationResult represents the result of sending a notification
type NotificationResult struct {
	SubscriptionID entities.SubscriptionID
	Success        bool
	Error          error
	DeliveredAt    *string
}
