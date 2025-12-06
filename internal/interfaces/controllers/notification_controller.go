package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/interfaces/presenters"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/notification"
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

func (c *NotificationController) RegisterRoutes(e *echo.Echo) {
	e.POST("/notification/subscribe", c.SubscribeNotifications)
	e.GET("/notification/subscribe", c.GetSubscriptions)
	e.DELETE("/notification/subscribe", c.DeleteSubscription)
	e.POST("/notifications/webhook", c.NotificationWebhook)
	e.GET("/notifications/history", c.GetNotificationHistory)
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
func (c *NotificationController) SendNotification(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract user ID from context
	userID, ok := reqCtx.Value("userID").(entities.UserID)
	if !ok {
		c.notificationPresenter.PresentError(ctx.Response(), "unauthorized", http.StatusUnauthorized)
		return nil
	}

	// Parse request body
	var req SendNotificationRequest
	if err := ctx.Bind(&req); err != nil {
		c.notificationPresenter.PresentError(ctx.Response(), "invalid request body", http.StatusBadRequest)
		return nil
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
	response, err := c.sendNotificationUC.Execute(reqCtx, ucReq)
	if err != nil {
		c.notificationPresenter.PresentError(ctx.Response(), err.Error(), http.StatusInternalServerError)
		return nil
	}

	// Present response
	c.notificationPresenter.PresentSendNotification(ctx.Response(), response)
	return nil
}

// CreateSubscription handles POST /subscriptions
func (c *NotificationController) CreateSubscription(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract user ID from context
	userID, ok := reqCtx.Value("userID").(entities.UserID)
	if !ok {
		c.notificationPresenter.PresentError(ctx.Response(), "unauthorized", http.StatusUnauthorized)
		return nil
	}

	// Parse request body
	var req CreateSubscriptionRequest
	if err := ctx.Bind(&req); err != nil {
		c.notificationPresenter.PresentError(ctx.Response(), "invalid request body", http.StatusBadRequest)
		return nil
	}

	// Convert to use case request
	ucReq := &notification.CreateSubscriptionRequest{
		UserID:   userID,
		Type:     entities.SubscriptionType(req.Type),
		Endpoint: req.Endpoint,
		Keys:     req.Keys,
	}

	// Execute use case
	response, err := c.manageSubscriptionUC.CreateSubscription(reqCtx, ucReq)
	if err != nil {
		c.notificationPresenter.PresentError(ctx.Response(), err.Error(), http.StatusBadRequest)
		return nil
	}

	// Present response
	c.notificationPresenter.PresentCreateSubscription(ctx.Response(), response)
	return nil
}

// DeleteSubscription handles DELETE /subscriptions/{id}
func (c *NotificationController) DeleteSubscription(ctx echo.Context) error {
	reqCtx := ctx.Request().Context()

	// Extract user ID from context
	userID, ok := reqCtx.Value("userID").(entities.UserID)
	if !ok {
		c.notificationPresenter.PresentError(ctx.Response(), "unauthorized", http.StatusUnauthorized)
		return nil
	}

	// Extract subscription ID from URL path or query parameter
	subscriptionID := ctx.Param("id")
	if subscriptionID == "" {
		// Try to get from query parameter for DELETE /notification/subscribe
		subscriptionID = ctx.QueryParam("subscription_id")
	}
	if subscriptionID == "" {
		c.notificationPresenter.PresentError(ctx.Response(), "subscription ID is required", http.StatusBadRequest)
		return nil
	}

	// Execute use case
	ucReq := &notification.DeleteSubscriptionRequest{
		SubscriptionID: entities.SubscriptionID(subscriptionID),
		UserID:         userID,
	}

	response, err := c.manageSubscriptionUC.DeleteSubscription(reqCtx, ucReq)
	if err != nil {
		c.notificationPresenter.PresentError(ctx.Response(), err.Error(), http.StatusInternalServerError)
		return nil
	}

	// Present response
	c.notificationPresenter.PresentDeleteSubscription(ctx.Response(), response)
	return nil
}

// SubscribeNotifications handles POST /notification/subscribe
func (c *NotificationController) SubscribeNotifications(ctx echo.Context) error {
	return c.CreateSubscription(ctx)
}

// GetSubscriptions handles GET /notification/subscribe
func (c *NotificationController) GetSubscriptions(ctx echo.Context) error {
	// In a real implementation, this would list user's subscriptions
	return ctx.JSON(http.StatusOK, []interface{}{})
}

// NotificationWebhook handles POST /notifications/webhook
func (c *NotificationController) NotificationWebhook(ctx echo.Context) error {
	// In a real implementation, this would process webhook notifications
	return ctx.JSON(http.StatusOK, map[string]string{"message": "Webhook processed successfully"})
}

// GetNotificationHistory handles GET /notifications/history
func (c *NotificationController) GetNotificationHistory(ctx echo.Context) error {
	// In a real implementation, this would return notification history
	return ctx.JSON(http.StatusOK, []interface{}{})
}
