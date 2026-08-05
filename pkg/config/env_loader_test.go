package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRoleEnvVars(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "env_loader_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create test environment files
	defaultEnvContent := `# Default environment variables
DB_HOST=localhost
DB_PORT=5432
LOG_LEVEL=info
`
	adminEnvContent := `# Admin-specific environment variables
LOG_LEVEL=debug
ADMIN_ACCESS=true
SECRET_KEY="admin-secret-123"
`
	userEnvContent := `# User-specific environment variables
USER_ACCESS=true
FEATURE_FLAG_A=enabled
`

	// Write test files
	if err := os.WriteFile(filepath.Join(tempDir, "default.env"), []byte(defaultEnvContent), 0644); err != nil {
		t.Fatalf("Failed to write default.env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "admin.env"), []byte(adminEnvContent), 0644); err != nil {
		t.Fatalf("Failed to write admin.env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "user.env"), []byte(userEnvContent), 0644); err != nil {
		t.Fatalf("Failed to write user.env: %v", err)
	}

	tests := []struct {
		name         string
		config       *RoleEnvFilesConfig
		role         string
		expectedVars map[string]string
		expectError  bool
	}{
		{
			name: "disabled config returns empty",
			config: &RoleEnvFilesConfig{
				Enabled:     false,
				Path:        tempDir,
				LoadDefault: true,
			},
			role:         "admin",
			expectedVars: map[string]string{},
		},
		{
			name: "load only role-specific env",
			config: &RoleEnvFilesConfig{
				Enabled:     true,
				Path:        tempDir,
				LoadDefault: false,
			},
			role: "admin",
			expectedVars: map[string]string{
				"LOG_LEVEL":    "debug",
				"ADMIN_ACCESS": "true",
				"SECRET_KEY":   "admin-secret-123",
			},
		},
		{
			name: "load default and role-specific env",
			config: &RoleEnvFilesConfig{
				Enabled:     true,
				Path:        tempDir,
				LoadDefault: true,
			},
			role: "admin",
			expectedVars: map[string]string{
				"DB_HOST":      "localhost",
				"DB_PORT":      "5432",
				"LOG_LEVEL":    "debug", // Overridden by role-specific
				"ADMIN_ACCESS": "true",
				"SECRET_KEY":   "admin-secret-123",
			},
		},
		{
			name: "load default only for unknown role",
			config: &RoleEnvFilesConfig{
				Enabled:     true,
				Path:        tempDir,
				LoadDefault: true,
			},
			role: "unknown",
			expectedVars: map[string]string{
				"DB_HOST":   "localhost",
				"DB_PORT":   "5432",
				"LOG_LEVEL": "info",
			},
		},
		{
			name: "empty role loads only default",
			config: &RoleEnvFilesConfig{
				Enabled:     true,
				Path:        tempDir,
				LoadDefault: true,
			},
			role: "",
			expectedVars: map[string]string{
				"DB_HOST":   "localhost",
				"DB_PORT":   "5432",
				"LOG_LEVEL": "info",
			},
		},
		{
			name: "user role env vars",
			config: &RoleEnvFilesConfig{
				Enabled:     true,
				Path:        tempDir,
				LoadDefault: true,
			},
			role: "user",
			expectedVars: map[string]string{
				"DB_HOST":        "localhost",
				"DB_PORT":        "5432",
				"LOG_LEVEL":      "info",
				"USER_ACCESS":    "true",
				"FEATURE_FLAG_A": "enabled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envVars, err := LoadRoleEnvVars(tt.config, tt.role)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Convert envVars slice to map for easier comparison
			actualVars := make(map[string]string)
			for _, env := range envVars {
				actualVars[env.Key] = env.Value
			}

			// Check expected variables
			for key, expectedValue := range tt.expectedVars {
				if actualValue, exists := actualVars[key]; !exists {
					t.Errorf("Expected key %s not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Key %s: expected value %s, got %s", key, expectedValue, actualValue)
				}
			}

			// Check for unexpected variables
			for key := range actualVars {
				if _, expected := tt.expectedVars[key]; !expected {
					t.Errorf("Unexpected key %s found", key)
				}
			}
		})
	}
}

func TestLoadEnvFile(t *testing.T) {
	// Create temporary directory for test files
	tempDir, err := os.MkdirTemp("", "env_file_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	tests := []struct {
		name         string
		content      string
		expectedVars map[string]string
		expectError  bool
	}{
		{
			name: "valid env file",
			content: `# Comment line
KEY1=value1
KEY2=value2

# Another comment
KEY3="quoted value"
KEY4='single quoted'
KEY5=value with spaces
`,
			expectedVars: map[string]string{
				"KEY1": "value1",
				"KEY2": "value2",
				"KEY3": "quoted value",
				"KEY4": "single quoted",
				"KEY5": "value with spaces",
			},
		},
		{
			name: "empty file",
			content: `# Only comments

# Nothing else
`,
			expectedVars: map[string]string{},
		},
		{
			name: "invalid format lines are skipped",
			content: `VALID_KEY=valid_value
INVALID LINE WITHOUT EQUALS
ANOTHER_VALID=test
=NO_KEY
KEY_WITHOUT_VALUE=
FINAL_KEY=final_value
`,
			expectedVars: map[string]string{
				"VALID_KEY":         "valid_value",
				"ANOTHER_VALID":     "test",
				"KEY_WITHOUT_VALUE": "",
				"FINAL_KEY":         "final_value",
			},
		},
		{
			name: "special characters in values",
			content: `DATABASE_URL=postgres://user:pass@localhost:5432/db
API_KEY=abc123!@#$%^&*()
PATH=/usr/local/bin:/usr/bin:/bin
EMPTY=
`,
			expectedVars: map[string]string{
				"DATABASE_URL": "postgres://user:pass@localhost:5432/db",
				"API_KEY":      "abc123!@#$%^&*()",
				"PATH":         "/usr/local/bin:/usr/bin:/bin",
				"EMPTY":        "",
			},
		},
		{
			name: "equals sign in value",
			content: `EQUATION=a=b+c
CONFIG=key1=value1,key2=value2
`,
			expectedVars: map[string]string{
				"EQUATION": "a=b+c",
				"CONFIG":   "key1=value1,key2=value2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test file
			testFile := filepath.Join(tempDir, "test.env")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			envVars, err := loadEnvFile(testFile)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Convert envVars slice to map for easier comparison
			actualVars := make(map[string]string)
			for _, env := range envVars {
				actualVars[env.Key] = env.Value
			}

			// Check expected variables
			for key, expectedValue := range tt.expectedVars {
				if actualValue, exists := actualVars[key]; !exists {
					t.Errorf("Expected key %s not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Key %s: expected value %q, got %q", key, expectedValue, actualValue)
				}
			}

			// Check for unexpected variables
			for key := range actualVars {
				if _, expected := tt.expectedVars[key]; !expected {
					t.Errorf("Unexpected key %s found", key)
				}
			}
		})
	}
}

func TestApplyEnvVars(t *testing.T) {
	// Save original environment to restore later
	originalEnv := os.Environ()
	defer func() {
		// Restore original environment
		os.Clearenv()
		for _, env := range originalEnv {
			if idx := strings.Index(env, "="); idx > 0 {
				if err := os.Setenv(env[:idx], env[idx+1:]); err != nil {
					t.Logf("Failed to restore env var %s: %v", env[:idx], err)
				}
			}
		}
	}()

	// Set up test environment
	if err := os.Setenv("EXISTING_VAR", "original_value"); err != nil {
		t.Fatalf("Failed to set EXISTING_VAR: %v", err)
	}

	envVars := []EnvVar{
		{Key: "NEW_VAR", Value: "new_value"},
		{Key: "EXISTING_VAR", Value: "updated_value"},
		{Key: "EMPTY_VAR", Value: ""},
	}

	applied := ApplyEnvVars(envVars)

	// Check that all variables were applied
	if len(applied) != len(envVars) {
		t.Errorf("Expected %d applied vars, got %d", len(envVars), len(applied))
	}

	// Verify environment variables were set correctly
	if val := os.Getenv("NEW_VAR"); val != "new_value" {
		t.Errorf("NEW_VAR: expected 'new_value', got '%s'", val)
	}
	if val := os.Getenv("EXISTING_VAR"); val != "updated_value" {
		t.Errorf("EXISTING_VAR: expected 'updated_value', got '%s'", val)
	}
	if val := os.Getenv("EMPTY_VAR"); val != "" {
		t.Errorf("EMPTY_VAR: expected empty string, got '%s'", val)
	}
}
