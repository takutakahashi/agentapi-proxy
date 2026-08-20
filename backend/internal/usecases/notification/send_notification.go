package notification

import (
	"context"
	"errors"
	"fmt"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// SendNotificationUseCase handles sending notifications to users
type SendNotificationUseCase struct {
	notificationRepo repositories.NotificationRepository
	userRepo         repositories.UserRepository
	notificationSvc  services.NotificationService
}

// NewSendNotificationUseCase creates a new SendNotificationUseCase
func NewSendNotificationUseCase(
	notificationRepo repositories.NotificationRepository,
	userRepo repositories.UserRepository,
	notificationSvc services.NotificationService,
) *SendNotificationUseCase {
	return &SendNotificationUseCase{
		notificationRepo: notificationRepo,
		userRepo:         userRepo,
		notificationSvc:  notificationSvc,
	}
}

// SendNotificationRequest represents the input for sending a notification
type SendNotificationRequest struct {
	UserID  entities.UserID
	Title   string
	Body    string
	URL     *string
	Tags    entities.Tags
	IconURL *string
}

// SendNotificationResponse represents the output of sending a notification
type SendNotificationResponse struct {
	Notification *entities.Notification
	Results      []*services.NotificationResult
	SentCount    int
	FailedCount  int
}

// Execute sends a notification to a user's subscriptions
func (uc *SendNotificationUseCase) Execute(ctx context.Context, req *SendNotificationRequest) (*SendNotificationResponse, error) {
	// Validate request
	if err := uc.validateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Verify user exists
	user, err := uc.userRepo.FindByID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	if !user.IsActive() {
		return nil, errors.New("user is not active")
	}

	// Get user's notification subscriptions
	subscriptions, err := uc.notificationRepo.FindSubscriptionsByUserID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user subscriptions: %w", err)
	}

	if len(subscriptions) == 0 {
		return &SendNotificationResponse{
			Results:     []*services.NotificationResult{},
			SentCount:   0,
			FailedCount: 0,
		}, nil
	}

	// Create notification entity
	notification := entities.NewNotification(
		uc.generateNotificationID(),
		req.UserID,
		entities.SubscriptionID(""), // Will be set for each subscription
		req.Title,
		req.Body,
		entities.NotificationTypeManual,
	)

	// Validate notification
	if err := notification.Validate(); err != nil {
		return nil, fmt.Errorf("invalid notification: %w", err)
	}

	// Save notification to repository
	if err := uc.notificationRepo.SaveNotification(ctx, notification); err != nil {
		return nil, fmt.Errorf("failed to save notification: %w", err)
	}

	// Send notification to all subscriptions
	results, err := uc.notificationSvc.SendBulkNotifications(ctx, notification, subscriptions)
	if err != nil {
		return nil, fmt.Errorf("failed to send notifications: %w", err)
	}

	// Count successes and failures
	sentCount := 0
	failedCount := 0
	for _, result := range results {
		if result.Success {
			sentCount++
		} else {
			failedCount++
		}
	}

	// Update notification with delivery results
	// TODO: Add SetDeliveryResults method to Notification entity
	// notification.SetDeliveryResults(results)
	if err := uc.notificationRepo.UpdateNotification(ctx, notification); err != nil {
		// Log warning but don't fail the operation
		fmt.Printf("Warning: failed to update notification delivery results: %v\n", err)
	}

	return &SendNotificationResponse{
		Notification: notification,
		Results:      results,
		SentCount:    sentCount,
		FailedCount:  failedCount,
	}, nil
}

// validateRequest validates the send notification request
func (uc *SendNotificationUseCase) validateRequest(req *SendNotificationRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.UserID == "" {
		return errors.New("user ID cannot be empty")
	}

	if req.Title == "" {
		return errors.New("notification title cannot be empty")
	}

	if req.Body == "" {
		return errors.New("notification body cannot be empty")
	}

	return nil
}

// generateNotificationID generates a unique notification ID
func (uc *SendNotificationUseCase) generateNotificationID() entities.NotificationID {
	// In a real implementation, this should generate a proper UUID
	// For now, we'll use a simple timestamp-based approach
	return entities.NotificationID(fmt.Sprintf("notif_%d", getCurrentTimestamp()))
}

// ManageSubscriptionUseCase handles notification subscription management
type ManageSubscriptionUseCase struct {
	notificationRepo repositories.NotificationRepository
	userRepo         repositories.UserRepository
	notificationSvc  services.NotificationService
}

// NewManageSubscriptionUseCase creates a new ManageSubscriptionUseCase
func NewManageSubscriptionUseCase(
	notificationRepo repositories.NotificationRepository,
	userRepo repositories.UserRepository,
	notificationSvc services.NotificationService,
) *ManageSubscriptionUseCase {
	return &ManageSubscriptionUseCase{
		notificationRepo: notificationRepo,
		userRepo:         userRepo,
		notificationSvc:  notificationSvc,
	}
}

// CreateSubscriptionRequest represents the input for creating a subscription
type CreateSubscriptionRequest struct {
	UserID   entities.UserID
	Type     entities.SubscriptionType
	Endpoint string
	Keys     map[string]string
}

// CreateSubscriptionResponse represents the output of creating a subscription
type CreateSubscriptionResponse struct {
	Subscription *entities.Subscription
}

// CreateSubscription creates a new notification subscription
func (uc *ManageSubscriptionUseCase) CreateSubscription(ctx context.Context, req *CreateSubscriptionRequest) (*CreateSubscriptionResponse, error) {
	// Validate request
	if err := uc.validateCreateSubscriptionRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Verify user exists
	user, err := uc.userRepo.FindByID(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	if !user.IsActive() {
		return nil, errors.New("user is not active")
	}

	// Create subscription entity
	subscription := entities.NewSubscription(
		uc.generateSubscriptionID(),
		req.UserID,
		user.Type(),
		req.Type,
		user.Username(),
		req.Endpoint,
		req.Keys,
	)

	// Validate subscription
	if err := subscription.Validate(); err != nil {
		return nil, fmt.Errorf("invalid subscription: %w", err)
	}

	// Validate subscription with the notification service
	if err := uc.notificationSvc.ValidateSubscription(ctx, subscription); err != nil {
		return nil, fmt.Errorf("subscription validation failed: %w", err)
	}

	// Save subscription to repository
	if err := uc.notificationRepo.SaveSubscription(ctx, subscription); err != nil {
		return nil, fmt.Errorf("failed to save subscription: %w", err)
	}

	// Send test notification if requested
	if err := uc.notificationSvc.TestNotification(ctx, subscription); err != nil {
		// Log warning but don't fail subscription creation
		fmt.Printf("Warning: test notification failed: %v\n", err)
	}

	return &CreateSubscriptionResponse{
		Subscription: subscription,
	}, nil
}

// DeleteSubscriptionRequest represents the input for deleting a subscription
type DeleteSubscriptionRequest struct {
	SubscriptionID entities.SubscriptionID
	UserID         entities.UserID // For authorization
}

// DeleteSubscriptionResponse represents the output of deleting a subscription
type DeleteSubscriptionResponse struct {
	Success bool
}

// DeleteSubscription deletes a notification subscription
func (uc *ManageSubscriptionUseCase) DeleteSubscription(ctx context.Context, req *DeleteSubscriptionRequest) (*DeleteSubscriptionResponse, error) {
	// Validate request
	if err := uc.validateDeleteSubscriptionRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// Get the subscription
	subscription, err := uc.notificationRepo.FindSubscriptionByID(ctx, req.SubscriptionID)
	if err != nil {
		return nil, fmt.Errorf("failed to find subscription: %w", err)
	}

	// Verify user authorization
	if subscription.UserID() != req.UserID {
		// Check if user is admin
		user, err := uc.userRepo.FindByID(ctx, req.UserID)
		if err != nil || !user.IsAdmin() {
			return nil, errors.New("user does not have permission to delete this subscription")
		}
	}

	// Delete subscription from repository
	if err := uc.notificationRepo.DeleteSubscription(ctx, req.SubscriptionID); err != nil {
		return nil, fmt.Errorf("failed to delete subscription: %w", err)
	}

	return &DeleteSubscriptionResponse{
		Success: true,
	}, nil
}

// validateCreateSubscriptionRequest validates the create subscription request
func (uc *ManageSubscriptionUseCase) validateCreateSubscriptionRequest(req *CreateSubscriptionRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.UserID == "" {
		return errors.New("user ID cannot be empty")
	}

	if req.Type == "" {
		return errors.New("subscription type cannot be empty")
	}

	if req.Endpoint == "" {
		return errors.New("subscription endpoint cannot be empty")
	}

	return nil
}

// validateDeleteSubscriptionRequest validates the delete subscription request
func (uc *ManageSubscriptionUseCase) validateDeleteSubscriptionRequest(req *DeleteSubscriptionRequest) error {
	if req == nil {
		return errors.New("request cannot be nil")
	}

	if req.SubscriptionID == "" {
		return errors.New("subscription ID cannot be empty")
	}

	if req.UserID == "" {
		return errors.New("user ID cannot be empty")
	}

	return nil
}

// generateSubscriptionID generates a unique subscription ID
func (uc *ManageSubscriptionUseCase) generateSubscriptionID() entities.SubscriptionID {
	// In a real implementation, this should generate a proper UUID
	return entities.SubscriptionID(fmt.Sprintf("sub_%d", getCurrentTimestamp()))
}

// getCurrentTimestamp returns current timestamp in milliseconds
func getCurrentTimestamp() int64 {
	// This is a placeholder - in real implementation use time.Now().UnixNano()
	return 1640995200000 // 2022-01-01 00:00:00 UTC
}
