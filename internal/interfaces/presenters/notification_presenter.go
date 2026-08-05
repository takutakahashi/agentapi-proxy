package presenters

import (
	"encoding/json"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/notification"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"net/http"
	"time"
)

// NotificationPresenter defines the interface for presenting notification data
type NotificationPresenter interface {
	PresentSendNotification(w http.ResponseWriter, response *notification.SendNotificationResponse)
	PresentCreateSubscription(w http.ResponseWriter, response *notification.CreateSubscriptionResponse)
	PresentDeleteSubscription(w http.ResponseWriter, response *notification.DeleteSubscriptionResponse)
	PresentError(w http.ResponseWriter, message string, statusCode int)
}

// HTTPNotificationPresenter implements NotificationPresenter for HTTP responses
type HTTPNotificationPresenter struct{}

// NewHTTPNotificationPresenter creates a new HTTPNotificationPresenter
func NewHTTPNotificationPresenter() *HTTPNotificationPresenter {
	return &HTTPNotificationPresenter{}
}

// SendNotificationResponse represents the response for sending a notification
type SendNotificationResponse struct {
	Notification *NotificationResponse         `json:"notification"`
	Results      []*NotificationResultResponse `json:"results"`
	SentCount    int                           `json:"sent_count"`
	FailedCount  int                           `json:"failed_count"`
}

// CreateSubscriptionResponse represents the response for creating a subscription
type CreateSubscriptionResponse struct {
	Subscription *SubscriptionResponse `json:"subscription"`
}

// DeleteSubscriptionResponse represents the response for deleting a subscription
type DeleteSubscriptionResponse struct {
	Success bool `json:"success"`
}

// NotificationResponse represents a notification in HTTP responses
type NotificationResponse struct {
	ID        string            `json:"id"`
	UserID    string            `json:"user_id"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	URL       *string           `json:"url,omitempty"`
	Tags      map[string]string `json:"tags,omitempty"`
	IconURL   *string           `json:"icon_url,omitempty"`
	CreatedAt string            `json:"created_at"`
	Status    string            `json:"status"`
}

// SubscriptionResponse represents a subscription in HTTP responses
type SubscriptionResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Type      string `json:"type"`
	Endpoint  string `json:"endpoint"`
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// NotificationResultResponse represents a notification delivery result in HTTP responses
type NotificationResultResponse struct {
	SubscriptionID string  `json:"subscription_id"`
	Success        bool    `json:"success"`
	Error          *string `json:"error,omitempty"`
	DeliveredAt    *string `json:"delivered_at,omitempty"`
}

// PresentSendNotification presents a send notification response
func (p *HTTPNotificationPresenter) PresentSendNotification(w http.ResponseWriter, response *notification.SendNotificationResponse) {
	notificationResp := p.convertNotificationToResponse(response.Notification)
	results := p.convertNotificationResultsToResponse(response.Results)

	sendResp := &SendNotificationResponse{
		Notification: notificationResp,
		Results:      results,
		SentCount:    response.SentCount,
		FailedCount:  response.FailedCount,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(sendResp)
}

// PresentCreateSubscription presents a create subscription response
func (p *HTTPNotificationPresenter) PresentCreateSubscription(w http.ResponseWriter, response *notification.CreateSubscriptionResponse) {
	subscriptionResp := p.convertSubscriptionToResponse(response.Subscription)

	createResp := &CreateSubscriptionResponse{
		Subscription: subscriptionResp,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(createResp)
}

// PresentDeleteSubscription presents a delete subscription response
func (p *HTTPNotificationPresenter) PresentDeleteSubscription(w http.ResponseWriter, response *notification.DeleteSubscriptionResponse) {
	deleteResp := &DeleteSubscriptionResponse{
		Success: response.Success,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(deleteResp)
}

// PresentError presents an error response
func (p *HTTPNotificationPresenter) PresentError(w http.ResponseWriter, message string, statusCode int) {
	errorResp := &entities.ErrorResponse{
		Error:   http.StatusText(statusCode),
		Message: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(errorResp)
}

// convertNotificationToResponse converts a domain notification to HTTP response format
func (p *HTTPNotificationPresenter) convertNotificationToResponse(notification *entities.Notification) *NotificationResponse {
	resp := &NotificationResponse{
		ID:        string(notification.ID()),
		UserID:    string(notification.UserID()),
		Title:     notification.Title(),
		Body:      notification.Body(),
		CreatedAt: notification.CreatedAt().Format(time.RFC3339),
		Status:    string(notification.Status()),
	}

	if url := notification.URL(); url != nil {
		resp.URL = url
	}

	if tags := notification.Tags(); tags != nil {
		resp.Tags = map[string]string(tags)
	}

	if iconURL := notification.IconURL(); iconURL != nil {
		resp.IconURL = iconURL
	}

	return resp
}

// convertSubscriptionToResponse converts a domain subscription to HTTP response format
func (p *HTTPNotificationPresenter) convertSubscriptionToResponse(subscription *entities.Subscription) *SubscriptionResponse {
	resp := &SubscriptionResponse{
		ID:        string(subscription.ID()),
		UserID:    string(subscription.UserID()),
		Type:      string(subscription.Type()),
		Endpoint:  subscription.Endpoint(),
		Active:    subscription.IsActive(),
		CreatedAt: subscription.CreatedAt().Format(time.RFC3339),
		UpdatedAt: subscription.UpdatedAt().Format(time.RFC3339),
	}

	return resp
}

// convertNotificationResultsToResponse converts notification results to HTTP response format
func (p *HTTPNotificationPresenter) convertNotificationResultsToResponse(results []*services.NotificationResult) []*NotificationResultResponse {
	responses := make([]*NotificationResultResponse, len(results))

	for i, result := range results {
		resp := &NotificationResultResponse{
			SubscriptionID: string(result.SubscriptionID),
			Success:        result.Success,
		}

		if result.Error != nil {
			errorMsg := result.Error.Error()
			resp.Error = &errorMsg
		}

		if result.DeliveredAt != nil {
			resp.DeliveredAt = result.DeliveredAt
		}

		responses[i] = resp
	}

	return responses
}
