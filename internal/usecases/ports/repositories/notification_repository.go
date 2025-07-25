package repositories

import (
	"context"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// NotificationRepository defines the interface for notification data persistence
type NotificationRepository interface {
	// SaveSubscription persists a subscription
	SaveSubscription(ctx context.Context, subscription *entities.Subscription) error

	// FindSubscriptionByID retrieves a subscription by its ID
	FindSubscriptionByID(ctx context.Context, id entities.SubscriptionID) (*entities.Subscription, error)

	// FindSubscriptionsByUserID retrieves all subscriptions for a specific user
	FindSubscriptionsByUserID(ctx context.Context, userID entities.UserID) ([]*entities.Subscription, error)

	// FindActiveSubscriptions retrieves all active subscriptions
	FindActiveSubscriptions(ctx context.Context) ([]*entities.Subscription, error)

	// UpdateSubscription updates an existing subscription
	UpdateSubscription(ctx context.Context, subscription *entities.Subscription) error

	// DeleteSubscription removes a subscription
	DeleteSubscription(ctx context.Context, id entities.SubscriptionID) error

	// SaveNotification persists a notification
	SaveNotification(ctx context.Context, notification *entities.Notification) error

	// FindNotificationByID retrieves a notification by its ID
	FindNotificationByID(ctx context.Context, id entities.NotificationID) (*entities.Notification, error)

	// FindNotificationsByUserID retrieves notifications for a specific user
	FindNotificationsByUserID(ctx context.Context, userID entities.UserID) ([]*entities.Notification, error)

	// FindNotificationsBySubscriptionID retrieves notifications for a specific subscription
	FindNotificationsBySubscriptionID(ctx context.Context, subscriptionID entities.SubscriptionID) ([]*entities.Notification, error)

	// UpdateNotification updates an existing notification
	UpdateNotification(ctx context.Context, notification *entities.Notification) error

	// DeleteNotification removes a notification
	DeleteNotification(ctx context.Context, id entities.NotificationID) error

	// FindSubscriptionsWithFilters retrieves subscriptions with filtering options
	FindSubscriptionsWithFilters(ctx context.Context, filters SubscriptionFilters) ([]*entities.Subscription, error)

	// FindNotificationsWithFilters retrieves notifications with filtering options
	FindNotificationsWithFilters(ctx context.Context, filters NotificationFilters) ([]*entities.Notification, error)
}

// SubscriptionFilters defines filtering options for subscription queries
type SubscriptionFilters struct {
	UserID           *entities.UserID
	UserType         *entities.UserType
	Username         *string
	SessionID        *entities.SessionID
	NotificationType *entities.NotificationType
	Active           *bool
	Limit            int
	Offset           int
	SortBy           string
	SortOrder        string // "asc" or "desc"
}

// NotificationFilters defines filtering options for notification queries
type NotificationFilters struct {
	UserID         *entities.UserID
	SubscriptionID *entities.SubscriptionID
	SessionID      *entities.SessionID
	Type           *entities.NotificationType
	Status         *entities.NotificationStatus
	DateFrom       *string
	DateTo         *string
	Limit          int
	Offset         int
	SortBy         string
	SortOrder      string // "asc" or "desc"
}
