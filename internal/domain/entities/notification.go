package entities

import (
	"errors"
	"fmt"
	"time"
)

// NotificationID represents a unique notification identifier
type NotificationID string

// SubscriptionID represents a unique subscription identifier
type SubscriptionID string

// SubscriptionType represents the type of subscription
type SubscriptionType string

const (
	SubscriptionTypeWebPush SubscriptionType = "webpush"
	SubscriptionTypeWebhook SubscriptionType = "webhook"
	SubscriptionTypeEmail   SubscriptionType = "email"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationTypeMessage NotificationType = "message"
	NotificationTypeStatus  NotificationType = "status"
	NotificationTypeSession NotificationType = "session"
	NotificationTypeManual  NotificationType = "manual"
)

// NotificationStatus represents the status of a notification
type NotificationStatus string

const (
	NotificationStatusPending   NotificationStatus = "pending"
	NotificationStatusDelivered NotificationStatus = "delivered"
	NotificationStatusFailed    NotificationStatus = "failed"
	NotificationStatusClicked   NotificationStatus = "clicked"
)

// DeviceInfo contains information about the device
type DeviceInfo struct {
	UserAgent string            `json:"user_agent"`
	Platform  string            `json:"platform"`
	Browser   string            `json:"browser"`
	Language  string            `json:"language"`
	Timezone  string            `json:"timezone"`
	Extra     map[string]string `json:"extra,omitempty"`
}

// Subscription represents a notification subscription
type Subscription struct {
	id                SubscriptionID
	userID            UserID
	userType          UserType
	subscriptionType  SubscriptionType
	username          string
	endpoint          string
	keys              map[string]string
	sessionIDs        []SessionID
	notificationTypes []NotificationType
	deviceInfo        *DeviceInfo
	createdAt         time.Time
	updatedAt         time.Time
	lastUsed          time.Time
	active            bool
}

// NewSubscription creates a new notification subscription
func NewSubscription(id SubscriptionID, userID UserID, userType UserType, subscriptionType SubscriptionType, username, endpoint string, keys map[string]string) *Subscription {
	now := time.Now()
	return &Subscription{
		id:                id,
		userID:            userID,
		userType:          userType,
		subscriptionType:  subscriptionType,
		username:          username,
		endpoint:          endpoint,
		keys:              keys,
		notificationTypes: []NotificationType{NotificationTypeMessage, NotificationTypeStatus},
		createdAt:         now,
		updatedAt:         now,
		lastUsed:          now,
		active:            true,
	}
}

// ID returns the subscription ID
func (s *Subscription) ID() SubscriptionID {
	return s.id
}

// UserID returns the user ID
func (s *Subscription) UserID() UserID {
	return s.userID
}

// UserType returns the user type
func (s *Subscription) UserType() UserType {
	return s.userType
}

// Type returns the subscription type
func (s *Subscription) Type() SubscriptionType {
	return s.subscriptionType
}

// Username returns the username
func (s *Subscription) Username() string {
	return s.username
}

// Endpoint returns the push endpoint
func (s *Subscription) Endpoint() string {
	return s.endpoint
}

// Keys returns a copy of the subscription keys
func (s *Subscription) Keys() map[string]string {
	keys := make(map[string]string)
	for k, v := range s.keys {
		keys[k] = v
	}
	return keys
}

// SessionIDs returns a copy of subscribed session IDs
func (s *Subscription) SessionIDs() []SessionID {
	ids := make([]SessionID, len(s.sessionIDs))
	copy(ids, s.sessionIDs)
	return ids
}

// NotificationTypes returns a copy of subscribed notification types
func (s *Subscription) NotificationTypes() []NotificationType {
	types := make([]NotificationType, len(s.notificationTypes))
	copy(types, s.notificationTypes)
	return types
}

// DeviceInfo returns the device information
func (s *Subscription) DeviceInfo() *DeviceInfo {
	return s.deviceInfo
}

// CreatedAt returns when the subscription was created
func (s *Subscription) CreatedAt() time.Time {
	return s.createdAt
}

// UpdatedAt returns when the subscription was last updated
func (s *Subscription) UpdatedAt() time.Time {
	return s.updatedAt
}

// LastUsed returns when the subscription was last used
func (s *Subscription) LastUsed() time.Time {
	return s.lastUsed
}

// IsActive returns true if the subscription is active
func (s *Subscription) IsActive() bool {
	return s.active
}

// SetSessionIDs sets the subscribed session IDs
func (s *Subscription) SetSessionIDs(sessionIDs []SessionID) {
	s.sessionIDs = make([]SessionID, len(sessionIDs))
	copy(s.sessionIDs, sessionIDs)
	s.updatedAt = time.Now()
}

// AddSessionID adds a session ID to the subscription
func (s *Subscription) AddSessionID(sessionID SessionID) {
	// Check if session ID already exists
	for _, id := range s.sessionIDs {
		if id == sessionID {
			return
		}
	}
	s.sessionIDs = append(s.sessionIDs, sessionID)
	s.updatedAt = time.Now()
}

// RemoveSessionID removes a session ID from the subscription
func (s *Subscription) RemoveSessionID(sessionID SessionID) {
	for i, id := range s.sessionIDs {
		if id == sessionID {
			s.sessionIDs = append(s.sessionIDs[:i], s.sessionIDs[i+1:]...)
			s.updatedAt = time.Now()
			return
		}
	}
}

// SetNotificationTypes sets the subscribed notification types
func (s *Subscription) SetNotificationTypes(types []NotificationType) {
	s.notificationTypes = make([]NotificationType, len(types))
	copy(s.notificationTypes, types)
	s.updatedAt = time.Now()
}

// AddNotificationType adds a notification type to the subscription
func (s *Subscription) AddNotificationType(notType NotificationType) {
	// Check if notification type already exists
	for _, t := range s.notificationTypes {
		if t == notType {
			return
		}
	}
	s.notificationTypes = append(s.notificationTypes, notType)
	s.updatedAt = time.Now()
}

// SetDeviceInfo sets the device information
func (s *Subscription) SetDeviceInfo(deviceInfo *DeviceInfo) {
	s.deviceInfo = deviceInfo
	s.updatedAt = time.Now()
}

// UpdateLastUsed updates the last used timestamp
func (s *Subscription) UpdateLastUsed() {
	s.lastUsed = time.Now()
	s.updatedAt = time.Now()
}

// Deactivate deactivates the subscription
func (s *Subscription) Deactivate() {
	s.active = false
	s.updatedAt = time.Now()
}

// Activate activates the subscription
func (s *Subscription) Activate() {
	s.active = true
	s.updatedAt = time.Now()
}

// IsSubscribedToSession checks if the subscription is subscribed to a specific session
func (s *Subscription) IsSubscribedToSession(sessionID SessionID) bool {
	// Empty session IDs means subscribed to all sessions
	if len(s.sessionIDs) == 0 {
		return true
	}

	for _, id := range s.sessionIDs {
		if id == sessionID {
			return true
		}
	}
	return false
}

// IsSubscribedToNotificationType checks if the subscription is subscribed to a specific notification type
func (s *Subscription) IsSubscribedToNotificationType(notType NotificationType) bool {
	for _, t := range s.notificationTypes {
		if t == notType {
			return true
		}
	}
	return false
}

// Validate ensures the subscription is in a valid state
func (s *Subscription) Validate() error {
	if s.id == "" {
		return errors.New("subscription ID cannot be empty")
	}

	if s.userID == "" {
		return errors.New("user ID cannot be empty")
	}

	if s.endpoint == "" {
		return errors.New("endpoint cannot be empty")
	}

	if s.keys == nil || len(s.keys) == 0 {
		return errors.New("keys cannot be empty")
	}

	// Validate required keys for web push
	if _, ok := s.keys["p256dh"]; !ok {
		return errors.New("p256dh key is required")
	}

	if _, ok := s.keys["auth"]; !ok {
		return errors.New("auth key is required")
	}

	if s.createdAt.IsZero() {
		return errors.New("created at time cannot be zero")
	}

	return nil
}

// Notification represents a notification entity
type Notification struct {
	id             NotificationID
	userID         UserID
	subscriptionID SubscriptionID
	sessionID      SessionID
	title          string
	body           string
	notType        NotificationType
	url            *string
	iconURL        *string
	tags           Tags
	data           map[string]interface{}
	status         NotificationStatus
	createdAt      time.Time
	sentAt         time.Time
	deliveredAt    *time.Time
	clickedAt      *time.Time
	errorMessage   *string
}

// NewNotification creates a new notification
func NewNotification(id NotificationID, userID UserID, subscriptionID SubscriptionID, title, body string, notType NotificationType) *Notification {
	now := time.Now()
	return &Notification{
		id:             id,
		userID:         userID,
		subscriptionID: subscriptionID,
		title:          title,
		body:           body,
		notType:        notType,
		tags:           make(Tags),
		status:         NotificationStatusPending,
		createdAt:      now,
		sentAt:         now,
		data:           make(map[string]interface{}),
	}
}

// ID returns the notification ID
func (n *Notification) ID() NotificationID {
	return n.id
}

// UserID returns the user ID
func (n *Notification) UserID() UserID {
	return n.userID
}

// SubscriptionID returns the subscription ID
func (n *Notification) SubscriptionID() SubscriptionID {
	return n.subscriptionID
}

// SessionID returns the session ID
func (n *Notification) SessionID() SessionID {
	return n.sessionID
}

// Title returns the notification title
func (n *Notification) Title() string {
	return n.title
}

// Body returns the notification body
func (n *Notification) Body() string {
	return n.body
}

// Type returns the notification type
func (n *Notification) Type() NotificationType {
	return n.notType
}

// Data returns a copy of the notification data
func (n *Notification) Data() map[string]interface{} {
	data := make(map[string]interface{})
	for k, v := range n.data {
		data[k] = v
	}
	return data
}

// Status returns the notification status
func (n *Notification) Status() NotificationStatus {
	return n.status
}

// CreatedAt returns when the notification was created
func (n *Notification) CreatedAt() time.Time {
	return n.createdAt
}

// SentAt returns when the notification was sent
func (n *Notification) SentAt() time.Time {
	return n.sentAt
}

// URL returns the notification URL
func (n *Notification) URL() *string {
	return n.url
}

// IconURL returns the notification icon URL
func (n *Notification) IconURL() *string {
	return n.iconURL
}

// Tags returns a copy of the notification tags
func (n *Notification) Tags() Tags {
	tags := make(Tags)
	for k, v := range n.tags {
		tags[k] = v
	}
	return tags
}

// DeliveredAt returns when the notification was delivered
func (n *Notification) DeliveredAt() *time.Time {
	return n.deliveredAt
}

// ClickedAt returns when the notification was clicked
func (n *Notification) ClickedAt() *time.Time {
	return n.clickedAt
}

// ErrorMessage returns the error message if delivery failed
func (n *Notification) ErrorMessage() *string {
	return n.errorMessage
}

// SetSessionID sets the session ID
func (n *Notification) SetSessionID(sessionID SessionID) {
	n.sessionID = sessionID
}

// SetURL sets the notification URL
func (n *Notification) SetURL(url string) {
	n.url = &url
}

// SetIconURL sets the notification icon URL
func (n *Notification) SetIconURL(iconURL string) {
	n.iconURL = &iconURL
}

// SetData sets the notification data
func (n *Notification) SetData(data map[string]interface{}) {
	n.data = make(map[string]interface{})
	for k, v := range data {
		n.data[k] = v
	}
}

// AddData adds a key-value pair to the notification data
func (n *Notification) AddData(key string, value interface{}) {
	if n.data == nil {
		n.data = make(map[string]interface{})
	}
	n.data[key] = value
}

// MarkDelivered marks the notification as delivered
func (n *Notification) MarkDelivered() {
	n.status = NotificationStatusDelivered
	now := time.Now()
	n.deliveredAt = &now
}

// MarkFailed marks the notification as failed
func (n *Notification) MarkFailed(errorMessage string) {
	n.status = NotificationStatusFailed
	n.errorMessage = &errorMessage
}

// MarkClicked marks the notification as clicked
func (n *Notification) MarkClicked() {
	n.status = NotificationStatusClicked
	now := time.Now()
	n.clickedAt = &now
}

// IsDelivered returns true if the notification was delivered
func (n *Notification) IsDelivered() bool {
	return n.status == NotificationStatusDelivered || n.status == NotificationStatusClicked
}

// HasFailed returns true if the notification delivery failed
func (n *Notification) HasFailed() bool {
	return n.status == NotificationStatusFailed
}

// Validate ensures the notification is in a valid state
func (n *Notification) Validate() error {
	if n.id == "" {
		return errors.New("notification ID cannot be empty")
	}

	if n.userID == "" {
		return errors.New("user ID cannot be empty")
	}

	if n.subscriptionID == "" {
		return errors.New("subscription ID cannot be empty")
	}

	if n.title == "" {
		return errors.New("title cannot be empty")
	}

	if n.body == "" {
		return errors.New("body cannot be empty")
	}

	// Validate notification type
	validTypes := []NotificationType{NotificationTypeMessage, NotificationTypeStatus, NotificationTypeSession, NotificationTypeManual}
	typeValid := false
	for _, validType := range validTypes {
		if n.notType == validType {
			typeValid = true
			break
		}
	}
	if !typeValid {
		return fmt.Errorf("invalid notification type: %s", n.notType)
	}

	if n.sentAt.IsZero() {
		return errors.New("sent at time cannot be zero")
	}

	return nil
}
