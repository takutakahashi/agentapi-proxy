package presenters

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/notification"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

func TestNewHTTPNotificationPresenter(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()
	assert.NotNil(t, presenter)
}

func TestHTTPNotificationPresenter_PresentSendNotification(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	notif := entities.NewNotification(
		"notif_123",
		"user_123",
		"sub_123",
		"Test Title",
		"Test Body",
		entities.NotificationTypeManual,
	)

	results := []*services.NotificationResult{
		{
			SubscriptionID: "sub_123",
			Success:        true,
		},
		{
			SubscriptionID: "sub_456",
			Success:        false,
			Error:          assert.AnError,
		},
	}

	response := &notification.SendNotificationResponse{
		Notification: notif,
		Results:      results,
		SentCount:    1,
		FailedCount:  1,
	}

	w := httptest.NewRecorder()
	presenter.PresentSendNotification(w, response)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result SendNotificationResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.NotNil(t, result.Notification)
	assert.Equal(t, "notif_123", result.Notification.ID)
	assert.Equal(t, "Test Title", result.Notification.Title)
	assert.Equal(t, 1, result.SentCount)
	assert.Equal(t, 1, result.FailedCount)
	assert.Len(t, result.Results, 2)
}

func TestHTTPNotificationPresenter_PresentCreateSubscription(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	subscription := entities.NewSubscription(
		"sub_123",
		"user_123",
		entities.UserTypeAPIKey,
		entities.SubscriptionTypeWebPush,
		"testuser",
		"https://push.example.com",
		map[string]string{"p256dh": "key1", "auth": "key2"},
	)

	response := &notification.CreateSubscriptionResponse{
		Subscription: subscription,
	}

	w := httptest.NewRecorder()
	presenter.PresentCreateSubscription(w, response)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result CreateSubscriptionResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.NotNil(t, result.Subscription)
	assert.Equal(t, "sub_123", result.Subscription.ID)
	assert.Equal(t, "user_123", result.Subscription.UserID)
	assert.Equal(t, "webpush", result.Subscription.Type)
	assert.Equal(t, "https://push.example.com", result.Subscription.Endpoint)
	assert.True(t, result.Subscription.Active)
}

func TestHTTPNotificationPresenter_PresentDeleteSubscription(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	response := &notification.DeleteSubscriptionResponse{
		Success: true,
	}

	w := httptest.NewRecorder()
	presenter.PresentDeleteSubscription(w, response)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result DeleteSubscriptionResponse
	err := json.Unmarshal(w.Body.Bytes(), &result)
	assert.NoError(t, err)
	assert.True(t, result.Success)
}

func TestHTTPNotificationPresenter_PresentError(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		statusCode int
	}{
		{
			name:       "Bad Request",
			message:    "invalid subscription",
			statusCode: http.StatusBadRequest,
		},
		{
			name:       "Not Found",
			message:    "subscription not found",
			statusCode: http.StatusNotFound,
		},
		{
			name:       "Internal Server Error",
			message:    "failed to send notification",
			statusCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			presenter := NewHTTPNotificationPresenter()

			w := httptest.NewRecorder()
			presenter.PresentError(w, tt.message, tt.statusCode)

			assert.Equal(t, tt.statusCode, w.Code)
			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

			var result entities.ErrorResponse
			err := json.Unmarshal(w.Body.Bytes(), &result)
			assert.NoError(t, err)
			assert.Equal(t, tt.message, result.Message)
			assert.Equal(t, http.StatusText(tt.statusCode), result.Error)
		})
	}
}

func TestHTTPNotificationPresenter_convertNotificationToResponse(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	notif := entities.NewNotification(
		"notif_123",
		"user_123",
		"sub_123",
		"Test Title",
		"Test Body",
		entities.NotificationTypeManual,
	)
	notif.SetURL("https://example.com")
	notif.SetIconURL("https://example.com/icon.png")

	result := presenter.convertNotificationToResponse(notif)

	assert.Equal(t, "notif_123", result.ID)
	assert.Equal(t, "user_123", result.UserID)
	assert.Equal(t, "Test Title", result.Title)
	assert.Equal(t, "Test Body", result.Body)
	assert.NotNil(t, result.URL)
	assert.Equal(t, "https://example.com", *result.URL)
	assert.NotNil(t, result.IconURL)
	assert.Equal(t, "https://example.com/icon.png", *result.IconURL)
	assert.Equal(t, "pending", result.Status)
}

func TestHTTPNotificationPresenter_convertNotificationToResponse_WithTags(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	notif := entities.NewNotification(
		"notif_123",
		"user_123",
		"sub_123",
		"Test Title",
		"Test Body",
		entities.NotificationTypeManual,
	)

	result := presenter.convertNotificationToResponse(notif)

	assert.Equal(t, "notif_123", result.ID)
	// Tags should be empty but not nil
	assert.NotNil(t, result.Tags)
}

func TestHTTPNotificationPresenter_convertSubscriptionToResponse(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	subscription := entities.NewSubscription(
		"sub_123",
		"user_123",
		entities.UserTypeAPIKey,
		entities.SubscriptionTypeWebPush,
		"testuser",
		"https://push.example.com",
		map[string]string{"p256dh": "key1", "auth": "key2"},
	)

	result := presenter.convertSubscriptionToResponse(subscription)

	assert.Equal(t, "sub_123", result.ID)
	assert.Equal(t, "user_123", result.UserID)
	assert.Equal(t, "webpush", result.Type)
	assert.Equal(t, "https://push.example.com", result.Endpoint)
	assert.True(t, result.Active)
	assert.NotEmpty(t, result.CreatedAt)
	assert.NotEmpty(t, result.UpdatedAt)
}

func TestHTTPNotificationPresenter_convertSubscriptionToResponse_Inactive(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	subscription := entities.NewSubscription(
		"sub_123",
		"user_123",
		entities.UserTypeAPIKey,
		entities.SubscriptionTypeWebPush,
		"testuser",
		"https://push.example.com",
		map[string]string{"p256dh": "key1", "auth": "key2"},
	)
	subscription.Deactivate()

	result := presenter.convertSubscriptionToResponse(subscription)

	assert.False(t, result.Active)
}

func TestHTTPNotificationPresenter_convertNotificationResultsToResponse(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	deliveredAt := "2024-01-01T12:00:00Z"
	results := []*services.NotificationResult{
		{
			SubscriptionID: "sub_123",
			Success:        true,
			DeliveredAt:    &deliveredAt,
		},
		{
			SubscriptionID: "sub_456",
			Success:        false,
			Error:          assert.AnError,
		},
	}

	converted := presenter.convertNotificationResultsToResponse(results)

	assert.Len(t, converted, 2)

	// First result - success
	assert.Equal(t, "sub_123", converted[0].SubscriptionID)
	assert.True(t, converted[0].Success)
	assert.NotNil(t, converted[0].DeliveredAt)
	assert.Equal(t, deliveredAt, *converted[0].DeliveredAt)
	assert.Nil(t, converted[0].Error)

	// Second result - failure
	assert.Equal(t, "sub_456", converted[1].SubscriptionID)
	assert.False(t, converted[1].Success)
	assert.NotNil(t, converted[1].Error)
	assert.Nil(t, converted[1].DeliveredAt)
}

func TestHTTPNotificationPresenter_convertNotificationResultsToResponse_Empty(t *testing.T) {
	presenter := NewHTTPNotificationPresenter()

	results := []*services.NotificationResult{}

	converted := presenter.convertNotificationResultsToResponse(results)

	assert.Len(t, converted, 0)
}
