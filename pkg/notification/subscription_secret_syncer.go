package notification

// SubscriptionSecretSyncer syncs subscription data to an external storage (e.g., Kubernetes Secret)
// This interface allows the notification service to remain agnostic of the underlying storage mechanism
type SubscriptionSecretSyncer interface {
	// Sync creates or updates the subscription storage for a user
	// It reads the current subscriptions from the storage and syncs them to the external storage
	Sync(userID string) error
}
