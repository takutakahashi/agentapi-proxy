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

// ProxyService defines the interface for HTTP proxy services
type ProxyService interface {
	// RouteRequest routes an HTTP request to the appropriate session
	RouteRequest(ctx context.Context, sessionID entities.SessionID, request *HTTPRequest) (*HTTPResponse, error)

	// IsSessionReachable checks if a session is reachable via HTTP
	IsSessionReachable(ctx context.Context, sessionID entities.SessionID) (bool, error)

	// GetSessionURL constructs the URL for accessing a session
	GetSessionURL(ctx context.Context, sessionID entities.SessionID) (string, error)
}

// NotificationResult represents the result of sending a notification
type NotificationResult struct {
	SubscriptionID entities.SubscriptionID
	Success        bool
	Error          error
	DeliveredAt    *string
}

// HTTPRequest represents an HTTP request
type HTTPRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    []byte
	Query   map[string]string
}

// HTTPResponse represents an HTTP response
type HTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}
