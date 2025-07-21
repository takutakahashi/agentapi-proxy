package proxy

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

// NotificationHandlers handles notification-related HTTP requests
type NotificationHandlers struct {
	service *notification.Service
}

// NewNotificationHandlers creates new notification handlers
func NewNotificationHandlers(service *notification.Service) *NotificationHandlers {
	return &NotificationHandlers{
		service: service,
	}
}

// Subscribe handles POST /notification/subscribe
func (h *NotificationHandlers) Subscribe(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req notification.SubscribeRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Validate request
	if req.Endpoint == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Endpoint is required")
	}
	if req.Keys == nil || req.Keys["p256dh"] == "" || req.Keys["auth"] == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Keys with p256dh and auth are required")
	}

	// Create subscription
	sub, err := h.service.Subscribe(user, req.Endpoint, req.Keys)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create subscription")
	}

	return c.JSON(http.StatusOK, notification.SubscribeResponse{
		Success:        true,
		SubscriptionID: sub.ID,
	})
}

// GetSubscriptions handles GET /notification/subscribe
func (h *NotificationHandlers) GetSubscriptions(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	subscriptions, err := h.service.GetSubscriptions(user.UserID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get subscriptions")
	}

	return c.JSON(http.StatusOK, subscriptions)
}

// DeleteSubscription handles DELETE /notification/subscribe
func (h *NotificationHandlers) DeleteSubscription(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req notification.DeleteSubscriptionRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if req.Endpoint == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Endpoint is required")
	}

	err := h.service.DeleteSubscription(user.UserID, req.Endpoint)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Subscription not found")
	}

	return c.JSON(http.StatusOK, map[string]bool{
		"success": true,
	})
}

// Webhook handles POST /notifications/webhook
func (h *NotificationHandlers) Webhook(c echo.Context) error {
	// This endpoint should be protected by internal-only access
	// or a shared secret in production

	var webhook notification.WebhookRequest
	if err := c.Bind(&webhook); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid webhook payload")
	}

	// Validate webhook
	if webhook.SessionID == "" || webhook.EventType == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "SessionID and EventType are required")
	}

	// Process webhook asynchronously to avoid blocking
	go func() {
		if err := h.service.ProcessWebhook(webhook); err != nil {
			// Log error but don't fail the webhook
			c.Logger().Errorf("Failed to process webhook: %v", err)
		}
	}()

	return c.JSON(http.StatusOK, map[string]bool{
		"success": true,
	})
}

// GetHistory handles GET /notifications/history
func (h *NotificationHandlers) GetHistory(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	// Parse query parameters
	limit := 50
	offset := 0

	if limitStr := c.QueryParam("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	if offsetStr := c.QueryParam("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Build filters
	filters := make(map[string]string)
	if sessionID := c.QueryParam("session_id"); sessionID != "" {
		filters["session_id"] = sessionID
	}
	if notificationType := c.QueryParam("type"); notificationType != "" {
		filters["type"] = notificationType
	}

	// Get history
	history, err := h.service.GetNotificationHistory(user.UserID, limit, offset, filters)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get notification history")
	}

	return c.JSON(http.StatusOK, history)
}
