package notification

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SherClockHolmes/webpush-go"
)

// CLIUtils provides utilities for command-line notification operations
type CLIUtils struct{}

// NewCLIUtils creates a new CLI utilities instance
func NewCLIUtils() *CLIUtils {
	return &CLIUtils{}
}

// GetMatchingSubscriptions retrieves subscriptions matching filter criteria
func (u *CLIUtils) GetMatchingSubscriptions(userID, userType, username, sessionID string, verbose bool) ([]Subscription, error) {
	var allSubscriptions []Subscription

	// Get base directory for user data
	baseDir := os.Getenv("USERHOME_BASEDIR")
	if baseDir == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		baseDir = filepath.Join(homeDir, ".agentapi-proxy")
	}
	baseDir = filepath.Join(baseDir, "myclaudes")

	// Check if base directory exists
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return allSubscriptions, nil
	}

	// Read all user directories
	userDirs, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read user directories: %w", err)
	}

	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		userDirID := userDir.Name()
		subscriptions, err := u.getSubscriptionsForUser(userDirID)
		if err != nil {
			if verbose {
				fmt.Printf("Warning: failed to read subscriptions for user %s: %v\n", userDirID, err)
			}
			continue
		}

		// Filter subscriptions based on criteria
		for _, sub := range subscriptions {
			if u.matchesFilter(sub, userID, userType, username, sessionID) {
				allSubscriptions = append(allSubscriptions, sub)
			}
		}
	}

	return allSubscriptions, nil
}

// getSubscriptionsForUser retrieves subscriptions for a specific user
func (u *CLIUtils) getSubscriptionsForUser(userID string) ([]Subscription, error) {
	// Get base directory for user data
	baseDir := os.Getenv("USERHOME_BASEDIR")
	if baseDir == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		baseDir = filepath.Join(homeDir, ".agentapi-proxy")
	}

	subscriptionsFile := filepath.Join(baseDir, "myclaudes", userID, "notifications", "subscriptions.json")

	if _, err := os.Stat(subscriptionsFile); os.IsNotExist(err) {
		return []Subscription{}, nil
	}

	file, err := os.Open(subscriptionsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open subscriptions file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file: %v\n", err)
		}
	}()

	var allSubscriptions []Subscription
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&allSubscriptions); err != nil {
		// If decode fails, return empty slice
		return []Subscription{}, nil
	}

	// Filter active subscriptions
	var subscriptions []Subscription
	for _, sub := range allSubscriptions {
		if sub.Active {
			subscriptions = append(subscriptions, sub)
		}
	}

	return subscriptions, nil
}

// matchesFilter checks if a subscription matches the filter criteria
func (u *CLIUtils) matchesFilter(sub Subscription, userID, userType, username, sessionID string) bool {
	// If user-id is specified, it must match
	if userID != "" && sub.UserID != userID {
		return false
	}

	// If user-type is specified, it must match
	if userType != "" && sub.UserType != userType {
		return false
	}

	// If username is specified, it must match
	if username != "" && sub.Username != username {
		return false
	}

	// If session-id is specified, user must be subscribed to that session
	if sessionID != "" {
		// Empty session_ids means subscribed to all sessions
		if len(sub.SessionIDs) == 0 {
			return true
		}
		// Check if the specified session is in the user's session list
		for _, sessID := range sub.SessionIDs {
			if sessID == sessionID {
				return true
			}
		}
		return false
	}

	return true
}

// SendNotifications sends notifications to multiple subscriptions
func (u *CLIUtils) SendNotifications(subscriptions []Subscription, title, body, url, icon, badge string, ttl int, urgency string, vapidPublicKey, vapidPrivateKey, vapidContactEmail string) ([]NotificationResult, error) {
	var results []NotificationResult

	for _, sub := range subscriptions {
		result := NotificationResult{Subscription: sub}

		// Create notification payload
		payload := map[string]interface{}{
			"title": title,
			"body":  body,
			"icon":  icon,
			"data": map[string]interface{}{
				"url": url,
			},
		}

		if badge != "" {
			payload["badge"] = badge
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			result.Error = fmt.Errorf("failed to marshal payload: %w", err)
			results = append(results, result)
			continue
		}

		// Create webpush subscription
		webpushSub := &webpush.Subscription{
			Endpoint: sub.Endpoint,
			Keys: webpush.Keys{
				P256dh: sub.Keys["p256dh"],
				Auth:   sub.Keys["auth"],
			},
		}

		// Create webpush options
		options := &webpush.Options{
			Subscriber:      vapidContactEmail,
			VAPIDPublicKey:  vapidPublicKey,
			VAPIDPrivateKey: vapidPrivateKey,
			TTL:             ttl,
		}

		switch urgency {
		case "low":
			options.Urgency = webpush.UrgencyLow
		case "high":
			options.Urgency = webpush.UrgencyHigh
		default:
			options.Urgency = webpush.UrgencyNormal
		}

		// Send notification
		resp, err := webpush.SendNotification(payloadBytes, webpushSub, options)
		if err != nil {
			result.Error = fmt.Errorf("failed to send notification: %w", err)
		} else if resp.StatusCode >= 400 {
			result.Error = fmt.Errorf("notification rejected with status %d", resp.StatusCode)
		}

		if resp != nil {
			if err := resp.Body.Close(); err != nil {
				fmt.Printf("Warning: failed to close response body: %v\n", err)
			}
		}

		results = append(results, result)

		// Save to history
		if err := u.saveNotificationHistory(sub, title, body, url, icon, badge, ttl, urgency, result.Error == nil, result.Error); err != nil {
			fmt.Printf("Warning: failed to save notification history: %v\n", err)
		}
	}

	return results, nil
}

// saveNotificationHistory saves notification history to file
func (u *CLIUtils) saveNotificationHistory(sub Subscription, title, body, url, icon, badge string, ttl int, urgency string, delivered bool, sendError error) error {
	// Get base directory for user data
	baseDir := os.Getenv("USERHOME_BASEDIR")
	if baseDir == "" {
		homeDir := os.Getenv("HOME")
		if homeDir == "" {
			homeDir = "/home/agentapi"
		}
		baseDir = filepath.Join(homeDir, ".agentapi-proxy")
	}

	historyFile := filepath.Join(baseDir, "myclaudes", sub.UserID, "notifications", "history.jsonl")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(historyFile), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	history := NotificationHistory{
		ID:             u.generateNotificationID(),
		UserID:         sub.UserID,
		SubscriptionID: sub.ID,
		Title:          title,
		Body:           body,
		Type:           "manual", // Sent via command line
		Data: map[string]interface{}{
			"url":     url,
			"icon":    icon,
			"badge":   badge,
			"ttl":     ttl,
			"urgency": urgency,
		},
		SentAt:    time.Now(),
		Delivered: delivered,
		Clicked:   false, // Will be updated when clicked
	}

	if sendError != nil {
		errorMsg := sendError.Error()
		history.ErrorMessage = &errorMsg
	}

	// Append to history file
	file, err := os.OpenFile(historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open history file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close history file: %v\n", err)
		}
	}()

	encoder := json.NewEncoder(file)
	return encoder.Encode(history)
}

// generateNotificationID generates a unique notification ID
func (u *CLIUtils) generateNotificationID() string {
	// Generate 4 random bytes for uniqueness
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to current time nanoseconds if random fails
		return fmt.Sprintf("notif_%d_%d", time.Now().Unix(), time.Now().Nanosecond())
	}

	// Convert to hex and combine with timestamp
	randomHex := fmt.Sprintf("%x", randomBytes)
	return fmt.Sprintf("notif_%d_%s", time.Now().Unix(), randomHex)
}

// NotificationResult represents the result of sending a notification
type NotificationResult struct {
	Subscription Subscription
	Error        error
}
