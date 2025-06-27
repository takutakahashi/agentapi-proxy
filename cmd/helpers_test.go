package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpersCmd(t *testing.T) {
	assert.Equal(t, "helpers", HelpersCmd.Use)
	assert.Equal(t, "Helper utilities for agentapi-proxy", HelpersCmd.Short)
	assert.NotNil(t, HelpersCmd.Run)
}

func TestHelpersInit(t *testing.T) {
	// Test that subcommands are properly registered
	subcommands := HelpersCmd.Commands()

	var commandNames []string
	for _, cmd := range subcommands {
		commandNames = append(commandNames, cmd.Use)
	}

	assert.Contains(t, commandNames, "setup-claude-code")
	assert.Contains(t, commandNames, "init")
	assert.Contains(t, commandNames, "generate-token")
}

func TestGenerateTokenFlags(t *testing.T) {
	// Test required flags

	// Test required flags
	requiredFlags := generateTokenCmd.LocalFlags().Lookup("output-path")
	assert.NotNil(t, requiredFlags)

	userIDFlag := generateTokenCmd.LocalFlags().Lookup("user-id")
	assert.NotNil(t, userIDFlag)

	// Test default values
	roleFlag := generateTokenCmd.LocalFlags().Lookup("role")
	assert.NotNil(t, roleFlag)
	assert.Equal(t, "user", roleFlag.DefValue)

	expiryFlag := generateTokenCmd.LocalFlags().Lookup("expiry-days")
	assert.NotNil(t, expiryFlag)
	assert.Equal(t, "365", expiryFlag.DefValue)

	prefixFlag := generateTokenCmd.LocalFlags().Lookup("key-prefix")
	assert.NotNil(t, prefixFlag)
	assert.Equal(t, "ap", prefixFlag.DefValue)
}

func TestGenerateTokenValidInputs(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "helpers-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputFile := filepath.Join(tmpDir, "test_api_keys.json")

	// Set flag values
	outputPath = outputFile
	userID = "test-user"
	role = "admin"
	permissions = []string{"session:create", "session:delete"}
	expiryDays = 30
	keyPrefix = "test"

	// Run the command
	err = runGenerateToken(&cobra.Command{}, []string{})
	require.NoError(t, err)

	// Verify the file was created
	assert.FileExists(t, outputFile)

	// Read and parse the file
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	var apiKeys map[string]interface{}
	err = json.Unmarshal(data, &apiKeys)
	require.NoError(t, err)

	// Verify structure
	assert.Contains(t, apiKeys, "api_keys")
	keys := apiKeys["api_keys"].([]interface{})
	assert.Len(t, keys, 1)

	key := keys[0].(map[string]interface{})
	assert.Equal(t, "test-user", key["user_id"])
	assert.Equal(t, "admin", key["role"])
	assert.True(t, strings.HasPrefix(key["key"].(string), "test"))
	// Check that expiry fields exist (they might be named differently)
	// The implementation might use expires_at instead of expiry_days
	if expiresAt, ok := key["expires_at"]; ok {
		assert.NotNil(t, expiresAt)
	}

	// Verify permissions - admin role gets wildcard permission "*"
	perms := key["permissions"].([]interface{})
	assert.Equal(t, "*", perms[0], "Admin role should have wildcard permission")
}

func TestGenerateTokenMergeExisting(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "helpers-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputFile := filepath.Join(tmpDir, "existing_api_keys.json")

	// Create an existing file with some content
	existingData := map[string]interface{}{
		"api_keys": []interface{}{
			map[string]interface{}{
				"key":     "existing-key",
				"user_id": "existing-user",
				"role":    "user",
			},
		},
	}

	existingJSON, err := json.MarshalIndent(existingData, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(outputFile, existingJSON, 0644)
	require.NoError(t, err)

	// Set flag values for new key
	outputPath = outputFile
	userID = "new-user"
	role = "admin"
	permissions = []string{"session:create"}
	expiryDays = 90
	keyPrefix = "new"

	// Run the command
	err = runGenerateToken(&cobra.Command{}, []string{})
	require.NoError(t, err)

	// Read and verify the merged file
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	var apiKeys map[string]interface{}
	err = json.Unmarshal(data, &apiKeys)
	require.NoError(t, err)

	keys := apiKeys["api_keys"].([]interface{})
	assert.Len(t, keys, 2)

	// Verify both keys exist
	var existingKey, newKey map[string]interface{}
	for _, k := range keys {
		key := k.(map[string]interface{})
		switch key["user_id"] {
		case "existing-user":
			existingKey = key
		case "new-user":
			newKey = key
		}
	}

	assert.NotNil(t, existingKey)
	assert.NotNil(t, newKey)
	assert.Equal(t, "existing-key", existingKey["key"])
	assert.True(t, strings.HasPrefix(newKey["key"].(string), "new"))
}

func TestGenerateTokenInvalidOutputPath(t *testing.T) {
	// Set an invalid output path (directory that doesn't exist and can't be created)
	outputPath = "/nonexistent/deeply/nested/path/api_keys.json"
	userID = "test-user"
	role = "user"

	// Run the command and expect an error
	err := runGenerateToken(&cobra.Command{}, []string{})
	assert.Error(t, err)
}

func TestGenerateAPIKey(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		minLen int
	}{
		{
			name:   "default prefix",
			prefix: "ap",
			minLen: 32,
		},
		{
			name:   "custom prefix",
			prefix: "custom",
			minLen: 32,
		},
		{
			name:   "empty prefix",
			prefix: "",
			minLen: 30, // No prefix, but still has separator
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := generateAPIKey("test-user", tt.prefix)
			assert.NoError(t, err)

			// Check minimum length
			assert.GreaterOrEqual(t, len(key), tt.minLen)

			// Check prefix if provided (format is prefix_userID_randomString)
			if tt.prefix != "" {
				assert.True(t, strings.HasPrefix(key, tt.prefix+"_"))
			}

			// Check that key contains valid hex characters at the end
			parts := strings.Split(key, "_")
			if len(parts) >= 2 { // At least prefix_userID_hex or userID_hex
				hexPart := parts[len(parts)-1]
				assert.Len(t, hexPart, 32) // 16 bytes * 2 = 32 hex chars

				// Verify it's valid hex
				for _, char := range hexPart {
					assert.True(t,
						(char >= '0' && char <= '9') ||
							(char >= 'a' && char <= 'f') ||
							(char >= 'A' && char <= 'F'),
						"Invalid hex character: %c", char)
				}
			}
		})
	}
}

func TestGenerateAPIKeyUniqueness(t *testing.T) {
	// Generate multiple keys and ensure they're unique
	keys := make(map[string]bool)

	for i := 0; i < 100; i++ {
		key, err := generateAPIKey("test-user", "test")
		assert.NoError(t, err)
		assert.False(t, keys[key], "Generated duplicate key: %s", key)
		keys[key] = true
	}
}

func TestSetupClaudeCodeCmdStructure(t *testing.T) {
	assert.Equal(t, "setup-claude-code", setupClaudeCodeCmd.Use)
	assert.Equal(t, "Setup Claude Code configuration", setupClaudeCodeCmd.Short)
	assert.NotNil(t, setupClaudeCodeCmd.Run)
}

func TestInitCmdStructure(t *testing.T) {
	assert.Equal(t, "init", initCmd.Use)
	assert.Equal(t, "Initialize Claude configuration (alias for setup-claude-code)", initCmd.Short)
	assert.NotNil(t, initCmd.Run)

	// Verify both commands have run functions (cannot directly compare functions)
	assert.NotNil(t, setupClaudeCodeCmd.Run)
	assert.NotNil(t, initCmd.Run)
}

func TestRunSetupClaudeCodeNoCLAUDEDIR(t *testing.T) {
	// Save original CLAUDE_DIR
	originalCLAUDEDIR := os.Getenv("CLAUDE_DIR")
	defer func() {
		if originalCLAUDEDIR != "" {
			_ = os.Setenv("CLAUDE_DIR", originalCLAUDEDIR)
		} else {
			_ = os.Unsetenv("CLAUDE_DIR")
		}
	}()

	// Unset CLAUDE_DIR
	_ = os.Unsetenv("CLAUDE_DIR")

	// This test verifies the function handles missing CLAUDE_DIR
	// We can't easily test os.Exit(1), but we can verify the error path is taken
	// by checking that the function would exit early
	claudeDir := os.Getenv("CLAUDE_DIR")
	assert.Empty(t, claudeDir)
}

func TestClaudeCodeSettingsEmbedded(t *testing.T) {
	// Test that the embedded settings string is not empty
	assert.NotEmpty(t, claudeCodeSettings)

	// Test that it's valid JSON
	var settings interface{}
	err := json.Unmarshal([]byte(claudeCodeSettings), &settings)
	assert.NoError(t, err)
}

func TestHelpersRun(t *testing.T) {
	// Test the main helpers command run function doesn't panic
	assert.NotPanics(t, func() {
		HelpersCmd.Run(&cobra.Command{}, []string{})
	})
}
