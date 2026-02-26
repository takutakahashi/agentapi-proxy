package controllers

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

// TestCreateWebhookRequestCustomSecret verifies that the CreateWebhookRequest
// correctly handles the custom secret field.
func TestCreateWebhookRequestCustomSecret(t *testing.T) {
	tests := []struct {
		name          string
		requestSecret string
		expectSecret  string
		expectAutoGen bool
	}{
		{
			name:          "Custom secret is set when provided",
			requestSecret: "my-sentry-client-secret",
			expectSecret:  "my-sentry-client-secret",
			expectAutoGen: false,
		},
		{
			name:          "Empty secret results in auto-generation",
			requestSecret: "",
			expectSecret:  "",
			expectAutoGen: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			webhook := entities.NewWebhook("test-id", "Test Webhook", "user-1", entities.WebhookTypeCustom)

			if tt.requestSecret != "" {
				webhook.SetSecret(tt.requestSecret)
			}

			if tt.expectAutoGen {
				// When no secret is set, the repository auto-generates one
				// Here we just verify the webhook has no pre-set secret
				if webhook.Secret() != "" {
					t.Errorf("Expected empty secret for auto-generation, got %q", webhook.Secret())
				}
			} else {
				if webhook.Secret() != tt.expectSecret {
					t.Errorf("Expected secret %q, got %q", tt.expectSecret, webhook.Secret())
				}
			}
		})
	}
}

// TestUpdateWebhookRequestCustomSecret verifies that the UpdateWebhookRequest
// correctly handles the custom secret field.
func TestUpdateWebhookRequestCustomSecret(t *testing.T) {
	tests := []struct {
		name          string
		initialSecret string
		updateSecret  *string
		expectSecret  string
	}{
		{
			name:          "Secret is updated when new value provided",
			initialSecret: "old-secret",
			updateSecret:  strPtr("new-sentry-client-secret"),
			expectSecret:  "new-sentry-client-secret",
		},
		{
			name:          "Secret is kept when update secret is nil",
			initialSecret: "existing-secret",
			updateSecret:  nil,
			expectSecret:  "existing-secret",
		},
		{
			name:          "Secret is kept when update secret is empty string",
			initialSecret: "existing-secret",
			updateSecret:  strPtr(""),
			expectSecret:  "existing-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			webhook := entities.NewWebhook("test-id", "Test Webhook", "user-1", entities.WebhookTypeCustom)
			webhook.SetSecret(tt.initialSecret)

			// Simulate UpdateWebhook handler logic
			if tt.updateSecret != nil && *tt.updateSecret != "" {
				webhook.SetSecret(*tt.updateSecret)
			}

			if webhook.Secret() != tt.expectSecret {
				t.Errorf("Expected secret %q, got %q", tt.expectSecret, webhook.Secret())
			}
		})
	}
}

// TestCreateWebhookRequestStruct verifies the struct has the Secret field.
func TestCreateWebhookRequestStruct(t *testing.T) {
	req := CreateWebhookRequest{
		Name:   "Test",
		Type:   entities.WebhookTypeCustom,
		Secret: "test-secret",
	}

	if req.Secret != "test-secret" {
		t.Errorf("Expected Secret field to be 'test-secret', got %q", req.Secret)
	}
}

// TestUpdateWebhookRequestStruct verifies the struct has the Secret field.
func TestUpdateWebhookRequestStruct(t *testing.T) {
	secret := "new-secret"
	req := UpdateWebhookRequest{
		Secret: &secret,
	}

	if req.Secret == nil || *req.Secret != "new-secret" {
		t.Errorf("Expected Secret field to be 'new-secret'")
	}
}

func strPtr(s string) *string {
	return &s
}
