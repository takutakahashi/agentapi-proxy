package notification

// SubscriptionSecretSyncer syncs subscription data to an external storage (e.g., Kubernetes Secret)
// This interface allows the notification service to remain agnostic of the underlying storage mechanism
type SubscriptionSecretSyncer interface {
	// Sync creates or updates the subscription storage for a user
	// It reads the current subscriptions from the storage and syncs them to the external storage
	Sync(userID string) error
}

// SubscriptionReader reads subscription data from an external storage (e.g., Kubernetes Secret)
// This interface is used when subscriptions are stored externally (e.g., in Kubernetes Secrets)
// rather than in the local file-based storage.
type SubscriptionReader interface {
	// GetSubscriptions returns all subscriptions for a user (including inactive)
	GetSubscriptions(userID string) ([]Subscription, error)
	// GetAllSubscriptions returns all subscriptions across all users (including inactive)
	GetAllSubscriptions() ([]Subscription, error)
}

// SubscriptionWriter writes subscription data directly to an external storage (e.g., Kubernetes Secret).
// When set on the Service, all subscription mutations bypass local file storage entirely and use
// read-modify-write operations against the external storage.
type SubscriptionWriter interface {
	// UpdateSubscriptions replaces all subscriptions for a user with the provided list.
	UpdateSubscriptions(userID string, subs []Subscription) error
}
