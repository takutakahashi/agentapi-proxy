package notification

// Storage interface for notification data persistence
type Storage interface {
	// Subscription methods
	AddSubscription(userID string, sub Subscription) error
	GetSubscriptions(userID string) ([]Subscription, error)
	GetAllSubscriptions() ([]Subscription, error)
	UpdateSubscription(userID string, subscriptionID string, updates Subscription) error
	DeleteSubscription(userID string, endpoint string) error

	// History methods
	AddNotificationHistory(userID string, notification NotificationHistory) error
	GetNotificationHistory(userID string, limit, offset int, filters map[string]string) ([]NotificationHistory, int, error)
	RotateNotificationHistory(userID string, maxEntries int) error
}

// JSONLStorage is deprecated, use JSONStorage instead for better performance and duplicate prevention
type JSONLStorage = JSONStorage

// NewJSONLStorage creates a new JSON-based storage (deprecated name for backward compatibility)
func NewJSONLStorage(baseDir string) *JSONStorage {
	return NewJSONStorage(baseDir)
}
