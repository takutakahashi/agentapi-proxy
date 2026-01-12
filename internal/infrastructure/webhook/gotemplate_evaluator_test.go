package webhook

import (
	"testing"
)

func TestGoTemplateEvaluator_Evaluate(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]interface{}
		template    string
		expected    bool
		expectError bool
	}{
		{
			name: "simple equality check",
			payload: map[string]interface{}{
				"event": map[string]interface{}{
					"type": "push",
				},
			},
			template: `{{ eq .event.type "push" }}`,
			expected: true,
		},
		{
			name: "simple inequality check",
			payload: map[string]interface{}{
				"event": map[string]interface{}{
					"type": "push",
				},
			},
			template: `{{ eq .event.type "pull_request" }}`,
			expected: false,
		},
		{
			name: "nested field access",
			payload: map[string]interface{}{
				"deployment": map[string]interface{}{
					"status": "success",
					"environment": map[string]interface{}{
						"name": "production",
					},
				},
			},
			template: `{{ eq .deployment.environment.name "production" }}`,
			expected: true,
		},
		{
			name: "and logic",
			payload: map[string]interface{}{
				"event": map[string]interface{}{
					"type":   "deployment",
					"status": "success",
				},
			},
			template: `{{ and (eq .event.type "deployment") (eq .event.status "success") }}`,
			expected: true,
		},
		{
			name: "or logic",
			payload: map[string]interface{}{
				"deployment": map[string]interface{}{
					"environment": "staging",
				},
			},
			template: `{{ or (eq .deployment.environment "production") (eq .deployment.environment "staging") }}`,
			expected: true,
		},
		{
			name: "not logic",
			payload: map[string]interface{}{
				"event": map[string]interface{}{
					"type": "push",
				},
			},
			template: `{{ not (eq .event.type "pull_request") }}`,
			expected: true,
		},
		{
			name: "complex condition",
			payload: map[string]interface{}{
				"event": "deployment.success",
				"deployment": map[string]interface{}{
					"status":      "success",
					"environment": "production",
				},
			},
			template: `{{ and (contains .event "deployment") (eq .deployment.status "success") (or (eq .deployment.environment "production") (eq .deployment.environment "staging")) }}`,
			expected: true,
		},
		{
			name: "contains function",
			payload: map[string]interface{}{
				"message": "This is a test message",
			},
			template: `{{ contains .message "test" }}`,
			expected: true,
		},
		{
			name: "hasPrefix function",
			payload: map[string]interface{}{
				"branch": "feature/new-feature",
			},
			template: `{{ hasPrefix .branch "feature/" }}`,
			expected: true,
		},
		{
			name: "hasSuffix function",
			payload: map[string]interface{}{
				"file": "document.pdf",
			},
			template: `{{ hasSuffix .file ".pdf" }}`,
			expected: true,
		},
		{
			name: "len function",
			payload: map[string]interface{}{
				"tags": []interface{}{"tag1", "tag2", "tag3"},
			},
			template: `{{ eq (len .tags) 3 }}`,
			expected: true,
		},
		{
			name: "comparison operators",
			payload: map[string]interface{}{
				"count": 10,
			},
			template: `{{ and (gt .count 5) (lt .count 15) }}`,
			expected: true,
		},
		{
			name: "empty template",
			payload: map[string]interface{}{
				"event": "test",
			},
			template: ``,
			expected: true,
		},
		{
			name: "invalid template syntax",
			payload: map[string]interface{}{
				"event": "test",
			},
			template:    `{{ eq .event "test"`,
			expected:    false,
			expectError: true,
		},
		{
			name: "template execution error",
			payload: map[string]interface{}{
				"event": "test",
			},
			template:    `{{ nonExistentFunc .event }}`,
			expected:    false,
			expectError: true,
		},
		{
			name: "Slack message type check",
			payload: map[string]interface{}{
				"type": "message",
				"channel": map[string]interface{}{
					"id": "C1234567890",
				},
				"user": "U1234567890",
			},
			template: `{{ and (eq .type "message") (ne .user "") }}`,
			expected: true,
		},
		{
			name: "Datadog alert check",
			payload: map[string]interface{}{
				"alert_type": "error",
				"priority":   "P1",
				"tags":       []interface{}{"service:api", "env:production"},
			},
			template: `{{ and (eq .alert_type "error") (or (eq .priority "P1") (eq .priority "P2")) }}`,
			expected: true,
		},
		{
			name: "Custom deployment webhook",
			payload: map[string]interface{}{
				"event": "deployment.completed",
				"deployment": map[string]interface{}{
					"status":      "success",
					"environment": "production",
					"service":     "api-gateway",
				},
			},
			template: `{{ and (contains .event "deployment") (eq .deployment.status "success") (eq .deployment.environment "production") }}`,
			expected: true,
		},
	}

	evaluator := NewGoTemplateEvaluator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(tt.payload, tt.template)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %v but got %v", tt.expected, result)
			}
		})
	}
}

func TestGoTemplateEvaluator_CustomFunctions(t *testing.T) {
	tests := []struct {
		name     string
		payload  map[string]interface{}
		template string
		expected bool
	}{
		{
			name: "toString function",
			payload: map[string]interface{}{
				"code": 200,
			},
			template: `{{ eq (toString .code) "200" }}`,
			expected: true,
		},
		{
			name: "toLower function",
			payload: map[string]interface{}{
				"status": "SUCCESS",
			},
			template: `{{ eq (toLower .status) "success" }}`,
			expected: true,
		},
		{
			name: "toUpper function",
			payload: map[string]interface{}{
				"env": "prod",
			},
			template: `{{ eq (toUpper .env) "PROD" }}`,
			expected: true,
		},
		{
			name: "trimSpace function",
			payload: map[string]interface{}{
				"message": "  hello  ",
			},
			template: `{{ eq (trimSpace .message) "hello" }}`,
			expected: true,
		},
	}

	evaluator := NewGoTemplateEvaluator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := evaluator.Evaluate(tt.payload, tt.template)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %v but got %v", tt.expected, result)
			}
		})
	}
}
