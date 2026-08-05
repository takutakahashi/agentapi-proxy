package controllers

import (
	"encoding/json"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/presenters"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/notification"
	"net/http"
)

// NotificationController handles HTTP requests for notification operations
type NotificationController struct {
	sendNotificationUC    *notification.SendNotificationUseCase
	manageSubscriptionUC  *notification.ManageSubscriptionUseCase
	notificationPresenter presenters.NotificationPresenter
}

// NewNotificationController creates a new NotificationController
func NewNotificationController(
	sendNotificationUC *notification.SendNotificationUseCase,
	manageSubscriptionUC *notification.ManageSubscriptionUseCase,
	notificationPresenter presenters.NotificationPresenter,
) *NotificationController {
	return &NotificationController{
		sendNotificationUC:    sendNotificationUC,
		manageSubscriptionUC:  manageSubscriptionUC,
		notificationPresenter: notificationPresenter,
	}
}

// SendNotificationRequest represents the HTTP request for sending a notification
type SendNotificationRequest struct {
	Title   string            `json:"title"`
	Body    string            `json:"body"`
	URL     *string           `json:"url,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
	IconURL *string           `json:"icon_url,omitempty"`
}

// CreateSubscriptionRequest represents the HTTP request for creating a subscription
type CreateSubscriptionRequest struct {
	Type     string            `json:"type"`
	Endpoint string            `json:"endpoint"`
	Keys     map[string]string `json:"keys,omitempty"`
}

// SendNotification handles POST /notifications
func (c *NotificationController) SendNotification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context
	userID, ok := ctx.Value("userID").(entities.UserID)
	if !ok {
		c.notificationPresenter.PresentError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req SendNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.notificationPresenter.PresentError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Convert to use case request
	ucReq := &notification.SendNotificationRequest{
		UserID:  userID,
		Title:   req.Title,
		Body:    req.Body,
		URL:     req.URL,
		Tags:    entities.Tags(req.Tags),
		IconURL: req.IconURL,
	}

	// Execute use case
	response, err := c.sendNotificationUC.Execute(ctx, ucReq)
	if err != nil {
		c.notificationPresenter.PresentError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Present response
	c.notificationPresenter.PresentSendNotification(w, response)
}

// CreateSubscription handles POST /subscriptions
func (c *NotificationController) CreateSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context
	userID, ok := ctx.Value("userID").(entities.UserID)
	if !ok {
		c.notificationPresenter.PresentError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req CreateSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.notificationPresenter.PresentError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Convert to use case request
	ucReq := &notification.CreateSubscriptionRequest{
		UserID:   userID,
		Type:     entities.SubscriptionType(req.Type),
		Endpoint: req.Endpoint,
		Keys:     req.Keys,
	}

	// Execute use case
	response, err := c.manageSubscriptionUC.CreateSubscription(ctx, ucReq)
	if err != nil {
		c.notificationPresenter.PresentError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Present response
	c.notificationPresenter.PresentCreateSubscription(w, response)
}

// DeleteSubscription handles DELETE /subscriptions/{id}
func (c *NotificationController) DeleteSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract user ID from context
	userID, ok := ctx.Value("userID").(entities.UserID)
	if !ok {
		c.notificationPresenter.PresentError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract subscription ID from URL path
	subscriptionID := extractSubscriptionID(r)
	if subscriptionID == "" {
		c.notificationPresenter.PresentError(w, "subscription ID is required", http.StatusBadRequest)
		return
	}

	// Execute use case
	ucReq := &notification.DeleteSubscriptionRequest{
		SubscriptionID: entities.SubscriptionID(subscriptionID),
		UserID:         userID,
	}

	response, err := c.manageSubscriptionUC.DeleteSubscription(ctx, ucReq)
	if err != nil {
		c.notificationPresenter.PresentError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Present response
	c.notificationPresenter.PresentDeleteSubscription(w, response)
}

// extractSubscriptionID extracts subscription ID from URL path
func extractSubscriptionID(r *http.Request) string {
	// Placeholder implementation - use proper router in real applications
	return "sub_123"
}
