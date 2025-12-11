package proxy

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

// mockStorage implements notification.Storage for testing
type mockStorage struct {
	subscriptions map[string][]notification.Subscription
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		subscriptions: make(map[string][]notification.Subscription),
	}
}

func (m *mockStorage) AddSubscription(userID string, sub notification.Subscription) error {
	m.subscriptions[userID] = append(m.subscriptions[userID], sub)
	return nil
}

func (m *mockStorage) GetSubscriptions(userID string) ([]notification.Subscription, error) {
	subs, ok := m.subscriptions[userID]
	if !ok {
		return []notification.Subscription{}, nil
	}
	return subs, nil
}

func (m *mockStorage) GetAllSubscriptions() ([]notification.Subscription, error) {
	var all []notification.Subscription
	for _, subs := range m.subscriptions {
		all = append(all, subs...)
	}
	return all, nil
}

func (m *mockStorage) DeleteSubscription(userID string, endpoint string) error {
	subs := m.subscriptions[userID]
	for i, sub := range subs {
		if sub.Endpoint == endpoint {
			m.subscriptions[userID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	return nil
}

func (m *mockStorage) UpdateSubscription(userID string, subscriptionID string, updates notification.Subscription) error {
	return nil
}

func (m *mockStorage) RotateNotificationHistory(userID string, maxEntries int) error {
	return nil
}

func (m *mockStorage) AddNotificationHistory(userID string, history notification.NotificationHistory) error {
	return nil
}

func (m *mockStorage) GetNotificationHistory(userID string, limit, offset int, filters map[string]string) ([]notification.NotificationHistory, int, error) {
	return nil, 0, nil
}

func TestKubernetesSubscriptionSecretSyncer_Sync_CreateNew(t *testing.T) {
	// Setup fake client
	clientset := fake.NewSimpleClientset()
	namespace := "test-namespace"
	storage := newMockStorage()

	// Add test subscription
	userID := "test-user"
	sub := notification.Subscription{
		ID:       "sub-1",
		UserID:   userID,
		Endpoint: "https://example.com/push",
		Keys: map[string]string{
			"p256dh": "test-key",
			"auth":   "test-auth",
		},
		Active: true,
	}
	if err := storage.AddSubscription(userID, sub); err != nil {
		t.Fatalf("Failed to add subscription: %v", err)
	}

	// Create syncer
	syncer := NewKubernetesSubscriptionSecretSyncer(clientset, namespace, storage, "")

	// Create namespace first
	ctx := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}
	_, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}

	// Sync
	err = syncer.Sync(userID)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify secret was created
	secretName := syncer.GetSecretName(userID)
	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Verify secret content
	data, ok := secret.Data["subscriptions.json"]
	if !ok {
		t.Fatal("subscriptions.json not found in secret")
	}

	var subs []notification.Subscription
	if err := json.Unmarshal(data, &subs); err != nil {
		t.Fatalf("Failed to unmarshal subscriptions: %v", err)
	}

	if len(subs) != 1 {
		t.Fatalf("Expected 1 subscription, got %d", len(subs))
	}

	if subs[0].Endpoint != sub.Endpoint {
		t.Errorf("Expected endpoint %s, got %s", sub.Endpoint, subs[0].Endpoint)
	}

	// Verify labels
	if secret.Labels["agentapi.proxy/user-id"] != userID {
		t.Errorf("Expected user-id label %s, got %s", userID, secret.Labels["agentapi.proxy/user-id"])
	}
}

func TestKubernetesSubscriptionSecretSyncer_Sync_Update(t *testing.T) {
	// Setup fake client
	clientset := fake.NewSimpleClientset()
	namespace := "test-namespace"
	storage := newMockStorage()

	userID := "test-user"

	// Create syncer
	syncer := NewKubernetesSubscriptionSecretSyncer(clientset, namespace, storage, "")

	// Create namespace
	ctx := context.Background()
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}
	_, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create namespace: %v", err)
	}

	// Add first subscription and sync
	sub1 := notification.Subscription{
		ID:       "sub-1",
		UserID:   userID,
		Endpoint: "https://example.com/push1",
		Active:   true,
	}
	if err := storage.AddSubscription(userID, sub1); err != nil {
		t.Fatalf("Failed to add subscription: %v", err)
	}
	err = syncer.Sync(userID)
	if err != nil {
		t.Fatalf("First sync failed: %v", err)
	}

	// Add second subscription and sync again
	sub2 := notification.Subscription{
		ID:       "sub-2",
		UserID:   userID,
		Endpoint: "https://example.com/push2",
		Active:   true,
	}
	if err := storage.AddSubscription(userID, sub2); err != nil {
		t.Fatalf("Failed to add subscription: %v", err)
	}
	err = syncer.Sync(userID)
	if err != nil {
		t.Fatalf("Second sync failed: %v", err)
	}

	// Verify secret was updated
	secretName := syncer.GetSecretName(userID)
	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	data := secret.Data["subscriptions.json"]
	var subs []notification.Subscription
	if err := json.Unmarshal(data, &subs); err != nil {
		t.Fatalf("Failed to unmarshal subscriptions: %v", err)
	}

	if len(subs) != 2 {
		t.Fatalf("Expected 2 subscriptions, got %d", len(subs))
	}
}

func TestKubernetesSubscriptionSecretSyncer_GetSecretName(t *testing.T) {
	syncer := NewKubernetesSubscriptionSecretSyncer(nil, "default", nil, "")

	tests := []struct {
		userID   string
		expected string
	}{
		{"user123", "notification-subscriptions-user123"},
		{"user@example.com", "notification-subscriptions-user-example.com"},
		{"User_Name", "notification-subscriptions-User_Name"},
	}

	for _, tt := range tests {
		t.Run(tt.userID, func(t *testing.T) {
			result := syncer.GetSecretName(tt.userID)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestKubernetesSubscriptionSecretSyncer_CustomPrefix(t *testing.T) {
	syncer := NewKubernetesSubscriptionSecretSyncer(nil, "default", nil, "custom-prefix")

	secretName := syncer.GetSecretName("user123")
	expected := "custom-prefix-user123"
	if secretName != expected {
		t.Errorf("Expected %s, got %s", expected, secretName)
	}
}
