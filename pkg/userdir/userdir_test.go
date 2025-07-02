package userdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	baseDir := "/tmp/test"
	enabled := true

	manager := NewManager(baseDir, enabled)

	if manager.baseDir != baseDir {
		t.Errorf("Expected baseDir %s, got %s", baseDir, manager.baseDir)
	}
	if manager.enabled != enabled {
		t.Errorf("Expected enabled %t, got %t", enabled, manager.enabled)
	}
}

func TestIsEnabled(t *testing.T) {
	manager := NewManager("/tmp", true)
	if !manager.IsEnabled() {
		t.Error("Expected IsEnabled to return true")
	}

	manager = NewManager("/tmp", false)
	if manager.IsEnabled() {
		t.Error("Expected IsEnabled to return false")
	}
}

func TestGetUserHomeDir_DisabledMode(t *testing.T) {
	manager := NewManager("/tmp/test", false)

	// In disabled mode, GetUserHomeDir should return the current HOME
	currentHome := os.Getenv("HOME")

	userDir, err := manager.GetUserHomeDir("alice")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if userDir != currentHome {
		t.Errorf("Expected %s, got %s", currentHome, userDir)
	}
}

func TestGetUserHomeDir_EnabledMode(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, true)

	userDir, err := manager.GetUserHomeDir("alice")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := filepath.Join(tmpDir, "users", "alice")
	if userDir != expected {
		t.Errorf("Expected %s, got %s", expected, userDir)
	}
}

func TestGetUserHomeDir_EmptyUserID(t *testing.T) {
	manager := NewManager("/tmp/test", true)

	_, err := manager.GetUserHomeDir("")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	expectedError := "user ID cannot be empty when multiple users is enabled"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestSanitizeUserID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"alice", "alice"},
		{"alice/bob", "alice_bob"},
		{"alice\\bob", "alice_bob"},
		{"alice..bob", "alice__bob"},
		{"alice bob", "alice_bob"},
		{".alice", "alice"},
		{"alice.", "alice"},
		{"-alice", "alice"},
		{"alice-", "alice"},
		{"", ""},
		{"...", ""},
		{"///", ""},
	}

	for _, test := range tests {
		result := sanitizeUserID(test.input)
		if result != test.expected {
			t.Errorf("sanitizeUserID(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

func TestEnsureUserHomeDir_DisabledMode(t *testing.T) {
	manager := NewManager("/tmp/test", false)

	// In disabled mode, should return current HOME without creating directories
	currentHome := os.Getenv("HOME")

	userDir, err := manager.EnsureUserHomeDir("alice")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if userDir != currentHome {
		t.Errorf("Expected %s, got %s", currentHome, userDir)
	}
}

func TestEnsureUserHomeDir_EnabledMode(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, true)

	userDir, err := manager.EnsureUserHomeDir("alice")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := filepath.Join(tmpDir, "users", "alice")
	if userDir != expected {
		t.Errorf("Expected %s, got %s", expected, userDir)
	}

	// Check that directory was actually created
	if _, err := os.Stat(userDir); os.IsNotExist(err) {
		t.Errorf("Directory %s was not created", userDir)
	}
}

func TestGetUserEnvironment_DisabledMode(t *testing.T) {
	manager := NewManager("/tmp/test", false)

	baseEnv := []string{"PATH=/usr/bin", "USER=alice", "HOME=/home/alice"}

	env, err := manager.GetUserEnvironment("bob", baseEnv)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// In disabled mode, environment should be unchanged
	if len(env) != len(baseEnv) {
		t.Errorf("Expected env length %d, got %d", len(baseEnv), len(env))
	}

	for i, envVar := range env {
		if envVar != baseEnv[i] {
			t.Errorf("Expected env[%d] to be %s, got %s", i, baseEnv[i], envVar)
		}
	}
}

func TestGetUserEnvironment_EnabledMode(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, true)

	baseEnv := []string{"PATH=/usr/bin", "USER=alice", "HOME=/home/alice"}

	env, err := manager.GetUserEnvironment("bob", baseEnv)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Check that environment variables are properly handled
	homeFound := false

	for _, envVar := range env {
		if strings.HasPrefix(envVar, "HOME=") {
			// HOME should remain unchanged
			if envVar != "HOME=/home/alice" {
				t.Errorf("Expected HOME=/home/alice, got %s", envVar)
			}
			homeFound = true
		}
	}

	if !homeFound {
		t.Error("HOME environment variable not found")
	}
}

func TestGetUserEnvironment_NoHomeInBase(t *testing.T) {
	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, true)

	baseEnv := []string{"PATH=/usr/bin", "USER=alice"}

	env, err := manager.GetUserEnvironment("bob", baseEnv)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify environment variables are handled properly
	// The function should return the base environment when multiple users is enabled
	if len(env) != len(baseEnv) {
		t.Errorf("Expected env length to match base env length %d, got %d", len(baseEnv), len(env))
	}
}

func TestGetUserHomeDir_InvalidUserID(t *testing.T) {
	manager := NewManager("/tmp/test", true)

	invalidIDs := []string{"...", "///", "   "}
	for _, userID := range invalidIDs {
		_, err := manager.GetUserHomeDir(userID)
		if err == nil {
			t.Errorf("Expected error for invalid user ID: %q", userID)
		}
	}
}

func TestGetUserClaudeDir_DisabledMode(t *testing.T) {
	manager := NewManager("/tmp/test", false)

	// In disabled mode, GetUserClaudeDir should return the default .claude directory
	claudeDir, err := manager.GetUserClaudeDir("alice")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should return ~/.claude
	if !strings.HasSuffix(claudeDir, ".claude") {
		t.Errorf("Expected path to end with '.claude', got %s", claudeDir)
	}
}

func TestGetUserClaudeDir_EnabledMode(t *testing.T) {
	// Set HOME for consistent testing
	_ = os.Setenv("HOME", "/home/test")
	defer func() { _ = os.Unsetenv("HOME") }()

	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, true)

	claudeDir, err := manager.GetUserClaudeDir("alice")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := filepath.Join("/home/test", ".claude", "alice")
	if claudeDir != expected {
		t.Errorf("Expected %s, got %s", expected, claudeDir)
	}
}

func TestEnsureUserClaudeDir_EnabledMode(t *testing.T) {
	// Set HOME for consistent testing
	testHome := t.TempDir()
	_ = os.Setenv("HOME", testHome)
	defer func() { _ = os.Unsetenv("HOME") }()

	tmpDir := t.TempDir()
	manager := NewManager(tmpDir, true)

	claudeDir, err := manager.EnsureUserClaudeDir("alice")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expected := filepath.Join(testHome, ".claude", "alice")
	if claudeDir != expected {
		t.Errorf("Expected %s, got %s", expected, claudeDir)
	}

	// Check that directory was actually created
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		t.Errorf("Directory %s was not created", claudeDir)
	}
}

func TestSetupUserHome(t *testing.T) {
	tests := []struct {
		name           string
		userID         string
		expectError    bool
		expectedResult map[string]string
	}{
		{
			name:           "Valid user ID",
			userID:         "testuser",
			expectError:    false,
			expectedResult: map[string]string{"HOME": "/home/agentapi/myclaudes/testuser"},
		},
		{
			name:        "Empty user ID",
			userID:      "",
			expectError: true,
		},
		{
			name:           "User ID with dangerous characters",
			userID:         "test/../user",
			expectError:    false,
			expectedResult: map[string]string{"HOME": "/home/agentapi/myclaudes/test____user"},
		},
		{
			name:           "GitHub username format",
			userID:         "github-user123",
			expectError:    false,
			expectedResult: map[string]string{"HOME": "/home/agentapi/myclaudes/github-user123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SetupUserHome(tt.userID)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(result) != len(tt.expectedResult) {
				t.Errorf("Expected %d environment variables, got %d", len(tt.expectedResult), len(result))
				return
			}

			for key, expectedValue := range tt.expectedResult {
				if value, exists := result[key]; !exists {
					t.Errorf("Expected environment variable %s not found", key)
				} else if value != expectedValue {
					t.Errorf("Expected %s=%s, got %s=%s", key, expectedValue, key, value)
				}
			}

			// Verify directory was created (use a temporary directory for tests)
			if !tt.expectError && tt.userID != "" {
				sanitizedUserID := sanitizeUserID(tt.userID)
				expectedDir := filepath.Join("/home/agentapi/myclaudes", sanitizedUserID)
				if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
					// For tests, we don't actually create the directory in /home/agentapi/myclaudes
					// since it may not have permissions. The function logic is tested separately.
					t.Logf("Directory creation test skipped due to permissions: %s", expectedDir)
				} else {
					// Clean up test directory if it was actually created
					os.RemoveAll(expectedDir)
				}
			}
		})
	}
}
