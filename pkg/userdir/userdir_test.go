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

	// Check that CLAUDE_DIR was added with user-specific directory
	expectedClaudeDir := filepath.Join("/home/alice", ".claude", "bob")
	claudeDirFound := false
	homeFound := false

	for _, envVar := range env {
		if strings.HasPrefix(envVar, "CLAUDE_DIR=") {
			if envVar != "CLAUDE_DIR="+expectedClaudeDir {
				t.Errorf("Expected CLAUDE_DIR=%s, got %s", expectedClaudeDir, envVar)
			}
			claudeDirFound = true
		} else if strings.HasPrefix(envVar, "HOME=") {
			// HOME should remain unchanged
			if envVar != "HOME=/home/alice" {
				t.Errorf("Expected HOME=/home/alice, got %s", envVar)
			}
			homeFound = true
		}
	}

	if !claudeDirFound {
		t.Error("CLAUDE_DIR environment variable not found")
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

	// Check that CLAUDE_DIR was added with default HOME
	claudeDirFound := false

	for _, envVar := range env {
		if strings.HasPrefix(envVar, "CLAUDE_DIR=") {
			// When HOME is not set, it should use os.Getenv("HOME") or fallback to /home/agentapi
			// The exact value depends on the test environment
			if !strings.Contains(envVar, ".claude/bob") {
				t.Errorf("Expected CLAUDE_DIR to contain '.claude/bob', got %s", envVar)
			}
			claudeDirFound = true
		}
	}

	if !claudeDirFound {
		t.Error("CLAUDE_DIR environment variable not found")
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
