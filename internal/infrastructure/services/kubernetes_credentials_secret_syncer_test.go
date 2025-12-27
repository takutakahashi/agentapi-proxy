package services

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestKubernetesCredentialsSecretSyncer_Sync(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings with Bedrock config
	settings := entities.NewSettings("test-user")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	settings.SetBedrock(bedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	// Verify Secret was created
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-credentials-test-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Verify labels
	if secret.Labels[LabelCredentials] != "true" {
		t.Errorf("Expected label %s to be 'true'", LabelCredentials)
	}
	if secret.Labels[LabelManagedBy] != "settings" {
		t.Errorf("Expected label %s to be 'settings'", LabelManagedBy)
	}

	// Verify data
	if string(secret.Data["CLAUDE_CODE_USE_BEDROCK"]) != "1" {
		t.Error("Expected CLAUDE_CODE_USE_BEDROCK to be '1'")
	}
	if string(secret.Data["ANTHROPIC_MODEL"]) != "anthropic.claude-sonnet-4-20250514-v1:0" {
		t.Error("Expected ANTHROPIC_MODEL to match")
	}
	if string(secret.Data["AWS_ACCESS_KEY_ID"]) != "AKIAIOSFODNN7EXAMPLE" {
		t.Error("Expected AWS_ACCESS_KEY_ID to match")
	}
	if string(secret.Data["AWS_SECRET_ACCESS_KEY"]) != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Error("Expected AWS_SECRET_ACCESS_KEY to match")
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_Update(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create initial settings
	settings := entities.NewSettings("update-user")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetAccessKeyID("INITIAL_KEY")
	settings.SetBedrock(bedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync initial: %v", err)
	}

	// Update settings
	newBedrock := entities.NewBedrockSettings(true)
	newBedrock.SetAccessKeyID("UPDATED_KEY")
	settings.SetBedrock(newBedrock)

	err = syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync update: %v", err)
	}

	// Verify Secret was updated
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-credentials-update-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	if string(secret.Data["AWS_ACCESS_KEY_ID"]) != "UPDATED_KEY" {
		t.Errorf("Expected AWS_ACCESS_KEY_ID to be 'UPDATED_KEY', got '%s'", string(secret.Data["AWS_ACCESS_KEY_ID"]))
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_AllFields(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	settings := entities.NewSettings("all-fields")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("anthropic.claude-opus-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("secret-key")
	bedrock.SetRoleARN("arn:aws:iam::123456789012:role/ExampleRole")
	bedrock.SetProfile("production")
	settings.SetBedrock(bedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-credentials-all-fields", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	expectedData := map[string]string{
		"CLAUDE_CODE_USE_BEDROCK": "1",
		"ANTHROPIC_MODEL":         "anthropic.claude-opus-4-20250514-v1:0",
		"AWS_ACCESS_KEY_ID":       "AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY":   "secret-key",
		"AWS_ROLE_ARN":            "arn:aws:iam::123456789012:role/ExampleRole",
		"AWS_PROFILE":             "production",
	}

	for key, expected := range expectedData {
		if string(secret.Data[key]) != expected {
			t.Errorf("Expected %s to be '%s', got '%s'", key, expected, string(secret.Data[key]))
		}
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_DisabledBedrock(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings with disabled Bedrock
	settings := entities.NewSettings("disabled-bedrock")
	bedrock := entities.NewBedrockSettings(false)
	settings.SetBedrock(bedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	// Secret should be created but with no Bedrock data
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-credentials-disabled-bedrock", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Should not have CLAUDE_CODE_USE_BEDROCK key
	if _, ok := secret.Data["CLAUDE_CODE_USE_BEDROCK"]; ok {
		t.Error("Expected no CLAUDE_CODE_USE_BEDROCK key for disabled bedrock")
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_NilSettings(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	err := syncer.Sync(ctx, nil)
	if err == nil {
		t.Error("Expected error for nil settings")
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_OverwritesExistingSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create an existing secret
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-credentials-existing-user",
			Namespace: "default",
			Labels: map[string]string{
				LabelCredentials: "true",
			},
		},
		Data: map[string][]byte{
			"CUSTOM_KEY": []byte("custom-value"),
		},
	}
	_, err := client.CoreV1().Secrets("default").Create(ctx, existingSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create existing secret: %v", err)
	}

	// Sync settings for the same user
	settings := entities.NewSettings("existing-user")
	bedrock := entities.NewBedrockSettings(true)
	settings.SetBedrock(bedrock)

	err = syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Sync should not fail: %v", err)
	}

	// Verify secret was updated with new data
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-credentials-existing-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Secret should have Bedrock data
	if string(secret.Data["CLAUDE_CODE_USE_BEDROCK"]) != "1" {
		t.Error("Secret should have CLAUDE_CODE_USE_BEDROCK set to '1'")
	}
	// Secret should have LabelManagedBy set
	if secret.Labels[LabelManagedBy] != "settings" {
		t.Error("Secret should have LabelManagedBy set to 'settings'")
	}
}

func TestKubernetesCredentialsSecretSyncer_Delete(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings
	settings := entities.NewSettings("delete-user")
	bedrock := entities.NewBedrockSettings(true)
	settings.SetBedrock(bedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	// Verify Secret exists
	_, err = client.CoreV1().Secrets("default").Get(ctx, "agent-credentials-delete-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret should exist: %v", err)
	}

	// Delete
	err = syncer.Delete(ctx, "delete-user")
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Verify Secret was deleted
	_, err = client.CoreV1().Secrets("default").Get(ctx, "agent-credentials-delete-user", metav1.GetOptions{})
	if err == nil {
		t.Error("Secret should be deleted")
	}
}

func TestKubernetesCredentialsSecretSyncer_Delete_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Delete non-existent secret should not fail
	err := syncer.Delete(ctx, "nonexistent")
	if err != nil {
		t.Errorf("Delete should not fail for non-existent secret: %v", err)
	}
}

func TestKubernetesCredentialsSecretSyncer_Delete_DeletesAnySecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create a secret without LabelManagedBy
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-credentials-any-delete",
			Namespace: "default",
			Labels: map[string]string{
				LabelCredentials: "true",
			},
		},
		Data: map[string][]byte{
			"CUSTOM_KEY": []byte("custom-value"),
		},
	}
	_, err := client.CoreV1().Secrets("default").Create(ctx, existingSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create secret: %v", err)
	}

	// Delete
	err = syncer.Delete(ctx, "any-delete")
	if err != nil {
		t.Fatalf("Delete should not fail: %v", err)
	}

	// Verify secret was deleted
	_, err = client.CoreV1().Secrets("default").Get(ctx, "agent-credentials-any-delete", metav1.GetOptions{})
	if err == nil {
		t.Error("Secret should be deleted")
	}
}

func TestSanitizeSecretName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"UPPERCASE", "uppercase"},
		{"with spaces", "with-spaces"},
		{"org/team-slug", "org-team-slug"},
		{"user@example.com", "user-example-com"},
		{"--leading-trailing--", "leading-trailing"},
		{"multiple---dashes", "multiple-dashes"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeSecretName(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeSecretName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeLabelValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with spaces", "with-spaces"},
		{"org/team-slug", "org-team-slug"},
		{"--leading-trailing--", "leading-trailing"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeLabelValue(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeLabelValue(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
