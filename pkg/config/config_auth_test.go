package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAuthConfigFromFile(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "auth-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name        string
		fileContent string
		fileName    string
		expectError bool
		validate    func(*Config) bool
	}{
		{
			name: "valid YAML auth config",
			fileContent: `github:
  user_mapping:
    default_role: "user"
    default_permissions:
      - "read"
      - "session:create"
    team_role_mapping:
      "myorg/admins":
        role: "admin"
        permissions:
          - "*"
      "myorg/developers":
        role: "developer"
        permissions:
          - "read"
          - "write"`,
			fileName:    "auth-config.yaml",
			expectError: false,
			validate: func(c *Config) bool {
				if c.Auth.GitHub == nil {
					t.Logf("GitHub config is nil")
					return false
				}
				if c.Auth.GitHub.UserMapping.DefaultRole != "user" {
					t.Logf("Expected default role 'user', got '%s'", c.Auth.GitHub.UserMapping.DefaultRole)
					return false
				}
				if len(c.Auth.GitHub.UserMapping.DefaultPermissions) != 2 {
					t.Logf("Expected 2 default permissions, got %d: %v", len(c.Auth.GitHub.UserMapping.DefaultPermissions), c.Auth.GitHub.UserMapping.DefaultPermissions)
					return false
				}
				if len(c.Auth.GitHub.UserMapping.TeamRoleMapping) != 2 {
					t.Logf("Expected 2 team role mappings, got %d", len(c.Auth.GitHub.UserMapping.TeamRoleMapping))
					return false
				}
				adminRule, exists := c.Auth.GitHub.UserMapping.TeamRoleMapping["myorg/admins"]
				if !exists {
					t.Logf("myorg/admins rule not found")
					return false
				}
				if adminRule.Role != "admin" {
					t.Logf("Expected admin role, got '%s'", adminRule.Role)
					return false
				}
				return true
			},
		},
		{
			name: "valid JSON auth config",
			fileContent: `{
  "github": {
    "user_mapping": {
      "default_role": "viewer",
      "default_permissions": ["read"],
      "team_role_mapping": {
        "myorg/testers": {
          "role": "tester",
          "permissions": ["read", "session:create"]
        }
      }
    }
  }
}`,
			fileName:    "auth-config.json",
			expectError: false,
			validate: func(c *Config) bool {
				return c.Auth.GitHub != nil &&
					c.Auth.GitHub.UserMapping.DefaultRole == "viewer" &&
					len(c.Auth.GitHub.UserMapping.DefaultPermissions) == 1 &&
					c.Auth.GitHub.UserMapping.DefaultPermissions[0] == "read" &&
					c.Auth.GitHub.UserMapping.TeamRoleMapping["myorg/testers"].Role == "tester"
			},
		},
		{
			name:        "invalid YAML",
			fileContent: "invalid: yaml: content: [",
			fileName:    "invalid.yaml",
			expectError: true,
			validate:    nil,
		},
		{
			name:        "invalid JSON",
			fileContent: `{"invalid": json}`,
			fileName:    "invalid.json",
			expectError: true,
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			filePath := filepath.Join(tempDir, tt.fileName)
			err := os.WriteFile(filePath, []byte(tt.fileContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Create a basic config
			config := &Config{
				Auth: AuthConfig{
					Enabled: true,
				},
			}

			// Load auth config from file
			err = loadAuthConfigFromFile(config, filePath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error, but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Validate the result
			if tt.validate != nil && !tt.validate(config) {
				t.Errorf("Validation failed for test case: %s", tt.name)
			}
		})
	}
}

func TestLoadConfigWithAuthConfigFile(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create auth config file
	authConfigContent := `github:
  user_mapping:
    default_role: "user"
    default_permissions:
      - "read"
    team_role_mapping:
      "myorg/admins":
        role: "admin"
        permissions:
          - "*"`

	authConfigPath := filepath.Join(tempDir, "auth-config.yaml")
	err = os.WriteFile(authConfigPath, []byte(authConfigContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write auth config file: %v", err)
	}

	// Create main config with path to auth config
	mainConfigWithPath := `{
  "start_port": 8080,
  "auth_config_file": "` + authConfigPath + `",
  "auth": {
    "enabled": true,
    "github": {
      "enabled": true,
      "base_url": "https://api.github.com"
    }
  }
}`

	mainConfigPath := filepath.Join(tempDir, "config.json")
	err = os.WriteFile(mainConfigPath, []byte(mainConfigWithPath), 0644)
	if err != nil {
		t.Fatalf("Failed to write main config file: %v", err)
	}

	// Load config
	config, err := LoadConfig(mainConfigPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Validate
	if config.StartPort != 8080 {
		t.Errorf("Expected start_port 8080, got %d", config.StartPort)
	}

	if config.AuthConfigFile != authConfigPath {
		t.Errorf("Expected auth_config_file %s, got %s", authConfigPath, config.AuthConfigFile)
	}

	if !config.Auth.Enabled {
		t.Errorf("Expected auth to be enabled")
	}

	if config.Auth.GitHub == nil {
		t.Fatalf("Expected GitHub config to be initialized")
	}

	if !config.Auth.GitHub.Enabled {
		t.Errorf("Expected GitHub auth to be enabled")
	}

	if config.Auth.GitHub.UserMapping.DefaultRole != "user" {
		t.Errorf("Expected default role 'user', got '%s'", config.Auth.GitHub.UserMapping.DefaultRole)
	}

	if len(config.Auth.GitHub.UserMapping.TeamRoleMapping) != 1 {
		t.Errorf("Expected 1 team role mapping, got %d", len(config.Auth.GitHub.UserMapping.TeamRoleMapping))
	}

	adminRule, exists := config.Auth.GitHub.UserMapping.TeamRoleMapping["myorg/admins"]
	if !exists {
		t.Errorf("Expected 'myorg/admins' team role mapping to exist")
	} else {
		if adminRule.Role != "admin" {
			t.Errorf("Expected admin role, got '%s'", adminRule.Role)
		}
		if len(adminRule.Permissions) != 1 || adminRule.Permissions[0] != "*" {
			t.Errorf("Expected admin permissions ['*'], got %v", adminRule.Permissions)
		}
	}
}
