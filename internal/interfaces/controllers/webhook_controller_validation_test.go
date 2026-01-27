package controllers

import (
	"testing"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestValidateInitialMessageTemplate(t *testing.T) {
	controller := &WebhookController{}

	tests := []struct {
		name        string
		webhookType entities.WebhookType
		template    string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "Valid GitHub template with pull_request fields",
			webhookType: entities.WebhookTypeGitHub,
			template:    "PR #{{ .pull_request.number }}: {{ .pull_request.title }}",
			shouldError: false,
		},
		{
			name:        "Valid GitHub template with repository fields",
			webhookType: entities.WebhookTypeGitHub,
			template:    "Repository: {{ .repository.full_name }}",
			shouldError: false,
		},
		{
			name:        "Valid GitHub template with sender fields",
			webhookType: entities.WebhookTypeGitHub,
			template:    "Author: {{ .sender.login }}",
			shouldError: false,
		},
		{
			name:        "Valid custom webhook template",
			webhookType: entities.WebhookTypeCustom,
			template:    "Event: {{ .event }}, Data: {{ .data.message }}",
			shouldError: false,
		},
		{
			name:        "Invalid template - unclosed action",
			webhookType: entities.WebhookTypeGitHub,
			template:    "{{ .pull_request.number",
			shouldError: true,
			errorMsg:    "template parse failed",
		},
		{
			name:        "Invalid template - undefined function",
			webhookType: entities.WebhookTypeGitHub,
			template:    "{{ invalidFunc .pull_request.number }}",
			shouldError: true,
			errorMsg:    "template execution failed",
		},
		{
			name:        "Empty template - should pass",
			webhookType: entities.WebhookTypeGitHub,
			template:    "",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.template == "" {
				// Empty templates should not be validated
				return
			}

			err := controller.validateInitialMessageTemplate(tt.webhookType, tt.template)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestValidateGoTemplateCondition(t *testing.T) {
	controller := &WebhookController{}

	tests := []struct {
		name        string
		webhookType entities.WebhookType
		template    string
		shouldError bool
	}{
		{
			name:        "Valid GoTemplate with eq function",
			webhookType: entities.WebhookTypeGitHub,
			template:    `{{ eq .action "opened" }}`,
			shouldError: false,
		},
		{
			name:        "Valid GoTemplate with contains function",
			webhookType: entities.WebhookTypeGitHub,
			template:    `{{ contains .repository.full_name "example" }}`,
			shouldError: false,
		},
		{
			name:        "Valid GoTemplate with and/or",
			webhookType: entities.WebhookTypeGitHub,
			template:    `{{ and (eq .action "opened") (contains .repository.full_name "test") }}`,
			shouldError: false,
		},
		{
			name:        "Valid custom webhook GoTemplate with nested field access",
			webhookType: entities.WebhookTypeCustom,
			template:    `{{ eq .event.t "alert" }}`,
			shouldError: false,
		},
		{
			name:        "Valid custom webhook GoTemplate with nested event.type",
			webhookType: entities.WebhookTypeCustom,
			template:    `{{ eq .event.type "test_event" }}`,
			shouldError: false,
		},
		{
			name:        "Valid custom webhook GoTemplate with data fields",
			webhookType: entities.WebhookTypeCustom,
			template:    `{{ and (eq .data.status "success") (eq .data.message "test message") }}`,
			shouldError: false,
		},
		{
			name:        "Invalid GoTemplate - syntax error",
			webhookType: entities.WebhookTypeGitHub,
			template:    `{{ eq .action `,
			shouldError: true,
		},
		{
			name:        "Empty template - should pass",
			webhookType: entities.WebhookTypeGitHub,
			template:    "",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.template == "" {
				// Empty templates should not be validated
				return
			}

			err := controller.validateGoTemplateCondition(tt.webhookType, tt.template)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestValidateWebhookTemplates(t *testing.T) {
	controller := &WebhookController{}

	tests := []struct {
		name        string
		request     CreateWebhookRequest
		shouldError bool
		errorMsg    string
	}{
		{
			name: "Valid webhook with valid initial message template",
			request: CreateWebhookRequest{
				Type: entities.WebhookTypeGitHub,
				SessionConfig: &SessionConfigRequest{
					InitialMessageTemplate: "PR #{{ .pull_request.Number }}",
				},
				Triggers: []TriggerRequest{
					{Name: "test"},
				},
			},
			shouldError: false,
		},
		{
			name: "Valid webhook with valid GoTemplate condition",
			request: CreateWebhookRequest{
				Type: entities.WebhookTypeGitHub,
				Triggers: []TriggerRequest{
					{
						Name: "test",
						Conditions: TriggerConditionsRequest{
							GoTemplate: `{{ eq .action "opened" }}`,
						},
					},
				},
			},
			shouldError: false,
		},
		{
			name: "Invalid webhook - bad initial message template",
			request: CreateWebhookRequest{
				Type: entities.WebhookTypeGitHub,
				SessionConfig: &SessionConfigRequest{
					InitialMessageTemplate: "{{ .invalid syntax",
				},
				Triggers: []TriggerRequest{
					{Name: "test"},
				},
			},
			shouldError: true,
			errorMsg:    "session_config.initial_message_template",
		},
		{
			name: "Invalid webhook - bad GoTemplate condition",
			request: CreateWebhookRequest{
				Type: entities.WebhookTypeGitHub,
				Triggers: []TriggerRequest{
					{
						Name: "test",
						Conditions: TriggerConditionsRequest{
							GoTemplate: `{{ invalid syntax`,
						},
					},
				},
			},
			shouldError: true,
			errorMsg:    "conditions.go_template",
		},
		{
			name: "Invalid webhook - bad trigger session config template",
			request: CreateWebhookRequest{
				Type: entities.WebhookTypeGitHub,
				Triggers: []TriggerRequest{
					{
						Name: "test",
						SessionConfig: &SessionConfigRequest{
							InitialMessageTemplate: "{{ unclosed",
						},
					},
				},
			},
			shouldError: true,
			errorMsg:    "session_config.initial_message_template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := controller.validateWebhookTemplates(tt.request.Type, tt.request)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestCreateTestPayload(t *testing.T) {
	controller := &WebhookController{}

	tests := []struct {
		name        string
		webhookType entities.WebhookType
		checkFields []string
	}{
		{
			name:        "GitHub webhook test payload",
			webhookType: entities.WebhookTypeGitHub,
			checkFields: []string{"action", "repository", "pull_request", "sender"},
		},
		{
			name:        "Custom webhook test payload",
			webhookType: entities.WebhookTypeCustom,
			checkFields: []string{"event", "data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := controller.createTestPayload(tt.webhookType)

			for _, field := range tt.checkFields {
				if _, ok := payload[field]; !ok {
					t.Errorf("Expected field %s in test payload but not found", field)
				}
			}
		})
	}
}
