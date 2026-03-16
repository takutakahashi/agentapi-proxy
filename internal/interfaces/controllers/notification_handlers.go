package controllers

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

// NotificationHandlers handles notification-related HTTP requests
// This is a simpler handler that uses the notification.Service directly
type NotificationHandlers struct {
	service        *notification.Service
	sessionManager portrepos.SessionManager
}

// NewNotificationHandlers creates new notification handlers
func NewNotificationHandlers(service *notification.Service, sessionManager portrepos.SessionManager) *NotificationHandlers {
	return &NotificationHandlers{
		service:        service,
		sessionManager: sessionManager,
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

	// Extract device information from request
	deviceInfo := notification.ExtractDeviceInfo(c.Request())

	// Create subscription
	sub, err := h.service.Subscribe(user, req.Endpoint, req.Keys, deviceInfo)
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

	subscriptions, err := h.service.GetSubscriptions(string(user.ID()))
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

	err := h.service.DeleteSubscription(string(user.ID()), req.Endpoint)
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

	// Enrich webhook data with session information before async processing
	if h.sessionManager != nil {
		session := h.sessionManager.GetSession(webhook.SessionID)
		if session != nil {
			if webhook.Data == nil {
				webhook.Data = make(map[string]interface{})
			}
			// Add initial message (description) so Slack notifications show what task triggered this
			if desc := session.Description(); desc != "" {
				if _, exists := webhook.Data["initial_message"]; !exists {
					webhook.Data["initial_message"] = desc
				}
			}
			// Build full URL using NOTIFICATION_BASE_URL if not already set
			if _, exists := webhook.Data["url"]; !exists {
				if baseURL := os.Getenv("NOTIFICATION_BASE_URL"); baseURL != "" {
					webhook.Data["url"] = baseURL + "/sessions/" + session.ID()
				}
			}
		}
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

// SendNotification handles POST /notifications/send
//
// Routing logic:
//   - session_id provided: look up the session via SessionManager.
//   - team-scoped session → no notification is sent (return success).
//   - user-scoped session → resolve to the session owner's user_id and send.
//   - user_id provided: send directly to that user.
func (h *NotificationHandlers) SendNotification(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	var req notification.SendNotificationRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	if req.Title == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Title is required")
	}
	if req.Body == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Body is required")
	}
	if req.SessionID == "" && req.UserID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Either session_id or user_id is required")
	}

	// When session_id is provided, resolve the target user from the session.
	if req.SessionID != "" {
		if h.sessionManager == nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Session manager not available")
		}

		session := h.sessionManager.GetSession(req.SessionID)
		if session == nil {
			// Session not found – nothing to notify.
			return c.JSON(http.StatusOK, &notification.SendNotificationResponse{
				Success: true,
				Message: fmt.Sprintf("session %s not found, no notifications sent", req.SessionID),
			})
		}

		// Team-scoped sessions do not trigger push notifications.
		if session.Scope() == entities.ScopeTeam {
			return c.JSON(http.StatusOK, &notification.SendNotificationResponse{
				Success: true,
				Message: "team-scoped sessions do not trigger push notifications",
			})
		}

		// User-scoped session: resolve to the owner's user_id.
		req.UserID = session.UserID()

		// Auto-construct session URL from NOTIFICATION_BASE_URL if not provided.
		if req.URL == "" {
			if baseURL := os.Getenv("NOTIFICATION_BASE_URL"); baseURL != "" {
				req.URL = baseURL + "/sessions/" + session.ID()
			}
		}

		req.SessionID = ""
	}

	resp, err := h.service.SendNotification(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to send notification: %v", err))
	}

	return c.JSON(http.StatusOK, resp)
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
	history, err := h.service.GetNotificationHistory(string(user.ID()), limit, offset, filters)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get notification history")
	}

	return c.JSON(http.StatusOK, history)
}
