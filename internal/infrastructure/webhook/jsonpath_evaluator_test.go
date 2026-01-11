package webhook

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestJSONPathEvaluator_Evaluate(t *testing.T) {
	evaluator := NewJSONPathEvaluator()

	payload := map[string]interface{}{
		"event": map[string]interface{}{
			"type":        "deployment",
			"severity":    "critical",
			"environment": "production",
			"tags":        []interface{}{"backend", "api", "production"},
		},
		"service": map[string]interface{}{
			"name":    "api-server",
			"version": "v2.1.0",
		},
		"metadata": map[string]interface{}{
			"timestamp": "2026-01-11T10:30:00Z",
			"count":     42,
		},
	}

	tests := []struct {
		name           string
		conditions     []entities.WebhookJSONPathCondition
		expectedResult bool
		expectError    bool
	}{
		{
			name: "eq operator - string match",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.type", "eq", "deployment"),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "eq operator - string mismatch",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.type", "eq", "incident"),
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "ne operator",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.severity", "ne", "low"),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "contains operator - string",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.service.name", "contains", "api"),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "contains operator - array",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.tags", "contains", "production"),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "matches operator - regex match",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.service.name", "matches", "^api-.*"),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "matches operator - regex mismatch",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.service.name", "matches", "^web-.*"),
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "in operator - value in array",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.environment", "in", []interface{}{"production", "staging"}),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "in operator - value not in array",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.environment", "in", []interface{}{"development", "staging"}),
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "exists operator - path exists",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.metadata.timestamp", "exists", true),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "exists operator - path does not exist",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.nonexistent.field", "exists", false),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "multiple conditions - all match",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.type", "eq", "deployment"),
				entities.NewWebhookJSONPathCondition("$.event.severity", "eq", "critical"),
				entities.NewWebhookJSONPathCondition("$.event.environment", "eq", "production"),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "multiple conditions - one fails",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.type", "eq", "deployment"),
				entities.NewWebhookJSONPathCondition("$.event.severity", "eq", "low"), // This will fail
				entities.NewWebhookJSONPathCondition("$.event.environment", "eq", "production"),
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name:           "no conditions",
			conditions:     []entities.WebhookJSONPathCondition{},
			expectedResult: true,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(payload, tt.conditions)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != tt.expectedResult {
				t.Errorf("Evaluate() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestJSONPathEvaluator_EvaluateAny(t *testing.T) {
	evaluator := NewJSONPathEvaluator()

	payload := map[string]interface{}{
		"event": map[string]interface{}{
			"type":     "alert",
			"severity": "medium",
		},
	}

	tests := []struct {
		name           string
		conditions     []entities.WebhookJSONPathCondition
		expectedResult bool
		expectError    bool
	}{
		{
			name: "one condition matches",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.severity", "eq", "low"),
				entities.NewWebhookJSONPathCondition("$.event.severity", "eq", "medium"), // This matches
				entities.NewWebhookJSONPathCondition("$.event.severity", "eq", "high"),
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "no conditions match",
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.severity", "eq", "low"),
				entities.NewWebhookJSONPathCondition("$.event.severity", "eq", "high"),
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name:           "empty conditions",
			conditions:     []entities.WebhookJSONPathCondition{},
			expectedResult: true,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.EvaluateAny(payload, tt.conditions)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != tt.expectedResult {
				t.Errorf("EvaluateAny() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestJSONPathEvaluator_ComplexScenarios(t *testing.T) {
	evaluator := NewJSONPathEvaluator()

	tests := []struct {
		name           string
		payload        map[string]interface{}
		conditions     []entities.WebhookJSONPathCondition
		expectedResult bool
	}{
		{
			name: "Slack incident alert",
			payload: map[string]interface{}{
				"event": map[string]interface{}{
					"type":        "incident",
					"title":       "Database connection pool exhausted",
					"severity":    "critical",
					"environment": "production",
				},
				"user": map[string]interface{}{
					"id":   "U12345",
					"name": "john.doe",
				},
			},
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event.type", "eq", "incident"),
				entities.NewWebhookJSONPathCondition("$.event.severity", "eq", "critical"),
				entities.NewWebhookJSONPathCondition("$.event.environment", "eq", "production"),
			},
			expectedResult: true,
		},
		{
			name: "Datadog CPU alert",
			payload: map[string]interface{}{
				"alert_type":    "metric_alert",
				"current_value": 95.5,
				"threshold":     90.0,
				"host":          "api-server-01",
				"tags":          []interface{}{"env:production", "service:api"},
			},
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.alert_type", "eq", "metric_alert"),
				entities.NewWebhookJSONPathCondition("$.tags", "contains", "env:production"),
			},
			expectedResult: true,
		},
		{
			name: "Custom deployment webhook",
			payload: map[string]interface{}{
				"event": "deployment.succeeded",
				"deployment": map[string]interface{}{
					"id":          "deploy-123",
					"environment": "production",
					"status":      "success",
				},
				"service": map[string]interface{}{
					"name":    "api-server",
					"version": "v2.1.0",
				},
			},
			conditions: []entities.WebhookJSONPathCondition{
				entities.NewWebhookJSONPathCondition("$.event", "matches", "^deployment\\..*"),
				entities.NewWebhookJSONPathCondition("$.deployment.status", "eq", "success"),
				entities.NewWebhookJSONPathCondition("$.deployment.environment", "in", []interface{}{"production", "staging"}),
			},
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(tt.payload, tt.conditions)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if result != tt.expectedResult {
				t.Errorf("Evaluate() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}
