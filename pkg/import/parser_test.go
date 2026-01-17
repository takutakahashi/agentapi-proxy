package importexport

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestParser_ParseYAML(t *testing.T) {
	parser := NewParser()

	yamlData := `
apiVersion: agentapi.proxy/v1
kind: TeamResources
metadata:
  team_id: "myorg-backend-team"
  description: "Backend team resources"
schedules:
  - name: "Daily Test"
    status: "active"
    cron_expr: "0 9 * * MON-FRI"
    timezone: "Asia/Tokyo"
    session_config:
      environment:
        TEST_ENV: "integration"
      tags:
        purpose: "test"
      params:
        initial_message: "Run tests"
webhooks:
  - name: "PR Webhook"
    status: "active"
    webhook_type: "github"
    signature_type: "hmac"
    max_sessions: 10
    github:
      allowed_events:
        - "pull_request"
      allowed_repositories:
        - "myorg/*"
    triggers:
      - name: "Backend PR"
        priority: 1
        enabled: true
        stop_on_match: true
        conditions:
          github:
            actions:
              - "opened"
            branches:
              - "main"
        session_config:
          tags:
            webhook: "pr"
`

	resources, err := parser.ParseYAML([]byte(yamlData))
	if err != nil {
		t.Fatalf("Failed to parse YAML: %v", err)
	}

	// Validate metadata
	if resources.Metadata.TeamID != "myorg-backend-team" {
		t.Errorf("Expected team_id 'myorg-backend-team', got '%s'", resources.Metadata.TeamID)
	}

	// Validate schedules
	if len(resources.Schedules) != 1 {
		t.Fatalf("Expected 1 schedule, got %d", len(resources.Schedules))
	}
	schedule := resources.Schedules[0]
	if schedule.Name != "Daily Test" {
		t.Errorf("Expected schedule name 'Daily Test', got '%s'", schedule.Name)
	}
	if schedule.CronExpr != "0 9 * * MON-FRI" {
		t.Errorf("Expected cron_expr '0 9 * * MON-FRI', got '%s'", schedule.CronExpr)
	}

	// Validate webhooks
	if len(resources.Webhooks) != 1 {
		t.Fatalf("Expected 1 webhook, got %d", len(resources.Webhooks))
	}
	webhook := resources.Webhooks[0]
	if webhook.Name != "PR Webhook" {
		t.Errorf("Expected webhook name 'PR Webhook', got '%s'", webhook.Name)
	}
	if webhook.MaxSessions != 10 {
		t.Errorf("Expected max_sessions 10, got %d", webhook.MaxSessions)
	}
}

func TestParser_ParseJSON(t *testing.T) {
	parser := NewParser()

	jsonData := `{
		"apiVersion": "agentapi.proxy/v1",
		"kind": "TeamResources",
		"metadata": {
			"team_id": "myorg-backend-team"
		},
		"schedules": [
			{
				"name": "Daily Test",
				"status": "active",
				"cron_expr": "0 9 * * MON-FRI",
				"timezone": "Asia/Tokyo",
				"session_config": {
					"environment": {
						"TEST_ENV": "integration"
					}
				}
			}
		],
		"webhooks": []
	}`

	resources, err := parser.ParseJSON([]byte(jsonData))
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if resources.Metadata.TeamID != "myorg-backend-team" {
		t.Errorf("Expected team_id 'myorg-backend-team', got '%s'", resources.Metadata.TeamID)
	}

	if len(resources.Schedules) != 1 {
		t.Fatalf("Expected 1 schedule, got %d", len(resources.Schedules))
	}
}

func TestParser_ParseTOML(t *testing.T) {
	parser := NewParser()

	tomlData := `
[metadata]
team_id = "myorg-backend-team"
api_version = "agentapi.proxy/v1"
kind = "TeamResources"

[[schedules]]
name = "Daily Test"
status = "active"
cron_expr = "0 9 * * MON-FRI"
timezone = "Asia/Tokyo"

[schedules.session_config]
environment = { TEST_ENV = "integration" }
`

	resources, err := parser.ParseTOML([]byte(tomlData))
	if err != nil {
		t.Fatalf("Failed to parse TOML: %v", err)
	}

	if resources.Metadata.TeamID != "myorg-backend-team" {
		t.Errorf("Expected team_id 'myorg-backend-team', got '%s'", resources.Metadata.TeamID)
	}

	if len(resources.Schedules) != 1 {
		t.Fatalf("Expected 1 schedule, got %d", len(resources.Schedules))
	}
}

func TestParser_DetectFormat(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name        string
		contentType string
		data        string
		expected    ExportFormat
	}{
		{
			name:        "JSON with content type",
			contentType: "application/json",
			data:        `{"key": "value"}`,
			expected:    ExportFormatJSON,
		},
		{
			name:        "YAML with content type",
			contentType: "application/x-yaml",
			data:        "key: value",
			expected:    ExportFormatYAML,
		},
		{
			name:        "TOML with content type",
			contentType: "application/toml",
			data:        "key = \"value\"",
			expected:    ExportFormatTOML,
		},
		{
			name:        "JSON by content",
			contentType: "",
			data:        `{"key": "value"}`,
			expected:    ExportFormatJSON,
		},
		{
			name:        "YAML by content",
			contentType: "",
			data:        "---\nkey: value",
			expected:    ExportFormatYAML,
		},
		{
			name:        "TOML by content",
			contentType: "",
			data:        "key = \"value\"",
			expected:    ExportFormatTOML,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, err := parser.detectFormat([]byte(tt.data), tt.contentType)
			if err != nil {
				t.Fatalf("Failed to detect format: %v", err)
			}
			if format != tt.expected {
				t.Errorf("Expected format %s, got %s", tt.expected, format)
			}
		})
	}
}

func TestFormatter_FormatYAML(t *testing.T) {
	formatter := NewFormatter()

	scheduledAt := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	resources := &TeamResources{
		APIVersion: "agentapi.proxy/v1",
		Kind:       "TeamResources",
		Metadata: ResourceMetadata{
			TeamID:      "myorg-backend-team",
			Description: "Test resources",
		},
		Schedules: []ScheduleImport{
			{
				Name:        "Daily Test",
				Status:      "active",
				ScheduledAt: &scheduledAt,
				Timezone:    "UTC",
				SessionConfig: SessionConfigImport{
					Environment: map[string]string{"TEST": "true"},
				},
			},
		},
		Webhooks: []WebhookImport{},
	}

	var buf bytes.Buffer
	err := formatter.FormatYAML(resources, &buf)
	if err != nil {
		t.Fatalf("Failed to format YAML: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "apiVersion: agentapi.proxy/v1") {
		t.Error("Output does not contain apiVersion")
	}
	if !strings.Contains(output, "team_id: myorg-backend-team") {
		t.Error("Output does not contain team_id")
	}
}

func TestFormatter_FormatJSON(t *testing.T) {
	formatter := NewFormatter()

	resources := &TeamResources{
		APIVersion: "agentapi.proxy/v1",
		Kind:       "TeamResources",
		Metadata: ResourceMetadata{
			TeamID: "myorg-backend-team",
		},
		Schedules: []ScheduleImport{},
		Webhooks:  []WebhookImport{},
	}

	var buf bytes.Buffer
	err := formatter.FormatJSON(resources, &buf)
	if err != nil {
		t.Fatalf("Failed to format JSON: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"apiVersion": "agentapi.proxy/v1"`) {
		t.Error("Output does not contain apiVersion")
	}
}

func TestContentTypeForFormat(t *testing.T) {
	tests := []struct {
		format   ExportFormat
		expected string
	}{
		{ExportFormatYAML, "application/x-yaml"},
		{ExportFormatTOML, "application/toml"},
		{ExportFormatJSON, "application/json"},
		{ExportFormat("unknown"), "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			result := ContentTypeForFormat(tt.format)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFileExtensionForFormat(t *testing.T) {
	tests := []struct {
		format   ExportFormat
		expected string
	}{
		{ExportFormatYAML, ".yaml"},
		{ExportFormatTOML, ".toml"},
		{ExportFormatJSON, ".json"},
		{ExportFormat("unknown"), ".dat"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			result := FileExtensionForFormat(tt.format)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
