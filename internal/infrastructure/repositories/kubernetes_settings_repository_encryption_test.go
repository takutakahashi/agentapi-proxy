package repositories

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestKubernetesSettingsRepository_EncryptDecrypt_Bedrock(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create settings with Bedrock credentials
	settings := entities.NewSettings("test-encrypt")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetModel("anthropic.claude-sonnet-4-20250514-v1:0")
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	bedrock.SetRoleARN("arn:aws:iam::123456789012:role/ExampleRole")
	bedrock.SetProfile("production")
	settings.SetBedrock(bedrock)

	// Save
	err := repo.Save(ctx, settings)
	require.NoError(t, err)

	// Load
	loaded, err := repo.FindByName(ctx, "test-encrypt")
	require.NoError(t, err)

	// Verify credentials are preserved (Noop encryption returns plaintext)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", loaded.Bedrock().AccessKeyID())
	assert.Equal(t, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", loaded.Bedrock().SecretAccessKey())
	assert.Equal(t, "arn:aws:iam::123456789012:role/ExampleRole", loaded.Bedrock().RoleARN())
	assert.Equal(t, "production", loaded.Bedrock().Profile())
}

func TestKubernetesSettingsRepository_EncryptDecrypt_OAuthToken(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create settings with OAuth token
	settings := entities.NewSettings("test-oauth")
	settings.SetClaudeCodeOAuthToken("test-oauth-token-12345")

	// Save
	err := repo.Save(ctx, settings)
	require.NoError(t, err)

	// Load
	loaded, err := repo.FindByName(ctx, "test-oauth")
	require.NoError(t, err)

	// Verify token is preserved
	assert.Equal(t, "test-oauth-token-12345", loaded.ClaudeCodeOAuthToken())
}

func TestKubernetesSettingsRepository_EncryptDecrypt_MCPServers(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create settings with MCP servers
	settings := entities.NewSettings("test-mcp")
	mcpServers := entities.NewMCPServersSettings()

	server := entities.NewMCPServer("github", "stdio")
	server.SetCommand("mcp-server-github")
	server.SetEnv(map[string]string{
		"GITHUB_TOKEN": "ghp_secrettoken123",
		"GITHUB_ORG":   "myorg",
		"API_KEY":      "secret-api-key-456",
	})
	server.SetHeaders(map[string]string{
		"Authorization": "Bearer secret-bearer-token",
		"X-API-Key":     "another-secret-key",
	})
	mcpServers.SetServer("github", server)

	settings.SetMCPServers(mcpServers)

	// Save
	err := repo.Save(ctx, settings)
	require.NoError(t, err)

	// Load
	loaded, err := repo.FindByName(ctx, "test-mcp")
	require.NoError(t, err)

	// Verify MCP server env and headers are preserved
	loadedServer := loaded.MCPServers().Servers()["github"]
	require.NotNil(t, loadedServer)

	assert.Equal(t, "ghp_secrettoken123", loadedServer.Env()["GITHUB_TOKEN"])
	assert.Equal(t, "myorg", loadedServer.Env()["GITHUB_ORG"])
	assert.Equal(t, "secret-api-key-456", loadedServer.Env()["API_KEY"])

	assert.Equal(t, "Bearer secret-bearer-token", loadedServer.Headers()["Authorization"])
	assert.Equal(t, "another-secret-key", loadedServer.Headers()["X-API-Key"])
}

func TestKubernetesSettingsRepository_EncryptDecrypt_AllFields(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create settings with all sensitive fields
	settings := entities.NewSettings("test-all")

	// Bedrock
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetAccessKeyID("AKIAIOSFODNN7EXAMPLE")
	bedrock.SetSecretAccessKey("wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	settings.SetBedrock(bedrock)

	// OAuth
	settings.SetClaudeCodeOAuthToken("oauth-token-123")

	// MCP Servers
	mcpServers := entities.NewMCPServersSettings()
	server := entities.NewMCPServer("test", "stdio")
	server.SetCommand("test-command")
	server.SetEnv(map[string]string{"SECRET_KEY": "secret-value"})
	server.SetHeaders(map[string]string{"Auth": "bearer-token"})
	mcpServers.SetServer("test", server)
	settings.SetMCPServers(mcpServers)

	// Save
	err := repo.Save(ctx, settings)
	require.NoError(t, err)

	// Load
	loaded, err := repo.FindByName(ctx, "test-all")
	require.NoError(t, err)

	// Verify all fields
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", loaded.Bedrock().AccessKeyID())
	assert.Equal(t, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", loaded.Bedrock().SecretAccessKey())
	assert.Equal(t, "oauth-token-123", loaded.ClaudeCodeOAuthToken())

	loadedServer := loaded.MCPServers().Servers()["test"]
	assert.Equal(t, "secret-value", loadedServer.Env()["SECRET_KEY"])
	assert.Equal(t, "bearer-token", loadedServer.Headers()["Auth"])
}

func TestKubernetesSettingsRepository_EmptyValues(t *testing.T) {
	client := fake.NewSimpleClientset()
	repo := NewKubernetesSettingsRepository(client, "default")
	ctx := context.Background()

	// Create settings with empty values
	settings := entities.NewSettings("test-empty")
	bedrock := entities.NewBedrockSettings(true)
	bedrock.SetAccessKeyID("")
	bedrock.SetSecretAccessKey("")
	settings.SetBedrock(bedrock)
	settings.SetClaudeCodeOAuthToken("")

	// Save
	err := repo.Save(ctx, settings)
	require.NoError(t, err)

	// Load
	loaded, err := repo.FindByName(ctx, "test-empty")
	require.NoError(t, err)

	// Verify empty values are preserved
	assert.Equal(t, "", loaded.Bedrock().AccessKeyID())
	assert.Equal(t, "", loaded.Bedrock().SecretAccessKey())
	assert.Equal(t, "", loaded.ClaudeCodeOAuthToken())
}
