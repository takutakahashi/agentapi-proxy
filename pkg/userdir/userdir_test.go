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
	// Save original environment
	originalHome := os.Getenv("HOME")
	originalUserHomeBaseDir := os.Getenv("USERHOME_BASEDIR")

	// Ensure cleanup
	defer func() {
		if originalHome != "" {
			os.Setenv("HOME", originalHome)
		} else {
			os.Unsetenv("HOME")
		}
		if originalUserHomeBaseDir != "" {
			os.Setenv("USERHOME_BASEDIR", originalUserHomeBaseDir)
		} else {
			os.Unsetenv("USERHOME_BASEDIR")
		}
	}()

	// Create temporary directories for testing
	tempHomeDir := t.TempDir()
	tempCustomBaseDir := t.TempDir()

	tests := []struct {
		name                string
		userID              string
		homeEnv             string
		userHomeBaseDirEnv  string
		expectError         bool
		expectedHomePattern string // Use pattern for path validation
	}{
		{
			name:                "Empty user ID returns current HOME",
			userID:              "",
			homeEnv:             tempHomeDir,
			expectError:         false,
			expectedHomePattern: tempHomeDir,
		},
		{
			name:                "Valid user ID with default base dir",
			userID:              "testuser",
			homeEnv:             tempHomeDir,
			expectError:         false,
			expectedHomePattern: filepath.Join(tempHomeDir, ".agentapi-proxy", "myclaudes", "testuser"),
		},
		{
			name:                "Valid user ID with custom USERHOME_BASEDIR",
			userID:              "testuser",
			homeEnv:             tempHomeDir,
			userHomeBaseDirEnv:  tempCustomBaseDir,
			expectError:         false,
			expectedHomePattern: filepath.Join(tempCustomBaseDir, "myclaudes", "testuser"),
		},
		{
			name:                "User ID with dangerous characters",
			userID:              "test/../user",
			homeEnv:             tempHomeDir,
			expectError:         false,
			expectedHomePattern: filepath.Join(tempHomeDir, ".agentapi-proxy", "myclaudes", "test____user"),
		},
		{
			name:                "GitHub username format",
			userID:              "github-user123",
			homeEnv:             tempHomeDir,
			expectError:         false,
			expectedHomePattern: filepath.Join(tempHomeDir, ".agentapi-proxy", "myclaudes", "github-user123"),
		},
		{
			name:        "Invalid user ID (only dangerous characters)",
			userID:      "../..",
			homeEnv:     tempHomeDir,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			os.Setenv("HOME", tt.homeEnv)
			if tt.userHomeBaseDirEnv != "" {
				os.Setenv("USERHOME_BASEDIR", tt.userHomeBaseDirEnv)
			} else {
				os.Unsetenv("USERHOME_BASEDIR")
			}

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

			// For empty userID, expect empty result (uses current HOME)
			if tt.userID == "" {
				if len(result) != 0 {
					t.Errorf("Expected 0 environment variables for empty userID, got %d", len(result))
					return
				}
			} else {
				if len(result) != 1 {
					t.Errorf("Expected 1 environment variable, got %d", len(result))
					return
				}

				if homeValue, exists := result["HOME"]; !exists {
					t.Errorf("Expected HOME environment variable not found")
				} else if homeValue != tt.expectedHomePattern {
					t.Errorf("Expected HOME=%s, got HOME=%s", tt.expectedHomePattern, homeValue)
				}
			}

			// For non-empty userID, verify directory was actually created
			if tt.userID != "" && !tt.expectError {
				if _, err := os.Stat(result["HOME"]); os.IsNotExist(err) {
					t.Errorf("Expected directory %s was not created", result["HOME"])
				} else {
					t.Logf("Directory successfully created: %s", result["HOME"])
				}
			}
		})
	}
}
