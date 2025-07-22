package notification

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// JSONStorage implements Storage using JSON files (not JSONL)
type JSONStorage struct {
	baseDir string
	mu      sync.RWMutex
}

// NewJSONStorage creates a new JSON-based storage
func NewJSONStorage(baseDir string) *JSONStorage {
	return &JSONStorage{
		baseDir: baseDir,
	}
}

// getNotificationsDir returns the notifications directory for a user
func (s *JSONStorage) getNotificationsDir(userID string) string {
	return filepath.Join(s.baseDir, "myclaudes", userID, "notifications")
}

// ensureNotificationsDir ensures the notifications directory exists
func (s *JSONStorage) ensureNotificationsDir(userID string) error {
	dir := s.getNotificationsDir(userID)
	return os.MkdirAll(dir, 0755)
}

// loadSubscriptions loads all subscriptions from JSON file
func (s *JSONStorage) loadSubscriptions(userID string) ([]Subscription, error) {
	filePath := filepath.Join(s.getNotificationsDir(userID), "subscriptions.json")

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Subscription{}, nil
		}
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	var subscriptions []Subscription
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&subscriptions); err != nil {
		// If decode fails, return empty slice
		return []Subscription{}, nil
	}

	return subscriptions, nil
}

// saveSubscriptions saves all subscriptions to JSON file
func (s *JSONStorage) saveSubscriptions(userID string, subscriptions []Subscription) error {
	filePath := filepath.Join(s.getNotificationsDir(userID), "subscriptions.json")
	tempFile := filePath + ".tmp"

	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(subscriptions); err != nil {
		_ = os.Remove(tempFile)
		return err
	}

	return os.Rename(tempFile, filePath)
}

// AddSubscription adds a new subscription for a user with duplicate prevention
func (s *JSONStorage) AddSubscription(userID string, sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureNotificationsDir(userID); err != nil {
		return err
	}

	subscriptions, err := s.loadSubscriptions(userID)
	if err != nil {
		return err
	}

	// Check for duplicates by endpoint
	for i, existing := range subscriptions {
		if existing.Endpoint == sub.Endpoint && existing.Active {
			// Update existing subscription instead of creating duplicate
			sub.ID = existing.ID
			sub.CreatedAt = existing.CreatedAt
			sub.Active = true
			sub.UserID = userID
			subscriptions[i] = sub
			return s.saveSubscriptions(userID, subscriptions)
		}
	}

	// Generate ID if not provided
	if sub.ID == "" {
		sub.ID = fmt.Sprintf("sub_%s", uuid.New().String())
	}

	// Set defaults
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
	}
	sub.Active = true
	sub.UserID = userID

	// Add new subscription
	subscriptions = append(subscriptions, sub)
	return s.saveSubscriptions(userID, subscriptions)
}

// GetSubscriptions returns all active subscriptions for a user with deduplication
func (s *JSONStorage) GetSubscriptions(userID string) ([]Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	subscriptions, err := s.loadSubscriptions(userID)
	if err != nil {
		return nil, err
	}

	// Filter and deduplicate by endpoint
	var activeSubscriptions []Subscription
	seenEndpoints := make(map[string]bool)

	for _, sub := range subscriptions {
		if sub.Active && !seenEndpoints[sub.Endpoint] {
			activeSubscriptions = append(activeSubscriptions, sub)
			seenEndpoints[sub.Endpoint] = true
		}
	}

	return activeSubscriptions, nil
}

// GetAllSubscriptions returns all active subscriptions from all users
func (s *JSONStorage) GetAllSubscriptions() ([]Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var allSubscriptions []Subscription

	baseDir := filepath.Join(s.baseDir, "myclaudes")
	userDirs, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return allSubscriptions, nil
		}
		return nil, err
	}

	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		userID := userDir.Name()
		subscriptions, err := s.GetSubscriptions(userID)
		if err != nil {
			continue // Skip users with errors
		}

		allSubscriptions = append(allSubscriptions, subscriptions...)
	}

	return allSubscriptions, nil
}

// UpdateSubscription updates an existing subscription
func (s *JSONStorage) UpdateSubscription(userID string, subscriptionID string, updates Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriptions, err := s.loadSubscriptions(userID)
	if err != nil {
		return err
	}

	// Update the subscription
	found := false
	for i, sub := range subscriptions {
		if sub.ID == subscriptionID {
			found = true
			updates.ID = subscriptionID
			updates.UserID = userID
			subscriptions[i] = updates
			break
		}
	}

	if !found {
		return fmt.Errorf("subscription not found")
	}

	return s.saveSubscriptions(userID, subscriptions)
}

// DeleteSubscription marks a subscription as inactive
func (s *JSONStorage) DeleteSubscription(userID string, endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	subscriptions, err := s.loadSubscriptions(userID)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("subscription not found")
		}
		return err
	}

	// Mark subscription as inactive
	found := false
	for i, sub := range subscriptions {
		if sub.Endpoint == endpoint && sub.Active {
			found = true
			subscriptions[i].Active = false
			break
		}
	}

	if !found {
		return fmt.Errorf("subscription not found")
	}

	return s.saveSubscriptions(userID, subscriptions)
}

// loadNotificationHistory loads all notification history from JSON file
func (s *JSONStorage) loadNotificationHistory(userID string) ([]NotificationHistory, error) {
	filePath := filepath.Join(s.getNotificationsDir(userID), "history.json")

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []NotificationHistory{}, nil
		}
		return nil, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	var history []NotificationHistory
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&history); err != nil {
		// If decode fails, return empty slice
		return []NotificationHistory{}, nil
	}

	return history, nil
}

// saveNotificationHistory saves all notification history to JSON file
func (s *JSONStorage) saveNotificationHistory(userID string, history []NotificationHistory) error {
	filePath := filepath.Join(s.getNotificationsDir(userID), "history.json")
	tempFile := filePath + ".tmp"

	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(history); err != nil {
		_ = os.Remove(tempFile)
		return err
	}

	return os.Rename(tempFile, filePath)
}

// AddNotificationHistory adds a notification to the history
func (s *JSONStorage) AddNotificationHistory(userID string, notification NotificationHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureNotificationsDir(userID); err != nil {
		return err
	}

	// Generate ID if not provided
	if notification.ID == "" {
		notification.ID = fmt.Sprintf("notif_%d_%s", time.Now().Unix(), uuid.New().String()[:8])
	}

	// Load existing history
	history, err := s.loadNotificationHistory(userID)
	if err != nil {
		return err
	}

	// Add new notification
	history = append(history, notification)

	return s.saveNotificationHistory(userID, history)
}

// GetNotificationHistory retrieves notification history with pagination and filtering
func (s *JSONStorage) GetNotificationHistory(userID string, limit, offset int, filters map[string]string) ([]NotificationHistory, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	allNotifications, err := s.loadNotificationHistory(userID)
	if err != nil {
		return nil, 0, err
	}

	// Apply filters
	var filteredNotifications []NotificationHistory
	for _, notification := range allNotifications {
		// Apply filters
		if sessionID := filters["session_id"]; sessionID != "" && notification.SessionID != sessionID {
			continue
		}
		if notificationType := filters["type"]; notificationType != "" && notification.Type != notificationType {
			continue
		}

		filteredNotifications = append(filteredNotifications, notification)
	}

	// Sort by newest first
	sort.Slice(filteredNotifications, func(i, j int) bool {
		return filteredNotifications[i].SentAt.After(filteredNotifications[j].SentAt)
	})

	total := len(filteredNotifications)

	// Apply pagination
	if offset >= total {
		return []NotificationHistory{}, total, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return filteredNotifications[offset:end], total, nil
}

// RotateNotificationHistory keeps only the most recent N entries
func (s *JSONStorage) RotateNotificationHistory(userID string, maxEntries int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	allNotifications, err := s.loadNotificationHistory(userID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No file means no rotation needed
		}
		return err
	}

	// Sort by newest first
	sort.Slice(allNotifications, func(i, j int) bool {
		return allNotifications[i].SentAt.After(allNotifications[j].SentAt)
	})

	if len(allNotifications) <= maxEntries {
		return nil // No rotation needed
	}

	// Keep only the most recent maxEntries
	keepNotifications := allNotifications[:maxEntries]

	return s.saveNotificationHistory(userID, keepNotifications)
}
