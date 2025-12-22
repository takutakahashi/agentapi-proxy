package repositories

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestKubernetesSettingsRepository_Save(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")

	settings := entities.NewSettings("test-user")
	bedrock := entities.NewBedrockSettings(true, "us-east-1")
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	settings.SetBedrock(bedrock)

	ctx := context.Background()
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Verify Secret was created
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-test-user", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}

	if secret.Labels[LabelSettings] != "true" {
		t.Errorf("Expected label %s to be 'true'", LabelSettings)
	}
	if _, ok := secret.Data[SecretKeySettings]; !ok {
		t.Error("Expected settings.json key in secret data")
	}
}

func TestKubernetesSettingsRepository_Save_VerifySecretContent(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")

	settings := entities.NewSettings("verify-content")
	bedrock := entities.NewBedrockSettings(true, "ap-northeast-1")
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	bedrock.SetRoleARN("arn:aws:iam::123456789012:role/ExampleRole")
	bedrock.SetProfile("production")
	settings.SetBedrock(bedrock)

	ctx := context.Background()
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Verify Secret was created in Kubernetes
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-verify-content", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get secret from Kubernetes: %v", err)
	}

	// Verify Secret labels
	if secret.Labels[LabelSettings] != "true" {
		t.Errorf("Expected label %s to be 'true', got '%s'", LabelSettings, secret.Labels[LabelSettings])
	}
	if secret.Labels[LabelSettingsName] != "verify-content" {
		t.Errorf("Expected label %s to be 'verify-content', got '%s'", LabelSettingsName, secret.Labels[LabelSettingsName])
	}

	// Verify Secret type
	if secret.Type != corev1.SecretTypeOpaque {
		t.Errorf("Expected Secret type to be Opaque, got '%s'", secret.Type)
	}

	// Verify Secret data contains settings.json
	data, ok := secret.Data[SecretKeySettings]
	if !ok {
		t.Fatal("Expected settings.json key in secret data")
	}

	// Parse and verify JSON content
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse secret data as JSON: %v", err)
	}

	// Verify name
	if parsed["name"] != "verify-content" {
		t.Errorf("Expected name 'verify-content' in JSON, got '%v'", parsed["name"])
	}

	// Verify bedrock section exists
	bedrockData, ok := parsed["bedrock"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected bedrock section in JSON")
	}

	// Verify all bedrock fields
	if bedrockData["enabled"] != true {
		t.Errorf("Expected enabled=true, got %v", bedrockData["enabled"])
	}
	if bedrockData["region"] != "ap-northeast-1" {
		t.Errorf("Expected region='ap-northeast-1', got '%v'", bedrockData["region"])
	}
	if bedrockData["model"] != "anthropic.claude-sonnet-4-20250514-v1:0" {
		t.Errorf("Expected model='anthropic.claude-sonnet-4-20250514-v1:0', got '%v'", bedrockData["model"])
	}
	if bedrockData["access_key_id"] != "AKIAIOSFODNN7EXAMPLE" {
		t.Errorf("Expected access_key_id='AKIAIOSFODNN7EXAMPLE', got '%v'", bedrockData["access_key_id"])
	}
	if bedrockData["secret_access_key"] != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("Expected secret_access_key to be preserved, got '%v'", bedrockData["secret_access_key"])
	}
	if bedrockData["role_arn"] != "arn:aws:iam::123456789012:role/ExampleRole" {
		t.Errorf("Expected role_arn to be preserved, got '%v'", bedrockData["role_arn"])
	}
	if bedrockData["profile"] != "production" {
		t.Errorf("Expected profile='production', got '%v'", bedrockData["profile"])
	}

	// Verify timestamps exist
	if parsed["created_at"] == nil {
		t.Error("Expected created_at in JSON")
	}
	if parsed["updated_at"] == nil {
		t.Error("Expected updated_at in JSON")
	}
}

func TestKubernetesSettingsRepository_SaveUpdate(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create initial settings
	settings := entities.NewSettings("test-user")
	settings.SetBedrock(entities.NewBedrockSettings(true, "us-east-1"))
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save initial settings: %v", err)
	}

	// Update settings
	settings.SetBedrock(entities.NewBedrockSettings(true, "ap-northeast-1"))
	err = repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to update settings: %v", err)
	}

	// Verify updated
	loaded, err := repo.FindByName(ctx, "test-user")
	if err != nil {
		t.Fatalf("Failed to load settings: %v", err)
	}
	if loaded.Bedrock().Region() != "ap-northeast-1" {
		t.Errorf("Expected region 'ap-northeast-1', got '%s'", loaded.Bedrock().Region())
	}
}

func TestKubernetesSettingsRepository_SaveUpdate_AllFields(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create initial settings with all fields
	settings := entities.NewSettings("update-all-fields")
	bedrock := entities.NewBedrockSettings(true, "us-east-1")
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7INITIAL")
	bedrock.SetSecretAccessKey("initial-secret-key")
	bedrock.SetRoleARN("arn:aws:iam::111111111111:role/InitialRole")
	bedrock.SetProfile("initial-profile")
	settings.SetBedrock(bedrock)

	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save initial settings: %v", err)
	}

	// Verify initial save in Kubernetes Secret
	secret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-update-all-fields", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get initial secret: %v", err)
	}
	if secret.Labels[LabelSettings] != "true" {
		t.Error("Expected settings label on initial secret")
	}

	// Update all fields
	updatedBedrock := entities.NewBedrockSettings(true, "ap-northeast-1")
	updatedBedrock.SetModel("anthropic.claude-opus-4-20250514-v1:0")
	updatedBedrock.SetAccessKeyID("AKIAIOSFODNN7UPDATED")
	updatedBedrock.SetSecretAccessKey("updated-secret-key")
	updatedBedrock.SetRoleARN("arn:aws:iam::222222222222:role/UpdatedRole")
	updatedBedrock.SetProfile("updated-profile")
	settings.SetBedrock(updatedBedrock)

	err = repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to update settings: %v", err)
	}

	// Verify Secret was updated in Kubernetes (not recreated)
	updatedSecret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-update-all-fields", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}

	// Parse and verify updated content
	data, ok := updatedSecret.Data[SecretKeySettings]
	if !ok {
		t.Fatal("Expected settings.json key in updated secret")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Failed to parse updated secret data: %v", err)
	}

	bedrockData, ok := parsed["bedrock"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected bedrock section in updated JSON")
	}

	// Verify all updated values
	if bedrockData["region"] != "ap-northeast-1" {
		t.Errorf("Expected updated region 'ap-northeast-1', got '%v'", bedrockData["region"])
	}
	if bedrockData["model"] != "anthropic.claude-opus-4-20250514-v1:0" {
		t.Errorf("Expected updated model 'anthropic.claude-opus-4-20250514-v1:0', got '%v'", bedrockData["model"])
	}
	if bedrockData["access_key_id"] != "AKIAIOSFODNN7UPDATED" {
		t.Errorf("Expected updated access_key_id 'AKIAIOSFODNN7UPDATED', got '%v'", bedrockData["access_key_id"])
	}
	if bedrockData["secret_access_key"] != "updated-secret-key" {
		t.Errorf("Expected updated secret_access_key, got '%v'", bedrockData["secret_access_key"])
	}
	if bedrockData["role_arn"] != "arn:aws:iam::222222222222:role/UpdatedRole" {
		t.Errorf("Expected updated role_arn, got '%v'", bedrockData["role_arn"])
	}
	if bedrockData["profile"] != "updated-profile" {
		t.Errorf("Expected updated profile 'updated-profile', got '%v'", bedrockData["profile"])
	}

	// Verify through FindByName as well
	loaded, err := repo.FindByName(ctx, "update-all-fields")
	if err != nil {
		t.Fatalf("Failed to load settings after update: %v", err)
	}

	if loaded.Bedrock().Region() != "ap-northeast-1" {
		t.Errorf("FindByName: Expected region 'ap-northeast-1', got '%s'", loaded.Bedrock().Region())
	}
	if loaded.Bedrock().Model() != "anthropic.claude-opus-4-20250514-v1:0" {
		t.Errorf("FindByName: Expected model 'anthropic.claude-opus-4-20250514-v1:0', got '%s'", loaded.Bedrock().Model())
	}
	if loaded.Bedrock().AccessKeyID() != "AKIAIOSFODNN7UPDATED" {
		t.Errorf("FindByName: Expected access_key_id 'AKIAIOSFODNN7UPDATED', got '%s'", loaded.Bedrock().AccessKeyID())
	}
	if loaded.Bedrock().SecretAccessKey() != "updated-secret-key" {
		t.Errorf("FindByName: Expected secret_access_key 'updated-secret-key', got '%s'", loaded.Bedrock().SecretAccessKey())
	}
	if loaded.Bedrock().RoleARN() != "arn:aws:iam::222222222222:role/UpdatedRole" {
		t.Errorf("FindByName: Expected role_arn to be updated, got '%s'", loaded.Bedrock().RoleARN())
	}
	if loaded.Bedrock().Profile() != "updated-profile" {
		t.Errorf("FindByName: Expected profile 'updated-profile', got '%s'", loaded.Bedrock().Profile())
	}
}

func TestKubernetesSettingsRepository_SaveUpdate_VerifySecretOverwritten(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create initial settings
	settings := entities.NewSettings("overwrite-test")
	settings.SetBedrock(entities.NewBedrockSettings(true, "us-east-1"))
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save initial settings: %v", err)
	}

	// Get initial Secret version
	initialSecret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-overwrite-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get initial secret: %v", err)
	}
	initialData := string(initialSecret.Data[SecretKeySettings])

	// Update settings
	settings.SetBedrock(entities.NewBedrockSettings(true, "eu-central-1"))
	err = repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to update settings: %v", err)
	}

	// Verify Secret was updated (not recreated with different name)
	updatedSecret, err := client.CoreV1().Secrets("default").Get(ctx, "agentapi-settings-overwrite-test", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get updated secret: %v", err)
	}
	updatedData := string(updatedSecret.Data[SecretKeySettings])

	// Data should be different
	if initialData == updatedData {
		t.Error("Expected Secret data to be different after update")
	}

	// Parse updated data and verify region
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(updatedData), &parsed); err != nil {
		t.Fatalf("Failed to parse updated data: %v", err)
	}
	bedrockData := parsed["bedrock"].(map[string]interface{})
	if bedrockData["region"] != "eu-central-1" {
		t.Errorf("Expected updated region 'eu-central-1', got '%v'", bedrockData["region"])
	}

	// Verify only one Secret exists with this prefix
	secrets, err := client.CoreV1().Secrets("default").List(ctx, metav1.ListOptions{
		LabelSelector: LabelSettings + "=true",
	})
	if err != nil {
		t.Fatalf("Failed to list secrets: %v", err)
	}

	overwriteSecretCount := 0
	for _, s := range secrets.Items {
		if s.Name == "agentapi-settings-overwrite-test" {
			overwriteSecretCount++
		}
	}
	if overwriteSecretCount != 1 {
		t.Errorf("Expected exactly 1 Secret for overwrite-test, got %d", overwriteSecretCount)
	}
}

func TestKubernetesSettingsRepository_FindByName(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Save settings
	settings := entities.NewSettings("find-test")
	bedrock := entities.NewBedrockSettings(true, "eu-west-1")
	bedrock.SetSecretAccessKey("test-secret")
	settings.SetBedrock(bedrock)
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Find settings
	loaded, err := repo.FindByName(ctx, "find-test")
	if err != nil {
		t.Fatalf("Failed to find settings: %v", err)
	}

	if loaded.Name() != "find-test" {
		t.Errorf("Expected name 'find-test', got '%s'", loaded.Name())
	}
	if loaded.Bedrock() == nil {
		t.Fatal("Expected Bedrock settings to be set")
	}
	if loaded.Bedrock().Region() != "eu-west-1" {
		t.Errorf("Expected region 'eu-west-1', got '%s'", loaded.Bedrock().Region())
	}
	if loaded.Bedrock().SecretAccessKey() != "test-secret" {
		t.Errorf("Expected secret access key to be preserved")
	}
}

func TestKubernetesSettingsRepository_FindByName_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	_, err := repo.FindByName(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent settings")
	}
}

func TestKubernetesSettingsRepository_Delete(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Save settings
	settings := entities.NewSettings("delete-test")
	settings.SetBedrock(entities.NewBedrockSettings(true, "us-east-1"))
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Verify exists
	exists, err := repo.Exists(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Failed to check existence: %v", err)
	}
	if !exists {
		t.Error("Expected settings to exist")
	}

	// Delete
	err = repo.Delete(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Failed to delete settings: %v", err)
	}

	// Verify deleted
	exists, err = repo.Exists(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Failed to check existence after delete: %v", err)
	}
	if exists {
		t.Error("Expected settings to be deleted")
	}
}

func TestKubernetesSettingsRepository_Delete_NotFound(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error when deleting nonexistent settings")
	}
}

func TestKubernetesSettingsRepository_List(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Save multiple settings
	for _, name := range []string{"user1", "user2", "team-a"} {
		settings := entities.NewSettings(name)
		settings.SetBedrock(entities.NewBedrockSettings(true, "us-east-1"))
		err := repo.Save(ctx, settings)
		if err != nil {
			t.Fatalf("Failed to save settings for %s: %v", name, err)
		}
	}

	// List all
	settingsList, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list settings: %v", err)
	}

	if len(settingsList) != 3 {
		t.Errorf("Expected 3 settings, got %d", len(settingsList))
	}
}

func TestKubernetesSettingsRepository_List_SkipsInvalid(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create a valid settings secret
	settings := entities.NewSettings("valid")
	settings.SetBedrock(entities.NewBedrockSettings(true, "us-east-1"))
	err := repo.Save(ctx, settings)
	if err != nil {
		t.Fatalf("Failed to save settings: %v", err)
	}

	// Create an invalid secret with the settings label but invalid data
	invalidSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agentapi-settings-invalid",
			Namespace: "default",
			Labels: map[string]string{
				LabelSettings: "true",
			},
		},
		Data: map[string][]byte{
			SecretKeySettings: []byte("invalid json"),
		},
	}
	_, err = client.CoreV1().Secrets("default").Create(ctx, invalidSecret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create invalid secret: %v", err)
	}

	// List should only return valid settings
	settingsList, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list settings: %v", err)
	}

	if len(settingsList) != 1 {
		t.Errorf("Expected 1 valid settings, got %d", len(settingsList))
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
