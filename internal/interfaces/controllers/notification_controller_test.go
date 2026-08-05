package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/notification"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
)

// MockNotificationPresenter implements NotificationPresenter for testing
type MockNotificationPresenter struct {
	lastError      string
	lastStatusCode int
	sendResponse   *notification.SendNotificationResponse
	createSubResp  *notification.CreateSubscriptionResponse
	deleteSubResp  *notification.DeleteSubscriptionResponse
}

func (m *MockNotificationPresenter) PresentSendNotification(w http.ResponseWriter, response *notification.SendNotificationResponse) {
	m.sendResponse = response
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *MockNotificationPresenter) PresentCreateSubscription(w http.ResponseWriter, response *notification.CreateSubscriptionResponse) {
	m.createSubResp = response
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *MockNotificationPresenter) PresentDeleteSubscription(w http.ResponseWriter, response *notification.DeleteSubscriptionResponse) {
	m.deleteSubResp = response
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (m *MockNotificationPresenter) PresentError(w http.ResponseWriter, message string, statusCode int) {
	m.lastError = message
	m.lastStatusCode = statusCode
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// MockNotificationRepository implements NotificationRepository for testing
type MockNotificationRepository struct {
	subscriptions map[entities.SubscriptionID]*entities.Subscription
	notifications map[entities.NotificationID]*entities.Notification
	saveSubErr    error
	findSubErr    error
	deleteSubErr  error
}

func NewMockNotificationRepository() *MockNotificationRepository {
	return &MockNotificationRepository{
		subscriptions: make(map[entities.SubscriptionID]*entities.Subscription),
		notifications: make(map[entities.NotificationID]*entities.Notification),
	}
}

func (m *MockNotificationRepository) FindSubscriptionsByUserID(ctx context.Context, userID entities.UserID) ([]*entities.Subscription, error) {
	if m.findSubErr != nil {
		return nil, m.findSubErr
	}
	var result []*entities.Subscription
	for _, sub := range m.subscriptions {
		if sub.UserID() == userID {
			result = append(result, sub)
		}
	}
	return result, nil
}

func (m *MockNotificationRepository) FindSubscriptionByID(ctx context.Context, id entities.SubscriptionID) (*entities.Subscription, error) {
	if m.findSubErr != nil {
		return nil, m.findSubErr
	}
	if sub, ok := m.subscriptions[id]; ok {
		return sub, nil
	}
	return nil, errors.New("subscription not found")
}

func (m *MockNotificationRepository) SaveSubscription(ctx context.Context, subscription *entities.Subscription) error {
	if m.saveSubErr != nil {
		return m.saveSubErr
	}
	m.subscriptions[subscription.ID()] = subscription
	return nil
}

func (m *MockNotificationRepository) DeleteSubscription(ctx context.Context, id entities.SubscriptionID) error {
	if m.deleteSubErr != nil {
		return m.deleteSubErr
	}
	delete(m.subscriptions, id)
	return nil
}

func (m *MockNotificationRepository) SaveNotification(ctx context.Context, notif *entities.Notification) error {
	m.notifications[notif.ID()] = notif
	return nil
}

func (m *MockNotificationRepository) UpdateNotification(ctx context.Context, notif *entities.Notification) error {
	m.notifications[notif.ID()] = notif
	return nil
}

func (m *MockNotificationRepository) FindNotificationsByUserID(ctx context.Context, userID entities.UserID) ([]*entities.Notification, error) {
	var result []*entities.Notification
	for _, notif := range m.notifications {
		if notif.UserID() == userID {
			result = append(result, notif)
		}
	}
	return result, nil
}

func (m *MockNotificationRepository) FindActiveSubscriptions(ctx context.Context) ([]*entities.Subscription, error) {
	var result []*entities.Subscription
	for _, sub := range m.subscriptions {
		if sub.IsActive() {
			result = append(result, sub)
		}
	}
	return result, nil
}

func (m *MockNotificationRepository) UpdateSubscription(ctx context.Context, subscription *entities.Subscription) error {
	m.subscriptions[subscription.ID()] = subscription
	return nil
}

func (m *MockNotificationRepository) FindNotificationByID(ctx context.Context, id entities.NotificationID) (*entities.Notification, error) {
	if notif, ok := m.notifications[id]; ok {
		return notif, nil
	}
	return nil, errors.New("notification not found")
}

func (m *MockNotificationRepository) FindNotificationsBySubscriptionID(ctx context.Context, subscriptionID entities.SubscriptionID) ([]*entities.Notification, error) {
	var result []*entities.Notification
	for _, notif := range m.notifications {
		if notif.SubscriptionID() == subscriptionID {
			result = append(result, notif)
		}
	}
	return result, nil
}

func (m *MockNotificationRepository) DeleteNotification(ctx context.Context, id entities.NotificationID) error {
	delete(m.notifications, id)
	return nil
}

func (m *MockNotificationRepository) FindSubscriptionsWithFilters(ctx context.Context, filters repositories.SubscriptionFilters) ([]*entities.Subscription, error) {
	var result []*entities.Subscription
	for _, sub := range m.subscriptions {
		result = append(result, sub)
	}
	return result, nil
}

func (m *MockNotificationRepository) FindNotificationsWithFilters(ctx context.Context, filters repositories.NotificationFilters) ([]*entities.Notification, error) {
	var result []*entities.Notification
	for _, notif := range m.notifications {
		result = append(result, notif)
	}
	return result, nil
}

// MockNotificationService implements NotificationService for testing
type MockNotificationService struct {
	sendBulkErr    error
	validateSubErr error
	testNotifErr   error
}

func (m *MockNotificationService) SendNotification(ctx context.Context, notif *entities.Notification, subscription *entities.Subscription) error {
	return nil
}

func (m *MockNotificationService) SendBulkNotifications(ctx context.Context, notif *entities.Notification, subscriptions []*entities.Subscription) ([]*services.NotificationResult, error) {
	if m.sendBulkErr != nil {
		return nil, m.sendBulkErr
	}
	var results []*services.NotificationResult
	for _, sub := range subscriptions {
		results = append(results, &services.NotificationResult{
			SubscriptionID: sub.ID(),
			Success:        true,
		})
	}
	return results, nil
}

func (m *MockNotificationService) ValidateSubscription(ctx context.Context, subscription *entities.Subscription) error {
	return m.validateSubErr
}

func (m *MockNotificationService) TestNotification(ctx context.Context, subscription *entities.Subscription) error {
	return m.testNotifErr
}

func (m *MockNotificationService) GetVAPIDPublicKey() string {
	return "test_vapid_key"
}

func TestNewNotificationController(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}
	presenter := &MockNotificationPresenter{}

	sendUC := notification.NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)
	manageUC := notification.NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	controller := NewNotificationController(sendUC, manageUC, presenter)

	assert.NotNil(t, controller)
}

func TestNotificationController_SendNotification_Unauthorized(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}
	presenter := &MockNotificationPresenter{}

	sendUC := notification.NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)
	manageUC := notification.NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	controller := NewNotificationController(sendUC, manageUC, presenter)

	reqBody := SendNotificationRequest{
		Title: "Test Title",
		Body:  "Test Body",
	}
	body, _ := json.Marshal(reqBody)

	// Request without userID in context
	req := httptest.NewRequest(http.MethodPost, "/notifications", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.SendNotification(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "unauthorized", presenter.lastError)
}

func TestNotificationController_SendNotification_InvalidRequestBody(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}
	presenter := &MockNotificationPresenter{}

	sendUC := notification.NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)
	manageUC := notification.NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	controller := NewNotificationController(sendUC, manageUC, presenter)

	// Request with userID but invalid body
	// Note: The controller uses string "userID" as context key, but using a typed key is better practice
	// Since we can't match the controller's key, we expect unauthorized
	req := httptest.NewRequest(http.MethodPost, "/notifications", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	//nolint:staticcheck // Using string key to match controller implementation
	ctx := context.WithValue(req.Context(), "userID", entities.UserID("user_123"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	controller.SendNotification(w, req)

	// The controller looks for entities.UserID type assertion from "userID" key
	// which succeeds with our context value
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid request body", presenter.lastError)
}

func TestNotificationController_CreateSubscription_Unauthorized(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}
	presenter := &MockNotificationPresenter{}

	sendUC := notification.NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)
	manageUC := notification.NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	controller := NewNotificationController(sendUC, manageUC, presenter)

	reqBody := CreateSubscriptionRequest{
		Type:     "webpush",
		Endpoint: "https://push.example.com",
	}
	body, _ := json.Marshal(reqBody)

	// Request without userID in context
	req := httptest.NewRequest(http.MethodPost, "/subscriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	controller.CreateSubscription(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "unauthorized", presenter.lastError)
}

func TestNotificationController_CreateSubscription_InvalidRequestBody(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}
	presenter := &MockNotificationPresenter{}

	sendUC := notification.NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)
	manageUC := notification.NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	controller := NewNotificationController(sendUC, manageUC, presenter)

	// Request with userID but invalid body
	req := httptest.NewRequest(http.MethodPost, "/subscriptions", bytes.NewReader([]byte("invalid")))
	req.Header.Set("Content-Type", "application/json")
	//nolint:staticcheck // Using string key to match controller implementation
	ctx := context.WithValue(req.Context(), "userID", entities.UserID("user_123"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	controller.CreateSubscription(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "invalid request body", presenter.lastError)
}

func TestNotificationController_DeleteSubscription_Unauthorized(t *testing.T) {
	notifRepo := NewMockNotificationRepository()
	userRepo := NewMockUserRepository()
	notifSvc := &MockNotificationService{}
	presenter := &MockNotificationPresenter{}

	sendUC := notification.NewSendNotificationUseCase(notifRepo, userRepo, notifSvc)
	manageUC := notification.NewManageSubscriptionUseCase(notifRepo, userRepo, notifSvc)

	controller := NewNotificationController(sendUC, manageUC, presenter)

	// Request without userID in context
	req := httptest.NewRequest(http.MethodDelete, "/subscriptions/sub_123", nil)
	w := httptest.NewRecorder()

	controller.DeleteSubscription(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "unauthorized", presenter.lastError)
}

func TestExtractSubscriptionID(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/subscriptions/sub_123", nil)
	result := extractSubscriptionID(req)

	// Note: The current implementation returns a placeholder value
	assert.Equal(t, "sub_123", result)
}
