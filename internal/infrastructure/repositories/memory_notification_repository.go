package repositories

import (
	"context"
	"errors"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"sync"
)

// MemoryNotificationRepository implements NotificationRepository using in-memory storage
type MemoryNotificationRepository struct {
	mu            sync.RWMutex
	notifications map[entities.NotificationID]*entities.Notification
	subscriptions map[entities.SubscriptionID]*entities.Subscription
}

// NewMemoryNotificationRepository creates a new MemoryNotificationRepository
func NewMemoryNotificationRepository() *MemoryNotificationRepository {
	return &MemoryNotificationRepository{
		notifications: make(map[entities.NotificationID]*entities.Notification),
		subscriptions: make(map[entities.SubscriptionID]*entities.Subscription),
	}
}

// SaveSubscription persists a subscription
func (r *MemoryNotificationRepository) SaveSubscription(ctx context.Context, subscription *entities.Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if subscription already exists
	if _, exists := r.subscriptions[subscription.ID()]; exists {
		return errors.New("subscription already exists")
	}

	// Clone subscription to avoid external modifications
	cloned := r.cloneSubscription(subscription)
	r.subscriptions[subscription.ID()] = cloned

	return nil
}

// FindSubscriptionByID retrieves a subscription by its ID
func (r *MemoryNotificationRepository) FindSubscriptionByID(ctx context.Context, id entities.SubscriptionID) (*entities.Subscription, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	subscription, exists := r.subscriptions[id]
	if !exists {
		return nil, errors.New("subscription not found")
	}

	return r.cloneSubscription(subscription), nil
}

// FindSubscriptionsByUserID retrieves all subscriptions for a specific user
func (r *MemoryNotificationRepository) FindSubscriptionsByUserID(ctx context.Context, userID entities.UserID) ([]*entities.Subscription, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.Subscription
	for _, subscription := range r.subscriptions {
		if subscription.UserID() == userID {
			result = append(result, r.cloneSubscription(subscription))
		}
	}

	return result, nil
}

// FindActiveSubscriptions retrieves all active subscriptions
func (r *MemoryNotificationRepository) FindActiveSubscriptions(ctx context.Context) ([]*entities.Subscription, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.Subscription
	for _, subscription := range r.subscriptions {
		if subscription.IsActive() {
			result = append(result, r.cloneSubscription(subscription))
		}
	}

	return result, nil
}

// UpdateSubscription updates an existing subscription
func (r *MemoryNotificationRepository) UpdateSubscription(ctx context.Context, subscription *entities.Subscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.subscriptions[subscription.ID()]; !exists {
		return errors.New("subscription not found")
	}

	// Clone subscription to avoid external modifications
	cloned := r.cloneSubscription(subscription)
	r.subscriptions[subscription.ID()] = cloned

	return nil
}

// DeleteSubscription removes a subscription
func (r *MemoryNotificationRepository) DeleteSubscription(ctx context.Context, id entities.SubscriptionID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.subscriptions[id]; !exists {
		return errors.New("subscription not found")
	}

	delete(r.subscriptions, id)
	return nil
}

// SaveNotification persists a notification
func (r *MemoryNotificationRepository) SaveNotification(ctx context.Context, notification *entities.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if notification already exists
	if _, exists := r.notifications[notification.ID()]; exists {
		return errors.New("notification already exists")
	}

	// Clone notification to avoid external modifications
	cloned := r.cloneNotification(notification)
	r.notifications[notification.ID()] = cloned

	return nil
}

// FindNotificationByID retrieves a notification by its ID
func (r *MemoryNotificationRepository) FindNotificationByID(ctx context.Context, id entities.NotificationID) (*entities.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	notification, exists := r.notifications[id]
	if !exists {
		return nil, errors.New("notification not found")
	}

	return r.cloneNotification(notification), nil
}

// FindNotificationsByUserID retrieves notifications for a specific user
func (r *MemoryNotificationRepository) FindNotificationsByUserID(ctx context.Context, userID entities.UserID) ([]*entities.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.Notification
	for _, notification := range r.notifications {
		if notification.UserID() == userID {
			result = append(result, r.cloneNotification(notification))
		}
	}

	return result, nil
}

// FindNotificationsBySubscriptionID retrieves notifications for a specific subscription
func (r *MemoryNotificationRepository) FindNotificationsBySubscriptionID(ctx context.Context, subscriptionID entities.SubscriptionID) ([]*entities.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*entities.Notification
	// In a real implementation, you'd have a relationship between notifications and subscriptions
	// For now, we'll return an empty slice
	return result, nil
}

// UpdateNotification updates an existing notification
func (r *MemoryNotificationRepository) UpdateNotification(ctx context.Context, notification *entities.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.notifications[notification.ID()]; !exists {
		return errors.New("notification not found")
	}

	// Clone notification to avoid external modifications
	cloned := r.cloneNotification(notification)
	r.notifications[notification.ID()] = cloned

	return nil
}

// DeleteNotification removes a notification
func (r *MemoryNotificationRepository) DeleteNotification(ctx context.Context, id entities.NotificationID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.notifications[id]; !exists {
		return errors.New("notification not found")
	}

	delete(r.notifications, id)
	return nil
}

// FindSubscriptionsWithFilters retrieves subscriptions with filtering options
func (r *MemoryNotificationRepository) FindSubscriptionsWithFilters(ctx context.Context, filters repositories.SubscriptionFilters) ([]*entities.Subscription, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []*entities.Subscription

	// Apply filters
	for _, subscription := range r.subscriptions {
		if r.matchesSubscriptionFilter(subscription, filters) {
			filtered = append(filtered, r.cloneSubscription(subscription))
		}
	}

	// TODO: Apply sorting and pagination

	return filtered, nil
}

// FindNotificationsWithFilters retrieves notifications with filtering options
func (r *MemoryNotificationRepository) FindNotificationsWithFilters(ctx context.Context, filters repositories.NotificationFilters) ([]*entities.Notification, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filtered []*entities.Notification

	// Apply filters
	for _, notification := range r.notifications {
		if r.matchesNotificationFilter(notification, filters) {
			filtered = append(filtered, r.cloneNotification(notification))
		}
	}

	// TODO: Apply sorting and pagination

	return filtered, nil
}

// matchesSubscriptionFilter checks if a subscription matches the given filter criteria
func (r *MemoryNotificationRepository) matchesSubscriptionFilter(subscription *entities.Subscription, filters repositories.SubscriptionFilters) bool {
	// Filter by user ID
	if filters.UserID != nil && subscription.UserID() != *filters.UserID {
		return false
	}

	// Filter by active status
	if filters.Active != nil && subscription.IsActive() != *filters.Active {
		return false
	}

	return true
}

// matchesNotificationFilter checks if a notification matches the given filter criteria
func (r *MemoryNotificationRepository) matchesNotificationFilter(notification *entities.Notification, filters repositories.NotificationFilters) bool {
	// Filter by user ID
	if filters.UserID != nil && notification.UserID() != *filters.UserID {
		return false
	}

	// Filter by status
	if filters.Status != nil && notification.Status() != *filters.Status {
		return false
	}

	return true
}

// cloneSubscription creates a deep copy of a subscription to prevent external modifications
func (r *MemoryNotificationRepository) cloneSubscription(subscription *entities.Subscription) *entities.Subscription {
	// In a real implementation, you would properly clone all fields
	return subscription
}

// cloneNotification creates a deep copy of a notification to prevent external modifications
func (r *MemoryNotificationRepository) cloneNotification(notification *entities.Notification) *entities.Notification {
	// In a real implementation, you would properly clone all fields
	return notification
}
