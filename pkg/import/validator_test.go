package importexport

import (
	"testing"
	"time"
)

func TestValidator_ValidateMetadata(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name      string
		metadata  ResourceMetadata
		expectErr bool
	}{
		{
			name: "valid metadata",
			metadata: ResourceMetadata{
				TeamID:      "myorg/backend-team",
				Description: "Test resources",
			},
			expectErr: false,
		},
		{
			name: "missing team_id",
			metadata: ResourceMetadata{
				Description: "Test resources",
			},
			expectErr: true,
		},
		{
			name: "invalid team_id format - no slash",
			metadata: ResourceMetadata{
				TeamID: "myorg",
			},
			expectErr: true,
		},
		{
			name: "invalid team_id format - empty org",
			metadata: ResourceMetadata{
				TeamID: "/backend-team",
			},
			expectErr: true,
		},
		{
			name: "invalid team_id format - empty team",
			metadata: ResourceMetadata{
				TeamID: "myorg/",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateMetadata(tt.metadata)
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestValidator_ValidateSchedule(t *testing.T) {
	validator := NewValidator()

	scheduledAt := time.Now()

	tests := []struct {
		name      string
		schedule  ScheduleImport
		expectErr bool
	}{
		{
			name: "valid schedule with cron",
			schedule: ScheduleImport{
				Name:     "Daily Test",
				Status:   "active",
				CronExpr: "0 9 * * MON-FRI",
				Timezone: "Asia/Tokyo",
				SessionConfig: SessionConfigImport{
					Environment: map[string]string{"TEST": "true"},
				},
			},
			expectErr: false,
		},
		{
			name: "valid schedule with scheduled_at",
			schedule: ScheduleImport{
				Name:        "One-time Test",
				Status:      "active",
				ScheduledAt: &scheduledAt,
				Timezone:    "UTC",
				SessionConfig: SessionConfigImport{
					Environment: map[string]string{"TEST": "true"},
				},
			},
			expectErr: false,
		},
		{
			name: "missing name",
			schedule: ScheduleImport{
				CronExpr: "0 9 * * *",
				SessionConfig: SessionConfigImport{
					Environment: map[string]string{"TEST": "true"},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid status",
			schedule: ScheduleImport{
				Name:     "Test",
				Status:   "invalid",
				CronExpr: "0 9 * * *",
				SessionConfig: SessionConfigImport{
					Environment: map[string]string{"TEST": "true"},
				},
			},
			expectErr: true,
		},
		{
			name: "missing both scheduled_at and cron_expr",
			schedule: ScheduleImport{
				Name:   "Test",
				Status: "active",
				SessionConfig: SessionConfigImport{
					Environment: map[string]string{"TEST": "true"},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid cron expression",
			schedule: ScheduleImport{
				Name:     "Test",
				CronExpr: "invalid cron",
				SessionConfig: SessionConfigImport{
					Environment: map[string]string{"TEST": "true"},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateSchedule(tt.schedule)
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestValidator_ValidateWebhook(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name      string
		webhook   WebhookImport
		expectErr bool
	}{
		{
			name: "valid webhook",
			webhook: WebhookImport{
				Name:          "PR Webhook",
				Status:        "active",
				WebhookType:   "github",
				SignatureType: "hmac",
				MaxSessions:   10,
				Triggers: []WebhookTriggerImport{
					{
						Name:    "Test Trigger",
						Enabled: true,
						Conditions: WebhookTriggerConditionsImport{
							GitHub: &GitHubConditionsImport{
								Events: []string{"pull_request"},
							},
						},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "missing name",
			webhook: WebhookImport{
				WebhookType: "github",
				Triggers: []WebhookTriggerImport{
					{
						Name:    "Test",
						Enabled: true,
						Conditions: WebhookTriggerConditionsImport{
							GitHub: &GitHubConditionsImport{
								Events: []string{"pull_request"},
							},
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid status",
			webhook: WebhookImport{
				Name:        "Test",
				Status:      "invalid",
				WebhookType: "github",
				Triggers: []WebhookTriggerImport{
					{
						Name:    "Test",
						Enabled: true,
						Conditions: WebhookTriggerConditionsImport{
							GitHub: &GitHubConditionsImport{
								Events: []string{"pull_request"},
							},
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "missing webhook_type",
			webhook: WebhookImport{
				Name: "Test",
				Triggers: []WebhookTriggerImport{
					{
						Name:    "Test",
						Enabled: true,
						Conditions: WebhookTriggerConditionsImport{
							GitHub: &GitHubConditionsImport{
								Events: []string{"pull_request"},
							},
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid webhook_type",
			webhook: WebhookImport{
				Name:        "Test",
				WebhookType: "invalid",
				Triggers: []WebhookTriggerImport{
					{
						Name:    "Test",
						Enabled: true,
						Conditions: WebhookTriggerConditionsImport{
							GitHub: &GitHubConditionsImport{
								Events: []string{"pull_request"},
							},
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "no triggers",
			webhook: WebhookImport{
				Name:        "Test",
				WebhookType: "github",
				Triggers:    []WebhookTriggerImport{},
			},
			expectErr: true,
		},
		{
			name: "invalid max_sessions - negative",
			webhook: WebhookImport{
				Name:        "Test",
				WebhookType: "github",
				MaxSessions: -1,
				Triggers: []WebhookTriggerImport{
					{
						Name:    "Test",
						Enabled: true,
						Conditions: WebhookTriggerConditionsImport{
							GitHub: &GitHubConditionsImport{
								Events: []string{"pull_request"},
							},
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "invalid max_sessions - too large",
			webhook: WebhookImport{
				Name:        "Test",
				WebhookType: "github",
				MaxSessions: 101,
				Triggers: []WebhookTriggerImport{
					{
						Name:    "Test",
						Enabled: true,
						Conditions: WebhookTriggerConditionsImport{
							GitHub: &GitHubConditionsImport{
								Events: []string{"pull_request"},
							},
						},
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateWebhook(tt.webhook)
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestValidator_ValidateTrigger(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name      string
		trigger   WebhookTriggerImport
		expectErr bool
	}{
		{
			name: "valid trigger with GitHub conditions",
			trigger: WebhookTriggerImport{
				Name:     "Test Trigger",
				Priority: 1,
				Enabled:  true,
				Conditions: WebhookTriggerConditionsImport{
					GitHub: &GitHubConditionsImport{
						Events:  []string{"pull_request"},
						Actions: []string{"opened"},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "valid trigger with JSONPath conditions",
			trigger: WebhookTriggerImport{
				Name:    "Test Trigger",
				Enabled: true,
				Conditions: WebhookTriggerConditionsImport{
					JSONPath: []JSONPathConditionImport{
						{
							Path:     "$.action",
							Operator: "eq",
							Value:    "opened",
						},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "missing name",
			trigger: WebhookTriggerImport{
				Enabled: true,
				Conditions: WebhookTriggerConditionsImport{
					GitHub: &GitHubConditionsImport{
						Events: []string{"pull_request"},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "negative priority",
			trigger: WebhookTriggerImport{
				Name:     "Test",
				Priority: -1,
				Enabled:  true,
				Conditions: WebhookTriggerConditionsImport{
					GitHub: &GitHubConditionsImport{
						Events: []string{"pull_request"},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "no conditions",
			trigger: WebhookTriggerImport{
				Name:       "Test",
				Enabled:    true,
				Conditions: WebhookTriggerConditionsImport{},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateTrigger(tt.trigger)
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestValidator_ValidateJSONPathCondition(t *testing.T) {
	validator := NewValidator()

	tests := []struct {
		name      string
		condition JSONPathConditionImport
		expectErr bool
	}{
		{
			name: "valid condition",
			condition: JSONPathConditionImport{
				Path:     "$.action",
				Operator: "eq",
				Value:    "opened",
			},
			expectErr: false,
		},
		{
			name: "valid condition with exists operator",
			condition: JSONPathConditionImport{
				Path:     "$.pull_request",
				Operator: "exists",
			},
			expectErr: false,
		},
		{
			name: "missing path",
			condition: JSONPathConditionImport{
				Operator: "eq",
				Value:    "opened",
			},
			expectErr: true,
		},
		{
			name: "missing operator",
			condition: JSONPathConditionImport{
				Path:  "$.action",
				Value: "opened",
			},
			expectErr: true,
		},
		{
			name: "invalid operator",
			condition: JSONPathConditionImport{
				Path:     "$.action",
				Operator: "invalid",
				Value:    "opened",
			},
			expectErr: true,
		},
		{
			name: "missing value for non-exists operator",
			condition: JSONPathConditionImport{
				Path:     "$.action",
				Operator: "eq",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateJSONPathCondition(tt.condition)
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestValidator_Validate(t *testing.T) {
	validator := NewValidator()

	scheduledAt := time.Now()

	tests := []struct {
		name      string
		resources TeamResources
		expectErr bool
	}{
		{
			name: "valid resources",
			resources: TeamResources{
				APIVersion: "agentapi.proxy/v1",
				Kind:       "TeamResources",
				Metadata: ResourceMetadata{
					TeamID: "myorg/backend-team",
				},
				Schedules: []ScheduleImport{
					{
						Name:        "Test",
						ScheduledAt: &scheduledAt,
						SessionConfig: SessionConfigImport{
							Environment: map[string]string{"TEST": "true"},
						},
					},
				},
				Webhooks: []WebhookImport{
					{
						Name:        "Test Webhook",
						WebhookType: "github",
						Triggers: []WebhookTriggerImport{
							{
								Name:    "Test Trigger",
								Enabled: true,
								Conditions: WebhookTriggerConditionsImport{
									GitHub: &GitHubConditionsImport{
										Events: []string{"pull_request"},
									},
								},
							},
						},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "duplicate schedule names",
			resources: TeamResources{
				Metadata: ResourceMetadata{
					TeamID: "myorg/backend-team",
				},
				Schedules: []ScheduleImport{
					{
						Name:        "Test",
						ScheduledAt: &scheduledAt,
						SessionConfig: SessionConfigImport{
							Environment: map[string]string{"TEST": "true"},
						},
					},
					{
						Name:        "Test",
						ScheduledAt: &scheduledAt,
						SessionConfig: SessionConfigImport{
							Environment: map[string]string{"TEST": "true"},
						},
					},
				},
			},
			expectErr: true,
		},
		{
			name: "duplicate webhook names",
			resources: TeamResources{
				Metadata: ResourceMetadata{
					TeamID: "myorg/backend-team",
				},
				Webhooks: []WebhookImport{
					{
						Name:        "Test",
						WebhookType: "github",
						Triggers: []WebhookTriggerImport{
							{
								Name:    "Trigger",
								Enabled: true,
								Conditions: WebhookTriggerConditionsImport{
									GitHub: &GitHubConditionsImport{
										Events: []string{"pull_request"},
									},
								},
							},
						},
					},
					{
						Name:        "Test",
						WebhookType: "github",
						Triggers: []WebhookTriggerImport{
							{
								Name:    "Trigger2",
								Enabled: true,
								Conditions: WebhookTriggerConditionsImport{
									GitHub: &GitHubConditionsImport{
										Events: []string{"pull_request"},
									},
								},
							},
						},
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(&tt.resources)
			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}
