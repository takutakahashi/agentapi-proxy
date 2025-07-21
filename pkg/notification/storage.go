package notification

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

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

// JSONLStorage implements Storage using JSONL files
type JSONLStorage struct {
	baseDir string
	mu      sync.RWMutex
}

// NewJSONLStorage creates a new JSONL-based storage
func NewJSONLStorage(baseDir string) *JSONLStorage {
	return &JSONLStorage{
		baseDir: baseDir,
	}
}

// getNotificationsDir returns the notifications directory for a user
func (s *JSONLStorage) getNotificationsDir(userID string) string {
	return filepath.Join(s.baseDir, "myclaudes", userID, "notifications")
}

// ensureNotificationsDir ensures the notifications directory exists
func (s *JSONLStorage) ensureNotificationsDir(userID string) error {
	dir := s.getNotificationsDir(userID)
	return os.MkdirAll(dir, 0755)
}

// AddSubscription adds a new subscription for a user
func (s *JSONLStorage) AddSubscription(userID string, sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureNotificationsDir(userID); err != nil {
		return err
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

	filePath := filepath.Join(s.getNotificationsDir(userID), "subscriptions.jsonl")
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(file)
	return encoder.Encode(sub)
}

// GetSubscriptions returns all active subscriptions for a user
func (s *JSONLStorage) GetSubscriptions(userID string) ([]Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filePath := filepath.Join(s.getNotificationsDir(userID), "subscriptions.jsonl")

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
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var sub Subscription
		if err := json.Unmarshal(scanner.Bytes(), &sub); err != nil {
			continue // Skip corrupted entries
		}
		// Only return active subscriptions
		if sub.Active {
			subscriptions = append(subscriptions, sub)
		}
	}

	return subscriptions, scanner.Err()
}

// GetAllSubscriptions returns all active subscriptions from all users
func (s *JSONLStorage) GetAllSubscriptions() ([]Subscription, error) {
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
func (s *JSONLStorage) UpdateSubscription(userID string, subscriptionID string, updates Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read all subscriptions
	filePath := filepath.Join(s.getNotificationsDir(userID), "subscriptions.jsonl")

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}

	var subscriptions []Subscription
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var sub Subscription
		if err := json.Unmarshal(scanner.Bytes(), &sub); err != nil {
			continue
		}
		subscriptions = append(subscriptions, sub)
	}
	if err := file.Close(); err != nil {
		return err
	}

	if err := scanner.Err(); err != nil {
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

	// Write back all subscriptions
	tempFile := filePath + ".tmp"
	file, err = os.Create(tempFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(file)
	for _, sub := range subscriptions {
		if err := encoder.Encode(sub); err != nil {
			_ = os.Remove(tempFile)
			return err
		}
	}

	return os.Rename(tempFile, filePath)
}

// DeleteSubscription marks a subscription as inactive
func (s *JSONLStorage) DeleteSubscription(userID string, endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read all subscriptions
	filePath := filepath.Join(s.getNotificationsDir(userID), "subscriptions.jsonl")

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("subscription not found")
		}
		return err
	}

	var subscriptions []Subscription
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var sub Subscription
		if err := json.Unmarshal(scanner.Bytes(), &sub); err != nil {
			continue
		}
		subscriptions = append(subscriptions, sub)
	}
	if err := file.Close(); err != nil {
		return err
	}

	if err := scanner.Err(); err != nil {
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

	// Write back all subscriptions
	tempFile := filePath + ".tmp"
	file, err = os.Create(tempFile)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(file)
	for _, sub := range subscriptions {
		if err := encoder.Encode(sub); err != nil {
			_ = os.Remove(tempFile)
			return err
		}
	}

	return os.Rename(tempFile, filePath)
}

// AddNotificationHistory adds a notification to the history
func (s *JSONLStorage) AddNotificationHistory(userID string, notification NotificationHistory) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureNotificationsDir(userID); err != nil {
		return err
	}

	// Generate ID if not provided
	if notification.ID == "" {
		notification.ID = fmt.Sprintf("notif_%d_%s", time.Now().Unix(), uuid.New().String()[:8])
	}

	filePath := filepath.Join(s.getNotificationsDir(userID), "history.jsonl")
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(file)
	return encoder.Encode(notification)
}

// GetNotificationHistory retrieves notification history with pagination and filtering
func (s *JSONLStorage) GetNotificationHistory(userID string, limit, offset int, filters map[string]string) ([]NotificationHistory, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filePath := filepath.Join(s.getNotificationsDir(userID), "history.jsonl")

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []NotificationHistory{}, 0, nil
		}
		return nil, 0, err
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	var allNotifications []NotificationHistory
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		var notification NotificationHistory
		if err := json.Unmarshal(scanner.Bytes(), &notification); err != nil {
			continue // Skip corrupted entries
		}

		// Apply filters
		if sessionID := filters["session_id"]; sessionID != "" && notification.SessionID != sessionID {
			continue
		}
		if notificationType := filters["type"]; notificationType != "" && notification.Type != notificationType {
			continue
		}

		allNotifications = append(allNotifications, notification)
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	// Sort by newest first
	sort.Slice(allNotifications, func(i, j int) bool {
		return allNotifications[i].SentAt.After(allNotifications[j].SentAt)
	})

	total := len(allNotifications)

	// Apply pagination
	if offset >= total {
		return []NotificationHistory{}, total, nil
	}

	end := offset + limit
	if end > total {
		end = total
	}

	return allNotifications[offset:end], total, nil
}

// RotateNotificationHistory keeps only the most recent N entries
func (s *JSONLStorage) RotateNotificationHistory(userID string, maxEntries int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Get all notifications
	notifications, _, err := s.GetNotificationHistory(userID, maxEntries*2, 0, nil)
	if err != nil {
		return err
	}

	if len(notifications) <= maxEntries {
		return nil // No rotation needed
	}

	// Keep only the most recent maxEntries
	keepNotifications := notifications[:maxEntries]

	filePath := filepath.Join(s.getNotificationsDir(userID), "history.jsonl")
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
	for _, notification := range keepNotifications {
		if err := encoder.Encode(notification); err != nil {
			_ = os.Remove(tempFile)
			return err
		}
	}

	return os.Rename(tempFile, filePath)
}
