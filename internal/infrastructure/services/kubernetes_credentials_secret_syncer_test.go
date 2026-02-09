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
	settings.SetAuthMode(entities.AuthModeBedrock) // Set auth mode to enable credential syncing

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	// Verify Secret was created
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-test-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Verify labels
	if secret.Labels[LabelEnv] != "true" {
		t.Errorf("Expected label %s to be 'true'", LabelEnv)
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
	if string(secret.Data["CLAUDE_CODE_ATTRIBUTION_HEADER"]) != "0" {
		t.Error("Expected CLAUDE_CODE_ATTRIBUTION_HEADER to be '0'")
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
	settings.SetAuthMode(entities.AuthModeBedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync initial: %v", err)
	}

	// Update settings
	newBedrock := entities.NewBedrockSettings(true)
	newBedrock.SetAccessKeyID("UPDATED_KEY")
	settings.SetBedrock(newBedrock)
	settings.SetAuthMode(entities.AuthModeBedrock)

	err = syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync update: %v", err)
	}

	// Verify Secret was updated
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-update-user", metav1.GetOptions{})
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
	settings.SetAuthMode(entities.AuthModeBedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-all-fields", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	expectedData := map[string]string{
		"CLAUDE_CODE_USE_BEDROCK":        "1",
		"ANTHROPIC_MODEL":                "anthropic.claude-opus-4-20250514-v1:0",
		"AWS_ACCESS_KEY_ID":              "AKIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY":          "secret-key",
		"AWS_ROLE_ARN":                   "arn:aws:iam::123456789012:role/ExampleRole",
		"AWS_PROFILE":                    "production",
		"CLAUDE_CODE_ATTRIBUTION_HEADER": "0",
	}

	for key, expected := range expectedData {
		if string(secret.Data[key]) != expected {
			t.Errorf("Expected %s to be '%s', got '%s'", key, expected, string(secret.Data[key]))
		}
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_LegacyBedrockWithoutAuthMode(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings with Bedrock enabled but without auth_mode set (legacy behavior)
	settings := entities.NewSettings("legacy-bedrock")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	settings.SetBedrock(bedrock)
	// Note: auth_mode is NOT set - this tests backward compatibility

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-legacy-bedrock", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Should have Bedrock credentials due to legacy fallback behavior
	if string(secret.Data["CLAUDE_CODE_USE_BEDROCK"]) != "1" {
		t.Errorf("Expected CLAUDE_CODE_USE_BEDROCK to be '1' for legacy behavior, got '%s'", string(secret.Data["CLAUDE_CODE_USE_BEDROCK"]))
	}
	if string(secret.Data["ANTHROPIC_MODEL"]) != "anthropic.claude-sonnet-4-20250514-v1:0" {
		t.Errorf("Expected ANTHROPIC_MODEL to match, got '%s'", string(secret.Data["ANTHROPIC_MODEL"]))
	}
	if string(secret.Data["AWS_ACCESS_KEY_ID"]) != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("Expected AWS_ACCESS_KEY_ID to match, got '%s'", string(secret.Data["AWS_ACCESS_KEY_ID"]))
	}
	if string(secret.Data["AWS_SECRET_ACCESS_KEY"]) != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("Expected AWS_SECRET_ACCESS_KEY to match, got '%s'", string(secret.Data["AWS_SECRET_ACCESS_KEY"]))
	}
	if string(secret.Data["CLAUDE_CODE_ATTRIBUTION_HEADER"]) != "0" {
		t.Errorf("Expected CLAUDE_CODE_ATTRIBUTION_HEADER to be '0', got '%s'", string(secret.Data["CLAUDE_CODE_ATTRIBUTION_HEADER"]))
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_NoAuthModeDisabledBedrock(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings with Bedrock disabled and no auth_mode
	settings := entities.NewSettings("no-auth-disabled")
	bedrock := entities.NewBedrockSettings(false) // Bedrock disabled
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	settings.SetBedrock(bedrock)
	// Note: auth_mode is not set

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-no-auth-disabled", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Should not have any credential keys when Bedrock is disabled and no auth_mode
	if _, ok := secret.Data["CLAUDE_CODE_USE_BEDROCK"]; ok {
		t.Error("Expected no CLAUDE_CODE_USE_BEDROCK key when Bedrock is disabled")
	}
	if _, ok := secret.Data["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Error("Expected no CLAUDE_CODE_OAUTH_TOKEN key when no OAuth token is set")
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_OAuthMode(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings with OAuth token
	settings := entities.NewSettings("oauth-user")
	settings.SetClaudeCodeOAuthToken("sk-ant-oauth-token-example")
	settings.SetAuthMode(entities.AuthModeOAuth)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-oauth-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Verify OAuth token is set and Bedrock is disabled
	if string(secret.Data["CLAUDE_CODE_OAUTH_TOKEN"]) != "sk-ant-oauth-token-example" {
		t.Errorf("Expected CLAUDE_CODE_OAUTH_TOKEN to be 'sk-ant-oauth-token-example', got '%s'", string(secret.Data["CLAUDE_CODE_OAUTH_TOKEN"]))
	}
	if string(secret.Data["CLAUDE_CODE_USE_BEDROCK"]) != "0" {
		t.Errorf("Expected CLAUDE_CODE_USE_BEDROCK to be '0', got '%s'", string(secret.Data["CLAUDE_CODE_USE_BEDROCK"]))
	}
	// Verify Bedrock credentials are set to empty (to override team settings)
	if string(secret.Data["ANTHROPIC_MODEL"]) != "" {
		t.Errorf("Expected ANTHROPIC_MODEL to be empty, got '%s'", string(secret.Data["ANTHROPIC_MODEL"]))
	}
	if string(secret.Data["AWS_ACCESS_KEY_ID"]) != "" {
		t.Errorf("Expected AWS_ACCESS_KEY_ID to be empty, got '%s'", string(secret.Data["AWS_ACCESS_KEY_ID"]))
	}
	if string(secret.Data["AWS_SECRET_ACCESS_KEY"]) != "" {
		t.Errorf("Expected AWS_SECRET_ACCESS_KEY to be empty, got '%s'", string(secret.Data["AWS_SECRET_ACCESS_KEY"]))
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_OAuthModeWithBedrock(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings with both OAuth token and Bedrock credentials
	// but auth_mode is set to OAuth
	settings := entities.NewSettings("oauth-with-bedrock")
	settings.SetClaudeCodeOAuthToken("sk-ant-oauth-token-example")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	settings.SetBedrock(bedrock)
	settings.SetAuthMode(entities.AuthModeOAuth) // OAuth takes priority

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-oauth-with-bedrock", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Verify OAuth token is set
	if string(secret.Data["CLAUDE_CODE_OAUTH_TOKEN"]) != "sk-ant-oauth-token-example" {
		t.Errorf("Expected CLAUDE_CODE_OAUTH_TOKEN to be set, got '%s'", string(secret.Data["CLAUDE_CODE_OAUTH_TOKEN"]))
	}
	if string(secret.Data["CLAUDE_CODE_USE_BEDROCK"]) != "0" {
		t.Errorf("Expected CLAUDE_CODE_USE_BEDROCK to be '0', got '%s'", string(secret.Data["CLAUDE_CODE_USE_BEDROCK"]))
	}

	// Verify Bedrock credentials are explicitly set to empty (to override team settings)
	if string(secret.Data["ANTHROPIC_MODEL"]) != "" {
		t.Errorf("Expected ANTHROPIC_MODEL to be empty in OAuth mode, got '%s'", string(secret.Data["ANTHROPIC_MODEL"]))
	}
	if string(secret.Data["AWS_ACCESS_KEY_ID"]) != "" {
		t.Errorf("Expected AWS_ACCESS_KEY_ID to be empty in OAuth mode, got '%s'", string(secret.Data["AWS_ACCESS_KEY_ID"]))
	}
	if string(secret.Data["AWS_SECRET_ACCESS_KEY"]) != "" {
		t.Errorf("Expected AWS_SECRET_ACCESS_KEY to be empty in OAuth mode, got '%s'", string(secret.Data["AWS_SECRET_ACCESS_KEY"]))
	}
	if string(secret.Data["AWS_ROLE_ARN"]) != "" {
		t.Errorf("Expected AWS_ROLE_ARN to be empty in OAuth mode, got '%s'", string(secret.Data["AWS_ROLE_ARN"]))
	}
	if string(secret.Data["AWS_PROFILE"]) != "" {
		t.Errorf("Expected AWS_PROFILE to be empty in OAuth mode, got '%s'", string(secret.Data["AWS_PROFILE"]))
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

func TestKubernetesCredentialsSecretSyncer_Sync_SkipsExternalSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create an external secret (not managed by settings)
	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-env-external-user",
			Namespace: "default",
			Labels: map[string]string{
				LabelEnv: "true",
				// No LabelManagedBy label
			},
		},
		Data: map[string][]byte{
			"CUSTOM_KEY": []byte("custom-value"),
		},
	}
	_, err := client.CoreV1().Secrets("default").Create(ctx, externalSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create external secret: %v", err)
	}

	// Try to sync settings for the same user
	settings := entities.NewSettings("external-user")
	bedrock := entities.NewBedrockSettings(true)
	settings.SetBedrock(bedrock)
	settings.SetAuthMode(entities.AuthModeBedrock)

	err = syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Sync should not fail for external secret: %v", err)
	}

	// Verify external secret was not modified
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-external-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// External secret should still have original data
	if string(secret.Data["CUSTOM_KEY"]) != "custom-value" {
		t.Error("External secret should not be modified")
	}
	if _, ok := secret.Data["CLAUDE_CODE_USE_BEDROCK"]; ok {
		t.Error("External secret should not have Bedrock data")
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
	settings.SetAuthMode(entities.AuthModeBedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	// Verify Secret exists
	_, err = client.CoreV1().Secrets("default").Get(ctx, "agent-env-delete-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret should exist: %v", err)
	}

	// Delete
	err = syncer.Delete(ctx, "delete-user")
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Verify Secret was deleted
	_, err = client.CoreV1().Secrets("default").Get(ctx, "agent-env-delete-user", metav1.GetOptions{})
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

func TestKubernetesCredentialsSecretSyncer_Delete_SkipsExternalSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create an external secret
	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-env-external-delete",
			Namespace: "default",
			Labels: map[string]string{
				LabelEnv: "true",
				// No LabelManagedBy = "settings"
			},
		},
		Data: map[string][]byte{
			"CUSTOM_KEY": []byte("custom-value"),
		},
	}
	_, err := client.CoreV1().Secrets("default").Create(ctx, externalSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create external secret: %v", err)
	}

	// Try to delete
	err = syncer.Delete(ctx, "external-delete")
	if err != nil {
		t.Fatalf("Delete should not fail: %v", err)
	}

	// Verify external secret still exists
	_, err = client.CoreV1().Secrets("default").Get(ctx, "agent-env-external-delete", metav1.GetOptions{})
	if err != nil {
		t.Error("External secret should not be deleted")
	}
}

func TestKubernetesCredentialsSecretSyncer_ResyncSecretsForOAuthMode(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create an OAuth mode secret without Bedrock override values (simulating old version)
	oldOAuthSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-env-old-oauth-user",
			Namespace: "default",
			Labels: map[string]string{
				LabelEnv:       "true",
				LabelEnvName:   "old-oauth-user",
				LabelManagedBy: "settings",
			},
		},
		Data: map[string][]byte{
			"CLAUDE_CODE_OAUTH_TOKEN": []byte("sk-ant-oauth-token"),
			"CLAUDE_CODE_USE_BEDROCK": []byte("0"),
			// Missing Bedrock override values
		},
	}
	_, err := client.CoreV1().Secrets("default").Create(ctx, oldOAuthSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create old OAuth secret: %v", err)
	}

	// Create a Bedrock mode secret (should not be touched)
	bedrockSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-env-bedrock-user",
			Namespace: "default",
			Labels: map[string]string{
				LabelEnv:       "true",
				LabelEnvName:   "bedrock-user",
				LabelManagedBy: "settings",
			},
		},
		Data: map[string][]byte{
			"CLAUDE_CODE_USE_BEDROCK": []byte("1"),
			"ANTHROPIC_MODEL":         []byte("anthropic.claude-sonnet-4-20250514-v1:0"),
		},
	}
	_, err = client.CoreV1().Secrets("default").Create(ctx, bedrockSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create Bedrock secret: %v", err)
	}

	// Create a new OAuth mode secret with all override values (should not be touched)
	newOAuthSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-env-new-oauth-user",
			Namespace: "default",
			Labels: map[string]string{
				LabelEnv:       "true",
				LabelEnvName:   "new-oauth-user",
				LabelManagedBy: "settings",
			},
		},
		Data: map[string][]byte{
			"CLAUDE_CODE_OAUTH_TOKEN": []byte("sk-ant-oauth-token"),
			"CLAUDE_CODE_USE_BEDROCK": []byte("0"),
			"ANTHROPIC_MODEL":         []byte(""),
			"AWS_ACCESS_KEY_ID":       []byte(""),
			"AWS_SECRET_ACCESS_KEY":   []byte(""),
			"AWS_ROLE_ARN":            []byte(""),
			"AWS_PROFILE":             []byte(""),
		},
	}
	_, err = client.CoreV1().Secrets("default").Create(ctx, newOAuthSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create new OAuth secret: %v", err)
	}

	// Run resync
	err = syncer.ResyncSecretsForOAuthMode(ctx)
	if err != nil {
		t.Fatalf("Resync failed: %v", err)
	}

	// Verify old OAuth secret was updated
	oldSecret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-old-oauth-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get old OAuth secret: %v", err)
	}
	if string(oldSecret.Data["ANTHROPIC_MODEL"]) != "" {
		t.Errorf("Expected ANTHROPIC_MODEL to be empty, got '%s'", string(oldSecret.Data["ANTHROPIC_MODEL"]))
	}
	if string(oldSecret.Data["AWS_ACCESS_KEY_ID"]) != "" {
		t.Errorf("Expected AWS_ACCESS_KEY_ID to be empty, got '%s'", string(oldSecret.Data["AWS_ACCESS_KEY_ID"]))
	}
	if string(oldSecret.Data["AWS_SECRET_ACCESS_KEY"]) != "" {
		t.Errorf("Expected AWS_SECRET_ACCESS_KEY to be empty, got '%s'", string(oldSecret.Data["AWS_SECRET_ACCESS_KEY"]))
	}
	if string(oldSecret.Data["AWS_ROLE_ARN"]) != "" {
		t.Errorf("Expected AWS_ROLE_ARN to be empty, got '%s'", string(oldSecret.Data["AWS_ROLE_ARN"]))
	}
	if string(oldSecret.Data["AWS_PROFILE"]) != "" {
		t.Errorf("Expected AWS_PROFILE to be empty, got '%s'", string(oldSecret.Data["AWS_PROFILE"]))
	}

	// Verify Bedrock secret was not modified
	bedrockSecretAfter, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-bedrock-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get Bedrock secret: %v", err)
	}
	if string(bedrockSecretAfter.Data["ANTHROPIC_MODEL"]) != "anthropic.claude-sonnet-4-20250514-v1:0" {
		t.Errorf("Bedrock secret should not be modified")
	}
	if _, exists := bedrockSecretAfter.Data["AWS_ACCESS_KEY_ID"]; exists {
		t.Errorf("Bedrock secret should not have empty override values added")
	}

	// Verify new OAuth secret was not modified (already has override values)
	newSecretAfter, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-new-oauth-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get new OAuth secret: %v", err)
	}
	if newSecretAfter.ResourceVersion != newOAuthSecret.ResourceVersion {
		t.Errorf("New OAuth secret should not be modified")
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

func TestKubernetesCredentialsSecretSyncer_ResyncSecretsForAttributionHeader(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")

	// Create test secrets with different states
	testSecrets := []*corev1.Secret{
		// Bedrock secret without CLAUDE_CODE_ATTRIBUTION_HEADER (should be updated)
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agent-env-bedrock-without-header",
				Namespace: "default",
				Labels: map[string]string{
					LabelManagedBy: "settings",
				},
			},
			Data: map[string][]byte{
				"CLAUDE_CODE_USE_BEDROCK": []byte("1"),
			},
		},
		// Bedrock secret with wrong CLAUDE_CODE_ATTRIBUTION_HEADER value (should be updated)
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agent-env-bedrock-wrong-value",
				Namespace: "default",
				Labels: map[string]string{
					LabelManagedBy: "settings",
				},
			},
			Data: map[string][]byte{
				"CLAUDE_CODE_USE_BEDROCK":        []byte("1"),
				"CLAUDE_CODE_ATTRIBUTION_HEADER": []byte("1"),
			},
		},
		// Bedrock secret with correct CLAUDE_CODE_ATTRIBUTION_HEADER (should be skipped)
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agent-env-bedrock-correct",
				Namespace: "default",
				Labels: map[string]string{
					LabelManagedBy: "settings",
				},
			},
			Data: map[string][]byte{
				"CLAUDE_CODE_USE_BEDROCK":        []byte("1"),
				"CLAUDE_CODE_ATTRIBUTION_HEADER": []byte("0"),
			},
		},
		// OAuth secret (should be skipped - no attribution header should be added)
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "agent-env-oauth",
				Namespace: "default",
				Labels: map[string]string{
					LabelManagedBy: "settings",
				},
			},
			Data: map[string][]byte{
				"CLAUDE_CODE_USE_BEDROCK": []byte("0"),
				"CLAUDE_CODE_OAUTH_TOKEN": []byte("test-token"),
			},
		},
		// Secret with different prefix (should be skipped)
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-secret",
				Namespace: "default",
				Labels: map[string]string{
					LabelManagedBy: "settings",
				},
			},
			Data: map[string][]byte{
				"SOME_KEY": []byte("value"),
			},
		},
	}

	for _, secret := range testSecrets {
		_, err := client.CoreV1().Secrets("default").Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Failed to create test secret: %v", err)
		}
	}

	// Run resync
	err := syncer.ResyncSecretsForAttributionHeader(ctx)
	if err != nil {
		t.Fatalf("ResyncSecretsForAttributionHeader failed: %v", err)
	}

	// Verify results
	tests := []struct {
		name                string
		expectedHeaderValue string
		shouldHaveHeader    bool
	}{
		{"agent-env-bedrock-without-header", "0", true},
		{"agent-env-bedrock-wrong-value", "0", true},
		{"agent-env-bedrock-correct", "0", true},
		{"agent-env-oauth", "", false}, // OAuth mode should not have attribution header
		{"other-secret", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, err := client.CoreV1().Secrets("default").Get(ctx, tt.name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get secret: %v", err)
			}

			headerValue, exists := secret.Data["CLAUDE_CODE_ATTRIBUTION_HEADER"]
			if tt.shouldHaveHeader {
				if !exists {
					t.Errorf("Expected CLAUDE_CODE_ATTRIBUTION_HEADER to exist")
				} else if string(headerValue) != tt.expectedHeaderValue {
					t.Errorf("Expected CLAUDE_CODE_ATTRIBUTION_HEADER to be %q, got %q", tt.expectedHeaderValue, string(headerValue))
				}
			} else {
				if exists {
					t.Errorf("Expected CLAUDE_CODE_ATTRIBUTION_HEADER to not exist")
				}
			}
		})
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_OAuthMode_NoAttributionHeader(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings with OAuth token
	settings := entities.NewSettings("oauth-user")
	settings.SetClaudeCodeOAuthToken("test-oauth-token")
	settings.SetAuthMode(entities.AuthModeOAuth)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	// Verify Secret was created
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-oauth-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Verify CLAUDE_CODE_ATTRIBUTION_HEADER is NOT set for OAuth mode
	if _, exists := secret.Data["CLAUDE_CODE_ATTRIBUTION_HEADER"]; exists {
		t.Error("Expected CLAUDE_CODE_ATTRIBUTION_HEADER to NOT be set for OAuth mode")
	}
}

func TestKubernetesCredentialsSecretSyncer_Sync_BedrockMode_HasAttributionHeader(t *testing.T) {
	client := fake.NewSimpleClientset()
	syncer := NewKubernetesCredentialsSecretSyncer(client, "default")
	ctx := context.Background()

	// Create settings with Bedrock credentials
	settings := entities.NewSettings("bedrock-user")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetAccessKeyID("test-access-key")
	bedrock.SetSecretAccessKey("test-secret-key")
	bedrock.SetModel("anthropic.claude-3-5-sonnet-20241022-v2:0")
	settings.SetBedrock(bedrock)
	settings.SetAuthMode(entities.AuthModeBedrock)

	err := syncer.Sync(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to sync: %v", err)
	}

	// Verify Secret was created
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agent-env-bedrock-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	// Verify CLAUDE_CODE_ATTRIBUTION_HEADER is set to "0" for Bedrock mode
	if string(secret.Data["CLAUDE_CODE_ATTRIBUTION_HEADER"]) != "0" {
		t.Error("Expected CLAUDE_CODE_ATTRIBUTION_HEADER to be '0' for Bedrock mode")
	}
}
